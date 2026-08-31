package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yafeng/mcp-marketplace/gateway/internal/auth"
	"github.com/yafeng/mcp-marketplace/gateway/internal/store"
)

func (s *Server) MarketplaceTools(c *gin.Context) {
	q := c.Query("q")
	category := c.Query("category")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := s.reg.ListTools(c.Request.Context(), q, category, limit)
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

type rateReq struct {
	Rating int `json:"rating" binding:"required"`
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
	if err := s.reg.RateTool(c.Request.Context(), id, u.ID, req.Rating); err != nil {
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
	// 一键接入：为该用户的角色授予 invoke 权限。
	if err := s.st.UpsertPermission(c.Request.Context(), "role", u.Role, "tool", tool.ID, "invoke", true); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"connected": true,
		"tool":      tool.Name,
		"endpoint":  "/mcp",
		"access":    "granted",
	})
}

