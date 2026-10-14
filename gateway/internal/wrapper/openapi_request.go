package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// request helpers begin
// 同一份请求映射逻辑也嵌入独立 Go Server，避免生成代码与网关行为分叉。
type ParameterBinding struct {
	Name     string   `json:"name"`
	In       string   `json:"in"`
	Argument []string `json:"argument"`
	Required bool     `json:"required"`
	Style    string   `json:"style"`
	Explode  bool     `json:"explode"`
}
type SecurityBinding struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	In     string `json:"in"`
	Name   string `json:"name"`
	Scheme string `json:"scheme"`
}
type RequestBinding struct {
	Method       string              `json:"method"`
	Path         string              `json:"path"`
	BaseURL      string              `json:"baseURL"`
	Parameters   []ParameterBinding  `json:"parameters"`
	Security     [][]SecurityBinding `json:"security"`
	Body         bool                `json:"body"`
	BodyRequired bool                `json:"bodyRequired"`
	ContentType  string              `json:"contentType"`
}

func argumentValue(args map[string]any, keys []string) (any, bool) {
	var current any = args
	for _, key := range keys {
		values, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = values[key]
		if !ok || current == nil {
			return nil, false
		}
	}
	return current, true
}

func parameterScalar(value any) (string, error) {
	switch value.(type) {
	case string, bool, float64, json.Number:
		return fmt.Sprint(value), nil
	default:
		return "", fmt.Errorf("parameter value must be a string, number or boolean")
	}
}

func orderedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func simpleParameter(value any, explode, escape bool) (string, error) {
	encode := func(value any) (string, error) {
		text, err := parameterScalar(value)
		if escape {
			text = url.PathEscape(text)
		}
		return text, err
	}
	parts := []string{}
	switch values := value.(type) {
	case []any:
		for _, item := range values {
			text, err := encode(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, text)
		}
	case map[string]any:
		for _, key := range orderedKeys(values) {
			text, err := encode(values[key])
			if err != nil {
				return "", err
			}
			if escape {
				key = url.PathEscape(key)
			}
			if explode {
				parts = append(parts, key+"="+text)
			} else {
				parts = append(parts, key, text)
			}
		}
	default:
		return encode(value)
	}
	return strings.Join(parts, ","), nil
}

func addParameter(values url.Values, p ParameterBinding, value any) error {
	if p.Style == "deepObject" {
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", p.Name)
		}
		for _, key := range orderedKeys(object) {
			text, err := parameterScalar(object[key])
			if err != nil {
				return err
			}
			values.Add(p.Name+"["+key+"]", text)
		}
		return nil
	}
	if p.Style == "spaceDelimited" || p.Style == "pipeDelimited" {
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", p.Name)
		}
		parts := []string{}
		for _, item := range array {
			text, err := parameterScalar(item)
			if err != nil {
				return err
			}
			parts = append(parts, text)
		}
		separator := " "
		if p.Style == "pipeDelimited" {
			separator = "|"
		}
		values.Add(p.Name, strings.Join(parts, separator))
		return nil
	}
	if p.Explode {
		switch object := value.(type) {
		case []any:
			for _, item := range object {
				text, err := parameterScalar(item)
				if err != nil {
					return err
				}
				values.Add(p.Name, text)
			}
			return nil
		case map[string]any:
			for _, key := range orderedKeys(object) {
				text, err := parameterScalar(object[key])
				if err != nil {
					return err
				}
				values.Add(key, text)
			}
			return nil
		}
	}
	text, err := simpleParameter(value, false, false)
	if err != nil {
		return err
	}
	values.Add(p.Name, text)
	return nil
}

func applySecurity(binding RequestBinding, args map[string]any, headers http.Header, query, cookies url.Values, bearer string) error {
	if len(binding.Security) == 0 {
		return nil
	}
	auth, _ := args["auth"].(map[string]any)
	for _, alternative := range binding.Security {
		credentials := make([]string, len(alternative))
		complete := true
		for i, scheme := range alternative {
			credentials[i], _ = auth[scheme.Key].(string)
			if credentials[i] == "" && scheme.Type == "http" && scheme.Scheme == "bearer" {
				credentials[i] = bearer
			}
			if credentials[i] == "" {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		for i, scheme := range alternative {
			if scheme.Type == "http" {
				headers.Set("Authorization", "Bearer "+credentials[i])
				continue
			}
			switch scheme.In {
			case "header":
				headers.Set(scheme.Name, credentials[i])
			case "query":
				query.Set(scheme.Name, credentials[i])
			case "cookie":
				cookies.Set(scheme.Name, credentials[i])
			}
		}
		return nil
	}
	return fmt.Errorf("authentication arguments do not satisfy any OpenAPI security requirement")
}

func newRESTRequest(ctx context.Context, binding RequestBinding, override string, args map[string]any, traceparent, bearer string) (*http.Request, error) {
	baseURL := binding.BaseURL
	if override != "" {
		baseURL = override
	}
	if err := validateRESTBaseURL(baseURL); err != nil {
		return nil, err
	}
	path := binding.Path
	query, cookies := url.Values{}, url.Values{}
	headers := http.Header{"Accept": []string{"application/json"}}
	if bearer != "" {
		headers.Set("Authorization", "Bearer "+bearer)
	}
	for _, parameter := range binding.Parameters {
		value, present := argumentValue(args, parameter.Argument)
		if !present {
			if parameter.Required {
				return nil, fmt.Errorf("missing argument: %s", strings.Join(parameter.Argument, "."))
			}
			continue
		}
		switch parameter.In {
		case "path":
			text, err := simpleParameter(value, parameter.Explode, true)
			if err != nil {
				return nil, err
			}
			path = strings.ReplaceAll(path, "{"+parameter.Name+"}", text)
		case "query":
			if err := addParameter(query, parameter, value); err != nil {
				return nil, err
			}
		case "header":
			text, err := simpleParameter(value, parameter.Explode, false)
			if err != nil {
				return nil, err
			}
			headers.Set(parameter.Name, text)
		case "cookie":
			text, err := parameterScalar(value)
			if err != nil {
				return nil, fmt.Errorf("cookie parameter %s only supports scalar values", parameter.Name)
			}
			cookies.Add(parameter.Name, text)
		}
	}
	if err := applySecurity(binding, args, headers, query, cookies, bearer); err != nil {
		return nil, err
	}
	target, err := url.Parse(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return nil, err
	}
	target.RawQuery = query.Encode()
	var body io.Reader
	if payload, present := args["body"]; binding.Body && present && payload != nil {
		switch {
		case binding.ContentType == "application/x-www-form-urlencoded":
			object, ok := payload.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("form body must be an object")
			}
			values := url.Values{}
			for _, key := range orderedKeys(object) {
				if err := addParameter(values, ParameterBinding{Name: key, Style: "form", Explode: true}, object[key]); err != nil {
					return nil, err
				}
			}
			body = strings.NewReader(values.Encode())
		case binding.ContentType == "text/plain":
			text, ok := payload.(string)
			if !ok {
				return nil, fmt.Errorf("text body must be a string")
			}
			body = strings.NewReader(text)
		default:
			encoded, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			body = bytes.NewReader(encoded)
		}
	} else if binding.BodyRequired {
		return nil, fmt.Errorf("missing argument: body")
	}
	req, err := http.NewRequestWithContext(ctx, binding.Method, target.String(), body)
	if err != nil {
		return nil, err
	}
	if binding.Body {
		headers.Set("Content-Type", binding.ContentType)
	}
	if traceparent != "" {
		headers.Set("traceparent", traceparent)
	}
	req.Header = headers
	keys := make([]string, 0, len(cookies))
	for key := range cookies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range cookies[key] {
			req.AddCookie(&http.Cookie{Name: key, Value: url.QueryEscape(value)})
		}
	}
	return req, nil
}

func validateRESTBaseURL(value string) error {
	base, err := url.Parse(value)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || strings.ContainsAny(base.Path, "{}") {
		return fmt.Errorf("REST base URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	return nil
}

func safeRESTError(err error) error {
	// http.Client 的 url.Error 包含完整 URL；去掉外层避免 query API key 进入审计链路。
	if requestError, ok := err.(*url.Error); ok {
		err = requestError.Err
	}
	return fmt.Errorf("REST request failed: %w", err)
}
