package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
)

// MCP 协议版本（2025 年稳定版）。
const (
	ProtocolVersion = "2025-03-26"
	JSONRPCVersion  = "2.0"
)

// JSONRPCRequest 描述一个 MCP JSON-RPC 请求（Streamable HTTP POST body）。
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // 允许 id 为任意 JSON，通知时为空
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse 描述一个 MCP JSON-RPC 响应。
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError 遵循 JSON-RPC 2.0 错误对象。
type JSONRPCError struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// Tool 一个 MCP 工具定义（inputSchema 沿用 JSON Schema）。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// InitializeResult initialize 方法返回体。
type InitializeResult struct {
	ProtocolVersion string      `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo  `json:"serverInfo"`
	Instructions    string      `json:"instructions,omitempty"`
}

type Capabilities struct {
	Tools *ToolCapability `json:"tools,omitempty"`
}

type ToolCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ListToolsResult tools/list 返回体。
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams tools/call 的参数。
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallToolResult tools/call 返回体。
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock 文本内容块。
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Handler 由网关/服务端实现，据此列出并调用工具。
type Handler interface {
	ListTools() ([]Tool, error)
	CallTool(ctx context.Context, name string, args []byte) (*CallToolResult, error)
}

// DecodeRequest 读取并解码一个 JSON-RPC 请求。
func DecodeRequest(r io.Reader) (*JSONRPCRequest, error) {
	req := &JSONRPCRequest{}
	if err := json.NewDecoder(r).Decode(req); err != nil {
		return nil, err
	}
	if req.JSONRPC != JSONRPCVersion {
		return nil, errors.New("invalid jsonrpc version")
	}
	return req, nil
}
