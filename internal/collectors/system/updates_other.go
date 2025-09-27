//go:build !linux && !windows && !darwin

package system

import (
	"context"
	"fmt"
)

// getAvailableUpdates is not implemented for unsupported platforms.
// The metric won't be collected on these platforms.
func (c *Collector) getAvailableUpdates(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("update checking not supported on this platform")
}