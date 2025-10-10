//go:build windows

package system

import (
	"context"
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
func (c *Collector) GetAvailableUpdates(ctx context.Context) (int, error) {
	updateCacheMutex.Lock()
	defer updateCacheMutex.Unlock()

	// Return cached value if still fresh (6 hours)
	if updateCacheInitialized && time.Now().Before(updateCacheExpiry) {
		c.logger.Debug("Returning cached Windows Update count",
			"total_updates", cachedUpdateCount,
			"security_updates", cachedSecurityCount,
			"cache_expires_in", time.Until(updateCacheExpiry).Round(time.Minute).String())
		return cachedUpdateCount, nil
	}

	c.logger.Debug("Querying Windows Update Agent API for available updates (cache expired or first run)")

	// Initialize COM for this thread/goroutine
	// COM must be initialized before using any COM objects
	// COINIT_MULTITHREADED allows concurrent COM calls from multiple goroutines
	err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if err != nil {
		c.logger.Warn("Failed to initialize COM", "error", err)
		// If COM initialization fails, return cached value if we have one, otherwise 0
		if updateCacheInitialized {
			c.logger.Info("Returning stale cached update count due to COM initialization error",
				"cached_count", cachedUpdateCount)
			return cachedUpdateCount, nil
		}
		return 0, err
	}
	defer ole.CoUninitialize()

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

	// Cache the results for 6 hours
	// Windows Update doesn't change frequently, aligns with background check interval
	cachedUpdateCount = updateCount
	cachedSecurityCount = securityCount
	updateCacheExpiry = time.Now().Add(6 * time.Hour)
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
func (c *Collector) GetSecurityUpdates(ctx context.Context) (int, error) {
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
	// Unlock mutex before calling GetAvailableUpdates to avoid deadlock
	updateCacheMutex.Unlock()
	_, err := c.GetAvailableUpdates(ctx)
	updateCacheMutex.Lock()

	// After getAvailableUpdates completes, security count should be cached
	if err != nil {
		c.logger.Warn("Failed to get security update count", "error", err)
		return 0, err
	}

	return cachedSecurityCount, nil
}
