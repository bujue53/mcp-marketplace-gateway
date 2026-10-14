package wrapper

import (
	"embed"
	"encoding/json"
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"

	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/mcp"
)

//go:embed templates/*.tmpl
var skeletonTemplates embed.FS

//go:embed openapi_request.go
var requestHelpers string

type SkeletonProject struct {
	Lang  string            `json:"lang"`
	Code  string            `json:"code"`
	Files map[string]string `json:"files"`
	Run   []string          `json:"run"`
}

type skeletonBinding struct {
	Tool mcp.Tool `json:"tool"`
	RequestBinding
}

// Skeleton 保留单文件源码接口；API 同时提供独立项目文件与运行命令。
func Skeleton(doc *OpenAPI, lang string) (string, error) {
	project, err := GenerateSkeletonProject(doc, lang, "")
	if err != nil {
		return "", err
	}
	return project.Code, nil
}

func GenerateSkeletonProject(doc *OpenAPI, lang, baseURL string) (*SkeletonProject, error) {
	if doc == nil {
		return nil, fmt.Errorf("openapi document is required")
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "", "go", "golang":
		lang = "go"
	case "ts", "typescript":
		lang = "ts"
	default:
		return nil, fmt.Errorf("unsupported language %q", lang)
	}
	tools, err := NewGenerator(baseURL).Build(doc)
	if err != nil {
		return nil, err
	}
	override := baseURL
	if baseURL == "" && len(tools) > 0 {
		baseURL = tools[0].BaseURL
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Tool.Name < tools[j].Tool.Name })
	bindings := make([]skeletonBinding, 0, len(tools))
	for i, tool := range tools {
		if i > 0 && tools[i-1].Tool.Name == tool.Tool.Name {
			return nil, fmt.Errorf("duplicate tool name %q; use unique operationId values", tool.Tool.Name)
		}
		bindings = append(bindings, skeletonBinding{Tool: tool.Tool, RequestBinding: tool.RequestBinding})
	}
	name, version := doc.Info.Title, doc.Info.Version
	if name == "" {
		name = "generated-mcp"
	}
	if version == "" {
		version = "1.0.0"
	}
	metadata, err := json.Marshal(struct {
		Name            string            `json:"name"`
		Version         string            `json:"version"`
		BaseURL         string            `json:"baseURL"`
		OverrideBaseURL string            `json:"overrideBaseURL"`
		Bindings        []skeletonBinding `json:"bindings"`
	}{name, version, baseURL, override, bindings})
	if err != nil {
		return nil, err
	}
	template, err := skeletonTemplates.ReadFile("templates/server." + lang + ".tmpl")
	if err != nil {
		return nil, err
	}
	var literal string
	if lang == "go" {
		literal = strconv.Quote(string(metadata))
	} else {
		quoted, _ := json.Marshal(string(metadata))
		literal = string(quoted)
	}
	code := strings.Replace(string(template), "__METADATA__", literal, 1)
	_, helpers, found := strings.Cut(strings.ReplaceAll(requestHelpers, "\r\n", "\n"), "// request helpers begin\n")
	if !found {
		return nil, fmt.Errorf("generated request helpers marker is missing")
	}
	code = strings.Replace(code, "__REQUEST_HELPERS__", helpers, 1)
	project := &SkeletonProject{Lang: lang, Files: map[string]string{}}
	if lang == "go" {
		formatted, err := format.Source([]byte(code))
		if err != nil {
			return nil, fmt.Errorf("format generated Go: %w", err)
		}
		code = string(formatted)
		project.Files["main.go"] = code
		project.Files["go.mod"] = "module generated-mcp\n\ngo 1.22\n"
		project.Run = []string{"go build -o mcp-server .", "go run ."}
	} else {
		project.Files["server.ts"] = code
		project.Files["package.json"] = `{"name":"generated-mcp","version":"1.0.0","private":true,"type":"module","scripts":{"build":"tsc -p tsconfig.json","start":"node dist/server.js"},"devDependencies":{"typescript":"5.6.3","@types/node":"22.7.4"}}` + "\n"
		project.Files["tsconfig.json"] = `{"compilerOptions":{"target":"ES2022","module":"NodeNext","moduleResolution":"NodeNext","strict":true,"outDir":"dist","lib":["ES2022","DOM"],"skipLibCheck":true},"include":["server.ts"]}` + "\n"
		project.Run = []string{"npm install", "npm run build", "npm start"}
	}
	project.Code = code
	return project, nil
}
