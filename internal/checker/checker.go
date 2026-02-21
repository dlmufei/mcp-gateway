// Package checker provides MCP server configuration checking functionality.
package checker

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cliffyan/mcp-gateway/internal/adapter"
	"github.com/cliffyan/mcp-gateway/internal/config"
)

// MCPStatus represents the check result of a single MCP server
type MCPStatus struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	URL        string     `json:"url,omitempty"`
	Command    string     `json:"command,omitempty"`
	Args       []string   `json:"args,omitempty"`
	Disabled   bool       `json:"disabled,omitempty"`
	Healthy    bool       `json:"healthy"`
	Error      string     `json:"error,omitempty"`
	ServerName string     `json:"serverName,omitempty"`
	ServerVer  string     `json:"serverVersion,omitempty"`
	Tools      []ToolInfo `json:"tools,omitempty"`
}

// ToolInfo contains tool information
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// UpstreamStatus represents the check result of a single upstream
type UpstreamStatus struct {
	Name       string      `json:"name"`
	Endpoint   string      `json:"endpoint"`
	MCPServers []MCPStatus `json:"mcpServers"`
}

// CheckResult contains the complete check result
type CheckResult struct {
	Upstreams    []UpstreamStatus `json:"upstreams"`
	TotalServers int              `json:"totalServers"`
	HealthyCount int              `json:"healthyCount"`
	FailedCount  int              `json:"failedCount"`
	DisabledCount int             `json:"disabledCount"`
	TotalTools   int              `json:"totalTools"`
}

// Checker performs MCP server configuration checks
type Checker struct {
	config  *config.Config
	timeout time.Duration
}

// NewChecker creates a new Checker
func NewChecker(cfg *config.Config, timeout time.Duration) *Checker {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Checker{
		config:  cfg,
		timeout: timeout,
	}
}

// Run executes the check and returns the result
func (c *Checker) Run(ctx context.Context) (*CheckResult, error) {
	result := &CheckResult{}

	factory := adapter.NewFactory()

	for _, ucfg := range c.config.Upstreams {
		us := UpstreamStatus{
			Name:     ucfg.Name,
			Endpoint: maskToken(ucfg.Endpoint),
		}

		for name, serverCfg := range ucfg.MCPServers {
			status := c.checkMCPServer(ctx, factory, name, serverCfg)
			us.MCPServers = append(us.MCPServers, status)

			result.TotalServers++
			if status.Disabled {
				result.DisabledCount++
			} else if status.Healthy {
				result.HealthyCount++
				result.TotalTools += len(status.Tools)
			} else {
				result.FailedCount++
			}
		}

		result.Upstreams = append(result.Upstreams, us)
	}

	return result, nil
}

// checkMCPServer checks a single MCP server
func (c *Checker) checkMCPServer(ctx context.Context, factory *adapter.Factory, name string, cfg config.ServerConfig) MCPStatus {
	status := MCPStatus{
		Name:     name,
		Type:     cfg.Type,
		URL:      cfg.URL,
		Command:  cfg.Command,
		Args:     cfg.Args,
		Disabled: cfg.Disabled,
	}

	if cfg.Disabled {
		status.Error = "disabled in config"
		return status
	}

	// Create timeout context for this check
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Create adapter
	a, err := factory.Create(name, cfg)
	if err != nil {
		status.Error = "create adapter failed: " + err.Error()
		return status
	}

	// Start adapter
	if err := a.Start(checkCtx); err != nil {
		status.Error = "start failed: " + err.Error()
		return status
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		a.Stop(stopCtx)
	}()

	// Initialize (MCP handshake)
	initResult, err := a.Initialize(checkCtx)
	if err != nil {
		status.Error = "initialize failed: " + err.Error()
		return status
	}

	status.ServerName = initResult.ServerInfo.Name
	status.ServerVer = initResult.ServerInfo.Version

	// Get tools list
	tools, err := a.ListTools(checkCtx)
	if err != nil {
		status.Error = "list tools failed: " + err.Error()
		return status
	}

	status.Healthy = true
	for _, t := range tools {
		status.Tools = append(status.Tools, ToolInfo{
			Name:        t.Name,
			Description: t.Description,
		})
	}

	return status
}

// maskToken masks the token in URL for security
func maskToken(endpoint string) string {
	// Parse URL to properly mask token
	u, err := url.Parse(endpoint)
	if err != nil {
		// Fallback: simple string replacement
		if idx := strings.Index(endpoint, "token="); idx > 0 {
			endIdx := strings.Index(endpoint[idx:], "&")
			if endIdx > 0 {
				return endpoint[:idx+6] + "***" + endpoint[idx+endIdx:]
			}
			return endpoint[:idx+6] + "***"
		}
		return endpoint
	}

	// Mask token in query parameters
	q := u.Query()
	if q.Get("token") != "" {
		q.Set("token", "***")
		u.RawQuery = q.Encode()
	}

	return u.String()
}

// RunCheck is a convenience function to run check with config path
func RunCheck(configPath string, timeout time.Duration, outputFormat string) error {
	// Suppress default logger output during check
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(originalLogger)

	// Also suppress standard log
	originalLogOutput := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	if devNull != nil {
		os.Stderr = devNull
		defer func() {
			os.Stderr = originalLogOutput
			devNull.Close()
		}()
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		// Restore stderr for error output
		os.Stderr = originalLogOutput
		return err
	}

	// Create checker
	checker := NewChecker(cfg, timeout)

	// Run check
	ctx := context.Background()
	result, err := checker.Run(ctx)
	if err != nil {
		os.Stderr = originalLogOutput
		return err
	}

	// Restore stderr for output
	os.Stderr = originalLogOutput

	// Output result
	return Output(result, outputFormat)
}
