package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yafeng/mcp-marketplace/gateway/internal/mcp"
)

// OpenAPI 是 OpenAPI 3.x 文档的局部模型（只解析本工具所需字段）。
type OpenAPI struct {
	OpenAPI string               `json:"openapi"`
	Info    Info                 `json:"info"`
	Servers []Server             `json:"servers"`
	Paths   map[string]*PathItem `json:"paths"`
	Components Components        `json:"components"`
}

type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type Server struct {
	URL string `json:"url"`
}

type Components struct {
	Schemas map[string]json.RawMessage `json:"schemas"`
}

type PathItem struct {
	Get    *Operation `json:"get,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Put    *Operation `json:"put,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
	Patch  *Operation `json:"patch,omitempty"`
}

type Operation struct {
	OperationID string             `json:"operationId"`
	Summary     string             `json:"summary"`
	Description string             `json:"description"`
	Tags        []string           `json:"tags"`
	Parameters  []Parameter        `json:"parameters"`
	RequestBody *RequestBody       `json:"requestBody"`
	Responses   map[string]*Response `json:"responses"`
}

type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"` // path | query | header
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Schema      any    `json:"schema"`
}

type RequestBody struct {
	Required bool                  `json:"required"`
	Content  map[string]*MediaType `json:"content"`
}

type MediaType struct {
	Schema any `json:"schema"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]*MediaType `json:"content"`
}

// GeneratedTool 描述从某个 Operation 自动生成的 MCP 工具与其到 REST 的绑定。
type GeneratedTool struct {
	Tool mcp.Tool
	// Binding 用于在调用时重建 HTTP 请求。
	Method string
	Path   string
	Query  []string // 允许透传到 query 的参数名
	Body   bool     // 是否携带请求体
}

// Generator 负责解析 OpenAPI 并生成 MCP 工具绑定。
type Generator struct {
	BaseURL string
	Client  *http.Client
}

func NewGenerator(baseURL string) *Generator {
	return &Generator{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Parse 从字节流解析 OpenAPI 文档。
func Parse(raw []byte) (*OpenAPI, error) {
	var doc OpenAPI
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi: %w", err)
	}
	if doc.OpenAPI == "" {
		return nil, errors.New("not an openapi 3.x document (missing openapi field)")
	}
	return &doc, nil
}

// Build 遍历 paths，为每个 operation 生成 MCP 工具。
func (g *Generator) Build(doc *OpenAPI) []GeneratedTool {
	var out []GeneratedTool
	for path, item := range doc.Paths {
		for _, m := range []struct {
			method string
			op     *Operation
		}{
			{"GET", item.Get}, {"POST", item.Post}, {"PUT", item.Put},
			{"DELETE", item.Delete}, {"PATCH", item.Patch},
		} {
			if m.op == nil {
				continue
			}
			out = append(out, g.buildTool(m.method, path, m.op))
		}
	}
	return out
}

func (g *Generator) buildTool(method, path string, op *Operation) GeneratedTool {
	name := op.OperationID
	if name == "" {
		name = sanitizeName(method + "_" + path)
	}
	desc := op.Summary
	if desc == "" {
		desc = op.Description
	}
	if desc == "" {
		desc = fmt.Sprintf("%s %s", method, path)
	}

	inSchema := g.buildInputSchema(path, op)
	outSchema := g.buildOutputSchema(op)

	var query []string
	for _, p := range op.Parameters {
		if p.In == "query" {
			query = append(query, p.Name)
		}
	}
	return GeneratedTool{
		Tool: mcp.Tool{
			Name:        name,
			Description: desc,
			InputSchema: inSchema,
		},
		Method: method,
		Path:   path,
		Query:  query,
		Body:   op.RequestBody != nil,
	}
}

func (g *Generator) buildInputSchema(path string, op *Operation) json.RawMessage {
	props := map[string]any{}
	required := []string{}
	for _, p := range op.Parameters {
		if p.In == "path" || p.In == "query" {
			props[p.Name] = schemaToAny(p.Schema)
			if p.Required {
				required = append(required, p.Name)
			}
		}
	}
	if op.RequestBody != nil {
		for _, mt := range op.RequestBody.Content {
			if mt.Schema != nil {
				key := "body"
				props[key] = schemaToAny(mt.Schema)
				if op.RequestBody.Required {
					required = append(required, key)
				}
			}
			break // 只取第一个 content
		}
	}
	if len(props) == 0 {
		return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	}
	sch := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sch["required"] = required
	}
	b, _ := json.Marshal(sch)
	return b
}

func (g *Generator) buildOutputSchema(op *Operation) json.RawMessage {
	if op.Responses == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	for _, code := range []string{"200", "201", "default"} {
		if r, ok := op.Responses[code]; ok && r.Content != nil {
			for _, mt := range r.Content {
				if mt.Schema != nil {
					b, _ := json.Marshal(map[string]any{
						"type":     "object",
						"response": schemaToAny(mt.Schema),
					})
					return b
				}
			}
		}
	}
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

// Handle 实现 mcp.Handler：把生成的工具包装成可运行的 MCP 端点处理器。
type Handle struct {
	tools map[string]GeneratedTool
	g     *Generator
}

func (g *Generator) Handler(gt []GeneratedTool) *Handle {
	h := &Handle{tools: map[string]GeneratedTool{}, g: g}
	for _, t := range gt {
		h.tools[t.Tool.Name] = t
	}
	return h
}

func (h *Handle) ListTools() ([]mcp.Tool, error) {
	var out []mcp.Tool
	for _, t := range h.tools {
		out = append(out, t.Tool)
	}
	return out, nil
}

// CallTool 依据绑定重建并对遗留 REST API 发起实际 HTTP 请求。
func (h *Handle) CallTool(ctx context.Context, name string, args []byte) (*mcp.CallToolResult, error) {
	b, ok := h.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	var arg map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arg); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	path := fillPath(b.Path, arg)
	var body io.Reader
	if b.Body && arg != nil {
		if payload, ok := arg["body"]; ok {
			raw, _ := json.Marshal(payload)
			body = bytes.NewReader(raw)
		}
	}
	req, err := http.NewRequestWithContext(ctx, b.Method, h.g.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	q := req.URL.Query()
	for _, k := range b.Query {
		if v, ok := arg[k]; ok {
			q.Set(k, fmt.Sprintf("%v", v))
		}
	}
	req.URL.RawQuery = q.Encode()

	resp, err := h.g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	text := string(bodyBytes)

	if resp.StatusCode >= 400 {
		return &mcp.CallToolResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: text}},
			IsError: true,
		}, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: text}},
	}, nil
}

// ---- helpers ----

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func fillPath(tpl string, arg map[string]any) string {
	out := tpl
	for k, v := range arg {
		ph := "{" + k + "}"
		if strings.Contains(out, ph) {
			out = strings.ReplaceAll(out, ph, urlEscape(fmt.Sprintf("%v", v)))
		}
	}
	return out
}

func urlEscape(s string) string {
	return strings.NewReplacer(" ", "%20", "/", "%2F").Replace(s)
}

func schemaToAny(s any) any {
	if s == nil {
		return map[string]any{"type": "string"}
	}
	if m, ok := s.(map[string]any); ok {
		if ref, ok := m["$ref"].(string); ok {
			return map[string]any{"$ref": ref}
		}
		return m
	}
	return s
}

