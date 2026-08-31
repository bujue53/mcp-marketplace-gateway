package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// RateTool 评分。
func (s *Service) RateTool(ctx context.Context, toolID, userID int64, rating int) error {
	return s.st.RateTool(ctx, toolID, userID, rating)
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
