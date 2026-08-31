package wrapper

import (
	"fmt"
	"strings"
)

// Skeleton 依据 OpenAPI 文档生成 MCP Server 骨架代码（Go 或 TypeScript）。
// 生成的骨架可自行编译/补齐工具转发逻辑，交付「自动生成 MCP Server」素材。
func Skeleton(doc *OpenAPI, lang string) (string, error) {
	tools := NewGenerator("").Build(doc)
	switch strings.ToLower(lang) {
	case "go", "golang":
		return skeletonGo(doc, tools), nil
	case "ts", "typescript":
		return skeletonTS(doc, tools), nil
	default:
		ids := []string{}
		for _, t := range tools {
			ids = append(ids, t.Tool.Name)
		}
		return "", fmt.Errorf("unsupported language %q; tools: %v", lang, ids)
	}
}

func skeletonGo(doc *OpenAPI, tools []GeneratedTool) string {
	name := sanitizeName(doc.Info.Title)
	if name == "" {
		name = "generatedMCP"
	}
	var b strings.Builder
	b.WriteString(`// 由 OpenAPI→MCP 自动生成器生成的 MCP Server 骨架（Go）
// 编译: go mod init ` + name + ` && go run main.go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", handleMCP)
	fmt.Println("mcp server listening on :8080")
	_ = http.ListenAndServe(":8080", mux)
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	// 骨架：此处实现 MCP initialize / tools/list / tools/call 分发。
	// tools:
`)
	for _, t := range tools {
		b.WriteString(fmt.Sprintf("\t//   - %s\n", t.Tool.Name))
	}
	b.WriteString(`}
`)
	return b.String()
}

func skeletonTS(doc *OpenAPI, tools []GeneratedTool) string {
	name := sanitizeName(doc.Info.Title)
	if name == "" {
		name = "generatedMCP"
	}
	var b strings.Builder
	b.WriteString("// 由 OpenAPI→MCP 自动生成器生成的 MCP Server 骨架（TypeScript）\n")
	b.WriteString("// 运行: npm i @modelcontextprotocol/sdk && npx tsx index.ts\n")
	b.WriteString("import { Server } from '@modelcontextprotocol/sdk/server/index.js';\nimport { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';\n\n")
	b.WriteString(fmt.Sprintf("const server = new Server({ name: '%s', version: '%s' }, { capabilities: { tools: {} } });\n\n", name, doc.Info.Version))
	b.WriteString("server.setRequestHandler(ListToolsRequestSchema, async () => {\n  return { tools: [\n")
	for _, t := range tools {
		b.WriteString(fmt.Sprintf("    { name: %q, description: %q, inputSchema: %s },\n",
			t.Tool.Name, t.Tool.Description, string(t.Tool.InputSchema)))
	}
	b.WriteString("  ] };\n});\n\n")
	b.WriteString(`server.setRequestHandler(CallToolRequestSchema, async (req) => {
  // 根据 req.params.name 分发到对应实现。
  return { content: [{ type: 'text', text: '骨架占位响应' }] };
});

await server.connect(new StdioServerTransport());
`)
	return b.String()
}
