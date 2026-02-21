package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cliffyan/mcp-gateway/internal/config"
	"github.com/cliffyan/mcp-gateway/internal/protocol"
)

// StdioAdapter manages a local MCP server process via stdio
type StdioAdapter struct {
	*BaseAdapter
	cfg    config.ServerConfig
	logger *slog.Logger

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	mu      sync.RWMutex
	running bool

	// Request tracking
	requestID int64
	pending   map[any]chan *protocol.Message
	pendingMu sync.Mutex

	// Read loop
	readCh chan *protocol.Message
}

// NewStdioAdapter creates a new stdio adapter
func NewStdioAdapter(name string, cfg config.ServerConfig) *StdioAdapter {
	return &StdioAdapter{
		BaseAdapter: NewBaseAdapter(name, "stdio", cfg.Timeout.Duration()),
		cfg:         cfg,
		logger:      slog.Default().With("adapter", name, "type", "stdio"),
		pending:     make(map[any]chan *protocol.Message),
		readCh:      make(chan *protocol.Message, 100),
	}
}

// Start starts the subprocess
func (a *StdioAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}

	a.logger.Info("starting process", "command", a.cfg.Command, "args", a.cfg.Args)

	// Build command
	a.cmd = exec.CommandContext(ctx, a.cfg.Command, a.cfg.Args...)

	// Set environment
	a.cmd.Env = os.Environ()
	for k, v := range a.cfg.Env {
		a.cmd.Env = append(a.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Get pipes
	var err error
	a.stdin, err = a.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	a.stdout, err = a.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	a.stderr, err = a.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	// Start process
	if err := a.cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	a.running = true

	// Start read loops
	go a.readLoop()
	go a.stderrLoop()

	a.logger.Info("process started", "pid", a.cmd.Process.Pid)
	return nil
}

// Stop stops the subprocess
func (a *StdioAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	a.logger.Info("stopping process")

	// Close stdin to signal EOF
	if a.stdin != nil {
		a.stdin.Close()
	}

	// Wait for process with timeout
	done := make(chan error, 1)
	go func() {
		done <- a.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited gracefully
	case <-time.After(5 * time.Second):
		// Force kill
		a.logger.Warn("force killing process")
		a.cmd.Process.Kill()
	case <-ctx.Done():
		a.cmd.Process.Kill()
		return ctx.Err()
	}

	a.running = false
	a.logger.Info("process stopped")
	return nil
}

// readLoop reads messages from stdout
func (a *StdioAdapter) readLoop() {
	scanner := bufio.NewScanner(a.stdout)
	// Increase buffer size for large messages
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		a.logger.Debug("received from process", "data", line)

		msg, err := protocol.ParseMessage([]byte(line))
		if err != nil {
			a.logger.Error("parse message failed", "error", err, "line", line)
			continue
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
				case ch <- msg:
				default:
				}
				continue
			}
		}

		// Send to general read channel
		select {
		case a.readCh <- msg:
		default:
			a.logger.Warn("read channel full, dropping message")
		}
	}

	if err := scanner.Err(); err != nil {
		a.logger.Error("scanner error", "error", err)
	}
}

// stderrLoop reads and logs stderr
func (a *StdioAdapter) stderrLoop() {
	scanner := bufio.NewScanner(a.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		a.logger.Debug("stderr", "line", line)
	}
}

// Send sends a message to the process and waits for response
func (a *StdioAdapter) Send(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	a.mu.RLock()
	if !a.running {
		a.mu.RUnlock()
		return nil, fmt.Errorf("adapter not running")
	}
	a.mu.RUnlock()

	// Ensure message has an ID for request/response matching
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

	a.logger.Debug("sending to process", "data", string(data))

	a.mu.RLock()
	stdin := a.stdin
	a.mu.RUnlock()

	if _, err := stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write to stdin: %w", err)
	}

	// If not a request, we're done
	if !msg.IsRequest() {
		return nil, nil
	}

	// Wait for response
	timeout := a.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// IsHealthy returns true if the process is running
func (a *StdioAdapter) IsHealthy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running && a.cmd != nil && a.cmd.ProcessState == nil
}

// Initialize performs MCP initialization
func (a *StdioAdapter) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
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
	notif.ID = nil // Make it a notification
	a.Send(ctx, notif)

	a.SetInitialized(true)
	return &result, nil
}

// ListTools fetches the list of tools from the server
func (a *StdioAdapter) ListTools(ctx context.Context) ([]protocol.Tool, error) {
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
