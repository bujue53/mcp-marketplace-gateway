package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Server 实现 MCP Streamable HTTP 的服务端行为。
// 网关用它把「已注册工具」暴露为 MCP 端点（/mcp）。
type Server struct {
	Info   ServerInfo
	Handle Handler
}

func New(name, version string, h Handler) *Server {
	return &Server{Info: ServerInfo{Name: name, Version: version}, Handle: h}
}

func (s *Server) Route(c *gin.Context) {
	if c.Request.Method != http.MethodPost {
		c.JSON(http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req JSONRPCRequest
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, JSONRPCResponse{Error: &JSONRPCError{Code: -32700, Message: "parse error"}})
		return
	}
	if req.JSONRPC != JSONRPCVersion || req.Method == "" {
		c.JSON(http.StatusBadRequest, JSONRPCResponse{Error: &JSONRPCError{Code: -32600, Message: "invalid request"}})
		return
	}
	if len(req.ID) == 0 {
		c.Status(http.StatusAccepted)
		return
	}

	span := trace.SpanFromContext(c.Request.Context())
	span.SetAttributes(attribute.String("rpc.system", "jsonrpc"), attribute.String("rpc.method", req.Method))
	switch req.Method {
	case "initialize":
		c.JSON(http.StatusOK, JSONRPCResponse{
			ID: req.ID,
			Result: &InitializeResult{
				ProtocolVersion: ProtocolVersion,
				Capabilities:    Capabilities{Tools: &ToolCapability{ListChanged: false}},
				ServerInfo:      s.Info,
			},
		})
	case "notifications/initialized", "ping":
		// 通知无响应体，直接 202。
		if req.Method == "ping" {
			c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Result: map[string]any{}})
		} else {
			c.Status(http.StatusAccepted)
		}
	case "tools/list":
		var tools []Tool
		var err error
		if lister, ok := s.Handle.(ContextLister); ok {
			tools, err = lister.ListToolsContext(c.Request.Context())
		} else {
			tools, err = s.Handle.ListTools()
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, JSONRPCResponse{ID: req.ID, Error: &JSONRPCError{Code: -32603, Message: err.Error()}})
			return
		}
		c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Result: &ListToolsResult{Tools: tools}})
	case "tools/call":
		var params CallToolParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				span.SetStatus(codes.Error, "invalid MCP parameters")
				c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "invalid params"}})
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		res, err := s.Handle.CallTool(ctx, params.Name, params.Arguments)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "MCP tool call failed")
			c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Error: &JSONRPCError{Code: -32000, Message: err.Error()}})
			return
		}
		if res != nil && res.IsError {
			span.SetStatus(codes.Error, "MCP tool returned an error")
		}
		c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Result: res})
	default:
		span.SetStatus(codes.Error, "unknown MCP method")
		c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Error: &JSONRPCError{Code: -32601, Message: errors.New("method not found").Error()}})
	}
}
