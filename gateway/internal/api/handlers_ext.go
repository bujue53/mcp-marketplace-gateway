package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/auth"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/wrapper"
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
	Spec    json.RawMessage `json:"spec"`
	Lang    string          `json:"lang"`
	BaseURL string          `json:"base_url"`
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
	project, err := wrapper.GenerateSkeletonProject(doc, req.Lang, req.BaseURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, project)
}

type skillsReq struct {
	Skills        []wrapper.SkillDef `json:"skills"`
	SkillMarkdown string             `json:"skill_md"`
	Name          string             `json:"name"`
}

// ConvertSkills 把 Agent Skills 适配为 MCP 工具并注册到市场。
func (s *Server) ConvertSkills(c *gin.Context) {
	u := auth.UserFrom(c.Request.Context())
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var req skillsReq
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, wrapper.MaxSkillsUpload+(1<<20))
	var parseErr error
	if c.ContentType() == "multipart/form-data" {
		if err := c.Request.ParseMultipartForm(wrapper.MaxSkillsUpload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or oversized skill upload"})
			return
		}
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
			return
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, wrapper.MaxSkillsUpload+1))
		if err != nil || len(raw) > wrapper.MaxSkillsUpload {
			c.JSON(http.StatusBadRequest, gin.H{"error": "skill upload exceeds 16 MiB"})
			return
		}
		req.Name = c.PostForm("name")
		if strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
			req.Skills, parseErr = wrapper.ParseSkillsPackage(raw)
		} else if strings.EqualFold(header.Filename, "SKILL.md") {
			var skill wrapper.SkillDef
			skill, parseErr = wrapper.ParseSkillMarkdown(raw, "")
			req.Skills = []wrapper.SkillDef{skill}
		} else {
			parseErr = fmt.Errorf("upload a ZIP package or SKILL.md")
		}
	} else {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid skill request"})
			return
		}
		if req.SkillMarkdown != "" {
			if len(req.Skills) > 0 {
				parseErr = fmt.Errorf("choose skills or skill_md")
			} else {
				var skill wrapper.SkillDef
				skill, parseErr = wrapper.ParseSkillMarkdown([]byte(req.SkillMarkdown), "")
				req.Skills = []wrapper.SkillDef{skill}
			}
		}
	}
	if parseErr != nil || len(req.Skills) == 0 {
		message := "skills, skill_md or file is required"
		if parseErr != nil {
			message = parseErr.Error()
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return
	}
	sv, tools, err := s.reg.CreateSkillsServerNamed(c.Request.Context(), req.Name, req.Skills, u.ID)
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
	c.Header("X-Analytics-Source", "postgres")
	if s.st.Analytics != nil {
		if analytics, err := s.st.Analytics.Summary(c.Request.Context(), true); err == nil {
			summary["calls"] = analytics["calls"]
			summary["cost_micros"] = analytics["cost_micros"]
			summary["errors"] = analytics["errors"]
			summary["p99_latency_ms"] = analytics["p99_latency_ms"]
			summary["window_days"] = 7
			c.Header("X-Analytics-Source", "clickhouse")
		}
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
