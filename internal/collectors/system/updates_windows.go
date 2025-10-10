//go:build windows

package system

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/ceshihao/windowsupdate"
	"github.com/go-ole/go-ole"
)

// Cache update count for 6 hours to avoid repeated COM calls
// Windows Update API is slow on first call (~500-1000ms) but provides accurate data
// Background goroutine checks for updates every 6 hours and populates this cache
// Caching eliminates the overhead for 60-second collection cycles
var (
	cachedUpdateCount     int
	cachedSecurityCount   int
	updateCacheExpiry     time.Time
	updateCacheMutex      sync.Mutex
	updateCacheInitialized bool
)

// getAvailableUpdates returns the number of available system updates using Windows Update Agent API.
// This replaces file I/O based methods which were:
// - Reading large log files (3-8 seconds with Windows Defender scanning)
// - Inaccurate (parsing text logs is unreliable)
// - Subject to file locks and permissions issues
//
// Windows Update Agent COM API is:
// - Official Microsoft API for update checking
// - Accurate and reliable
// - First call: 500-1000ms, cached calls: 0ms (instant)
// - Cached for 6 hours (background check runs every 6 hours)
//
// IMPORTANT: This function can take 60-120 seconds on first call.
// Always call from a background goroutine with a timeout context.
func (c *Collector) GetAvailableUpdates(ctx context.Context) (int, error) {
	updateCacheMutex.Lock()

	// Return cached value if still fresh (6 hours)
	if updateCacheInitialized && time.Now().Before(updateCacheExpiry) {
		c.logger.Debug("Returning cached Windows Update count",
			"total_updates", cachedUpdateCount,
			"security_updates", cachedSecurityCount,
			"cache_expires_in", time.Until(updateCacheExpiry).Round(time.Minute).String())
		updateCacheMutex.Unlock()
		return cachedUpdateCount, nil
	}

	// If another goroutine is already checking, return cached value or 0
	// This prevents multiple simultaneous COM API calls which can cause deadlocks
	if updateCacheInitialized {
		c.logger.Debug("Another update check may be in progress, returning cached value",
			"cached_count", cachedUpdateCount)
		updateCacheMutex.Unlock()
		return cachedUpdateCount, nil
	}

	updateCacheMutex.Unlock()

	// Check if context is already cancelled before starting expensive COM operations
	if ctx.Err() != nil {
		c.logger.Debug("Context cancelled before Windows Update check", "error", ctx.Err())
		return 0, fmt.Errorf("context cancelled: %w", ctx.Err())
	}

	c.logger.Debug("Querying Windows Update Agent API for available updates (cache expired or first run)")

	// Run COM operations in a separate goroutine with channel for results
	// This allows us to enforce a timeout even though COM calls can't be interrupted
	type result struct {
		count         int
		securityCount int
		err           error
	}

	resultChan := make(chan result, 1)

	go func() {
		// CRITICAL: Lock this goroutine to the current OS thread for COM API calls
		// COM objects are bound to specific threads - goroutines can migrate between threads
		// Without this, accessing COM from a different thread causes immediate crashes
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Initialize COM for this thread/goroutine
		// COM must be initialized before using any COM objects
		// COINIT_APARTMENTTHREADED enables message pumping to prevent service hangs
		// This matches the official windowsupdate Go library example
		err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
		if err != nil {
			c.logger.Error("CRITICAL: Failed to initialize COM for Windows Update",
				"error", err,
				"error_type", fmt.Sprintf("%T", err))
			resultChan <- result{err: fmt.Errorf("COM initialization failed: %w", err)}
			return
		}
		defer ole.CoUninitialize()

		// Use Windows Update Agent COM API
		// Note: COM objects are cleaned up by go-ole's garbage collection
		session, err := windowsupdate.NewUpdateSession()
		if err != nil {
			c.logger.Warn("Failed to create Windows Update session", "error", err)
			resultChan <- result{err: err}
			return
		}

		searcher, err := session.CreateUpdateSearcher()
		if err != nil {
			c.logger.Warn("Failed to create update searcher", "error", err)
			resultChan <- result{err: err}
			return
		}

		// Search for updates that are:
		// - Not installed (IsInstalled=0)
		// - Not hidden (IsHidden=0)
		// Criteria reference: https://docs.microsoft.com/en-us/windows/win32/api/wuapi/nf-wuapi-iupdatesearcher-search
		// NOTE: This call can take 60-120 seconds and CANNOT be interrupted
		searchResult, err := searcher.Search("IsInstalled=0 AND IsHidden=0")
		if err != nil {
			c.logger.Warn("Failed to search for updates via Windows Update Agent", "error", err)
			resultChan <- result{err: err}
			return
		}

		// Count total updates
		updateCount := len(searchResult.Updates)

		// Count security updates specifically
		// Security updates have MsrcSeverity field set (Critical, Important, Moderate, Low)
		securityCount := 0
		for _, update := range searchResult.Updates {
			if update.MsrcSeverity != "" {
				securityCount++
			}
		}

		resultChan <- result{count: updateCount, securityCount: securityCount}
	}()

	// Wait for result with timeout (30 seconds max for COM call)
	// Normal Windows Update checks should complete in 5-15 seconds
	select {
	case res := <-resultChan:
		if res.err != nil {
			// If COM fails, return cached value if we have one, otherwise error
			updateCacheMutex.Lock()
			if updateCacheInitialized {
				c.logger.Info("Returning stale cached update count due to COM error",
					"cached_count", cachedUpdateCount,
					"error", res.err)
				updateCacheMutex.Unlock()
				return cachedUpdateCount, nil
			}
			updateCacheMutex.Unlock()
			return 0, res.err
		}

		// Cache the results for 6 hours
		// Windows Update doesn't change frequently, aligns with background check interval
		updateCacheMutex.Lock()
		cachedUpdateCount = res.count
		cachedSecurityCount = res.securityCount
		updateCacheExpiry = time.Now().Add(6 * time.Hour)
		updateCacheInitialized = true
		updateCacheMutex.Unlock()

		c.logger.Info("Windows Update check completed via COM API",
			"total_updates", res.count,
			"security_updates", res.securityCount,
			"cache_valid_until", updateCacheExpiry.Format(time.RFC3339))

		return res.count, nil

	case <-time.After(30 * time.Second):
		c.logger.Warn("Windows Update check timed out after 30 seconds - this may indicate a Windows Update service issue")
		// Return cached value if we have one, otherwise return 0
		// The background goroutine will continue and eventually populate the cache
		updateCacheMutex.Lock()
		if updateCacheInitialized {
			c.logger.Info("Returning stale cached update count due to timeout",
				"cached_count", cachedUpdateCount)
			count := cachedUpdateCount
			updateCacheMutex.Unlock()
			return count, nil
		}
		updateCacheMutex.Unlock()
		return 0, fmt.Errorf("Windows Update check timed out after 30 seconds")

	case <-ctx.Done():
		c.logger.Debug("Windows Update check cancelled via context")
		// Return cached value if available
		updateCacheMutex.Lock()
		if updateCacheInitialized {
			count := cachedUpdateCount
			updateCacheMutex.Unlock()
			return count, nil
		}
		updateCacheMutex.Unlock()
		return 0, ctx.Err()
	}
}

// getSecurityUpdates returns the number of available security updates on Windows.
// Security updates are those with MsrcSeverity field set (Critical, Important, Moderate, Low).
// This data is cached along with total update count for 6 hours.
// NOTE: Caller must call GetAvailableUpdates() first to populate the cache.
// This function only reads from the cache to avoid deadlock issues with mutex + thread locking.
func (c *Collector) GetSecurityUpdates(ctx context.Context) (int, error) {
	updateCacheMutex.Lock()
	defer updateCacheMutex.Unlock()

	// Return cached value if initialized
	if updateCacheInitialized {
		c.logger.Debug("Returning cached security update count",
			"security_updates", cachedSecurityCount,
			"cache_valid", time.Now().Before(updateCacheExpiry))
		return cachedSecurityCount, nil
	}

	// Cache not initialized - caller should call GetAvailableUpdates first
	c.logger.Warn("Security update count requested but cache not initialized - caller should call GetAvailableUpdates first")
	return 0, fmt.Errorf("update cache not initialized")
}
