// Package router provides message routing between upstream and downstream MCP servers.
package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/cliffyan/mcp-gateway/internal/adapter"
	"github.com/cliffyan/mcp-gateway/internal/protocol"
)

// Router manages message routing between upstream and downstream adapters
type Router struct {
	adapters  map[string]adapter.Adapter
	toolIndex map[string]string // tool_name -> adapter_name
	mu        sync.RWMutex
	logger    *slog.Logger

	// Gateway info for initialize response
	gatewayInfo protocol.Implementation
}

// NewRouter creates a new message router
func NewRouter(logger *slog.Logger) *Router {
	return &Router{
		adapters:  make(map[string]adapter.Adapter),
		toolIndex: make(map[string]string),
		logger:    logger.With("component", "router"),
		gatewayInfo: protocol.Implementation{
			Name:    "mcp-gateway",
			Version: "1.0.0",
		},
	}
}

// RegisterAdapter registers an adapter with the router
func (r *Router) RegisterAdapter(name string, a adapter.Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = a
	r.logger.Info("registered adapter", "name", name, "type", a.Type())
}

// UnregisterAdapter removes an adapter from the router
func (r *Router) UnregisterAdapter(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.adapters, name)

	// Clean up tool index
	for tool, adapterName := range r.toolIndex {
		if adapterName == name {
			delete(r.toolIndex, tool)
		}
	}
}

// InitializeAll initializes all adapters and builds the tool index
func (r *Router) InitializeAll(ctx context.Context) error {
	r.mu.RLock()
	adapters := make(map[string]adapter.Adapter)
	for k, v := range r.adapters {
		adapters[k] = v
	}
	r.mu.RUnlock()

	var wg sync.WaitGroup
	errCh := make(chan error, len(adapters))

	for name, a := range adapters {
		wg.Add(1)
		go func(name string, a adapter.Adapter) {
			defer wg.Done()

			// Initialize
			result, err := a.Initialize(ctx)
			if err != nil {
				r.logger.Error("initialize adapter failed", "name", name, "error", err)
				errCh <- fmt.Errorf("initialize %s: %w", name, err)
				return
			}

			r.logger.Info("initialized adapter",
				"name", name,
				"serverName", result.ServerInfo.Name,
				"serverVersion", result.ServerInfo.Version,
			)

			// List tools
			tools, err := a.ListTools(ctx)
			if err != nil {
				r.logger.Error("list tools failed", "name", name, "error", err)
				errCh <- fmt.Errorf("list tools %s: %w", name, err)
				return
			}

			// Update tool index
			r.mu.Lock()
			for _, tool := range tools {
				r.toolIndex[tool.Name] = name
				r.logger.Debug("registered tool", "tool", tool.Name, "adapter", name)
			}
			r.mu.Unlock()

			r.logger.Info("loaded tools from adapter", "name", name, "count", len(tools))
		}(name, a)
	}

	wg.Wait()
	close(errCh)

	// Collect errors
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("initialization errors: %v", errors)
	}

	return nil
}

// Handle processes an incoming message from upstream
func (r *Router) Handle(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	r.logger.Debug("handling message", "method", msg.Method, "id", msg.ID)

	switch msg.Method {
	case protocol.MethodInitialize:
		return r.handleInitialize(ctx, msg)

	case protocol.MethodToolsList:
		return r.handleToolsList(ctx, msg)

	case protocol.MethodToolsCall:
		return r.handleToolsCall(ctx, msg)

	case protocol.MethodPing:
		return r.handlePing(ctx, msg)

	case protocol.MethodInitialized:
		// Notification, no response needed
		return nil, nil

	default:
		r.logger.Warn("unknown method", "method", msg.Method)
		return protocol.NewErrorResponse(msg.ID, protocol.ErrCodeMethodNotFound,
			fmt.Sprintf("method not found: %s", msg.Method), nil)
	}
}

// handleInitialize handles initialize requests
func (r *Router) handleInitialize(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	result := &protocol.InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: protocol.ServerCapability{
			Tools: &protocol.ToolsCapability{
				ListChanged: true,
			},
		},
		ServerInfo: r.gatewayInfo,
	}

	return protocol.NewResponse(msg.ID, result)
}

// handleToolsList handles tools/list requests
func (r *Router) handleToolsList(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var allTools []protocol.Tool
	for _, a := range r.adapters {
		var tools []protocol.Tool

		switch ta := a.(type) {
		case *adapter.StdioAdapter:
			tools = ta.GetCachedTools()
		case *adapter.HTTPAdapter:
			tools = ta.GetCachedTools()
		case *adapter.SSEAdapter:
			tools = ta.GetCachedTools()
		}

		allTools = append(allTools, tools...)
	}

	result := &protocol.ToolsListResult{
		Tools: allTools,
	}

	return protocol.NewResponse(msg.ID, result)
}

// handleToolsCall handles tools/call requests
func (r *Router) handleToolsCall(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	// Parse params
	var params protocol.ToolCallParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return protocol.NewErrorResponse(msg.ID, protocol.ErrCodeInvalidParams,
			"invalid params: "+err.Error(), nil)
	}

	r.logger.Info("tool call", "tool", params.Name)

	// Find adapter for this tool
	r.mu.RLock()
	adapterName, ok := r.toolIndex[params.Name]
	if !ok {
		r.mu.RUnlock()
		return protocol.NewErrorResponse(msg.ID, protocol.ErrCodeMethodNotFound,
			fmt.Sprintf("tool not found: %s", params.Name), nil)
	}
	a := r.adapters[adapterName]
	r.mu.RUnlock()

	// Forward request to adapter
	resp, err := a.Send(ctx, msg)
	if err != nil {
		r.logger.Error("tool call failed", "tool", params.Name, "error", err)
		return protocol.NewErrorResponse(msg.ID, protocol.ErrCodeInternalError, err.Error(), nil)
	}

	return resp, nil
}

// handlePing handles ping requests
func (r *Router) handlePing(ctx context.Context, msg *protocol.Message) (*protocol.Message, error) {
	return protocol.NewResponse(msg.ID, map[string]any{})
}

// GetAllTools returns all registered tools
func (r *Router) GetAllTools() []protocol.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var allTools []protocol.Tool
	seen := make(map[string]bool)

	for _, a := range r.adapters {
		var tools []protocol.Tool

		switch ta := a.(type) {
		case *adapter.StdioAdapter:
			tools = ta.GetCachedTools()
		case *adapter.HTTPAdapter:
			tools = ta.GetCachedTools()
		case *adapter.SSEAdapter:
			tools = ta.GetCachedTools()
		}

		for _, tool := range tools {
			if !seen[tool.Name] {
				seen[tool.Name] = true
				allTools = append(allTools, tool)
			}
		}
	}

	return allTools
}

// GetAdapterForTool returns the adapter name for a given tool
func (r *Router) GetAdapterForTool(toolName string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.toolIndex[toolName]
	return name, ok
}

// AdapterCount returns the number of registered adapters
func (r *Router) AdapterCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.adapters)
}

// ToolCount returns the number of registered tools
func (r *Router) ToolCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.toolIndex)
}
