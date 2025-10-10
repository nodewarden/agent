// Package container provides runtime detection for container monitoring.
package container

import (
	"context"
	"log/slog"
	"time"
	
	"netwarden/internal/config"
)

// RuntimeDetector handles automatic detection of available container runtimes.
type RuntimeDetector struct {
	logger *slog.Logger
}

// NewRuntimeDetector creates a new runtime detector.
func NewRuntimeDetector(logger *slog.Logger) *RuntimeDetector {
	return &RuntimeDetector{
		logger: logger,
	}
}

// DetectRuntime finds and creates the best available container runtime client.
func (r *RuntimeDetector) DetectRuntime(cfg config.ContainerConfig) RuntimeClient {
	r.logger.Debug("detection config",
		"runtime", cfg.ContainerRuntime,
		"socket", cfg.ContainerSocket,
		"enabled", cfg.EnableContainers)

	if cfg.ContainerRuntime != "auto" {
		r.logger.Debug("using specific runtime (not auto)", "runtime", cfg.ContainerRuntime)
		result := r.createSpecificRuntime(cfg.ContainerRuntime, cfg.ContainerSocket)
		return result
	}

	r.logger.Debug("auto-detecting container runtime (runtime=auto)")

	// Try runtimes in order of preference: Docker → Podman
	runtimes := []struct {
		name   string
		create func() RuntimeClient
	}{
		{
			name: "docker/podman",
			create: func() RuntimeClient {
				r.logger.Debug("creating docker/podman client", "configured_socket", cfg.ContainerSocket)
				// The unified client will auto-detect Docker vs Podman
				return NewDockerCompatibleClient(cfg.ContainerSocket)
			},
		},
		// Future: Add containerd when implemented
	}

	for i, runtime := range runtimes {
		r.logger.Debug("trying runtime", "index", i, "runtime", runtime.name)

		r.logger.Debug("creating client for runtime", "runtime", runtime.name)
		client := runtime.create()
		r.logger.Debug("client created, checking availability", "runtime", runtime.name)

		available := client.IsAvailable()
		r.logger.Debug("availability check complete", "runtime", runtime.name, "available", available)

		if available {
			name := client.Name()
			r.logger.Info("detected container runtime", "runtime", name)
			return client
		}

		r.logger.Debug("runtime not available, cleaning up", "runtime", runtime.name)
		// Clean up failed client
		client.Close()
	}

	r.logger.Info("no container runtime detected")
	return &NoOpClient{}
}

// createSpecificRuntime creates a client for a specific runtime type.
func (r *RuntimeDetector) createSpecificRuntime(runtimeType, socketPath string) RuntimeClient {
	r.logger.Debug("creating specific runtime client", "runtime", runtimeType)
	
	switch runtimeType {
	case "docker", "podman":
		// Both are handled by the same compatible client
		client := NewDockerCompatibleClient(socketPath)
		if !client.IsAvailable() {
			r.logger.Warn("container runtime not available", "runtime", runtimeType)
		}
		return client
		
	// Future: Add containerd case
	
	default:
		r.logger.Warn("unsupported container runtime", "runtime", runtimeType)
		return &NoOpClient{}
	}
}

// RuntimeMonitor periodically checks if the runtime is still available.
type RuntimeMonitor struct {
	client        RuntimeClient
	logger        *slog.Logger
	detector      *RuntimeDetector
	config        config.ContainerConfig
	available     bool
	lastCheck     time.Time
	checkInterval time.Duration
}

// NewRuntimeMonitor creates a new runtime monitor.
func NewRuntimeMonitor(client RuntimeClient, detector *RuntimeDetector, config config.ContainerConfig, logger *slog.Logger) *RuntimeMonitor {
	return &RuntimeMonitor{
		client:        client,
		logger:        logger,
		detector:      detector,
		config:        config,
		available:     client.IsAvailable(),
		lastCheck:     time.Now(),
		checkInterval: 30 * time.Second, // Check every 30 seconds
	}
}

// IsAvailable checks if the runtime is currently available.
// Includes periodic health checks and automatic failover.
func (rm *RuntimeMonitor) IsAvailable(ctx context.Context) bool {
	now := time.Now()
	
	// Don't check too frequently to avoid overhead
	if now.Sub(rm.lastCheck) < rm.checkInterval {
		return rm.available
	}
	
	rm.lastCheck = now
	
	// Quick availability check with timeout
	available := rm.client.IsAvailable()
	
	if available != rm.available {
		if available {
			rm.logger.Info("container runtime became available", "runtime", rm.client.Name())
		} else {
			rm.logger.Warn("container runtime became unavailable", "runtime", rm.client.Name())
			
			// Try to detect a new runtime if current one failed and auto-detection is enabled
			if rm.config.ContainerRuntime == "auto" {
				rm.logger.Info("attempting to detect alternative runtime")
				newClient := rm.detector.DetectRuntime(rm.config)
				if newClient.Name() != "none" && newClient.IsAvailable() {
					rm.logger.Info("switched to alternative runtime", 
						"old_runtime", rm.client.Name(),
						"new_runtime", newClient.Name())
					rm.client.Close()
					rm.client = newClient
					available = true
				}
			}
		}
		rm.available = available
	}
	
	return rm.available
}

// GetClient returns the current runtime client.
func (rm *RuntimeMonitor) GetClient() RuntimeClient {
	return rm.client
}

// Close shuts down the monitor and its client.
func (rm *RuntimeMonitor) Close() error {
	if rm.client != nil {
		return rm.client.Close()
	}
	return nil
}