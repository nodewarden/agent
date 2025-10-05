//go:build windows

package system

import (
	"context"
	"sync"
	"time"

	"github.com/ceshihao/windowsupdate"
)

// Cache update count for 30 minutes to avoid repeated COM calls
// Windows Update API is slow on first call (~500-1000ms) but provides accurate data
// Caching eliminates the overhead for subsequent checks (60-second collection cycles)
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
// - Cached for 30 minutes (updates don't change frequently)
func (c *Collector) getAvailableUpdates(ctx context.Context) (int, error) {
	updateCacheMutex.Lock()
	defer updateCacheMutex.Unlock()

	// Return cached value if still fresh (30 minutes)
	if updateCacheInitialized && time.Now().Before(updateCacheExpiry) {
		c.logger.Debug("Returning cached Windows Update count",
			"total_updates", cachedUpdateCount,
			"security_updates", cachedSecurityCount,
			"cache_expires_in", time.Until(updateCacheExpiry).Round(time.Minute).String())
		return cachedUpdateCount, nil
	}

	c.logger.Debug("Querying Windows Update Agent API for available updates (cache expired or first run)")

	// Use Windows Update Agent COM API
	// Note: COM objects are cleaned up by go-ole's garbage collection
	session, err := windowsupdate.NewUpdateSession()
	if err != nil {
		c.logger.Warn("Failed to create Windows Update session", "error", err)
		// If COM fails, return cached value if we have one, otherwise 0
		if updateCacheInitialized {
			c.logger.Info("Returning stale cached update count due to COM error",
				"cached_count", cachedUpdateCount)
			return cachedUpdateCount, nil
		}
		return 0, err
	}

	searcher, err := session.CreateUpdateSearcher()
	if err != nil {
		c.logger.Warn("Failed to create update searcher", "error", err)
		// If searcher creation fails, return cached value if we have one, otherwise 0
		if updateCacheInitialized {
			c.logger.Info("Returning stale cached update count due to searcher error",
				"cached_count", cachedUpdateCount)
			return cachedUpdateCount, nil
		}
		return 0, err
	}

	// Search for updates that are:
	// - Not installed (IsInstalled=0)
	// - Not hidden (IsHidden=0)
	// Criteria reference: https://docs.microsoft.com/en-us/windows/win32/api/wuapi/nf-wuapi-iupdatesearcher-search
	searchResult, err := searcher.Search("IsInstalled=0 AND IsHidden=0")
	if err != nil {
		c.logger.Warn("Failed to search for updates via Windows Update Agent", "error", err)
		// If search fails, return cached value if we have one, otherwise 0
		if updateCacheInitialized {
			c.logger.Info("Returning stale cached update count due to search error",
				"cached_count", cachedUpdateCount)
			return cachedUpdateCount, nil
		}
		return 0, err
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

	// Cache the results for 30 minutes
	// Windows Update doesn't change frequently, so this is safe
	cachedUpdateCount = updateCount
	cachedSecurityCount = securityCount
	updateCacheExpiry = time.Now().Add(30 * time.Minute)
	updateCacheInitialized = true

	c.logger.Info("Windows Update check completed via COM API",
		"total_updates", updateCount,
		"security_updates", securityCount,
		"cache_valid_until", updateCacheExpiry.Format(time.RFC3339))

	return updateCount, nil
}

// getSecurityUpdates returns the number of available security updates on Windows.
// Security updates are those with MsrcSeverity field set (Critical, Important, Moderate, Low).
// This data is cached along with total update count for 30 minutes.
func (c *Collector) getSecurityUpdates(ctx context.Context) (int, error) {
	updateCacheMutex.Lock()
	defer updateCacheMutex.Unlock()

	// If cache is valid, return cached security count
	if updateCacheInitialized && time.Now().Before(updateCacheExpiry) {
		c.logger.Debug("Returning cached security update count",
			"security_updates", cachedSecurityCount,
			"cache_expires_in", time.Until(updateCacheExpiry).Round(time.Minute).String())
		return cachedSecurityCount, nil
	}

	// Cache is expired or not initialized
	// Unlock mutex before calling getAvailableUpdates to avoid deadlock
	updateCacheMutex.Unlock()
	_, err := c.getAvailableUpdates(ctx)
	updateCacheMutex.Lock()

	// After getAvailableUpdates completes, security count should be cached
	if err != nil {
		c.logger.Warn("Failed to get security update count", "error", err)
		return 0, err
	}

	return cachedSecurityCount, nil
}
