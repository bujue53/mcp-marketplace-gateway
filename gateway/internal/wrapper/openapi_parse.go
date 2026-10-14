package wrapper

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse 接受 OpenAPI 3.0/3.1 JSON 或 YAML，并把组件引用规范化为独立工具可用的 Schema。
func Parse(raw []byte) (*OpenAPI, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		raw = []byte(text)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		if err := yaml.Unmarshal(raw, &root); err != nil {
			return nil, fmt.Errorf("parse OpenAPI JSON/YAML: %w", err)
		}
	}
	if root == nil {
		return nil, fmt.Errorf("OpenAPI document must be an object")
	}
	version, _ := root["openapi"].(string)
	parts := strings.Split(version, ".")
	if len(parts) != 3 || parts[0] != "3" || (parts[1] != "0" && parts[1] != "1") {
		return nil, fmt.Errorf("supported OpenAPI versions are 3.0.x and 3.1.x")
	}
	if _, err := strconv.Atoi(parts[2]); err != nil || strings.Trim(parts[2], "0123456789") != "" {
		return nil, fmt.Errorf("invalid OpenAPI version")
	}
	clean, err := yamlJSONValue(root)
	if err != nil {
		return nil, err
	}
	root = clean.(map[string]any)
	resolved, err := resolveOpenAPIValue(root, root, map[string]bool{}, 0)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(resolved)
	if err != nil {
		return nil, err
	}
	var doc OpenAPI
	if err := json.Unmarshal(encoded, &doc); err != nil {
		return nil, fmt.Errorf("invalid OpenAPI structure: %w", err)
	}
	if _, err := NewGenerator("").Build(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func yamlJSONValue(value any) (any, error) {
	switch value := value.(type) {
	case map[any]any:
		out := map[string]any{}
		for key, item := range value {
			var name string
			switch key := key.(type) {
			case string:
				name = key
			case int:
				name = strconv.Itoa(key)
			default:
				return nil, fmt.Errorf("YAML mapping keys must be strings or response codes")
			}
			converted, err := yamlJSONValue(item)
			if err != nil {
				return nil, err
			}
			out[name] = converted
		}
		return out, nil
	case map[string]any:
		out := map[string]any{}
		for key, item := range value {
			converted, err := yamlJSONValue(item)
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			converted, err := yamlJSONValue(item)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil
	default:
		return value, nil
	}
}

func openAPIPointer(root map[string]any, ref string) (any, error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("external reference %q is unsupported; bundle it into components first", ref)
	}
	path, err := url.PathUnescape(strings.TrimPrefix(ref, "#/"))
	if err != nil {
		return nil, err
	}
	var value any = root
	for _, part := range strings.Split(path, "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch object := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = object[part]
			if !ok {
				return nil, fmt.Errorf("unresolved reference %q", ref)
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(object) || strconv.Itoa(index) != part {
				return nil, fmt.Errorf("unresolved reference %q", ref)
			}
			value = object[index]
		default:
			return nil, fmt.Errorf("reference %q does not point to an object", ref)
		}
	}
	return value, nil
}

func resolveOpenAPIValue(root map[string]any, value any, stack map[string]bool, depth int) (any, error) {
	if depth > 128 {
		return nil, fmt.Errorf("OpenAPI nesting/reference depth exceeds 128")
	}
	switch object := value.(type) {
	case map[string]any:
		out := map[string]any{}
		if value, exists := object["$ref"]; exists {
			if _, ok := value.(string); !ok {
				return nil, fmt.Errorf("$ref must be a string")
			}
		}
		if ref, ok := object["$ref"].(string); ok {
			target, err := openAPIPointer(root, ref)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(ref, "#/components/schemas/") {
				// 保留 Schema 引用与递归结构，并改写到工具自身的 $defs。
				out["$ref"] = "#/$defs/" + strings.TrimPrefix(ref, "#/components/schemas/")
			} else {
				if stack[ref] {
					return nil, fmt.Errorf("cyclic non-schema reference %q", ref)
				}
				stack[ref] = true
				resolved, err := resolveOpenAPIValue(root, target, stack, depth+1)
				delete(stack, ref)
				if err != nil {
					return nil, err
				}
				fields, ok := resolved.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("reference %q must target an object", ref)
				}
				for key, value := range fields {
					out[key] = value
				}
			}
		}
		for key, item := range object {
			if key == "$ref" {
				continue
			}
			if key == "example" || key == "examples" || key == "default" || key == "enum" || key == "const" {
				out[key] = item
				continue
			}
			resolved, err := resolveOpenAPIValue(root, item, stack, depth+1)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(object))
		for i, item := range object {
			resolved, err := resolveOpenAPIValue(root, item, stack, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	default:
		return value, nil
	}
}
