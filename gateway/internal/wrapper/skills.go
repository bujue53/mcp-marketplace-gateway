package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/mcp"
)

// SkillDef 表示一个 Agent Skill（源自 SKILL.md 的 frontmatter + 说明）。
type SkillDef struct {
	Name          string                   `json:"name"`
	Description   string                   `json:"description"`
	Instructions  string                   `json:"instructions"`
	License       string                   `json:"license,omitempty"`
	Compatibility string                   `json:"compatibility,omitempty"`
	Metadata      map[string]string        `json:"metadata,omitempty"`
	AllowedTools  string                   `json:"allowed_tools,omitempty"`
	Resources     map[string]SkillResource `json:"resources,omitempty"`
}

type SkillResource struct {
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

// SkillsAdapter 把 Skills 集合暴露为 MCP 工具元数据；调用时返回技能说明。
// 解决「Agent Skills → MCP 工具」的适配诉求（骨架实现：返回 instructions 供 Agent 参考）。
type SkillsAdapter struct {
	tools  []mcp.Tool
	byName map[string]SkillDef
}

func NewSkillsAdapter(skills []SkillDef) *SkillsAdapter {
	a := &SkillsAdapter{byName: map[string]SkillDef{}}
	for _, sk := range skills {
		name := SkillsToolName(sk.Name)
		a.byName[name] = sk
		a.tools = append(a.tools, mcp.Tool{
			Name:        name,
			Description: firstNonEmpty(sk.Description, "Agent Skill："+sk.Name),
			InputSchema: SkillsInputSchema(),
		})
	}
	return a
}

// SkillsToolName 生成某个 Skill 对应的 MCP 工具名（与注册中心保持一致）。
func SkillsToolName(name string) string {
	return sanitizeName("skill_" + name)
}

func (a *SkillsAdapter) ListTools() ([]mcp.Tool, error) {
	return a.tools, nil
}

func (a *SkillsAdapter) CallTool(ctx context.Context, name string, args []byte) (*mcp.CallToolResult, error) {
	sk, ok := a.byName[name]
	if !ok {
		return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: "skill not found"}}, IsError: true}, nil
	}
	var input struct {
		Resource string `json:"resource"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("invalid skill arguments")
		}
	}
	if input.Resource != "" {
		resource, exists := sk.Resources[input.Resource]
		if !exists {
			return nil, fmt.Errorf("skill resource not found")
		}
		encoded, _ := json.Marshal(resource)
		return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: string(encoded)}}}, nil
	}
	body := sk.Instructions
	if body == "" {
		body = "（该 Skill 未提供说明，可由 Agent 自行执行技能流程。）"
	}
	if len(sk.Resources) > 0 || sk.License != "" || sk.Compatibility != "" || len(sk.Metadata) > 0 || sk.AllowedTools != "" {
		paths := make([]string, 0, len(sk.Resources))
		for path := range sk.Resources {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		metadata, _ := json.Marshal(map[string]any{"name": sk.Name, "license": sk.License, "compatibility": sk.Compatibility, "metadata": sk.Metadata, "allowed_tools": sk.AllowedTools, "resources": paths})
		body += "\n\nSkill metadata: " + string(metadata)
	}
	return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: body}}}, nil
}

func SkillsInputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"resource":{"type":"string","description":"Optional relative path of a bundled resource; omit to load instructions"}},"additionalProperties":false}`)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (a *SkillsAdapter) String() string {
	return fmt.Sprintf("skills(%d)", len(a.tools))
}
