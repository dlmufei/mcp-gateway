// Package config provides configuration management for mcp-gateway.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config represents the main configuration
type Config struct {
	Upstream   UpstreamConfig          `json:"upstream"`
	MCPServers map[string]ServerConfig `json:"mcpServers"`
	Logging    LoggingConfig           `json:"logging"`
	Metrics    MetricsConfig           `json:"metrics"`
}

// UpstreamConfig configures the upstream WebSocket connection
type UpstreamConfig struct {
	Endpoint  string          `json:"endpoint"`
	Reconnect ReconnectConfig `json:"reconnect"`
	Keepalive KeepaliveConfig `json:"keepalive"`
}

// ReconnectConfig configures reconnection behavior
type ReconnectConfig struct {
	Enabled        bool     `json:"enabled"`
	InitialBackoff Duration `json:"initialBackoff"`
	MaxBackoff     Duration `json:"maxBackoff"`
	Multiplier     float64  `json:"multiplier"`
}

// KeepaliveConfig configures keepalive behavior
type KeepaliveConfig struct {
	Interval Duration `json:"interval"`
	Timeout  Duration `json:"timeout"`
}

// ServerConfig represents a downstream MCP server configuration
type ServerConfig struct {
	Name        string            `json:"name,omitempty"`
	Type        string            `json:"type"`        // stdio, http, sse, streamablehttp
	Command     string            `json:"command,omitempty"`     // for stdio
	Args        []string          `json:"args,omitempty"`        // for stdio
	URL         string            `json:"url,omitempty"`         // for http/sse
	Headers     map[string]string `json:"headers,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Timeout     Duration          `json:"timeout,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
	Description string            `json:"description,omitempty"`
}

// LoggingConfig configures logging
type LoggingConfig struct {
	Level  string `json:"level"`  // debug, info, warn, error
	Format string `json:"format"` // json, text
}

// MetricsConfig configures metrics
type MetricsConfig struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

// Duration is a wrapper for time.Duration that supports JSON unmarshaling
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value) * time.Second)
	case string:
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(dur)
	default:
		return fmt.Errorf("invalid duration type: %T", v)
	}
	return nil
}

// MarshalJSON implements json.Marshaler
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Duration returns the time.Duration value
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		Upstream: UpstreamConfig{
			Reconnect: ReconnectConfig{
				Enabled:        true,
				InitialBackoff: Duration(time.Second),
				MaxBackoff:     Duration(10 * time.Minute),
				Multiplier:     2,
			},
			Keepalive: KeepaliveConfig{
				Interval: Duration(30 * time.Second),
				Timeout:  Duration(10 * time.Second),
			},
		},
		MCPServers: make(map[string]ServerConfig),
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Metrics: MetricsConfig{
			Enabled: false,
			Port:    9090,
		},
	}
}

// Load loads configuration from file and environment
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	// Try to find config file
	if configPath == "" {
		configPath = os.Getenv("MCP_CONFIG")
	}
	if configPath == "" {
		// Try default locations
		candidates := []string{
			"mcp_config.json",
			"configs/mcp_config.json",
			filepath.Join(os.Getenv("HOME"), ".mcp-gateway", "config.json"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				configPath = c
				break
			}
		}
	}

	// Load from file if exists
	if configPath != "" {
		if err := cfg.loadFromFile(configPath); err != nil {
			return nil, fmt.Errorf("load config file: %w", err)
		}
	}

	// Override with environment variables
	cfg.loadFromEnv()

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

// loadFromFile loads configuration from a JSON file
func (c *Config) loadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, c)
}

// loadFromEnv loads configuration from environment variables
func (c *Config) loadFromEnv() {
	if endpoint := os.Getenv("MCP_ENDPOINT"); endpoint != "" {
		c.Upstream.Endpoint = endpoint
	}
	if level := os.Getenv("MCP_LOG_LEVEL"); level != "" {
		c.Logging.Level = level
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Upstream.Endpoint == "" {
		return fmt.Errorf("upstream endpoint is required (set MCP_ENDPOINT)")
	}
	
	enabledCount := 0
	for name, server := range c.MCPServers {
		if server.Disabled {
			continue
		}
		enabledCount++
		
		switch server.Type {
		case "stdio":
			if server.Command == "" {
				return fmt.Errorf("server %q: command is required for stdio type", name)
			}
		case "http", "sse", "streamablehttp":
			if server.URL == "" {
				return fmt.Errorf("server %q: url is required for %s type", name, server.Type)
			}
		case "":
			return fmt.Errorf("server %q: type is required", name)
		default:
			return fmt.Errorf("server %q: unsupported type %q", name, server.Type)
		}
	}
	
	if enabledCount == 0 {
		return fmt.Errorf("at least one enabled MCP server is required")
	}
	
	return nil
}

// EnabledServers returns a list of enabled server names
func (c *Config) EnabledServers() []string {
	var servers []string
	for name, server := range c.MCPServers {
		if !server.Disabled {
			servers = append(servers, name)
		}
	}
	return servers
}
