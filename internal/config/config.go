// Package config provides simplified configuration for the Netwarden agent.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config represents the agent configuration.
type Config struct {
	// Core settings (required)
	TenantID string
	APIKey   string

	// Optional hostname override
	Hostname string

	// Logging
	LogLevel string
	LogFile  string

	// Buffer configuration
	Buffer BufferConfig

	// Collector flags
	Collectors CollectorToggles

	// Network configuration
	Network NetworkConfig

	// Container configuration
	Container ContainerConfig

	// VM configuration
	VM VMConfig

	// Database configuration
	Database DatabaseConfig

	// Process monitoring configuration
	Process ProcessConfig
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	// Check required fields
	if c.TenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}

	// Validate tenant ID format (should be 10 characters)
	if len(c.TenantID) != 10 {
		return fmt.Errorf("tenant_id must be exactly 10 characters, got %d", len(c.TenantID))
	}

	if c.APIKey == "" {
		return fmt.Errorf("api_key is required")
	}

	// Validate API key format (should start with nw_sk_)
	if !strings.HasPrefix(c.APIKey, "nw_sk_") {
		return fmt.Errorf("api_key must start with 'nw_sk_'")
	}


	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if c.LogLevel != "" && !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid log_level: %s (must be debug, info, warn, or error)", c.LogLevel)
	}

	// Validate buffer configuration
	if c.Buffer.MaxSize < 0 {
		return fmt.Errorf("buffer max_size cannot be negative")
	}

	// Validate database configuration if enabled
	if c.Database.EnableDatabaseMonitoring {
		if len(c.Database.DatabaseConnections) == 0 {
			return fmt.Errorf("database monitoring enabled but no connections configured")
		}

		for i, conn := range c.Database.DatabaseConnections {
			if conn.DSN == "" {
				return fmt.Errorf("database connection %d: DSN is required", i)
			}
			if conn.Type != "postgresql" && conn.Type != "mysql" {
				return fmt.Errorf("database connection %d: type must be 'postgresql' or 'mysql', got '%s'", i, conn.Type)
			}
		}
	}

	// Validate VM configuration if enabled
	if c.VM.EnableVMs {
		if c.VM.VMHypervisor == "" {
			c.VM.VMHypervisor = "auto" // Default to auto-detection
		}

		// Validate Proxmox settings if using Proxmox
		if strings.Contains(c.VM.VMHypervisor, "proxmox") || c.VM.ProxmoxAPI != "" {
			if c.VM.ProxmoxAPI == "" {
				return fmt.Errorf("VM monitoring with Proxmox requires proxmox_api URL")
			}
			if c.VM.ProxmoxUsername == "" && c.VM.ProxmoxTokenID == "" {
				return fmt.Errorf("VM monitoring with Proxmox requires either username/password or token authentication")
			}
		}

		// Validate parallel stats setting
		if c.VM.VMParallelStats < 0 {
			return fmt.Errorf("VM parallel_stats cannot be negative")
		}
		if c.VM.VMParallelStats > 100 {
			return fmt.Errorf("VM parallel_stats cannot exceed 100")
		}
	}

	// Validate container configuration if enabled
	if c.Container.EnableContainers {
		if c.Container.ContainerRuntime == "" {
			c.Container.ContainerRuntime = "auto" // Default to auto-detection
		}

		validRuntimes := map[string]bool{
			"auto":       true,
			"docker":     true,
			"podman":     true,
			"containerd": true,
		}
		if !validRuntimes[c.Container.ContainerRuntime] {
			return fmt.Errorf("invalid container_runtime: %s", c.Container.ContainerRuntime)
		}

		if c.Container.StatsInterval != 0 && c.Container.StatsInterval < 5*time.Second {
			return fmt.Errorf("container stats_interval must be at least 5 seconds")
		}
	}

	// Validate process configuration if enabled
	if c.Process.EnableProcessMonitoring {
		if c.Process.ConfigEndpoint != "" && !strings.HasPrefix(c.Process.ConfigEndpoint, "/") {
			return fmt.Errorf("process config_endpoint must start with /")
		}
	}

	return nil
}

// BufferConfig for metric buffering
type BufferConfig struct {
	MaxSize int
}

// CollectorToggles for enabling/disabling collectors
type CollectorToggles struct {
	CPU       bool
	Memory    bool
	System    bool
	Disk      bool
	Network   bool
	Container bool
	VM        bool
}

// NetworkConfig provides configuration for network monitoring
type NetworkConfig struct {
	EnableNetwork        bool
	MonitoredInterfaces  []string  // Interface names to monitor (empty = auto-detect)
	ExcludeInterfaces    []string  // Additional interface patterns to exclude
}

// ContainerConfig provides configuration for container monitoring
type ContainerConfig struct {
	EnableContainers       bool
	ContainerRuntime       string        // auto, docker, podman, containerd
	ContainerSocket        string        // custom socket path
	ContainerInclude       []string      // include patterns
	ContainerExclude       []string      // exclude patterns
	StatsInterval          time.Duration // interval for collecting detailed stats
}

// VMConfig provides configuration for VM monitoring
type VMConfig struct {
	EnableVMs             bool
	VMHypervisor          string        // auto, proxmox, libvirt, kvm, xen, qemu
	VMInclude             []string      // VM name patterns to include
	VMExclude             []string      // VM name patterns to exclude  
	StatsInterval         time.Duration // How often to collect detailed stats
	
	// Libvirt configuration
	LibvirtURI            string        // libvirt connection URI
	LibvirtSocket         string        // custom socket path
	
	// Proxmox configuration  
	ProxmoxAPI            string        // Proxmox API URL (https://host:8006)
	ProxmoxUsername       string        // username@realm
	ProxmoxPassword       string        // password (for ticket auth)
	ProxmoxTokenID        string        // token ID (for API token auth)
	ProxmoxTokenSecret    string        // token secret
	ProxmoxNode           string        // specific node name (empty = all nodes)
	ProxmoxSkipTLSVerify  bool         // skip TLS verification
	
	// Advanced settings
	VMTimeout             time.Duration // timeout for hypervisor operations
	VMCacheTimeout        time.Duration // how long to cache VM list
	VMParallelStats       int          // how many VMs to query stats in parallel
}

// SystemConfig for system collector
type SystemConfig struct {
	Enabled       bool
	CollectUptime bool
}

// DatabaseConnection represents a single database connection
type DatabaseConnection struct {
	Type string // "mysql" or "postgresql"
	DSN  string // Data Source Name
}

// DatabaseConfig for MySQL and PostgreSQL monitoring
type DatabaseConfig struct {
	// General database monitoring
	EnableDatabaseMonitoring bool
	DatabaseConnections      []DatabaseConnection

	// MySQL configuration
	EnableMySQL     bool
	MySQLSocket     string  // Auto-detected if empty
	MySQLHost       string  // Default: localhost:3306
	MySQLUser       string  // Optional service account
	MySQLPassword   string  // Optional service account

	// PostgreSQL configuration
	EnablePostgreSQL   bool
	PostgreSQLSocket   string  // Auto-detected if empty
	PostgreSQLHost     string  // Default: localhost:5432
	PostgreSQLUser     string  // Optional service account
	PostgreSQLPassword string // Optional service account
	PostgreSQLDatabase string // Default: postgres
}

// ProcessConfig for process monitoring
type ProcessConfig struct {
	EnableProcessMonitoring bool
	ConfigFetchInterval     time.Duration // Default: 5 minutes
	ConfigEndpoint         string        // Default: /agent-config/processes
}

// LoadConfig loads configuration from a simple key-value file.
func LoadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	// Create config with basic structure
	config := &Config{}

	// Parse key-value pairs
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split key-value
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "tenant_id", "customer_id":
			config.TenantID = value
		case "api_key":
			config.APIKey = value
		case "hostname":
			config.Hostname = value
		case "log_level":
			config.LogLevel = value
		case "logfile", "log_file":
			config.LogFile = value
		case "enable_cpu":
			config.Collectors.CPU = value == "true" || value == "yes" || value == "1"
		case "enable_memory":
			config.Collectors.Memory = value == "true" || value == "yes" || value == "1"
		case "enable_system":
			config.Collectors.System = value == "true" || value == "yes" || value == "1"
		case "enable_disk":
			config.Collectors.Disk = value == "true" || value == "yes" || value == "1"
		case "enable_network":
			config.Collectors.Network = value == "true" || value == "yes" || value == "1"
			config.Network.EnableNetwork = config.Collectors.Network
		case "network_interfaces":
			// Parse comma-separated interface names
			if value != "" {
				config.Network.MonitoredInterfaces = strings.Split(value, ",")
				for i := range config.Network.MonitoredInterfaces {
					config.Network.MonitoredInterfaces[i] = strings.TrimSpace(config.Network.MonitoredInterfaces[i])
				}
			}
		case "network_exclude":
			// Parse comma-separated exclusion patterns
			if value != "" {
				config.Network.ExcludeInterfaces = strings.Split(value, ",")
				for i := range config.Network.ExcludeInterfaces {
					config.Network.ExcludeInterfaces[i] = strings.TrimSpace(config.Network.ExcludeInterfaces[i])
				}
			}
		case "enable_containers", "enable_container", "enable_docker":
			config.Collectors.Container = value == "true" || value == "yes" || value == "1"
			config.Container.EnableContainers = config.Collectors.Container
		case "container_runtime":
			config.Container.ContainerRuntime = value
		case "container_socket":
			config.Container.ContainerSocket = value
		case "container_stats_interval":
			// Parse as integer seconds
			if seconds, err := strconv.Atoi(value); err == nil {
				config.Container.StatsInterval = time.Duration(seconds) * time.Second
			}
		case "enable_vms", "enable_vm":
			config.Collectors.VM = value == "true" || value == "yes" || value == "1"
			config.VM.EnableVMs = config.Collectors.VM
		case "vm_hypervisor":
			config.VM.VMHypervisor = value
		case "vm_stats_interval":
			// Parse as integer seconds
			if seconds, err := strconv.Atoi(value); err == nil {
				config.VM.StatsInterval = time.Duration(seconds) * time.Second
			}
		case "libvirt_uri":
			config.VM.LibvirtURI = value
		case "libvirt_socket":
			config.VM.LibvirtSocket = value
		case "proxmox_api":
			config.VM.ProxmoxAPI = value
		case "proxmox_username":
			config.VM.ProxmoxUsername = value
		case "proxmox_password":
			config.VM.ProxmoxPassword = value
		case "proxmox_token_id":
			config.VM.ProxmoxTokenID = value
		case "proxmox_token_secret":
			config.VM.ProxmoxTokenSecret = value
		case "proxmox_node":
			config.VM.ProxmoxNode = value
		case "proxmox_skip_tls_verify":
			config.VM.ProxmoxSkipTLSVerify = value == "true" || value == "yes" || value == "1"
		case "enable_mysql":
			config.Database.EnableMySQL = value == "true" || value == "yes" || value == "1"
		case "mysql_socket":
			config.Database.MySQLSocket = value
		case "mysql_host":
			config.Database.MySQLHost = value
		case "mysql_user":
			config.Database.MySQLUser = value
		case "mysql_password":
			config.Database.MySQLPassword = value
		case "enable_postgresql":
			config.Database.EnablePostgreSQL = value == "true" || value == "yes" || value == "1"
		case "postgresql_socket":
			config.Database.PostgreSQLSocket = value
		case "postgresql_host":
			config.Database.PostgreSQLHost = value
		case "postgresql_user":
			config.Database.PostgreSQLUser = value
		case "postgresql_password":
			config.Database.PostgreSQLPassword = value
		case "postgresql_database":
			config.Database.PostgreSQLDatabase = value
		case "enable_process_monitoring":
			config.Process.EnableProcessMonitoring = value == "true" || value == "yes" || value == "1"
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	// Validate required fields
	if config.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if config.APIKey == "" {
		return nil, fmt.Errorf("api_key is required")
	}

	// Set default values for unset fields
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	if config.Buffer.MaxSize == 0 {
		config.Buffer.MaxSize = 100
	}

	// Enable collectors by default (except MySQL and PostgreSQL which require explicit opt-in)
	// These defaults only apply if no explicit configuration was provided
	config.Collectors.CPU = true
	config.Collectors.Memory = true
	config.Collectors.System = true
	config.Collectors.Disk = true
	config.Collectors.Network = true
	config.Collectors.Container = true
	config.Collectors.VM = true

	// Network configuration defaults
	config.Network.EnableNetwork = config.Collectors.Network

	// Container configuration defaults
	config.Container.EnableContainers = config.Collectors.Container
	if config.Container.ContainerRuntime == "" {
		config.Container.ContainerRuntime = "auto"
	}
	if config.Container.StatsInterval == 0 {
		config.Container.StatsInterval = 120 * time.Second
	}

	// VM configuration defaults
	config.VM.EnableVMs = config.Collectors.VM
	if config.VM.VMHypervisor == "" {
		config.VM.VMHypervisor = "auto"
	}
	if config.VM.StatsInterval == 0 {
		config.VM.StatsInterval = 120 * time.Second
	}
	if config.VM.VMTimeout == 0 {
		config.VM.VMTimeout = 30 * time.Second
	}
	if config.VM.VMCacheTimeout == 0 {
		config.VM.VMCacheTimeout = 300 * time.Second // 5 minutes
	}
	if config.VM.VMParallelStats == 0 {
		config.VM.VMParallelStats = 5
	}

	// Updates collector removed (security risk)

	// Process monitoring defaults (disabled by default for security)
	// Users must explicitly enable with enable_process_monitoring: true
	// config.Process.EnableProcessMonitoring is false by default (zero value)
	if config.Process.ConfigFetchInterval == 0 {
		config.Process.ConfigFetchInterval = 300 * time.Second // 5 minutes
	}
	if config.Process.ConfigEndpoint == "" {
		config.Process.ConfigEndpoint = "/processes"
	}

	// Validate the configuration before returning
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// CreateConfigFile creates a minimal example configuration file.
func CreateConfigFile(filename string) error {
	content := `# Netwarden Agent Configuration
# Simple key-value configuration file

# Required: Your tenant/customer ID
tenant_id: CHANGE_ME

# Required: Your API key
api_key: nw_sk_CHANGE_ME

# Optional: Custom hostname (overrides system hostname)
# hostname: my-server

# Optional: Log level (debug, info, warn, error) (default: info)
log_level: info

# Optional: Log file path (default: .\netwarden.log on Windows, /var/log/netwarden.log on Linux)
# logfile: /path/to/custom/logfile.log

# Optional: Enable/disable collectors (all enabled by default except mysql/postgresql)
# enable_cpu: true
# enable_memory: true
# enable_system: true
# enable_disk: true
# enable_network: true
# enable_containers: true
# enable_vms: true
# enable_process_monitoring: true

# Network monitoring options
# network_interfaces: eth0,eth1  # Specific interfaces to monitor (empty = auto-detect)
# network_exclude: wg*,tun*      # Additional patterns to exclude

# Container monitoring options
# container_runtime: auto
# container_socket: /var/run/docker.sock
# container_stats_interval: 120

# VM monitoring options
# vm_hypervisor: auto
# vm_stats_interval: 120
#
# Libvirt configuration (for KVM, Xen, QEMU)
# libvirt_uri: qemu:///system
# libvirt_socket: /var/run/libvirt/libvirt-sock
#
# Proxmox configuration
# proxmox_api: https://proxmox.example.com:8006
# proxmox_username: monitoring@pve
# proxmox_password: your_password
# proxmox_token_id: monitoring@pve!token
# proxmox_token_secret: your_token_secret
# proxmox_node: node-name
# proxmox_skip_tls_verify: false
#
# Database monitoring (disabled by default - explicitly enable if needed)
# enable_mysql: false
# mysql_socket: auto  # or specify path
# mysql_host: localhost:3306
# mysql_user: monitoring
# mysql_password: secret
#
# enable_postgresql: false
# postgresql_socket: auto  # or specify path
# postgresql_host: localhost:5432
# postgresql_user: monitoring
# postgresql_password: secret
# postgresql_database: postgres
#
# Process monitoring (enabled by default)
# enable_process_monitoring: true
`

	return os.WriteFile(filename, []byte(content), 0600)
}
