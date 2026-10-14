package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/auth"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/store"
)

// CreateServer 注册一个 MCP Server。
func (s *Server) CreateServer(c *gin.Context) {
	u := auth.UserFrom(c.Request.Context())
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var in model.MCPServer
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	in.OwnerID = u.ID
	if _, err := s.reg.RegisterServer(c.Request.Context(), &in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, in)
}

func (s *Server) ListServers(c *gin.Context) {
	status := c.Query("status")
	list, err := s.reg.ListServers(c.Request.Context(), status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

// ServerStatusEvents streams online/offline transitions to authenticated clients.
func (s *Server) ServerStatusEvents(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	events, unsubscribe := s.reg.SubscribeStatusChanges()
	defer unsubscribe()
	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-events:
			if !ok {
				return false
			}
			c.SSEvent("server.status", event)
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func (s *Server) GetServer(c *gin.Context) {
	id := parseID(c.Param("id"))
	sv, err := s.reg.GetServer(c.Request.Context(), id)
	if err == store.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sv)
}

func (s *Server) DeleteServer(c *gin.Context) {
	id := parseID(c.Param("id"))
	if err := s.st.DeleteServer(c.Request.Context(), id); err == store.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

type generateReq struct {
	Spec    json.RawMessage `json:"spec"`
	BaseURL string          `json:"base_url"`
}

// GenerateFromOpenAPI 核心亮点：把 OpenAPI 文档自动包装为 MCP 工具并注册。
func (s *Server) GenerateFromOpenAPI(c *gin.Context) {
	u := auth.UserFrom(c.Request.Context())
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var req generateReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Spec) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "spec is required"})
		return
	}
	if req.BaseURL == "" {
		req.BaseURL = c.Query("base_url")
	}
	sv, tools, err := s.reg.CreateFromOpenAPI(c.Request.Context(), req.Spec, req.BaseURL, u.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"server": sv, "tools": tools})
}

func (s *Server) SyncServer(c *gin.Context) {
	id := parseID(c.Param("id"))
	n, err := s.reg.SyncNativeServer(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"synced": n})
}

func parseID(str string) int64 {
	var n int64
	for _, r := range str {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}
