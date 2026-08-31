package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yafeng/mcp-marketplace/gateway/internal/auth"
	"github.com/yafeng/mcp-marketplace/gateway/internal/model"
	"github.com/yafeng/mcp-marketplace/gateway/internal/wrapper"
)

// CheckServerHealth 主动探测某个 MCP Server 的健康状态。
func (s *Server) CheckServerHealth(c *gin.Context) {
	id := parseID(c.Param("id"))
	healthy, err := s.reg.CheckHealth(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"server_id": id, "healthy": healthy})
}

type skeletonReq struct {
	Spec json.RawMessage `json:"spec"`
	Lang string          `json:"lang"`
}

// GenerateSkeleton 依据 OpenAPI 文档生成 MCP Server 骨架代码（Go / TS）。
func (s *Server) GenerateSkeleton(c *gin.Context) {
	if auth.UserFrom(c.Request.Context()) == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var req skeletonReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Spec) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "spec is required"})
		return
	}
	doc, err := wrapper.Parse(req.Spec)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	lang := defaultLang(req.Lang)
	code, err := wrapper.Skeleton(doc, lang)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lang": lang, "code": code})
}

type skillsReq struct {
	Skills []wrapper.SkillDef `json:"skills"`
}

// ConvertSkills 把 Agent Skills 适配为 MCP 工具并注册到市场。
func (s *Server) ConvertSkills(c *gin.Context) {
	u := auth.UserFrom(c.Request.Context())
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var req skillsReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Skills) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "skills is required"})
		return
	}
	sv, tools, err := s.reg.CreateSkillsServer(c.Request.Context(), req.Skills, u.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"server": sv, "tools": tools})
}

// AuditAlerts 返回异常调用告警列表。
func (s *Server) AuditAlerts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, err := s.st.ListAuditAlerts(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = []model.AuditAlert{}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// AuditReport 返回治理周报摘要与敏感工具高频告警。
func (s *Server) AuditReport(c *gin.Context) {
	summary, err := s.st.GovernanceReport(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	hot, err := s.st.SensitiveHotTools(c.Request.Context(), 5, 60)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if hot == nil {
		hot = []model.SensitiveHot{}
	}
	c.JSON(http.StatusOK, gin.H{"summary": summary, "sensitive_hot": hot})
}

func defaultLang(lang string) string {
	for _, l := range []string{"go", "ts", "typescript", "golang"} {
		if l == lang {
			if lang == "golang" {
				return "go"
			}
			if lang == "typescript" {
				return "ts"
			}
			return lang
		}
	}
	return "go"
}
