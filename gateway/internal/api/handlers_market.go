package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/auth"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/store"
)

func (s *Server) MarketplaceTools(c *gin.Context) {
	q := c.Query("q")
	category := c.Query("category")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	sort := c.DefaultQuery("sort", "")
	var items []*model.MCPTool
	var err error
	if sort == "popular" {
		items, err = s.reg.ListPopularTools(c.Request.Context(), q, category, limit)
	} else if sort == "" || sort == "default" {
		items, err = s.reg.ListTools(c.Request.Context(), q, category, limit)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported sort; use popular"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) GetTool(c *gin.Context) {
	id := parseID(c.Param("id"))
	tool, err := s.reg.GetTool(c.Request.Context(), id)
	if err == store.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tool)
}

// ToolVersions 返回工具最近的版本快照。
func (s *Server) ToolVersions(c *gin.Context) {
	id := parseID(c.Param("id"))
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tool id"})
		return
	}
	if _, err := s.reg.GetTool(c.Request.Context(), id); err == store.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and 100"})
		return
	}
	if limit > 100 {
		limit = 100
	}
	items, err := s.st.ListToolVersions(c.Request.Context(), id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type rateReq struct {
	Rating  int    `json:"rating" binding:"required"`
	Comment string `json:"comment"`
}

func (s *Server) RateTool(c *gin.Context) {
	u := auth.UserFrom(c.Request.Context())
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var req rateReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Rating < 1 || req.Rating > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rating must be 1..5"})
		return
	}
	id := parseID(c.Param("id"))
	if err := s.reg.RateTool(c.Request.Context(), id, u.ID, req.Rating, req.Comment); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) ConnectTool(c *gin.Context) {
	u := auth.UserFrom(c.Request.Context())
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id := parseID(c.Param("id"))
	tool, err := s.reg.GetTool(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// 一键接入只为当前用户授予 invoke 权限，不影响同角色的其他用户。
	if err := s.st.UpsertPermission(c.Request.Context(), "user", strconv.FormatInt(u.ID, 10), "tool", tool.ID, "invoke", true); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"connected": true,
		"tool":      tool.Name,
		"endpoint":  "/mcp",
		"access":    "granted",
		"config":    buildClientConfig(tool),
	})
}

// Comments 返回某工具的评分评论。
func (s *Server) Comments(c *gin.Context) {
	id := parseID(c.Param("id"))
	items, err := s.st.ListComments(c.Request.Context(), id, 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = []model.ToolComment{}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// buildClientConfig 生成可直接粘贴到 MCP 客户端（如 Claude Desktop）的接入配置片段。
func buildClientConfig(tool *model.MCPTool) map[string]any {
	base := "/mcp"
	_ = tool
	return map[string]any{
		"mcpServers": map[string]any{
			"mcp-gateway": map[string]any{
				"url":     "http://localhost:8080" + base,
				"headers": map[string]any{"Authorization": "Bearer <你的JWT>"},
			},
		},
	}
}
