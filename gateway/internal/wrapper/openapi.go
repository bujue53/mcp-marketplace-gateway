package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/mcp"
	telemetry "github.com/bujue53/mcp-marketplace-gateway/gateway/internal/otel"
)

type OpenAPI struct {
	OpenAPI    string                `json:"openapi"`
	Info       Info                  `json:"info"`
	Servers    []Server              `json:"servers"`
	Paths      map[string]*PathItem  `json:"paths"`
	Components Components            `json:"components"`
	Security   []map[string][]string `json:"security"`
}
type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}
type Server struct {
	URL       string                    `json:"url"`
	Variables map[string]ServerVariable `json:"variables"`
}
type ServerVariable struct {
	Default string `json:"default"`
}
type Components struct {
	Schemas         map[string]json.RawMessage `json:"schemas"`
	SecuritySchemes map[string]SecurityScheme  `json:"securitySchemes"`
}
type SecurityScheme struct {
	Type   string `json:"type"`
	In     string `json:"in"`
	Name   string `json:"name"`
	Scheme string `json:"scheme"`
}
type PathItem struct {
	Get        *Operation  `json:"get"`
	Post       *Operation  `json:"post"`
	Put        *Operation  `json:"put"`
	Delete     *Operation  `json:"delete"`
	Patch      *Operation  `json:"patch"`
	Head       *Operation  `json:"head"`
	Options    *Operation  `json:"options"`
	Trace      *Operation  `json:"trace"`
	Parameters []Parameter `json:"parameters"`
	Servers    []Server    `json:"servers"`
}
type Operation struct {
	OperationID string                 `json:"operationId"`
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	Tags        []string               `json:"tags"`
	Parameters  []Parameter            `json:"parameters"`
	RequestBody *RequestBody           `json:"requestBody"`
	Responses   map[string]*Response   `json:"responses"`
	Servers     []Server               `json:"servers"`
	Security    *[]map[string][]string `json:"security"`
}
type Parameter struct {
	Name          string                `json:"name"`
	In            string                `json:"in"`
	Required      bool                  `json:"required"`
	Description   string                `json:"description"`
	Schema        any                   `json:"schema"`
	Style         string                `json:"style"`
	Explode       *bool                 `json:"explode"`
	Content       map[string]*MediaType `json:"content"`
	AllowReserved bool                  `json:"allowReserved"`
}
type RequestBody struct {
	Required bool                  `json:"required"`
	Content  map[string]*MediaType `json:"content"`
}
type MediaType struct {
	Schema   any                        `json:"schema"`
	Encoding map[string]json.RawMessage `json:"encoding"`
}
type Response struct {
	Description string                `json:"description"`
	Content     map[string]*MediaType `json:"content"`
}
type GeneratedTool struct {
	Tool mcp.Tool
	RequestBinding
	OutputSchema json.RawMessage
}
type Generator struct {
	BaseURL string
	Client  *http.Client
}

func NewGenerator(baseURL string) *Generator {
	return &Generator{BaseURL: baseURL, Client: &http.Client{Timeout: 10 * time.Second, Transport: telemetry.NewTransport(nil)}}
}

var pathParameter = regexp.MustCompile(`\{([^{}]+)\}`)

func serverURL(servers []Server) string {
	if len(servers) == 0 {
		return ""
	}
	value := servers[0].URL
	for key, variable := range servers[0].Variables {
		value = strings.ReplaceAll(value, "{"+key+"}", variable.Default)
	}
	return value
}

func parameterKey(p Parameter) string {
	name := p.Name
	if p.In == "header" {
		name = strings.ToLower(name)
	}
	return p.In + ":" + name
}
func mergeParameters(common, local []Parameter) ([]Parameter, error) {
	merged := []Parameter{}
	positions := map[string]int{}
	for _, level := range [][]Parameter{common, local} {
		seen := map[string]bool{}
		for _, p := range level {
			key := parameterKey(p)
			if seen[key] {
				return nil, fmt.Errorf("duplicate parameter %s", key)
			}
			seen[key] = true
			if position, ok := positions[key]; ok {
				merged[position] = p
			} else {
				positions[key] = len(merged)
				merged = append(merged, p)
			}
		}
	}
	return merged, nil
}

func parameterBinding(p Parameter, grouped bool) (ParameterBinding, error) {
	if p.Name == "" {
		return ParameterBinding{}, fmt.Errorf("parameter name is required")
	}
	if len(p.Content) > 0 || p.AllowReserved {
		return ParameterBinding{}, fmt.Errorf("parameter %s: content/allowReserved serialization is unsupported", p.Name)
	}
	b := ParameterBinding{Name: p.Name, In: p.In, Required: p.Required, Argument: []string{p.Name}, Style: p.Style}
	switch p.In {
	case "path", "header":
		if b.Style == "" {
			b.Style = "simple"
		}
		if b.Style != "simple" {
			return b, fmt.Errorf("parameter %s only supports simple style in %s", p.Name, p.In)
		}
	case "query":
		if b.Style == "" {
			b.Style = "form"
		}
		if b.Style != "form" && b.Style != "deepObject" && b.Style != "spaceDelimited" && b.Style != "pipeDelimited" {
			return b, fmt.Errorf("unsupported query style %q", b.Style)
		}
	case "cookie":
		if schema, ok := p.Schema.(map[string]any); ok && (schema["type"] == "array" || schema["type"] == "object") {
			return b, fmt.Errorf("cookie parameter %s only supports scalar values", p.Name)
		}
		if b.Style == "" {
			b.Style = "form"
		}
		if b.Style != "form" {
			return b, fmt.Errorf("cookie parameters only support form style")
		}
	default:
		return b, fmt.Errorf("unsupported parameter location %q", p.In)
	}
	b.Explode = b.Style == "form"
	if p.Explode != nil {
		b.Explode = *p.Explode
	}
	if (b.Style == "spaceDelimited" || b.Style == "pipeDelimited") && b.Explode {
		return b, fmt.Errorf("%s only supports explode=false", b.Style)
	}
	if b.Style == "deepObject" && !b.Explode {
		return b, fmt.Errorf("deepObject requires explode=true")
	}
	if p.In == "path" && !p.Required {
		return b, fmt.Errorf("path parameter %s must be required", p.Name)
	}
	if p.In == "header" {
		b.Argument = []string{"headers", p.Name}
	} else if p.In == "cookie" {
		b.Argument = []string{"cookies", p.Name}
	} else if grouped {
		b.Argument = []string{p.In, p.Name}
	}
	return b, nil
}

func inputProperty(schema any, description string) any {
	if schema == nil {
		schema = map[string]any{"type": "string"}
	}
	if description == "" {
		return schema
	}
	if object, ok := schema.(map[string]any); ok {
		cloned := map[string]any{}
		for key, value := range object {
			cloned[key] = value
		}
		if _, exists := cloned["description"]; !exists {
			cloned["description"] = description
		}
		return cloned
	}
	return schema
}
func addInputProperty(properties map[string]any, required *[]string, path []string, schema any, mandatory bool) {
	key := path[0]
	if len(path) == 1 {
		properties[key] = schema
	} else {
		group, exists := properties[key].(map[string]any)
		if !exists {
			group = map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}}
			properties[key] = group
		}
		nested := group["properties"].(map[string]any)
		nested[path[1]] = schema
		if mandatory {
			group["required"] = append(group["required"].([]string), path[1])
		}
	}
	if mandatory {
		for _, item := range *required {
			if item == key {
				return
			}
		}
		*required = append(*required, key)
	}
}
func withDefinitions(schema any, components Components) json.RawMessage {
	var object map[string]any
	if original, ok := schema.(map[string]any); ok {
		object = map[string]any{}
		for key, value := range original {
			object[key] = value
		}
	} else if schema != nil {
		object = map[string]any{"allOf": []any{schema}}
	} else {
		object = map[string]any{}
	}
	if len(components.Schemas) > 0 {
		object["$defs"] = components.Schemas
	}
	raw, _ := json.Marshal(object)
	return raw
}
func selectRequestMedia(content map[string]*MediaType) (string, *MediaType, error) {
	keys := make([]string, 0, len(content))
	for key := range content {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := []string{"application/json"}
	for _, key := range keys {
		if strings.HasSuffix(key, "+json") {
			ordered = append(ordered, key)
		}
	}
	ordered = append(ordered, "application/x-www-form-urlencoded", "text/plain")
	for _, key := range ordered {
		if media, exists := content[key]; exists && media != nil {
			if len(media.Encoding) > 0 {
				return "", nil, fmt.Errorf("custom requestBody encoding is unsupported")
			}
			return key, media, nil
		}
	}
	return "", nil, fmt.Errorf("requestBody supports JSON, +json, form-urlencoded or text/plain media types")
}
func responseSchema(operation *Operation, components Components) json.RawMessage {
	keys := make([]string, 0, len(operation.Responses))
	for key := range operation.Responses {
		if strings.HasPrefix(key, "2") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	keys = append(keys, "default")
	for _, key := range keys {
		response := operation.Responses[key]
		if response == nil {
			continue
		}
		media := response.Content["application/json"]
		if media == nil {
			names := make([]string, 0, len(response.Content))
			for name := range response.Content {
				names = append(names, name)
			}
			sort.Strings(names)
			if len(names) > 0 {
				media = response.Content[names[0]]
			}
		}
		if media != nil && media.Schema != nil {
			return withDefinitions(media.Schema, components)
		}
	}
	return json.RawMessage(`{}`)
}

func effectiveSecurity(doc *OpenAPI, operation *Operation) ([][]SecurityBinding, error) {
	requirements := doc.Security
	if operation.Security != nil {
		requirements = *operation.Security
	}
	alternatives := make([][]SecurityBinding, 0, len(requirements))
	for _, requirement := range requirements {
		keys := make([]string, 0, len(requirement))
		for key := range requirement {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		schemes := []SecurityBinding{}
		for _, key := range keys {
			scheme, ok := doc.Components.SecuritySchemes[key]
			if !ok {
				return nil, fmt.Errorf("unknown security scheme %q", key)
			}
			if scheme.Type == "http" {
				if strings.ToLower(scheme.Scheme) != "bearer" {
					return nil, fmt.Errorf("security scheme %s: only HTTP bearer is supported", key)
				}
			} else if scheme.Type == "apiKey" {
				if scheme.Name == "" || (scheme.In != "header" && scheme.In != "query" && scheme.In != "cookie") {
					return nil, fmt.Errorf("invalid API key scheme %s", key)
				}
			} else {
				return nil, fmt.Errorf("unsupported security scheme %s (%s)", key, scheme.Type)
			}
			if len(requirement[key]) > 0 {
				return nil, fmt.Errorf("security scopes are unsupported for scheme %s", key)
			}
			schemes = append(schemes, SecurityBinding{Key: key, Type: scheme.Type, In: scheme.In, Name: scheme.Name, Scheme: strings.ToLower(scheme.Scheme)})
		}
		alternatives = append(alternatives, schemes)
	}
	return alternatives, nil
}

func (g *Generator) Build(doc *OpenAPI) ([]GeneratedTool, error) {
	if doc == nil {
		return nil, fmt.Errorf("OpenAPI document is required")
	}
	paths := make([]string, 0, len(doc.Paths))
	for path := range doc.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	tools := []GeneratedTool{}
	names := map[string]bool{}
	for _, path := range paths {
		if strings.HasPrefix(path, "x-") {
			continue
		}
		item := doc.Paths[path]
		if item == nil || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
			return nil, fmt.Errorf("invalid OpenAPI path %q", path)
		}
		if item.Trace != nil {
			return nil, fmt.Errorf("TRACE operations are unsupported")
		}
		for _, entry := range []struct {
			method    string
			operation *Operation
		}{{"GET", item.Get}, {"POST", item.Post}, {"PUT", item.Put}, {"DELETE", item.Delete}, {"PATCH", item.Patch}, {"HEAD", item.Head}, {"OPTIONS", item.Options}} {
			operation := entry.operation
			if operation == nil {
				continue
			}
			name := operation.OperationID
			if name == "" {
				name = sanitizeName(entry.method + "_" + path)
			}
			if names[name] {
				return nil, fmt.Errorf("duplicate tool name %q; use unique operationId values", name)
			}
			names[name] = true
			fail := func(err error) ([]GeneratedTool, error) { return nil, fmt.Errorf("%s: %w", name, err) }
			parameters, err := mergeParameters(item.Parameters, operation.Parameters)
			if err != nil {
				return fail(err)
			}
			counts := map[string]int{}
			for _, p := range parameters {
				if p.In == "path" || p.In == "query" {
					counts[p.Name]++
				}
			}
			properties := map[string]any{}
			required := []string{}
			binding := RequestBinding{Method: entry.method, Path: path, BaseURL: serverURL(doc.Servers), Parameters: []ParameterBinding{}}
			if value := serverURL(item.Servers); value != "" {
				binding.BaseURL = value
			}
			if value := serverURL(operation.Servers); value != "" {
				binding.BaseURL = value
			}
			if g.BaseURL != "" {
				binding.BaseURL = g.BaseURL
			}
			if binding.BaseURL != "" {
				if err := validateRESTBaseURL(binding.BaseURL); err != nil {
					return fail(err)
				}
			}
			declaredPaths := map[string]bool{}
			for _, p := range parameters {
				if p.In == "header" && (strings.EqualFold(p.Name, "Accept") || strings.EqualFold(p.Name, "Content-Type") || strings.EqualFold(p.Name, "Authorization")) {
					continue
				}
				reserved := p.Name == "body" || p.Name == "headers" || p.Name == "cookies" || p.Name == "path" || p.Name == "query" || p.Name == "auth"
				parameter, err := parameterBinding(p, counts[p.Name] > 1 || reserved)
				if err != nil {
					return fail(err)
				}
				binding.Parameters = append(binding.Parameters, parameter)
				addInputProperty(properties, &required, parameter.Argument, inputProperty(p.Schema, p.Description), p.Required)
				if p.In == "path" {
					if !strings.Contains(path, "{"+p.Name+"}") {
						return fail(fmt.Errorf("path parameter %s is not in path", p.Name))
					}
					declaredPaths[p.Name] = true
				}
			}
			for _, match := range pathParameter.FindAllStringSubmatch(path, -1) {
				if !declaredPaths[match[1]] {
					return fail(fmt.Errorf("undeclared path parameter %s", match[1]))
				}
			}
			if operation.RequestBody != nil {
				if entry.method == "GET" || entry.method == "HEAD" {
					return fail(fmt.Errorf("requestBody on GET/HEAD is unsupported"))
				}
				contentType, media, err := selectRequestMedia(operation.RequestBody.Content)
				if err != nil {
					return fail(err)
				}
				binding.Body = true
				binding.BodyRequired = operation.RequestBody.Required
				binding.ContentType = contentType
				bodySchema := media.Schema
				if bodySchema == nil {
					bodySchema = map[string]any{}
				}
				addInputProperty(properties, &required, []string{"body"}, bodySchema, binding.BodyRequired)
			}
			binding.Security, err = effectiveSecurity(doc, operation)
			if err != nil {
				return fail(err)
			}
			credentials := map[string]any{}
			for _, alternative := range binding.Security {
				for _, scheme := range alternative {
					credentials[scheme.Key] = map[string]any{"type": "string", "writeOnly": true, "description": "Upstream credential for " + scheme.Key}
				}
			}
			if len(credentials) > 0 {
				properties["auth"] = map[string]any{"type": "object", "properties": credentials}
			}
			description := operation.Summary
			if description == "" {
				description = operation.Description
			}
			if description == "" {
				description = entry.method + " " + path
			}
			schema := withDefinitions(map[string]any{"type": "object", "properties": properties, "required": required}, doc.Components)
			tools = append(tools, GeneratedTool{Tool: mcp.Tool{Name: name, Description: description, InputSchema: schema}, RequestBinding: binding, OutputSchema: responseSchema(operation, doc.Components)})
		}
	}
	return tools, nil
}

type Handle struct {
	generator *Generator
	tools     map[string]GeneratedTool
}

func (g *Generator) Handler(tools []GeneratedTool) *Handle {
	h := &Handle{generator: g, tools: map[string]GeneratedTool{}}
	for _, tool := range tools {
		h.tools[tool.Tool.Name] = tool
	}
	return h
}
func (h *Handle) ListTools() ([]mcp.Tool, error) {
	keys := make([]string, 0, len(h.tools))
	for key := range h.tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]mcp.Tool, 0, len(keys))
	for _, key := range keys {
		result = append(result, h.tools[key].Tool)
	}
	return result, nil
}
func (h *Handle) CallTool(ctx context.Context, name string, raw []byte) (*mcp.CallToolResult, error) {
	binding, ok := h.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %s", name)
	}
	args := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	req, err := newRESTRequest(ctx, binding.RequestBinding, h.generator.BaseURL, args, "", "")
	if err != nil {
		return nil, err
	}
	response, err := h.generator.Client.Do(req)
	if err != nil {
		return nil, safeRESTError(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, fmt.Errorf("REST response exceeds 4 MiB")
	}
	return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: string(data)}}, IsError: response.StatusCode >= 400}, nil
}
func sanitizeName(value string) string {
	var result strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-' {
			result.WriteRune(character)
		} else {
			result.WriteByte('_')
		}
	}
	return result.String()
}
