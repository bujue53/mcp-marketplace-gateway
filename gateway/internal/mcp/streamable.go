package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, JSONRPCResponse{Error: &JSONRPCError{Code: -32700, Message: "parse error"}})
		return
	}
	if req.JSONRPC != JSONRPCVersion {
		c.JSON(http.StatusBadRequest, JSONRPCResponse{Error: &JSONRPCError{Code: -32600, Message: "invalid request"}})
		return
	}

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
		tools, err := s.Handle.ListTools()
		if err != nil {
			c.JSON(http.StatusInternalServerError, JSONRPCResponse{ID: req.ID, Error: &JSONRPCError{Code: -32603, Message: err.Error()}})
			return
		}
		c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Result: &ListToolsResult{Tools: tools}})
	case "tools/call":
		var params CallToolParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Error: &JSONRPCError{Code: -32602, Message: "invalid params"}})
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		res, err := s.Handle.CallTool(ctx, params.Name, params.Arguments)
		if err != nil {
			c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Error: &JSONRPCError{Code: -32000, Message: err.Error()}})
			return
		}
		c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Result: res})
	default:
		c.JSON(http.StatusOK, JSONRPCResponse{ID: req.ID, Error: &JSONRPCError{Code: -32601, Message: errors.New("method not found").Error()}})
	}
}

