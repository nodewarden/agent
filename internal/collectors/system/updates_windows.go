//go:build windows

package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// getAvailableUpdates returns the number of available system updates on Windows.
// It safely checks Windows Update information without executing commands.
func (c *Collector) getAvailableUpdates(ctx context.Context) (int, error) {
	// Try multiple methods to get update count

	// Method 1: Check Windows Update data files
	if count, err := checkWindowsUpdateFiles(); err == nil && count > 0 {
		return count, nil
	}

	// Method 2: Check WSUS client state
	if count, err := checkWSUSClientState(); err == nil && count > 0 {
		return count, nil
	}

	// Method 3: Check Windows Update log
	if count, err := checkWindowsUpdateLog(); err == nil && count > 0 {
		return count, nil
	}

	// No updates found or couldn't read update info
	return 0, nil
}

// checkWindowsUpdateFiles checks Windows Update data files for pending updates.
func checkWindowsUpdateFiles() (int, error) {
	// Windows Update stores information in various locations
	// We check for files that indicate pending updates

	// Check the SoftwareDistribution folder
	winDir := os.Getenv("WINDIR")
	if winDir == "" {
		winDir = "C:\\Windows"
	}

	// Check for pending.xml which lists pending updates
	pendingFile := filepath.Join(winDir, "SoftwareDistribution", "ReportingEvents.log")
	if data, err := os.ReadFile(pendingFile); err == nil {
		// Count occurrences of update events
		count := 0
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			// Look for download or install pending events
			if strings.Contains(line, "UpdateOrchestrator") &&
			   (strings.Contains(line, "Download") || strings.Contains(line, "Install")) &&
			   strings.Contains(line, "Pending") {
				count++
			}
		}
		if count > 0 {
			return count, nil
		}
	}

	// Check Windows Update database
	dbPath := filepath.Join(winDir, "SoftwareDistribution", "DataStore", "DataStore.edb")
	if _, err := os.Stat(dbPath); err == nil {
		// Database exists but we can't easily parse it without libraries
		// This at least confirms Windows Update is configured
		return 0, nil
	}

	return 0, fmt.Errorf("Windows Update files not found")
}

// checkWSUSClientState checks WSUS client state file for update information.
func checkWSUSClientState() (int, error) {
	// WSUS client creates state files with update information
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = "C:\\ProgramData"
	}

	// Check for WSUS state file
	stateFile := filepath.Join(programData, "Microsoft", "Windows", "WindowsUpdate", "States.xml")
	if data, err := os.ReadFile(stateFile); err == nil {
		// Parse for update count
		// Look for PendingUpdateCount or similar tags
		if strings.Contains(string(data), "PendingUpdateCount") {
			// Extract count from XML
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.Contains(line, "PendingUpdateCount") {
					// Extract number from tag like <PendingUpdateCount>5</PendingUpdateCount>
					start := strings.Index(line, ">")
					end := strings.LastIndex(line, "<")
					if start > 0 && end > start {
						countStr := line[start+1 : end]
						if count, err := strconv.Atoi(strings.TrimSpace(countStr)); err == nil {
							return count, nil
						}
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("WSUS state file not found")
}

// checkWindowsUpdateLog checks Windows Update log for pending updates.
func checkWindowsUpdateLog() (int, error) {
	// Check the ETL log for Windows Update
	winDir := os.Getenv("WINDIR")
	if winDir == "" {
		winDir = "C:\\Windows"
	}

	// Windows 10/11 stores logs in ETL format, but we can check for the log directory
	logDir := filepath.Join(winDir, "Logs", "WindowsUpdate")
	if _, err := os.Stat(logDir); err == nil {
		// Directory exists, Windows Update is configured
		// We can't easily parse ETL files without special libraries
		return 0, nil
	}

	// Fallback: Check older text-based log
	logFile := filepath.Join(winDir, "WindowsUpdate.log")
	if data, err := os.ReadFile(logFile); err == nil {
		// Count update entries
		count := 0
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			// Look for lines indicating available updates
			if strings.Contains(line, "updates available") {
				// Try to extract count
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "updates" && i > 0 {
						if num, err := strconv.Atoi(parts[i-1]); err == nil {
							count = num
							break
						}
					}
				}
			} else if strings.Contains(line, "Update available") ||
					  strings.Contains(line, "Download available") {
				count++
			}
		}
		if count > 0 {
			return count, nil
		}
	}

	return 0, fmt.Errorf("Windows Update log not found")
}

// getSecurityUpdates returns the number of available security updates on Windows.
// Windows doesn't easily distinguish between regular and security updates without WMI/COM.
func (c *Collector) getSecurityUpdates(ctx context.Context) (int, error) {
	// Windows doesn't provide separate security update counts without COM/WMI
	// Return 0 as security updates are included in general updates
	return 0, nil
}