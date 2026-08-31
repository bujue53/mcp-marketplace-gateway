package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yafeng/mcp-marketplace/gateway/internal/gateway"
	"github.com/yafeng/mcp-marketplace/gateway/internal/mcp"
	"github.com/yafeng/mcp-marketplace/gateway/internal/model"
	"github.com/yafeng/mcp-marketplace/gateway/internal/store"
	"github.com/yafeng/mcp-marketplace/gateway/internal/wrapper"
)

// Service 注册中心：MCP Server 注册 / 发现 / 版本 / 工具同步 / OpenAPI 自动包装。
type Service struct {
	st *store.Store
	gw *gateway.Service
}

func New(st *store.Store, gw *gateway.Service) *Service {
	return &Service{st: st, gw: gw}
}

// RegisterServer 注册一个 MCP Server。
func (s *Service) RegisterServer(ctx context.Context, in *model.MCPServer) (*model.MCPServer, error) {
	if err := s.st.UpsertServer(ctx, in); err != nil {
		return nil, err
	}
	return in, nil
}

// ListServers 发现全部（或指定状态）的 MCP Server。
func (s *Service) ListServers(ctx context.Context, status string) ([]*model.MCPServer, error) {
	return s.st.ListServers(ctx, status)
}

// GetServer 根据 ID 获取 Server。
func (s *Service) GetServer(ctx context.Context, id int64) (*model.MCPServer, error) {
	return s.st.GetServerByID(ctx, id)
}

// CheckHealth 探测指定 Server 是否健康并更新其状态（online/offline）。
func (s *Service) CheckHealth(ctx context.Context, serverID int64) (bool, error) {
	sv, err := s.st.GetServerByID(ctx, serverID)
	if err != nil {
		return false, err
	}
	healthy := probeServer(ctx, sv)
	status := "online"
	if !healthy {
		status = "offline"
	}
	_ = s.st.UpdateServerHealth(ctx, serverID, status, healthy)
	return healthy, nil
}

// StartHealthLoop 启动后台健康巡检：每 interval 间隔探测所有 Server 并上下线。
func (s *Service) StartHealthLoop(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				servers, err := s.st.ListServers(ctx, "")
				if err != nil {
					continue
				}
				for _, sv := range servers {
					healthy := probeServer(ctx, sv)
					status := "online"
					if !healthy {
						status = "offline"
					}
					_ = s.st.UpdateServerHealth(ctx, sv.ID, status, healthy)
				}
			}
		}
	}()
}

func probeServer(ctx context.Context, sv *model.MCPServer) bool {
	base := strings.TrimRight(sv.EndpointURL, "/")
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err == nil {
		if resp, err := client.Do(req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
	}
	// 退化到 MCP initialize 探活
	mcpClient := mcp.NewClient(base, "")
	_, err = mcpClient.Initialize(ctx)
	return err == nil
}

// CreateFromOpenAPI 核心亮点：把一份 OpenAPI 3.x 文档自动包装为 MCP 工具并注册到市场。
func (s *Service) CreateFromOpenAPI(ctx context.Context, spec []byte, baseURL string, ownerID int64) (*model.MCPServer, []model.MCPTool, error) {
	doc, err := wrapper.Parse(spec)
	if err != nil {
		return nil, nil, err
	}
	gen := wrapper.NewGenerator(baseURL)
	tools := gen.Build(doc)
	handle := gen.Handler(tools)

	serverName := doc.Info.Title
	if serverName == "" {
		serverName = "openapi-generated-server"
	}
	sv := &model.MCPServer{
		Name:        serverName,
		Description: fmt.Sprintf("由 OpenAPI %s v%s 自动包装生成", doc.Info.Title, doc.Info.Version),
		Version:     doc.Info.Version,
		EndpointURL: baseURL,
		Transport:   "http_rest",
		AuthType:    "none",
		Status:      "online",
		OwnerID:     ownerID,
		Tags:        []string{"openapi", "generated"},
	}
	if err := s.st.UpsertServer(ctx, sv); err != nil {
		return nil, nil, err
	}

	var registered []model.MCPTool
	for _, t := range tools {
		mt := &model.MCPTool{
			ServerID:     sv.ID,
			Name:         t.Tool.Name,
			Description:  t.Tool.Description,
			Category:     inferCategory(t.Tool.Name),
			Tags:         []string{"generated", "openapi"},
			InputSchema:  jsonToModel(t.Tool.InputSchema),
			OutputSchema: jsonToModel(genOutput(t.Tool.Name)),
			CostModel:    model.JSON{"tokens_per_call": 500, "cost_per_1k": 0.002},
			IsGenerated:  true,
			Version:      doc.Info.Version,
		}
		if err := s.st.UpsertTool(ctx, mt); err != nil {
			return nil, nil, err
		}
		registered = append(registered, *mt)
	}
	s.gw.RegisterOpenAPI(sv.ID, handle)
	return sv, registered, nil
}

// SyncNativeServer 从原生 MCP Server 拉取 tools/list 并同步工具到市场。
func (s *Service) SyncNativeServer(ctx context.Context, serverID int64) (int, error) {
	sv, err := s.st.GetServerByID(ctx, serverID)
	if err != nil {
		return 0, err
	}
	client := mcp.NewClient(sv.EndpointURL, "")
	tools, err := client.ListTools(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, t := range tools {
		mt := &model.MCPTool{
			ServerID:    serverID,
			Name:        t.Name,
			Description: t.Description,
			Category:    inferCategory(t.Name),
			InputSchema: jsonToModel(t.InputSchema),
			Version:     sv.Version,
		}
		if err := s.st.UpsertTool(ctx, mt); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// CreateSkillsServer 把一组 Agent Skills 适配为 MCP 工具并注册到市场。
func (s *Service) CreateSkillsServer(ctx context.Context, skills []wrapper.SkillDef, ownerID int64) (*model.MCPServer, []model.MCPTool, error) {
	adapter := wrapper.NewSkillsAdapter(skills)
	sv := &model.MCPServer{
		Name:        "agent-skills-adapter",
		Description: "由 Agent Skills 规范自动适配为 MCP 工具",
		Version:     "1.0.0",
		EndpointURL: "skills://internal",
		Transport:   "skills",
		AuthType:    "none",
		Status:      "online",
		OwnerID:     ownerID,
		Tags:        []string{"skills", "generated"},
	}
	if err := s.st.UpsertServer(ctx, sv); err != nil {
		return nil, nil, err
	}
	var registered []model.MCPTool
	for _, sk := range skills {
		mt := &model.MCPTool{
			ServerID:     sv.ID,
			Name:         wrapper.SkillsToolName(sk.Name),
			Description:  firstNonEmptyStr(sk.Description, "Agent Skill："+sk.Name),
			Category:     "skills",
			Tags:         []string{"skills", "generated"},
			InputSchema:  model.JSON{"type": "object", "properties": map[string]any{}},
			OutputSchema: model.JSON{"type": "object"},
			CostModel:    model.JSON{},
			IsGenerated:  true,
			Version:      sv.Version,
		}
		if err := s.st.UpsertTool(ctx, mt); err != nil {
			return nil, nil, err
		}
		registered = append(registered, *mt)
	}
	s.gw.RegisterSkills(sv.ID, adapter)
	return sv, registered, nil
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// RateTool 评分。
func (s *Service) RateTool(ctx context.Context, toolID, userID int64, rating int, comment string) error {
	return s.st.RateTool(ctx, toolID, userID, rating, comment)
}

// ListTools 市场搜索。
func (s *Service) ListTools(ctx context.Context, q, category string, limit int) ([]*model.MCPTool, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.st.ListTools(ctx, q, category, limit)
}

// GetTool 工具详情。
func (s *Service) GetTool(ctx context.Context, id int64) (*model.MCPTool, error) {
	return s.st.GetToolByID(ctx, id)
}

func inferCategory(name string) string {
	switch {
	case strings.Contains(name, "weather"):
		return "weather"
	case strings.Contains(name, "user"), strings.Contains(name, "account"):
		return "identity"
	case strings.Contains(name, "order"):
		return "commerce"
	case strings.Contains(name, "db_") || strings.Contains(name, "sql") || strings.Contains(name, "database"):
		return "数据库"
	case strings.Contains(name, "file") || strings.Contains(name, "doc") || strings.Contains(name, "sheet"):
		return "办公"
	case strings.Contains(name, "campaign") || strings.Contains(name, "market") || strings.Contains(name, "ad_") || strings.Contains(name, "traffic"):
		return "营销"
	case strings.Contains(name, "ai") || strings.Contains(name, "agent") || strings.Contains(name, "skill"):
		return "研发"
	default:
		return "general"
	}
}

func jsonToModel(raw json.RawMessage) model.JSON {
	var m model.JSON
	_ = json.Unmarshal(raw, &m)
	return m
}

func genOutput(name string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"result":{"type":"string"}}}`)
}
