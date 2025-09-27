// Package metrics provides self-monitoring metrics for the Nodewarden agent
// to track health, performance, and operational statistics.
package metrics

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// SelfMonitor tracks agent health and performance metrics
type SelfMonitor struct {
	startTime time.Time
	
	// Transmission metrics (atomic for thread safety)
	transmissionSuccess   atomic.Int64
	transmissionFailure   atomic.Int64
	transmissionRetries   atomic.Int64
	bytesTransmitted     atomic.Int64
	metricsTransmitted   atomic.Int64
	
	// Buffer metrics
	bufferUtilization    atomic.Int64  // Current buffer usage percentage
	bufferOverflows      atomic.Int64  // Number of times buffer was full
	bufferFlushes        atomic.Int64  // Number of buffer flushes
	
	// Collection metrics
	collectionsTotal     atomic.Int64
	collectionErrors     atomic.Int64
	collectionDuration   atomic.Int64  // Total duration in nanoseconds
	
	// Resource usage
	lastMemStats         runtime.MemStats
	lastMemStatTime      time.Time
	memStatsMu           sync.RWMutex
	
	// Retry statistics
	retryAttempts        atomic.Int64
	retrySuccesses       atomic.Int64
	retryExhausted       atomic.Int64
	
	// Agent health
	restarts             atomic.Int64
	configReloads        atomic.Int64
	lastError            atomic.Value  // stores string
	lastErrorTime        atomic.Value  // stores time.Time
}

// NewSelfMonitor creates a new self-monitoring instance
func NewSelfMonitor() *SelfMonitor {
	sm := &SelfMonitor{
		startTime: time.Now(),
	}
	
	// Initialize atomic.Value fields
	sm.lastError.Store("")
	sm.lastErrorTime.Store(time.Time{})
	
	// Capture initial memory stats
	runtime.ReadMemStats(&sm.lastMemStats)
	sm.lastMemStatTime = time.Now()
	
	return sm
}

// RecordTransmissionSuccess records a successful transmission
func (sm *SelfMonitor) RecordTransmissionSuccess(metricsCount int, bytes int64) {
	sm.transmissionSuccess.Add(1)
	sm.metricsTransmitted.Add(int64(metricsCount))
	sm.bytesTransmitted.Add(bytes)
}

// RecordTransmissionFailure records a failed transmission
func (sm *SelfMonitor) RecordTransmissionFailure(err error) {
	sm.transmissionFailure.Add(1)
	if err != nil {
		sm.lastError.Store(err.Error())
		sm.lastErrorTime.Store(time.Now())
	}
}

// RecordTransmissionRetry records a transmission retry
func (sm *SelfMonitor) RecordTransmissionRetry() {
	sm.transmissionRetries.Add(1)
}

// UpdateBufferUtilization updates the current buffer utilization percentage
func (sm *SelfMonitor) UpdateBufferUtilization(current, max int) {
	if max > 0 {
		utilization := int64(current * 100 / max)
		sm.bufferUtilization.Store(utilization)
	}
}

// RecordBufferOverflow records when the buffer is full
func (sm *SelfMonitor) RecordBufferOverflow() {
	sm.bufferOverflows.Add(1)
}

// RecordBufferFlush records a buffer flush event
func (sm *SelfMonitor) RecordBufferFlush(metricsCount int) {
	sm.bufferFlushes.Add(1)
}

// RecordCollection records a metric collection event
func (sm *SelfMonitor) RecordCollection(duration time.Duration, err error) {
	sm.collectionsTotal.Add(1)
	sm.collectionDuration.Add(int64(duration))
	
	if err != nil {
		sm.collectionErrors.Add(1)
		sm.lastError.Store(err.Error())
		sm.lastErrorTime.Store(time.Now())
	}
}

// RecordRetryAttempt records a retry attempt
func (sm *SelfMonitor) RecordRetryAttempt() {
	sm.retryAttempts.Add(1)
}

// RecordRetrySuccess records a successful retry
func (sm *SelfMonitor) RecordRetrySuccess() {
	sm.retrySuccesses.Add(1)
}

// RecordRetryExhausted records when retries are exhausted
func (sm *SelfMonitor) RecordRetryExhausted() {
	sm.retryExhausted.Add(1)
}

// RecordRestart records an agent restart
func (sm *SelfMonitor) RecordRestart() {
	sm.restarts.Add(1)
}

// RecordConfigReload records a configuration reload
func (sm *SelfMonitor) RecordConfigReload() {
	sm.configReloads.Add(1)
}

// updateMemStats updates memory statistics
func (sm *SelfMonitor) updateMemStats() {
	sm.memStatsMu.Lock()
	defer sm.memStatsMu.Unlock()
	
	runtime.ReadMemStats(&sm.lastMemStats)
	sm.lastMemStatTime = time.Now()
}

// GetMetrics returns all self-monitoring metrics as Metric objects
func (sm *SelfMonitor) GetMetrics() []Metric {
	// Update memory stats
	sm.updateMemStats()
	
	now := time.Now()
	uptime := now.Sub(sm.startTime).Seconds()
	
	metrics := []Metric{
		// Agent health metrics
		{
			Name:      "agent_uptime_seconds",
			Value:     uptime,
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "agent"},
		},
		{
			Name:      "agent_restarts_total",
			Value:     float64(sm.restarts.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "agent"},
		},
		{
			Name:      "agent_config_reloads_total",
			Value:     float64(sm.configReloads.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "agent"},
		},
		
		// Transmission metrics
		{
			Name:      "agent_transmission_success_total",
			Value:     float64(sm.transmissionSuccess.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "transmission"},
		},
		{
			Name:      "agent_transmission_failure_total",
			Value:     float64(sm.transmissionFailure.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "transmission"},
		},
		{
			Name:      "agent_transmission_retries_total",
			Value:     float64(sm.transmissionRetries.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "transmission"},
		},
		{
			Name:      "agent_bytes_transmitted_total",
			Value:     float64(sm.bytesTransmitted.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "transmission"},
		},
		{
			Name:      "agent_metrics_transmitted_total",
			Value:     float64(sm.metricsTransmitted.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "transmission"},
		},
		
		// Buffer metrics
		{
			Name:      "agent_buffer_utilization_percent",
			Value:     float64(sm.bufferUtilization.Load()),
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "buffer"},
		},
		{
			Name:      "agent_buffer_overflows_total",
			Value:     float64(sm.bufferOverflows.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "buffer"},
		},
		{
			Name:      "agent_buffer_flushes_total",
			Value:     float64(sm.bufferFlushes.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "buffer"},
		},
		
		// Collection metrics
		{
			Name:      "agent_collections_total",
			Value:     float64(sm.collectionsTotal.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "collection"},
		},
		{
			Name:      "agent_collection_errors_total",
			Value:     float64(sm.collectionErrors.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "collection"},
		},
		
		// Retry metrics
		{
			Name:      "agent_retry_attempts_total",
			Value:     float64(sm.retryAttempts.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "retry"},
		},
		{
			Name:      "agent_retry_successes_total",
			Value:     float64(sm.retrySuccesses.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "retry"},
		},
		{
			Name:      "agent_retry_exhausted_total",
			Value:     float64(sm.retryExhausted.Load()),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "retry"},
		},
	}
	
	// Add resource usage metrics
	sm.memStatsMu.RLock()
	memStats := sm.lastMemStats
	sm.memStatsMu.RUnlock()
	
	metrics = append(metrics, []Metric{
		{
			Name:      "agent_memory_alloc_bytes",
			Value:     float64(memStats.Alloc),
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "resources"},
		},
		{
			Name:      "agent_memory_sys_bytes",
			Value:     float64(memStats.Sys),
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "resources"},
		},
		{
			Name:      "agent_memory_heap_alloc_bytes",
			Value:     float64(memStats.HeapAlloc),
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "resources"},
		},
		{
			Name:      "agent_memory_heap_inuse_bytes",
			Value:     float64(memStats.HeapInuse),
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "resources"},
		},
		{
			Name:      "agent_gc_runs_total",
			Value:     float64(memStats.NumGC),
			Timestamp: now,
			Type:      MetricTypeCounter,
			Labels:    map[string]string{"component": "resources"},
		},
		{
			Name:      "agent_goroutines",
			Value:     float64(runtime.NumGoroutine()),
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "resources"},
		},
	}...)
	
	// Calculate success rate
	totalTransmissions := sm.transmissionSuccess.Load() + sm.transmissionFailure.Load()
	if totalTransmissions > 0 {
		successRate := float64(sm.transmissionSuccess.Load()) / float64(totalTransmissions) * 100
		metrics = append(metrics, Metric{
			Name:      "agent_transmission_success_rate",
			Value:     successRate,
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "transmission"},
		})
	}
	
	// Calculate average collection duration
	if sm.collectionsTotal.Load() > 0 {
		avgDuration := float64(sm.collectionDuration.Load()) / float64(sm.collectionsTotal.Load()) / 1e9 // Convert to seconds
		metrics = append(metrics, Metric{
			Name:      "agent_collection_duration_seconds",
			Value:     avgDuration,
			Timestamp: now,
			Type:      MetricTypeGauge,
			Labels:    map[string]string{"component": "collection"},
		})
	}
	
	return metrics
}

// GetStats returns a summary of current statistics
func (sm *SelfMonitor) GetStats() map[string]interface{} {
	totalTransmissions := sm.transmissionSuccess.Load() + sm.transmissionFailure.Load()
	successRate := float64(0)
	if totalTransmissions > 0 {
		successRate = float64(sm.transmissionSuccess.Load()) / float64(totalTransmissions) * 100
	}
	
	sm.memStatsMu.RLock()
	memAlloc := sm.lastMemStats.Alloc
	sm.memStatsMu.RUnlock()
	
	lastError, _ := sm.lastError.Load().(string)
	lastErrorTime, _ := sm.lastErrorTime.Load().(time.Time)
	
	return map[string]interface{}{
		"uptime_seconds":           time.Since(sm.startTime).Seconds(),
		"transmission_success":     sm.transmissionSuccess.Load(),
		"transmission_failure":     sm.transmissionFailure.Load(),
		"transmission_success_rate": successRate,
		"metrics_transmitted":      sm.metricsTransmitted.Load(),
		"bytes_transmitted":        sm.bytesTransmitted.Load(),
		"buffer_utilization":       sm.bufferUtilization.Load(),
		"buffer_overflows":         sm.bufferOverflows.Load(),
		"collections_total":        sm.collectionsTotal.Load(),
		"collection_errors":        sm.collectionErrors.Load(),
		"retry_attempts":           sm.retryAttempts.Load(),
		"retry_successes":          sm.retrySuccesses.Load(),
		"memory_alloc_mb":          float64(memAlloc) / 1024 / 1024,
		"goroutines":               runtime.NumGoroutine(),
		"last_error":               lastError,
		"last_error_time":          lastErrorTime,
	}
}

// Reset resets all counters (useful for testing)
func (sm *SelfMonitor) Reset() {
	sm.transmissionSuccess.Store(0)
	sm.transmissionFailure.Store(0)
	sm.transmissionRetries.Store(0)
	sm.bytesTransmitted.Store(0)
	sm.metricsTransmitted.Store(0)
	sm.bufferUtilization.Store(0)
	sm.bufferOverflows.Store(0)
	sm.bufferFlushes.Store(0)
	sm.collectionsTotal.Store(0)
	sm.collectionErrors.Store(0)
	sm.collectionDuration.Store(0)
	sm.retryAttempts.Store(0)
	sm.retrySuccesses.Store(0)
	sm.retryExhausted.Store(0)
	sm.restarts.Store(0)
	sm.configReloads.Store(0)
	sm.lastError.Store("")
	sm.lastErrorTime.Store(time.Time{})
	sm.startTime = time.Now()
}