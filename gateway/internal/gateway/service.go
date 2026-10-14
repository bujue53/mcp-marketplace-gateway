package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/mcp"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/store"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/wrapper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Invoker 抽象「如何真正执行一次工具调用」。
type Invoker interface {
	Invoke(ctx context.Context, tool *model.MCPTool, args json.RawMessage) (*mcp.CallToolResult, error)
}

// Service 网关转发服务：根据工具来源选择 MCP 客户端或 OpenAPI 绑定执行调用。
type Service struct {
	st       *store.Store
	mu       sync.RWMutex
	bindings map[string]*wrapper.Handle // server_id -> OpenAPI 绑定（自动生成工具专用）
	skills   map[string]*wrapper.SkillsAdapter
}

func NewService(st *store.Store) *Service {
	return &Service{st: st, bindings: map[string]*wrapper.Handle{}, skills: map[string]*wrapper.SkillsAdapter{}}
}

// RegisterOpenAPI 把一个 OpenAPI 绑定注册给某 server（用于自动生成工具）。
func (s *Service) RegisterOpenAPI(serverID int64, h *wrapper.Handle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[fmt.Sprintf("%d", serverID)] = h
}

func (s *Service) getOpenAPI(serverID int64) *wrapper.Handle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bindings[fmt.Sprintf("%d", serverID)]
}

// RegisterSkills 注册一个 Skills→MCP 适配器（按 server id）。
func (s *Service) RegisterSkills(serverID int64, a *wrapper.SkillsAdapter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skills[fmt.Sprintf("%d", serverID)] = a
}

func (s *Service) getSkills(serverID int64) *wrapper.SkillsAdapter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.skills[fmt.Sprintf("%d", serverID)]
}

// Invoke 执行工具调用，并记录成本、延迟与审计数据交给上层。
func (s *Service) Invoke(ctx context.Context, tool *model.MCPTool, args json.RawMessage) (result *mcp.CallToolResult, err error) {
	ctx, span := otel.Tracer("mcp-gateway/tools").Start(ctx, "tool.invoke")
	span.SetAttributes(attribute.Int64("tool.id", tool.ID), attribute.String("tool.name", tool.Name), attribute.Int64("server.id", tool.ServerID))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "tool invocation failed")
		} else if result != nil && result.IsError {
			span.SetStatus(codes.Error, "tool returned an error")
		}
		span.End()
	}()
	sv, err := s.st.InvocationServer(ctx, tool.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server of tool %d not found", tool.ID)
	}
	if sv.Status != "online" {
		return nil, fmt.Errorf("server %s is %s", sv.Name, sv.Status)
	}

	span.SetAttributes(attribute.String("server.transport", sv.Transport))
	if sv.Transport == "skills" && s.getSkills(sv.ID) == nil {
		raw, loadErr := s.st.LoadSkillsBindings(ctx, sv.ID)
		if loadErr != nil {
			return nil, fmt.Errorf("skill bindings unavailable: %w", loadErr)
		}
		var skills []wrapper.SkillDef
		if err := json.Unmarshal(raw, &skills); err != nil {
			return nil, fmt.Errorf("invalid stored skills: %w", err)
		}
		s.RegisterSkills(sv.ID, wrapper.NewSkillsAdapter(skills))
	}
	if sv.Transport == "http_rest" && s.getOpenAPI(sv.ID) == nil {
		raw, loadErr := s.st.LoadOpenAPIBindings(ctx, sv.ID)
		if loadErr != nil {
			return nil, fmt.Errorf("OpenAPI bindings unavailable for server %d: %w", sv.ID, loadErr)
		}
		var bindings []wrapper.GeneratedTool
		if err := json.Unmarshal(raw, &bindings); err != nil {
			return nil, fmt.Errorf("invalid stored OpenAPI bindings: %w", err)
		}
		// 每个工具保留自己的服务器地址；显式覆盖地址已在生成时写入绑定。
		s.RegisterOpenAPI(sv.ID, wrapper.NewGenerator("").Handler(bindings))
	}
	if a := s.getSkills(sv.ID); a != nil {
		result, err = a.CallTool(ctx, tool.Name, args)
	} else if h := s.getOpenAPI(sv.ID); h != nil {
		result, err = h.CallTool(ctx, tool.Name, args)
	} else {
		client := mcp.NewClient(sv.EndpointURL, "")
		result, err = client.CallTool(ctx, tool.Name, parseArgs(args))
	}
	return result, err
}

// EstimateTokens 简易估算：无 tokenizer，按字节近似。
func EstimateTokens(args json.RawMessage) int {
	return len(args) / 4
}

func parseArgs(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return map[string]any{}
	}
	return v
}

// Slug 供审计/指标使用的简短工具标识。
func Slug(tool *model.MCPTool) string {
	return strings.ReplaceAll(tool.Name, " ", "_")
}
