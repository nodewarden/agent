// Package metrics provides simple metric types and creation functions.
package metrics

import (
	"time"
)

// NewMetric creates a new metric with the provided values.
// This is a simple factory function that replaces the complex builder pattern.
func NewMetric(name string, value float64, hostname string, labels map[string]string) Metric {
	// Ensure labels is not nil
	if labels == nil {
		labels = make(map[string]string)
	}
	
	// Add hostname to labels if provided
	if hostname != "" {
		labels["host"] = hostname
	}
	
	return Metric{
		Name:      name,
		Value:     value,
		Timestamp: time.Now(),
		Type:      MetricTypeGauge,
		Labels:    labels,
	}
}

// NewMetricWithTime creates a new metric with a specific timestamp.
func NewMetricWithTime(name string, value float64, hostname string, labels map[string]string, timestamp time.Time) Metric {
	if labels == nil {
		labels = make(map[string]string)
	}
	
	// Add hostname to labels if provided
	if hostname != "" {
		labels["host"] = hostname
	}
	
	return Metric{
		Name:      name,
		Value:     value,
		Timestamp: timestamp,
		Type:      MetricTypeGauge,
		Labels:    labels,
	}
}

// Quick helper functions for common metric types

// CPUMetric creates a CPU usage metric.
func CPUMetric(value float64, hostname string, core string) Metric {
	labels := map[string]string{}
	if core != "" {
		labels["core"] = core
	}
	return NewMetric("cpu_usage", value, hostname, labels)
}

// MemoryMetric creates a memory usage metric.
func MemoryMetric(value float64, hostname string, metricType string) Metric {
	labels := map[string]string{}
	if metricType != "" {
		labels["type"] = metricType
	}
	return NewMetric("memory_usage", value, hostname, labels)
}

// DiskMetric creates a disk usage metric.
func DiskMetric(value float64, hostname string, device string) Metric {
	labels := map[string]string{}
	if device != "" {
		labels["device"] = device
	}
	return NewMetric("disk_usage", value, hostname, labels)
}

// NetworkMetric creates a network metric.
func NetworkMetric(name string, value float64, hostname string, iface string) Metric {
	labels := map[string]string{}
	if iface != "" {
		labels["interface"] = iface
	}
	return NewMetric(name, value, hostname, labels)
}

// ProcessMetric creates a process metric.
func ProcessMetric(name string, value float64, hostname string, processName string) Metric {
	labels := map[string]string{}
	if processName != "" {
		labels["process"] = processName
	}
	return NewMetric(name, value, hostname, labels)
}