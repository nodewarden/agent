// Package metrics provides adaptive batching for efficient metric transmission.
package metrics

import (
	"log/slog"
	"sync"
)

// AdaptiveBatcher dynamically adjusts batch sizes based on network latency and performance.
// This helps optimize network efficiency by using larger batches when latency is low
// and smaller batches when latency is high or when errors occur.
type AdaptiveBatcher struct {
	minBatchSize    int           // Minimum batch size (default: 100)
	maxBatchSize    int           // Maximum batch size (default: 1000)
	currentBatch    int           // Current batch size
	targetLatencyMs int64         // Target latency in milliseconds (default: 1000ms)
	
	// Performance tracking
	lastLatencyMs    int64         // Last observed latency
	consecutiveGood  int           // Consecutive successful low-latency transmissions
	consecutiveBad   int           // Consecutive high-latency or failed transmissions
	
	// Adjustment parameters
	increaseStep     int           // Step size for increasing batch (default: 50)
	decreaseStep     int           // Step size for decreasing batch (default: 100)
	stabilityThreshold int         // Transmissions needed before adjustment (default: 3)
	
	mu               sync.RWMutex
	logger           *slog.Logger
	
	// Statistics
	totalAdjustments   int64
	totalTransmissions int64
	avgLatencyMs       float64
	statsMu            sync.RWMutex
}

// AdaptiveBatchConfig configures the adaptive batching behavior.
type AdaptiveBatchConfig struct {
	// MinBatchSize is the minimum number of metrics per batch (default: 100)
	MinBatchSize int
	
	// MaxBatchSize is the maximum number of metrics per batch (default: 1000)
	MaxBatchSize int
	
	// InitialBatchSize is the starting batch size (default: 500)
	InitialBatchSize int
	
	// TargetLatencyMs is the target transmission latency in milliseconds (default: 1000)
	TargetLatencyMs int64
	
	// IncreaseStep is how much to increase batch size on good performance (default: 50)
	IncreaseStep int
	
	// DecreaseStep is how much to decrease batch size on poor performance (default: 100)
	DecreaseStep int
	
	// StabilityThreshold is consecutive transmissions before adjusting (default: 3)
	StabilityThreshold int
	
	// Logger for debug and monitoring information
	Logger *slog.Logger
}

// NewAdaptiveBatcher creates a new adaptive batching system with the specified configuration.
func NewAdaptiveBatcher(config AdaptiveBatchConfig) *AdaptiveBatcher {
	// Set defaults
	if config.MinBatchSize <= 0 {
		config.MinBatchSize = 100
	}
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 1000
	}
	if config.InitialBatchSize <= 0 {
		config.InitialBatchSize = 500
	}
	if config.TargetLatencyMs <= 0 {
		config.TargetLatencyMs = 1000 // 1 second target
	}
	if config.IncreaseStep <= 0 {
		config.IncreaseStep = 50
	}
	if config.DecreaseStep <= 0 {
		config.DecreaseStep = 100
	}
	if config.StabilityThreshold <= 0 {
		config.StabilityThreshold = 3
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	
	// Ensure initial batch size is within bounds
	initialSize := config.InitialBatchSize
	if initialSize < config.MinBatchSize {
		initialSize = config.MinBatchSize
	}
	if initialSize > config.MaxBatchSize {
		initialSize = config.MaxBatchSize
	}
	
	return &AdaptiveBatcher{
		minBatchSize:       config.MinBatchSize,
		maxBatchSize:       config.MaxBatchSize,
		currentBatch:       initialSize,
		targetLatencyMs:    config.TargetLatencyMs,
		increaseStep:       config.IncreaseStep,
		decreaseStep:       config.DecreaseStep,
		stabilityThreshold: config.StabilityThreshold,
		logger:             config.Logger.With("component", "adaptive_batcher"),
		avgLatencyMs:       float64(config.TargetLatencyMs),
	}
}

// GetBatchSize returns the current recommended batch size.
func (ab *AdaptiveBatcher) GetBatchSize() int {
	ab.mu.RLock()
	defer ab.mu.RUnlock()
	return ab.currentBatch
}

// RecordTransmission records the results of a transmission and adjusts batch size accordingly.
// latencyMs is the transmission latency in milliseconds. success indicates if transmission succeeded.
func (ab *AdaptiveBatcher) RecordTransmission(latencyMs int64, success bool) {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	
	ab.statsMu.Lock()
	ab.totalTransmissions++
	// Update rolling average latency
	if ab.totalTransmissions == 1 {
		ab.avgLatencyMs = float64(latencyMs)
	} else {
		// Exponential moving average with factor 0.1
		ab.avgLatencyMs = 0.9*ab.avgLatencyMs + 0.1*float64(latencyMs)
	}
	ab.statsMu.Unlock()
	
	ab.lastLatencyMs = latencyMs
	
	// Determine if this transmission was "good" or "bad"
	isGoodTransmission := success && latencyMs <= ab.targetLatencyMs
	
	if isGoodTransmission {
		ab.consecutiveGood++
		ab.consecutiveBad = 0
		
		ab.logger.Debug("Good transmission recorded",
			"latency_ms", latencyMs,
			"target_ms", ab.targetLatencyMs,
			"consecutive_good", ab.consecutiveGood,
			"current_batch", ab.currentBatch)
	} else {
		ab.consecutiveBad++
		ab.consecutiveGood = 0
		
		reason := "high_latency"
		if !success {
			reason = "failed"
		}
		
		ab.logger.Debug("Bad transmission recorded",
			"latency_ms", latencyMs,
			"target_ms", ab.targetLatencyMs,
			"success", success,
			"reason", reason,
			"consecutive_bad", ab.consecutiveBad,
			"current_batch", ab.currentBatch)
	}
	
	// Adjust batch size if we have enough consecutive results
	ab.adjustBatchSize()
}

// adjustBatchSize modifies the current batch size based on recent performance.
func (ab *AdaptiveBatcher) adjustBatchSize() {
	oldBatch := ab.currentBatch
	
	// Increase batch size after consecutive good transmissions
	if ab.consecutiveGood >= ab.stabilityThreshold {
		newSize := ab.currentBatch + ab.increaseStep
		if newSize <= ab.maxBatchSize {
			ab.currentBatch = newSize
			ab.consecutiveGood = 0 // Reset counter
			
			ab.statsMu.Lock()
			ab.totalAdjustments++
			ab.statsMu.Unlock()
			
			ab.logger.Info("Increased batch size due to good performance",
				"old_size", oldBatch,
				"new_size", ab.currentBatch,
				"avg_latency_ms", ab.avgLatencyMs,
				"target_latency_ms", ab.targetLatencyMs)
		}
	}
	
	// Decrease batch size after consecutive bad transmissions
	if ab.consecutiveBad >= ab.stabilityThreshold {
		newSize := ab.currentBatch - ab.decreaseStep
		if newSize >= ab.minBatchSize {
			ab.currentBatch = newSize
			ab.consecutiveBad = 0 // Reset counter
			
			ab.statsMu.Lock()
			ab.totalAdjustments++
			ab.statsMu.Unlock()
			
			ab.logger.Info("Decreased batch size due to poor performance",
				"old_size", oldBatch,
				"new_size", ab.currentBatch,
				"last_latency_ms", ab.lastLatencyMs,
				"target_latency_ms", ab.targetLatencyMs)
		}
	}
}

// BatchMetrics splits a slice of metrics into appropriately sized batches.
func (ab *AdaptiveBatcher) BatchMetrics(metrics []Metric) [][]Metric {
	if len(metrics) == 0 {
		return nil
	}
	
	batchSize := ab.GetBatchSize()
	var batches [][]Metric
	
	for i := 0; i < len(metrics); i += batchSize {
		end := i + batchSize
		if end > len(metrics) {
			end = len(metrics)
		}
		batches = append(batches, metrics[i:end])
	}
	
	ab.logger.Debug("Metrics batched",
		"total_metrics", len(metrics),
		"batch_size", batchSize,
		"num_batches", len(batches))
	
	return batches
}

// ForceAdjustBatchSize manually sets the batch size (for testing or manual tuning).
func (ab *AdaptiveBatcher) ForceAdjustBatchSize(newSize int) bool {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	
	if newSize < ab.minBatchSize || newSize > ab.maxBatchSize {
		ab.logger.Warn("Attempted to set batch size outside valid range",
			"requested_size", newSize,
			"min_size", ab.minBatchSize,
			"max_size", ab.maxBatchSize)
		return false
	}
	
	oldSize := ab.currentBatch
	ab.currentBatch = newSize
	
	// Reset counters
	ab.consecutiveGood = 0
	ab.consecutiveBad = 0
	
	ab.logger.Info("Manually adjusted batch size",
		"old_size", oldSize,
		"new_size", newSize)
	
	return true
}

// GetStats returns statistics about adaptive batching performance.
func (ab *AdaptiveBatcher) GetStats() AdaptiveBatchStats {
	ab.mu.RLock()
	ab.statsMu.RLock()
	defer ab.mu.RUnlock()
	defer ab.statsMu.RUnlock()
	
	return AdaptiveBatchStats{
		CurrentBatchSize:    ab.currentBatch,
		MinBatchSize:        ab.minBatchSize,
		MaxBatchSize:        ab.maxBatchSize,
		TargetLatencyMs:     ab.targetLatencyMs,
		LastLatencyMs:       ab.lastLatencyMs,
		AvgLatencyMs:        ab.avgLatencyMs,
		ConsecutiveGood:     ab.consecutiveGood,
		ConsecutiveBad:      ab.consecutiveBad,
		TotalAdjustments:    ab.totalAdjustments,
		TotalTransmissions:  ab.totalTransmissions,
		StabilityThreshold:  ab.stabilityThreshold,
	}
}

// AdaptiveBatchStats provides statistics about adaptive batching performance.
type AdaptiveBatchStats struct {
	CurrentBatchSize    int     // Current batch size
	MinBatchSize        int     // Minimum allowed batch size
	MaxBatchSize        int     // Maximum allowed batch size
	TargetLatencyMs     int64   // Target latency in milliseconds
	LastLatencyMs       int64   // Last measured latency
	AvgLatencyMs        float64 // Average latency over time
	ConsecutiveGood     int     // Current consecutive good transmissions
	ConsecutiveBad      int     // Current consecutive bad transmissions
	TotalAdjustments    int64   // Total number of batch size adjustments
	TotalTransmissions  int64   // Total number of transmissions recorded
	StabilityThreshold  int     // Required consecutive results before adjustment
}

// Reset clears all performance tracking and resets to initial batch size.
func (ab *AdaptiveBatcher) Reset() {
	ab.mu.Lock()
	ab.statsMu.Lock()
	defer ab.mu.Unlock()
	defer ab.statsMu.Unlock()
	
	initialSize := (ab.minBatchSize + ab.maxBatchSize) / 2
	ab.currentBatch = initialSize
	ab.lastLatencyMs = 0
	ab.consecutiveGood = 0
	ab.consecutiveBad = 0
	ab.totalAdjustments = 0
	ab.totalTransmissions = 0
	ab.avgLatencyMs = float64(ab.targetLatencyMs)
	
	ab.logger.Info("Adaptive batcher reset",
		"reset_batch_size", initialSize)
}