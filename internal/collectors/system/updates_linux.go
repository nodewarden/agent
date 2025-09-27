//go:build linux

package system

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// getAvailableUpdates returns the number of available system updates on Linux.
// It safely checks package manager databases without executing commands.
func (c *Collector) getAvailableUpdates(ctx context.Context) (int, error) {
	// Try different package managers in order of prevalence

	// Check for APT (Debian/Ubuntu)
	if count, err := getAPTUpdates(); err == nil {
		return count, nil
	}

	// Check for YUM/DNF (RHEL/Fedora/CentOS)
	if count, err := getYumDnfUpdates(); err == nil {
		return count, nil
	}

	// Check for Zypper (openSUSE)
	if count, err := getZypperUpdates(); err == nil {
		return count, nil
	}

	// Check for Pacman (Arch)
	if count, err := getPacmanUpdates(); err == nil {
		return count, nil
	}

	// No supported package manager found or couldn't read update info
	return 0, nil
}

// getAPTUpdates checks for available updates on APT-based systems.
func getAPTUpdates() (int, error) {
	// Check if this is an APT-based system
	if _, err := os.Stat("/usr/bin/apt"); os.IsNotExist(err) {
		return 0, err
	}

	// Try to read the update-notifier status file (Ubuntu/Debian with update-notifier)
	statusFile := "/var/lib/update-notifier/updates-available"
	if data, err := os.ReadFile(statusFile); err == nil {
		// Parse the file which contains lines like:
		// 35 updates can be applied immediately.
		// 12 of these updates are security updates.
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.Contains(line, "updates can be applied") || strings.Contains(line, "packages can be updated") {
				// Extract the number from the beginning of the line
				parts := strings.Fields(line)
				if len(parts) > 0 {
					if count, err := strconv.Atoi(parts[0]); err == nil {
						return count, nil
					}
				}
			}
		}
	}

	// Fallback: check the apt cache directly
	// This reads a pre-existing file that apt maintains
	cacheFile := "/var/lib/apt/periodic/update-success-stamp"
	if _, err := os.Stat(cacheFile); err == nil {
		// If the cache exists but we can't parse updates-available,
		// we at least know apt is present but can't determine count
		return 0, nil
	}

	return 0, os.ErrNotExist
}

// getYumDnfUpdates checks for available updates on YUM/DNF-based systems.
func getYumDnfUpdates() (int, error) {
	// Check if this is a YUM/DNF-based system
	hasDnf := false
	hasYum := false

	if _, err := os.Stat("/usr/bin/dnf"); err == nil {
		hasDnf = true
	}
	if _, err := os.Stat("/usr/bin/yum"); err == nil {
		hasYum = true
	}

	if !hasDnf && !hasYum {
		return 0, os.ErrNotExist
	}

	// Try to read the yum/dnf check-update cache
	// DNF/YUM creates a cache file when check-update is run
	// We read the existing cache, not run the command
	cacheFiles := []string{
		"/var/cache/dnf/packages.cache",
		"/var/cache/yum/packages.cache",
	}

	for _, cacheFile := range cacheFiles {
		if data, err := os.ReadFile(cacheFile); err == nil {
			// Count lines that look like package updates
			count := 0
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := scanner.Text()
				// Package update lines typically contain version numbers
				if strings.Contains(line, ".rpm") || strings.Contains(line, "updates") {
					count++
				}
			}
			if count > 0 {
				return count, nil
			}
		}
	}

	// On RHEL/CentOS, check needs-restarting file
	needsRestartingFile := "/var/run/reboot-required.pkgs"
	if data, err := os.ReadFile(needsRestartingFile); err == nil {
		// Each line is a package that needs updating
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			return len(lines), nil
		}
	}

	return 0, nil
}

// getZypperUpdates checks for available updates on Zypper-based systems.
func getZypperUpdates() (int, error) {
	// Check if this is a Zypper-based system
	if _, err := os.Stat("/usr/bin/zypper"); os.IsNotExist(err) {
		return 0, err
	}

	// Zypper stores update information in XML files
	cacheDir := "/var/cache/zypp"
	if _, err := os.Stat(cacheDir); err == nil {
		// Look for update metadata
		updateFile := filepath.Join(cacheDir, "solv", "@System", "cookie")
		if _, err := os.Stat(updateFile); err == nil {
			// System has zypper but we can't easily determine update count
			// without parsing complex solver files
			return 0, nil
		}
	}

	return 0, os.ErrNotExist
}

// getPacmanUpdates checks for available updates on Pacman-based systems.
func getPacmanUpdates() (int, error) {
	// Check if this is a Pacman-based system
	if _, err := os.Stat("/usr/bin/pacman"); os.IsNotExist(err) {
		return 0, err
	}

	// Check if checkupdates cache exists (created by pacman hooks)
	cacheFile := "/var/cache/pacman/updates.cache"
	if data, err := os.ReadFile(cacheFile); err == nil {
		// Each line is a package update
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			return len(lines), nil
		}
	}

	// Check the pacman log for pending updates
	logFile := "/var/log/pacman.log"
	if _, err := os.Stat(logFile); err == nil {
		// System has pacman but we can't determine update count easily
		return 0, nil
	}

	return 0, os.ErrNotExist
}