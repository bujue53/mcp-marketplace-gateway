package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yafeng/mcp-marketplace/gateway/internal/mcp"
)

// SkillDef 表示一个 Agent Skill（源自 SKILL.md 的 frontmatter + 说明）。
type SkillDef struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
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
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
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
	body := sk.Instructions
	if body == "" {
		body = "（该 Skill 未提供说明，可由 Agent 自行执行技能流程。）"
	}
	return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: body}}}, nil
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
