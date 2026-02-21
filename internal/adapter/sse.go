package adapter

import (
	"bufio"
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

// SSEAdapter manages connections to SSE-based MCP servers
type SSEAdapter struct {
	*BaseAdapter
	cfg    config.ServerConfig
	logger *slog.Logger
	client *http.Client

	mu      sync.RWMutex
	running bool

	// SSE connection
	sseResp   *http.Response
	sseCancel context.CancelFunc

	// Message handling
	requestID int64
	pending   map[any]chan *protocol.Message
	pendingMu sync.Mutex
	msgCh     chan *protocol.Message

	// Endpoint for sending messages (discovered from SSE)
	postEndpoint string
}

// NewSSEAdapter creates a new SSE adapter
func NewSSEAdapter(name string, cfg config.ServerConfig) *SSEAdapter {
	timeout := cfg.Timeout.Duration()
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	return &SSEAdapter{
		BaseAdapter: NewBaseAdapter(name, "sse", timeout),
		cfg:         cfg,
		logger:      slog.Default().With("adapter", name, "type", "sse"),
		client: &http.Client{
			Timeout: 0, // No timeout for SSE connections
		},
		pending: make(map[any]chan *protocol.Message),
		msgCh:   make(chan *protocol.Message, 100),
	}
}

// Start connects to the SSE endpoint
func (a *SSEAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}

	a.logger.Info("connecting to SSE endpoint", "url", a.cfg.URL)

	// Create SSE connection
	sseCtx, cancel := context.WithCancel(ctx)
	a.sseCancel = cancel

	req, err := http.NewRequestWithContext(sseCtx, "GET", a.cfg.URL, nil)
	if err != nil {
		cancel()
		return fmt.Errorf("create SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for k, v := range a.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		cancel()
		return fmt.Errorf("connect to SSE: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return fmt.Errorf("SSE HTTP %d: %s", resp.StatusCode, string(body))
	}

	a.sseResp = resp
	a.running = true

	// Start reading SSE events
	go a.readLoop(sseCtx)

	a.logger.Info("connected to SSE endpoint")
	return nil
}

// Stop disconnects from the SSE endpoint
func (a *SSEAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	a.logger.Info("disconnecting from SSE endpoint")

	if a.sseCancel != nil {
		a.sseCancel()
	}

	if a.sseResp != nil {
		a.sseResp.Body.Close()
	}

	a.running = false
	return nil
}

// readLoop reads events from the SSE connection
func (a *SSEAdapter) readLoop(ctx context.Context) {
	scanner := bufio.NewScanner(a.sseResp.Body)
	var eventType string
	var dataLines []string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == "" && len(dataLines) > 0 {
			// End of event
			data := strings.Join(dataLines, "")
			a.handleSSEEvent(eventType, data)
			eventType = ""
			dataLines = nil
		}
	}

	if err := scanner.Err(); err != nil {
		a.logger.Error("SSE read error", "error", err)
	}

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
}

// handleSSEEvent processes an SSE event
func (a *SSEAdapter) handleSSEEvent(eventType, data string) {
	a.logger.Debug("received SSE event", "type", eventType, "data", data)

	switch eventType {
	case "endpoint":
		// Server is telling us where to POST messages
		a.mu.Lock()
		a.postEndpoint = data
		a.mu.Unlock()
		a.logger.Info("got POST endpoint", "endpoint", data)

	case "message":
		// Parse JSON-RPC message
		var msg protocol.Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			a.logger.Error("parse SSE message failed", "error", err)
			return
		}

		// Check for pending request
		if msg.IsResponse() && msg.ID != nil {
			a.pendingMu.Lock()
			ch, ok := a.pending[msg.ID]
			if ok {
				delete(a.pending, msg.ID)
			}
			a.pendingMu.Unlock()

			if ok {
				select {
				case ch <- &msg:
				default:
				}
				return
			}
		}

		// Send to general channel
		select {
		case a.msgCh <- &msg:
		default:
			a.logger.Warn("message channel full")
		}
	}
}

// Send sends a message to the MCP server
func (a *SSEAdapter) Send(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	a.mu.RLock()
	if !a.running {
		a.mu.RUnlock()
		return nil, fmt.Errorf("adapter not running")
	}
	endpoint := a.postEndpoint
	a.mu.RUnlock()

	if endpoint == "" {
		// Use the original URL if no endpoint was provided
		endpoint = a.cfg.URL
	}

	// Ensure message has an ID
	if msg.ID == nil && msg.Method != "" {
		msg.ID = atomic.AddInt64(&a.requestID, 1)
	}

	// Create response channel if this is a request
	var respCh chan *protocol.Message
	if msg.IsRequest() {
		respCh = make(chan *protocol.Message, 1)
		a.pendingMu.Lock()
		a.pending[msg.ID] = respCh
		a.pendingMu.Unlock()

		defer func() {
			a.pendingMu.Lock()
			delete(a.pending, msg.ID)
			a.pendingMu.Unlock()
		}()
	}

	// Marshal and send
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	a.logger.Debug("sending message", "endpoint", endpoint, "data", string(data))

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// If not a request, we're done
	if !msg.IsRequest() {
		return nil, nil
	}

	// Wait for response via SSE
	timeout := a.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	select {
	case result := <-respCh:
		return result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// IsHealthy returns true if connected
func (a *SSEAdapter) IsHealthy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Initialize performs MCP initialization
func (a *SSEAdapter) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
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
func (a *SSEAdapter) ListTools(ctx context.Context) ([]protocol.Tool, error) {
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
