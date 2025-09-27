// Package metrics provides enterprise-grade metric types and interfaces.
// Eliminates interface{} usage and implements proper error handling patterns.
package metrics

import (
	"context"
	"time"
)

// MetricType represents the type of metric being collected.
type MetricType string

const (
	// MetricTypeGauge represents a metric that can go up or down.
	MetricTypeGauge MetricType = "gauge"
	// MetricTypeCounter represents a metric that only increases.
	MetricTypeCounter MetricType = "counter"
	// MetricTypeHistogram represents a metric that samples observations.
	MetricTypeHistogram MetricType = "histogram"
)

// Metric represents a single metric data point with associated metadata.
// All fields are strongly typed to eliminate interface{} usage.
type Metric struct {
	// Timestamp is when this metric was collected.
	Timestamp time.Time `json:"timestamp"`

	// Type indicates the metric type (gauge, counter, histogram).
	Type MetricType `json:"type"`

	// Name is the metric name following Prometheus naming conventions.
	Name string `json:"name"`

	// Value is the numeric value of the metric.
	Value float64 `json:"value"`

	// Labels provide dimensional metadata for the metric.
	Labels map[string]string `json:"labels"`
}

// Collector defines the interface that all metric collectors must implement.
// Follows enterprise patterns with comprehensive error handling.
type Collector interface {
	// Name returns the unique name of this collector.
	Name() string

	// Collect gathers metrics and returns them. The context allows for
	// cancellation and timeout handling.
	Collect(ctx context.Context) ([]Metric, error)

	// Enabled returns true if this collector should be active.
	Enabled() bool

	// Close performs any necessary cleanup. Collectors should be safe
	// to call Close multiple times.
	Close() error
}

// HealthChecker is an optional interface that collectors can implement
// to provide health status information.
type HealthChecker interface {
	// HealthCheck returns nil if the collector is healthy, or an error
	// describing the health issue.
	HealthCheck(ctx context.Context) error
}

// ConnectionManager is an interface for collectors that manage connections
// to external services (like libvirt, Docker, etc.).
type ConnectionManager interface {
	// Connect establishes connection(s) to the external service.
	Connect(ctx context.Context) error

	// Disconnect cleanly closes connection(s).
	Disconnect() error

	// IsConnected returns true if the connection is active and healthy.
	IsConnected() bool
}


// Registry manages a collection of metric collectors and orchestrates
// their execution with enterprise patterns.
type Registry interface {
	// Register adds a collector to the registry with validation.
	Register(collector Collector) error

	// Unregister removes a collector from the registry.
	Unregister(name string) error

	// CollectAll gathers metrics from all enabled collectors.
	// Collection happens concurrently with proper error handling.
	CollectAll(ctx context.Context) ([]Metric, error)

	// ListCollectors returns the names of all registered collectors.
	ListCollectors() []string

	// GetCollector returns a collector by name, or nil if not found.
	GetCollector(name string) Collector

	// HealthCheck checks the health of all collectors that implement HealthChecker.
	HealthCheck(ctx context.Context) map[string]error

	// Close shuts down all collectors gracefully.
	Close() error

	// GetStats returns registry statistics for monitoring.
	GetStats() RegistryStats
}

// RegistryStats provides registry operational statistics.
type RegistryStats struct {
	// TotalCollectors is the number of registered collectors.
	TotalCollectors int `json:"total_collectors"`

	// EnabledCollectors is the number of enabled collectors.
	EnabledCollectors int `json:"enabled_collectors"`

	// LastCollectionDuration is the time taken for the last collection cycle.
	LastCollectionDuration time.Duration `json:"last_collection_duration"`

	// TotalCollections is the total number of collection cycles performed.
	TotalCollections int64 `json:"total_collections"`

	// FailedCollections is the number of failed collection cycles.
	FailedCollections int64 `json:"failed_collections"`

	// LastCollectionTime is when the last collection occurred.
	LastCollectionTime time.Time `json:"last_collection_time"`
}

// CollectorConfig provides enterprise configuration options for collectors.
type CollectorConfig struct {
	// Enabled indicates if the collector should run.
	Enabled bool `json:"enabled"`

	// Timeout is the maximum time allowed for collection.
	Timeout time.Duration `json:"timeout"`

	// Retries is the number of retry attempts on failure.
	Retries int `json:"retries"`

	// RetryDelay is the delay between retry attempts.
	RetryDelay time.Duration `json:"retry_delay"`

	// Tags are additional labels to add to all metrics from this collector.
	Tags map[string]string `json:"tags"`
}

// CollectorResult represents the result of a collection operation.
type CollectorResult struct {
	// CollectorName is the name of the collector that produced this result.
	CollectorName string `json:"collector_name"`

	// Metrics are the collected metrics.
	Metrics []Metric `json:"metrics"`

	// Error is any error that occurred during collection.
	Error error `json:"error,omitempty"`

	// Duration is how long the collection took.
	Duration time.Duration `json:"duration"`

	// Timestamp is when the collection occurred.
	Timestamp time.Time `json:"timestamp"`
}

// ValidationError represents a metric validation error with details.
type ValidationError struct {
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (v ValidationError) Error() string {
	return v.Message
}

// NewValidationError creates a new validation error.
func NewValidationError(field, value, message string) ValidationError {
	return ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}

// MetricLimits defines enterprise limits for metrics.
type MetricLimits struct {
	// MaxMetricsPerCollection is the maximum number of metrics per collection cycle.
	MaxMetricsPerCollection int `json:"max_metrics_per_collection"`

	// MaxLabelCount is the maximum number of labels per metric.
	MaxLabelCount int `json:"max_label_count"`

	// MaxLabelKeyLength is the maximum length of label keys.
	MaxLabelKeyLength int `json:"max_label_key_length"`

	// MaxLabelValueLength is the maximum length of label values.
	MaxLabelValueLength int `json:"max_label_value_length"`

	// MaxMetricNameLength is the maximum length of metric names.
	MaxMetricNameLength int `json:"max_metric_name_length"`
}

// DefaultLimits returns sensible enterprise limits.
func DefaultLimits() MetricLimits {
	return MetricLimits{
		MaxMetricsPerCollection: 10000,
		MaxLabelCount:           20,
		MaxLabelKeyLength:       100,
		MaxLabelValueLength:     255,
		MaxMetricNameLength:     100,
	}
}
