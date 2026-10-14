package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	telemetry "github.com/bujue53/mcp-marketplace-gateway/gateway/internal/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Client 是面向后端 MCP Server 的 Streamable HTTP 客户端，网关据此发起 tools/list / tools/call。
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token, HTTP: &http.Client{Timeout: 15 * time.Second, Transport: telemetry.NewTransport(nil)}}
}

func (c *Client) do(ctx context.Context, method string, params any) (response *JSONRPCResponse, callErr error) {
	ctx, span := otel.Tracer("mcp-gateway/mcp-client").Start(ctx, "mcp."+method, trace.WithSpanKind(trace.SpanKindClient))
	span.SetAttributes(attribute.String("rpc.system", "jsonrpc"), attribute.String("rpc.method", method))
	defer func() {
		if callErr != nil {
			span.RecordError(callErr)
			span.SetStatus(codes.Error, "MCP request failed")
		}
		span.End()
	}()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": JSONRPCVersion,
		"id":      1,
		"method":  method,
		"params":  params,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", method, err)
	}
	var r JSONRPCResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	if r.Error != nil {
		return &r, fmt.Errorf("rpc error %d: %s", r.Error.Code, r.Error.Message)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("call %s: HTTP %d", method, resp.StatusCode)
	}
	return &r, nil
}

func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	resp, err := c.do(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "mcp-gateway", "version": "1.0.0"},
	})
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(resp.Result)
	out := &InitializeResult{}
	_ = json.Unmarshal(b, out)
	return out, nil
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	// 部分服务端需要先发送 initialized 通知，这里不承担会话状态，直接尝试。
	resp, err := c.do(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(resp.Result)
	out := &ListToolsResult{}
	if err := json.Unmarshal(b, out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, args any) (*CallToolResult, error) {
	resp, err := c.do(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(resp.Result)
	out := &CallToolResult{}
	if err := json.Unmarshal(b, out); err != nil {
		return nil, err
	}
	return out, nil
}
