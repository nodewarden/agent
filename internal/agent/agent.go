// Package agent provides the main Nodewarden agent implementation
// that coordinates metric collection, processing, and transmission.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"nodewarden/internal/cache"
	"nodewarden/internal/collectors/container"
	"nodewarden/internal/collectors/cpu"
	"nodewarden/internal/collectors/disk"
	"nodewarden/internal/collectors/memory"
	"nodewarden/internal/collectors/mysql"
	"nodewarden/internal/collectors/postgresql"
	"nodewarden/internal/collectors/process"
	"nodewarden/internal/collectors/system"
	"nodewarden/internal/config"
	"nodewarden/internal/metrics"
	"nodewarden/internal/registry"
)

// Agent represents the main Nodewarden agent.
type Agent struct {
	config   *config.Config
	logger   *slog.Logger
	registry metrics.Registry
	hostname string

	// Control channels
	stopChan chan struct{}
	stopOnce sync.Once

	// Metrics transmission
	transmitter MetricTransmitter

	// Phase 4 optimizations
	deltaTracker     *metrics.DeltaTracker
	batchSize        int

	// Cache cleanup
	cacheCleanupStop chan struct{}

	// Connection tracking
	lastTransmissionFailed bool
	consecutiveFailures   int
	mu                    sync.RWMutex
}

// MetricTransmitter defines the interface for transmitting metrics to the server.
type MetricTransmitter interface {
	Send(ctx context.Context, metrics []metrics.Metric) error
	Close() error
}

// New creates a new Nodewarden agent instance.
func New(cfg *config.Config, logger *slog.Logger) (*Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Get hostname
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
		logger.Warn("failed to get hostname", "error", err)
	}

	// Create registry
	reg := registry.NewRegistry(logger.With("component", "registry"))

	// Create transmitter
	transmitter, err := NewHTTPTransmitter(cfg, logger.With("component", "transmitter"))
	if err != nil {
		return nil, fmt.Errorf("failed to create transmitter: %w", err)
	}

	// Initialize Phase 4 optimizations
	deltaTracker := metrics.NewDeltaTracker(metrics.DeltaConfig{
		Threshold: 1.0,                  // 1% change threshold
		MaxAge:    5 * time.Minute,      // Force send after 5 minutes
		Enabled:   true,
		Logger:    logger.With("component", "delta"),
	})

	// Use simple fixed batch size instead of adaptive batching
	batchSize := 500

	// Start global cache cleanup to prevent memory leaks
	cacheCleanupStop := cache.StartGlobalCleanup(5 * time.Minute)

	agent := &Agent{
		config:           cfg,
		logger:           logger,
		registry:         reg,
		hostname:         hostname,
		stopChan:         make(chan struct{}),
		transmitter:      transmitter,
		deltaTracker:     deltaTracker,
		batchSize:        batchSize,
		cacheCleanupStop: cacheCleanupStop,
	}

	// Register all collectors
	if err := agent.registerCollectors(); err != nil {
		return nil, fmt.Errorf("failed to register collectors: %w", err)
	}

	logger.Info("Nodewarden agent created successfully",
		"hostname", hostname,
		"collectors", reg.ListCollectors())

	return agent, nil
}

// registerCollectors registers all configured collectors with the registry.
func (a *Agent) registerCollectors() error {
	collectorLogger := a.logger.With("component", "collector")

	// CPU collector (always enabled with defaults)
	if a.config.Collectors.CPU {
		cpuCollector := cpu.NewCollector(
			nil, // Use defaults
			a.hostname,
			cpu.WithLogger(collectorLogger.With("type", "cpu")),
		)
		if err := a.registry.Register(cpuCollector); err != nil {
			return fmt.Errorf("failed to register CPU collector: %w", err)
		}
	}

	// Memory collector (always enabled with defaults)
	if a.config.Collectors.Memory {
		memoryCollector := memory.NewCollector(
			nil, // Use defaults
			a.hostname,
			memory.WithLogger(collectorLogger.With("type", "memory")),
		)
		if err := a.registry.Register(memoryCollector); err != nil {
			return fmt.Errorf("failed to register memory collector: %w", err)
		}
	}

	// System collector (enabled by default for OS info and uptime)
	if a.config.Collectors.System {
		systemCollector := system.NewCollector(
			config.SystemConfig{
				Enabled:       true,
				CollectUptime: true,
			},
			a.hostname,
			system.WithLogger(collectorLogger.With("type", "system")),
		)
		if err := a.registry.Register(systemCollector); err != nil {
			return fmt.Errorf("failed to register system collector: %w", err)
		}
	}

	// Disk collector (enabled for disk usage monitoring)
	if a.config.Collectors.Disk {
		diskCollector := disk.NewCollector(
			a.hostname,
			disk.WithLogger(collectorLogger.With("type", "disk")),
		)
		if err := a.registry.Register(diskCollector); err != nil {
			return fmt.Errorf("failed to register disk collector: %w", err)
		}
		a.logger.Info("registered disk collector")
	}

	// Container collector (enabled when configured)
	if a.config.Collectors.Container {
		containerCollector := container.NewCollector(
			a.config.Container,
			a.hostname,
			container.WithLogger(collectorLogger.With("type", "container")),
		)
		if err := a.registry.Register(containerCollector); err != nil {
			return fmt.Errorf("failed to register container collector: %w", err)
		}
		a.logger.Info("registered container collector")
	}

	// VM collector (Linux only)
	if err := a.registerVMCollector(collectorLogger); err != nil {
		return err
	}

	// MySQL collector (auto-detects running MySQL/MariaDB)
	mysqlCollector := mysql.NewCollector(
		a.config.Database,
		a.hostname,
		mysql.WithLogger(collectorLogger.With("type", "mysql")),
	)
	// Only register if explicitly enabled OR if auto-detected and connected
	if a.config.Database.EnableMySQL || mysqlCollector.Enabled() {
		if err := a.registry.Register(mysqlCollector); err != nil {
			// Don't fail if MySQL collector can't be registered (process might not be running)
			a.logger.Debug("MySQL collector not registered", "error", err)
		} else {
			a.logger.Info("registered MySQL collector")
		}
	}

	// PostgreSQL collector (auto-detects running PostgreSQL)
	postgresqlCollector := postgresql.NewCollector(
		a.config.Database,
		a.hostname,
		postgresql.WithLogger(collectorLogger.With("type", "postgresql")),
	)
	// Only register if explicitly enabled OR if auto-detected and connected
	if a.config.Database.EnablePostgreSQL || postgresqlCollector.Enabled() {
		if err := a.registry.Register(postgresqlCollector); err != nil {
			// Don't fail if PostgreSQL collector can't be registered (process might not be running)
			a.logger.Debug("PostgreSQL collector not registered", "error", err)
		} else {
			a.logger.Info("registered PostgreSQL collector")
		}
	}

	// Process collector (monitors configured application processes)
	if a.config.Process.EnableProcessMonitoring {
		processCollector := process.NewCollector(
			a.config.Process,
			a.hostname,
			a.config.APIKey,
			a.config.ServerURL,
			process.WithLogger(collectorLogger.With("type", "process")),
		)
		if err := a.registry.Register(processCollector); err != nil {
			a.logger.Warn("failed to register process collector", "error", err)
		} else {
			a.logger.Info("registered process collector")
		}
	}

	return nil
}

// Run starts the agent's main collection and transmission loop.
func (a *Agent) Run(ctx context.Context) error {
	a.logger.Info("starting Nodewarden agent",
		"collection_interval", a.config.CollectionInterval,
		"buffer_size", a.config.Buffer.MaxSize)

	// Perform initial health check
	if err := a.performHealthCheck(ctx); err != nil {
		a.logger.Warn("initial health check failed", "error", err)
	}

	// Create ticker for collection intervals
	ticker := time.NewTicker(a.config.CollectionInterval)
	defer ticker.Stop()
	
	// Create ticker for delta tracker cleanup (every hour)
	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	// Main collection loop
	for {
		select {
		case <-ctx.Done():
			a.logger.Info("context cancelled, stopping agent")
			return ctx.Err()
		case <-cleanupTicker.C:
			// Periodic cleanup of delta tracker to prevent memory leaks
			removed := a.deltaTracker.Cleanup(24 * time.Hour)
			if removed > 0 {
				a.logger.Debug("Delta tracker cleanup completed", "removed_metrics", removed)
			}

		case <-a.stopChan:
			a.logger.Info("stop signal received, stopping agent")
			return nil

		case <-ticker.C:
			if err := a.collectAndTransmit(ctx); err != nil {
				a.logger.Error("collection and transmission failed", "error", err)
				// Continue running despite errors
			}
		}
	}
}

// collectAndTransmit performs a single collection cycle and transmits metrics.
func (a *Agent) collectAndTransmit(ctx context.Context) error {
	// Collect metrics from all registered collectors
	collectedMetrics, err := a.registry.CollectAll(ctx)

	// Log partial collection failures but continue if we have any metrics
	if err != nil {
		a.logger.Warn("partial collection failure", "error", err)
		// Don't return error here - continue with metrics we did collect
	}

	// Only skip transmission if we have no metrics at all
	if len(collectedMetrics) == 0 {
		if err != nil {
			// No metrics AND errors - this is a complete failure
			return fmt.Errorf("metric collection completely failed: %w", err)
		}
		// No metrics but no error either (all collectors disabled?)
		return nil
	}

	// Transmit metrics in batches (even if some collectors failed)
	if err := a.transmitMetrics(ctx, collectedMetrics); err != nil {
		// Track failure
		a.mu.Lock()
		a.consecutiveFailures++
		a.lastTransmissionFailed = true
		a.mu.Unlock()

		return fmt.Errorf("metric transmission failed: %w", err)
	}

	// Check if we recovered from previous failures
	a.mu.Lock()
	wasFailedBefore := a.lastTransmissionFailed
	previousFailures := a.consecutiveFailures
	if wasFailedBefore {
		// Log recovery message at INFO level so it's visible
		a.logger.Info("CONNECTION RESTORED: Successfully reconnected to Nodewarden backend and resumed metric transmission",
			"previous_consecutive_failures", previousFailures,
			"downtime_estimate", fmt.Sprintf("~%d minutes", previousFailures * int(a.config.CollectionInterval.Minutes())))
		a.lastTransmissionFailed = false
		a.consecutiveFailures = 0
	}
	a.mu.Unlock()

	return nil
}

// transmitMetrics sends metrics to the server using Phase 4 optimizations.
func (a *Agent) transmitMetrics(ctx context.Context, allMetrics []metrics.Metric) error {
	a.logger.Debug("starting metric transmission", "total_metrics", len(allMetrics))

	// Phase 4.1: Apply delta compression to filter out unchanged metrics
	filteredMetrics := a.deltaTracker.FilterMetrics(allMetrics)

	a.logger.Debug("delta filtering completed", "total_metrics", len(allMetrics), "filtered_metrics", len(filteredMetrics))

	// Skip transmission if no metrics need to be sent
	if len(filteredMetrics) == 0 {
		a.logger.Debug("no metrics need to be sent after delta filtering")
		return nil
	}
	
	// Use simple fixed-size batching
	batches := a.createBatches(filteredMetrics, a.batchSize)

	// Transmit each batch
	for i, batch := range batches {
		start := time.Now()
		err := a.transmitter.Send(ctx, batch)
		transmitDuration := time.Since(start)

		if err != nil {
			a.logger.Error("Failed to send batch",
				"batch_index", i,
				"batch_size", len(batch),
				"latency_ms", transmitDuration.Milliseconds(),
				"error", err)
			return fmt.Errorf("failed to send batch %d: %w", i, err)
		}

		a.logger.Debug("Batch sent successfully",
			"batch_index", i,
			"batch_size", len(batch),
			"latency_ms", transmitDuration.Milliseconds())
	}
	
	return nil
}

// performHealthCheck checks the health of all collectors.
func (a *Agent) performHealthCheck(ctx context.Context) error {
	healthResults := a.registry.HealthCheck(ctx)

	var healthErrors []string
	healthyCount := 0

	for collectorName, err := range healthResults {
		if err != nil {
			healthErrors = append(healthErrors, fmt.Sprintf("%s: %v", collectorName, err))
			a.logger.Warn("collector health check failed", "collector", collectorName, "error", err)
		} else {
			healthyCount++
		}
	}

	totalCollectors := len(healthResults)
	a.logger.Info("health check completed",
		"healthy", healthyCount,
		"total", totalCollectors,
		"failed", len(healthErrors))

	if len(healthErrors) > 0 {
		return fmt.Errorf("health check failures: %v", healthErrors)
	}

	return nil
}

// Stop gracefully stops the agent.
func (a *Agent) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopChan)
	})
}

// Shutdown performs graceful shutdown with timeout.
func (a *Agent) Shutdown(ctx context.Context) error {
	a.logger.Info("shutting down Nodewarden agent")

	// Stop the main loop
	a.Stop()

	var shutdownErrors []error

	// Close transmitter
	if err := a.transmitter.Close(); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("transmitter shutdown error: %w", err))
	}

	// Close registry (which closes all collectors)
	if err := a.registry.Close(); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("registry shutdown error: %w", err))
	}

	// Stop cache cleanup
	if a.cacheCleanupStop != nil {
		close(a.cacheCleanupStop)
	}

	if len(shutdownErrors) > 0 {
		return fmt.Errorf("shutdown errors: %v", shutdownErrors)
	}

	a.logger.Info("Nodewarden agent shutdown completed")
	return nil
}

// createBatches creates fixed-size batches from metrics.
func (a *Agent) createBatches(allMetrics []metrics.Metric, batchSize int) [][]metrics.Metric {
	if len(allMetrics) == 0 {
		return nil
	}

	if batchSize <= 0 {
		batchSize = 500 // Default batch size
	}

	var batches [][]metrics.Metric
	for i := 0; i < len(allMetrics); i += batchSize {
		end := i + batchSize
		if end > len(allMetrics) {
			end = len(allMetrics)
		}
		batches = append(batches, allMetrics[i:end])
	}

	return batches
}

// GetStatus returns the current status of the agent.
func (a *Agent) GetStatus(ctx context.Context) map[string]interface{} {
	status := map[string]interface{}{
		"hostname":            a.hostname,
		"collection_interval": a.config.CollectionInterval.String(),
		"buffer_size":         a.config.Buffer.MaxSize,
		"collectors":          a.registry.ListCollectors(),
	}

	// Add health check results
	healthResults := a.registry.HealthCheck(ctx)
	healthStatus := make(map[string]string)
	for name, err := range healthResults {
		if err != nil {
			healthStatus[name] = fmt.Sprintf("error: %v", err)
		} else {
			healthStatus[name] = "healthy"
		}
	}
	status["health"] = healthStatus

	return status
}

