// Package adapter defines interfaces and implementations for MCP server adapters.
package adapter

import (
	"context"
	"time"

	"github.com/cliffyan/mcp-gateway/internal/config"
	"github.com/cliffyan/mcp-gateway/internal/protocol"
)

// Adapter defines the interface for MCP server adapters
type Adapter interface {
	// Name returns the adapter name
	Name() string

	// Type returns the transport type (stdio, http, sse, streamablehttp)
	Type() string

	// Start starts the adapter
	Start(ctx context.Context) error

	// Stop stops the adapter gracefully
	Stop(ctx context.Context) error

	// Send sends a request to the downstream MCP server and returns the response
	Send(ctx context.Context, msg *protocol.Message) (*protocol.Message, error)

	// IsHealthy returns true if the adapter is healthy
	IsHealthy() bool

	// Initialize performs MCP initialization handshake
	Initialize(ctx context.Context) (*protocol.InitializeResult, error)

	// ListTools returns the list of tools provided by this adapter
	ListTools(ctx context.Context) ([]protocol.Tool, error)
}

// BaseAdapter provides common functionality for adapters
type BaseAdapter struct {
	name        string
	adapterType string
	timeout     time.Duration
	initialized bool
	tools       []protocol.Tool
}

// NewBaseAdapter creates a new base adapter
func NewBaseAdapter(name, adapterType string, timeout time.Duration) *BaseAdapter {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &BaseAdapter{
		name:        name,
		adapterType: adapterType,
		timeout:     timeout,
	}
}

// Name returns the adapter name
func (b *BaseAdapter) Name() string {
	return b.name
}

// Type returns the transport type
func (b *BaseAdapter) Type() string {
	return b.adapterType
}

// SetTools sets the cached tools list
func (b *BaseAdapter) SetTools(tools []protocol.Tool) {
	b.tools = tools
}

// GetCachedTools returns the cached tools list
func (b *BaseAdapter) GetCachedTools() []protocol.Tool {
	return b.tools
}

// SetInitialized marks the adapter as initialized
func (b *BaseAdapter) SetInitialized(v bool) {
	b.initialized = v
}

// IsInitialized returns true if the adapter has been initialized
func (b *BaseAdapter) IsInitialized() bool {
	return b.initialized
}

// Factory creates adapters based on configuration
type Factory struct{}

// NewFactory creates a new adapter factory
func NewFactory() *Factory {
	return &Factory{}
}

// Create creates an adapter based on the server configuration
func (f *Factory) Create(name string, cfg config.ServerConfig) (Adapter, error) {
	switch cfg.Type {
	case "stdio":
		return NewStdioAdapter(name, cfg), nil
	case "http", "streamablehttp":
		return NewHTTPAdapter(name, cfg), nil
	case "sse":
		return NewSSEAdapter(name, cfg), nil
	default:
		return nil, &UnsupportedTypeError{Type: cfg.Type}
	}
}

// UnsupportedTypeError indicates an unsupported adapter type
type UnsupportedTypeError struct {
	Type string
}

func (e *UnsupportedTypeError) Error() string {
	return "unsupported adapter type: " + e.Type
}
