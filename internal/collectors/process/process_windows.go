//go:build windows
// +build windows

package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// getProcessStats gets statistics for a running process on Windows.
func (c *Collector) getProcessStats(processName string) *ProcessStats {
	// Use PowerShell to get process information
	// This command gets process by name and returns PID, CPU, and WorkingSet (memory)
	psScript := fmt.Sprintf(`
		$processes = Get-Process -Name '%s' -ErrorAction SilentlyContinue | Select-Object -First 1
		if ($processes) {
			$cpu = 0
			try {
				$cpu = [math]::Round($processes.CPU, 2)
			} catch {}
			Write-Output "$($processes.Id)|$cpu|$([math]::Round($processes.WorkingSet64 / 1MB, 2))"
		}
	`, strings.TrimSuffix(processName, ".exe"))
	
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err != nil {
		// Try with .exe extension if not already present
		if !strings.HasSuffix(processName, ".exe") {
			return c.getProcessStatsWithExe(processName + ".exe")
		}
		// Process not found
		return nil
	}
	
	result := strings.TrimSpace(string(output))
	if result == "" {
		return nil
	}
	
	// Parse the output: PID|CPU|MemoryMB
	parts := strings.Split(result, "|")
	if len(parts) != 3 {
		c.logger.Debug("unexpected PowerShell output format", "output", result)
		return nil
	}
	
	pid, err := strconv.Atoi(parts[0])
	if err != nil {
		c.logger.Debug("failed to parse PID", "pid", parts[0], "error", err)
		return nil
	}
	
	cpu, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		c.logger.Debug("failed to parse CPU", "cpu", parts[1], "error", err)
		cpu = 0 // CPU might not be available for some processes
	}
	
	memory, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		c.logger.Debug("failed to parse memory", "memory", parts[2], "error", err)
		memory = 0
	}
	
	return &ProcessStats{
		PID:        pid,
		CPUPercent: cpu,
		MemoryMB:   memory,
	}
}

// getProcessStatsWithExe is a helper to retry with .exe extension
func (c *Collector) getProcessStatsWithExe(processName string) *ProcessStats {
	psScript := fmt.Sprintf(`
		$processes = Get-Process -Name '%s' -ErrorAction SilentlyContinue | Select-Object -First 1
		if ($processes) {
			$cpu = 0
			try {
				$cpu = [math]::Round($processes.CPU, 2)
			} catch {}
			Write-Output "$($processes.Id)|$cpu|$([math]::Round($processes.WorkingSet64 / 1MB, 2))"
		}
	`, strings.TrimSuffix(processName, ".exe"))
	
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	
	result := strings.TrimSpace(string(output))
	if result == "" {
		return nil
	}
	
	parts := strings.Split(result, "|")
	if len(parts) != 3 {
		return nil
	}
	
	pid, _ := strconv.Atoi(parts[0])
	cpu, _ := strconv.ParseFloat(parts[1], 64)
	memory, _ := strconv.ParseFloat(parts[2], 64)
	
	return &ProcessStats{
		PID:        pid,
		CPUPercent: cpu,
		MemoryMB:   memory,
	}
}

// readProcStats is not used on Windows (handled in getProcessStats)
func (c *Collector) readProcStats(pid int, stats *ProcessStats) error {
	// All stats are collected in getProcessStats on Windows
	return nil
}