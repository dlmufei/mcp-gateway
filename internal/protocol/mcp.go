// Package protocol defines MCP (Model Context Protocol) message types and structures.
package protocol

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 message types
const (
	JSONRPCVersion = "2.0"
)

// MCP method names
const (
	MethodInitialize     = "initialize"
	MethodInitialized    = "notifications/initialized"
	MethodToolsList      = "tools/list"
	MethodToolsCall      = "tools/call"
	MethodResourcesList  = "resources/list"
	MethodResourcesRead  = "resources/read"
	MethodPromptsList    = "prompts/list"
	MethodPromptsGet     = "prompts/get"
	MethodPing           = "ping"
	MethodCancelled      = "notifications/cancelled"
)

// Message represents a JSON-RPC 2.0 message
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`      // Can be string, number, or null
	Method  string          `json:"method,omitempty"`  // For requests and notifications
	Params  json.RawMessage `json:"params,omitempty"`  // Parameters
	Result  json.RawMessage `json:"result,omitempty"`  // For responses
	Error   *RPCError       `json:"error,omitempty"`   // For error responses
}

// RPCError represents a JSON-RPC 2.0 error
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// Standard JSON-RPC 2.0 error codes
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// IsRequest returns true if the message is a request (has method and id)
func (m *Message) IsRequest() bool {
	return m.Method != "" && m.ID != nil
}

// IsNotification returns true if the message is a notification (has method but no id)
func (m *Message) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

// IsResponse returns true if the message is a response (has result or error)
func (m *Message) IsResponse() bool {
	return m.Result != nil || m.Error != nil
}

// NewRequest creates a new JSON-RPC request
func NewRequest(id any, method string, params any) (*Message, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		paramsRaw = data
	}
	return &Message{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

// NewResponse creates a new JSON-RPC response
func NewResponse(id any, result any) (*Message, error) {
	var resultRaw json.RawMessage
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("marshal result: %w", err)
		}
		resultRaw = data
	}
	return &Message{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  resultRaw,
	}, nil
}

// NewErrorResponse creates a new JSON-RPC error response
func NewErrorResponse(id any, code int, message string, data any) (*Message, error) {
	rpcErr := &RPCError{
		Code:    code,
		Message: message,
	}
	if data != nil {
		dataRaw, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("marshal error data: %w", err)
		}
		rpcErr.Data = dataRaw
	}
	return &Message{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   rpcErr,
	}, nil
}

// Tool represents an MCP tool definition
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ToolsListResult represents the result of tools/list
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// ToolCallParams represents parameters for tools/call
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResult represents the result of tools/call
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock represents a content block in tool results
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
	// Add other fields as needed for different content types
}

// InitializeParams represents parameters for initialize request
type InitializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    ClientCapability  `json:"capabilities"`
	ClientInfo      Implementation    `json:"clientInfo"`
}

// InitializeResult represents the result of initialize
type InitializeResult struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    ServerCapability  `json:"capabilities"`
	ServerInfo      Implementation    `json:"serverInfo"`
}

// ClientCapability represents client capabilities
type ClientCapability struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

// ServerCapability represents server capabilities
type ServerCapability struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
	Logging   *LoggingCapability   `json:"logging,omitempty"`
}

// Various capability structs
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct{}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

// Implementation represents client/server info
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ParseMessage parses a JSON-RPC message from bytes
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return &msg, nil
}

// Marshal serializes the message to JSON bytes
func (m *Message) Marshal() ([]byte, error) {
	return json.Marshal(m)
}
