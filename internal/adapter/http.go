package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cliffyan/mcp-gateway/internal/config"
	"github.com/cliffyan/mcp-gateway/internal/protocol"
)

// HTTPAdapter manages connections to HTTP/StreamableHTTP MCP servers
type HTTPAdapter struct {
	*BaseAdapter
	cfg    config.ServerConfig
	logger *slog.Logger
	client *http.Client

	mu        sync.RWMutex
	running   bool
	sessionID string

	requestID int64
}

// NewHTTPAdapter creates a new HTTP adapter
func NewHTTPAdapter(name string, cfg config.ServerConfig) *HTTPAdapter {
	timeout := cfg.Timeout.Duration()
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	return &HTTPAdapter{
		BaseAdapter: NewBaseAdapter(name, cfg.Type, timeout),
		cfg:         cfg,
		logger:      slog.Default().With("adapter", name, "type", cfg.Type),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Start starts the adapter
func (a *HTTPAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}

	a.logger.Info("starting HTTP adapter", "url", a.cfg.URL)
	a.running = true
	return nil
}

// Stop stops the adapter
func (a *HTTPAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	a.logger.Info("stopping HTTP adapter")
	a.running = false
	a.sessionID = ""
	return nil
}

// Send sends a message to the HTTP MCP server
func (a *HTTPAdapter) Send(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	a.mu.RLock()
	if !a.running {
		a.mu.RUnlock()
		return nil, fmt.Errorf("adapter not running")
	}
	sessionID := a.sessionID
	a.mu.RUnlock()

	// Ensure message has an ID
	if msg.ID == nil && msg.Method != "" {
		msg.ID = atomic.AddInt64(&a.requestID, 1)
	}

	// Marshal request
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	a.logger.Debug("sending HTTP request", "data", string(data))

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", a.cfg.URL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	// Add session ID if we have one
	if sessionID != "" {
		req.Header.Set("mcp-session-id", sessionID)
	}

	// Add custom headers
	for k, v := range a.cfg.Headers {
		req.Header.Set(k, v)
	}

	// Send request
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for new session ID
	if newSessionID := resp.Header.Get("mcp-session-id"); newSessionID != "" {
		a.mu.Lock()
		a.sessionID = newSessionID
		a.mu.Unlock()
		a.logger.Debug("got session ID", "sessionID", newSessionID)
	}

	// Check status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Parse response based on content type
	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "text/event-stream") {
		// Parse SSE response
		return a.parseSSEResponse(resp.Body)
	}

	// Parse JSON response
	var result protocol.Message
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// parseSSEResponse parses an SSE response and returns the first message
func (a *HTTPAdapter) parseSSEResponse(r io.Reader) (*protocol.Message, error) {
	scanner := bufio.NewScanner(r)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == "" && len(dataLines) > 0 {
			// End of event, parse the data
			data := strings.Join(dataLines, "")
			dataLines = nil

			var msg protocol.Message
			if err := json.Unmarshal([]byte(data), &msg); err != nil {
				a.logger.Error("parse SSE data failed", "error", err, "data", data)
				continue
			}

			// Return the first valid message
			return &msg, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read SSE stream: %w", err)
	}

	return nil, fmt.Errorf("no valid message in SSE response")
}

// IsHealthy returns true if the adapter is running
func (a *HTTPAdapter) IsHealthy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Initialize performs MCP initialization
func (a *HTTPAdapter) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
	params := &protocol.InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    protocol.ClientCapability{},
		ClientInfo: protocol.Implementation{
			Name:    "mcp-gateway",
			Version: "1.0.0",
		},
	}

	req, err := protocol.NewRequest(atomic.AddInt64(&a.requestID, 1), protocol.MethodInitialize, params)
	if err != nil {
		return nil, err
	}

	resp, err := a.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("initialize request: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result protocol.InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	// Send initialized notification
	notif, _ := protocol.NewRequest(nil, protocol.MethodInitialized, nil)
	notif.ID = nil
	a.Send(ctx, notif)

	a.SetInitialized(true)
	return &result, nil
}

// ListTools fetches the list of tools from the server
func (a *HTTPAdapter) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	req, err := protocol.NewRequest(atomic.AddInt64(&a.requestID, 1), protocol.MethodToolsList, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list tools request: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result protocol.ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	a.SetTools(result.Tools)
	return result.Tools, nil
}
