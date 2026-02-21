// Package upstream manages the upstream WebSocket connection to MCP endpoint.
package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/cliffyan/mcp-gateway/internal/config"
	"github.com/cliffyan/mcp-gateway/internal/protocol"
	"github.com/cliffyan/mcp-gateway/pkg/retry"
)

// MessageHandler is a function that handles incoming messages
type MessageHandler func(ctx context.Context, msg *protocol.Message) (*protocol.Message, error)

// Client manages the upstream WebSocket connection
type Client struct {
	cfg     config.UpstreamConfig
	conn    *websocket.Conn
	handler MessageHandler
	logger  *slog.Logger

	mu       sync.RWMutex
	closed   bool
	closeCh  chan struct{}
	
	// Pending requests waiting for responses
	pending   map[any]chan *protocol.Message
	pendingMu sync.Mutex
}

// NewClient creates a new upstream WebSocket client
func NewClient(cfg config.UpstreamConfig, handler MessageHandler, logger *slog.Logger) *Client {
	return &Client{
		cfg:     cfg,
		handler: handler,
		logger:  logger.With("component", "upstream"),
		closeCh: make(chan struct{}),
		pending: make(map[any]chan *protocol.Message),
	}
}

// Run starts the client with automatic reconnection
func (c *Client) Run(ctx context.Context) error {
	backoff := retry.NewBackoff(
		c.cfg.Reconnect.InitialBackoff.Duration(),
		c.cfg.Reconnect.MaxBackoff.Duration(),
		c.cfg.Reconnect.Multiplier,
	)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closeCh:
			return nil
		default:
		}

		err := c.connect(ctx)
		if err != nil {
			c.logger.Warn("connection failed", "error", err, "attempt", backoff.Attempt())
			
			if !c.cfg.Reconnect.Enabled {
				return err
			}
			
			if waitErr := backoff.Wait(ctx); waitErr != nil {
				return waitErr
			}
			continue
		}

		// Connection successful, reset backoff
		backoff.Reset()

		// Run the connection
		if err := c.runConnection(ctx); err != nil {
			c.logger.Warn("connection closed", "error", err)
		}
	}
}

// connect establishes the WebSocket connection
func (c *Client) connect(ctx context.Context) error {
	c.logger.Info("connecting to upstream", "endpoint", c.cfg.Endpoint)

	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.cfg.Endpoint, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	c.logger.Info("connected to upstream")
	return nil
}

// runConnection handles the connected WebSocket
func (c *Client) runConnection(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	// Reader goroutine
	go func() {
		errCh <- c.readLoop(ctx)
	}()

	// Keepalive goroutine
	if c.cfg.Keepalive.Interval.Duration() > 0 {
		go func() {
			errCh <- c.keepaliveLoop(ctx)
		}()
	}

	// Wait for error or context cancellation
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closeCh:
		return nil
	}
}

// readLoop reads messages from the WebSocket
func (c *Client) readLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			return fmt.Errorf("connection closed")
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		c.logger.Debug("received message", "data", string(data))

		// Parse and handle the message
		msg, err := protocol.ParseMessage(data)
		if err != nil {
			c.logger.Error("parse message failed", "error", err, "data", string(data))
			continue
		}

		// Handle message asynchronously
		go c.handleMessage(ctx, msg)
	}
}

// handleMessage processes an incoming message
func (c *Client) handleMessage(ctx context.Context, msg *protocol.Message) {
	// Check if this is a response to a pending request
	if msg.IsResponse() && msg.ID != nil {
		c.pendingMu.Lock()
		ch, ok := c.pending[msg.ID]
		if ok {
			delete(c.pending, msg.ID)
		}
		c.pendingMu.Unlock()

		if ok {
			select {
			case ch <- msg:
			default:
			}
			return
		}
	}

	// Handle request/notification via handler
	if c.handler == nil {
		c.logger.Warn("no handler for message", "method", msg.Method)
		return
	}

	resp, err := c.handler(ctx, msg)
	if err != nil {
		c.logger.Error("handler error", "error", err, "method", msg.Method)
		// Send error response if this was a request
		if msg.IsRequest() {
			errResp, _ := protocol.NewErrorResponse(msg.ID, protocol.ErrCodeInternalError, err.Error(), nil)
			c.Send(ctx, errResp)
		}
		return
	}

	// Send response if handler returned one
	if resp != nil {
		if err := c.Send(ctx, resp); err != nil {
			c.logger.Error("send response failed", "error", err)
		}
	}
}

// keepaliveLoop sends periodic ping messages
func (c *Client) keepaliveLoop(ctx context.Context) error {
	ticker := time.NewTicker(c.cfg.Keepalive.Interval.Duration())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn == nil {
				return fmt.Errorf("connection closed")
			}

			if err := conn.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(c.cfg.Keepalive.Timeout.Duration()),
			); err != nil {
				return fmt.Errorf("ping: %w", err)
			}
		}
	}
}

// Send sends a message to the upstream
func (c *Client) Send(ctx context.Context, msg *protocol.Message) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	c.logger.Debug("sending message", "data", string(data))

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	return nil
}

// Request sends a request and waits for response
func (c *Client) Request(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	if msg.ID == nil {
		return nil, fmt.Errorf("request must have an ID")
	}

	// Create response channel
	respCh := make(chan *protocol.Message, 1)
	c.pendingMu.Lock()
	c.pending[msg.ID] = respCh
	c.pendingMu.Unlock()

	// Ensure cleanup
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, msg.ID)
		c.pendingMu.Unlock()
	}()

	// Send request
	if err := c.Send(ctx, msg); err != nil {
		return nil, err
	}

	// Wait for response
	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes the client
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	close(c.closeCh)

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns true if currently connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil && !c.closed
}
