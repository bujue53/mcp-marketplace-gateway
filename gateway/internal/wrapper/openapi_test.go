package wrapper

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const compatibilityYAML = `openapi: 3.1.0
info: {title: Compatibility, version: '1.0'}
servers:
  - url: 'http://{host}/api'
    variables:
      host: {default: example.test}
security: [{token: []}]
components:
  schemas:
    Node:
      type: object
      properties:
        name: {type: string}
        child: {$ref: '#/components/schemas/Node'}
  parameters:
    ID: {name: id, in: path, required: true, schema: {type: string}}
    Tenant: {name: X-Tenant, in: header, required: true, schema: {type: string}}
  requestBodies:
    Payload:
      required: true
      content:
        application/json:
          schema: {$ref: '#/components/schemas/Node'}
  responses:
    Result:
      description: OK
      content:
        application/json:
          schema: {$ref: '#/components/schemas/Node'}
  securitySchemes:
    token: {type: http, scheme: bearer}
    key: {type: apiKey, in: header, name: X-API-Key}
    queryKey: {type: apiKey, in: query, name: secret}
    cookieKey: {type: apiKey, in: cookie, name: sessionKey}
paths:
  /items/{id}:
    parameters:
      - {$ref: '#/components/parameters/ID'}
      - {name: q, in: query, required: true, schema: {type: string}}
    get:
      operationId: read
      parameters:
        - {name: q, in: query, required: false, schema: {type: string}}
        - {$ref: '#/components/parameters/Tenant'}
        - {name: tags, in: query, schema: {type: array, items: {type: string}}}
        - {name: filter, in: query, style: deepObject, explode: true, schema: {type: object}}
        - {name: X-Flags, in: header, schema: {type: array, items: {type: string}}}
        - {name: session, in: cookie, schema: {type: string}}
      security: [{key: []}, {token: []}]
      responses: {200: {$ref: '#/components/responses/Result'}}
    post:
      operationId: write
      requestBody: {$ref: '#/components/requestBodies/Payload'}
  /form:
    post:
      operationId: form
      security: []
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded: {schema: {type: object}}
  /text:
    put:
      operationId: text
      security: [{queryKey: [], cookieKey: []}]
      requestBody:
        content:
          text/plain: {schema: {type: string}}
  /health:
    head: {operationId: head, security: []}
    options: {operationId: options, security: []}
`

func TestOpenAPICompatibilityCalls(t *testing.T) {
	var observed struct{ Method, URI, Body, ContentType, Tenant, Flags, Key, Bearer, Cookie string }
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		observed.Method = r.Method
		observed.URI = r.RequestURI
		observed.Body = string(raw)
		observed.ContentType = r.Header.Get("Content-Type")
		observed.Tenant = r.Header.Get("X-Tenant")
		observed.Flags = r.Header.Get("X-Flags")
		observed.Key = r.Header.Get("X-API-Key")
		observed.Bearer = r.Header.Get("Authorization")
		observed.Cookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"ok"}`))
	}))
	defer mock.Close()
	doc, err := Parse([]byte(compatibilityYAML))
	if err != nil {
		t.Fatal(err)
	}
	generator := NewGenerator(mock.URL + "/api")
	generated, err := generator.Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	handler := generator.Handler(generated)
	call := func(name string, args string) {
		t.Helper()
		result, err := handler.CallTool(context.Background(), name, []byte(args))
		if err != nil || result.IsError {
			t.Fatalf("%s: result=%v err=%v", name, result, err)
		}
	}
	call("read", `{"id":"用户 /?#汉","tags":["a&b","中文"],"filter":{"active":true,"name":"x+y"},"headers":{"X-Tenant":"tenant","X-Flags":["one","two"]},"cookies":{"session":"中文 value"},"auth":{"key":"qa-key"}}`)
	parsed, err := url.Parse(observed.URI)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/api/items/用户 /?#汉" {
		t.Fatalf("path escaping: %s", observed.URI)
	}
	query := parsed.Query()
	if strings.Join(query["tags"], ",") != "a&b,中文" || query.Get("filter[active]") != "true" || query.Get("filter[name]") != "x+y" {
		t.Fatalf("query serialization: %v", query)
	}
	if observed.Tenant != "tenant" || observed.Flags != "one,two" || observed.Key != "qa-key" || observed.Bearer != "" || !strings.Contains(observed.Cookie, "session=%E4%B8%AD%E6%96%87+value") {
		t.Fatalf("headers/cookie/auth: %+v", observed)
	}
	call("write", `{"id":"1","q":"yes","body":{"name":"parent","child":{"name":"child"}},"auth":{"token":"qa-bearer"}}`)
	if observed.Bearer != "Bearer qa-bearer" || observed.ContentType != "application/json" || !strings.Contains(observed.Body, `"child"`) {
		t.Fatalf("JSON body/bearer: %+v", observed)
	}
	call("form", `{"body":{"tags":["a&b","中文"],"enabled":true}}`)
	form, _ := url.ParseQuery(observed.Body)
	if form.Get("enabled") != "true" || strings.Join(form["tags"], ",") != "a&b,中文" || observed.ContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("form serialization: %+v", observed)
	}
	call("text", `{"body":"plain 中文","auth":{"queryKey":"key+value","cookieKey":"cookie value"}}`)
	parsed, _ = url.Parse(observed.URI)
	if observed.Body != "plain 中文" || observed.ContentType != "text/plain" || parsed.Query().Get("secret") != "key+value" || !strings.Contains(observed.Cookie, "sessionKey=cookie+value") {
		t.Fatalf("text/security AND: %+v", observed)
	}
	for _, method := range []string{"head", "options"} {
		call(method, `{}`)
		if observed.Method != strings.ToUpper(method) {
			t.Fatal(observed.Method)
		}
	}
	for _, args := range []string{`{"id":"1","headers":{"X-Tenant":"tenant"}}`, `{"id":"1","auth":{"key":"key"}}`} {
		if _, err := handler.CallTool(context.Background(), "read", []byte(args)); err == nil {
			t.Fatal("missing nested required argument or auth accepted")
		}
	}
	for _, tool := range generated {
		if tool.Tool.Name != "read" {
			continue
		}
		var schema map[string]any
		_ = json.Unmarshal(tool.Tool.InputSchema, &schema)
		defs := schema["$defs"].(map[string]any)
		child := defs["Node"].(map[string]any)["properties"].(map[string]any)["child"].(map[string]any)
		if child["$ref"] != "#/$defs/Node" {
			t.Fatal("recursive schema reference lost")
		}
		_ = json.Unmarshal(tool.OutputSchema, &schema)
		if schema["$ref"] != "#/$defs/Node" || schema["$defs"] == nil {
			t.Fatal("response schema lost")
		}
	}
	quoted, _ := json.Marshal(compatibilityYAML)
	if _, err := Parse(quoted); err != nil {
		t.Fatalf("YAML spec string: %v", err)
	}
}

func TestOpenAPIRejectsUnsupportedAndInvalidDocuments(t *testing.T) {
	cases := []string{
		`{"openapi":"2.0","paths":{}}`,
		`{"openapi":"3.0.-1","paths":{}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"get":{"parameters":[{"name":"session","in":"cookie","schema":{"type":"array"}}]}}}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"get":{"parameters":[{"name":"filter","in":"query","style":"deepObject","explode":false}]}}}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"get":{"parameters":[{"$ref":"other.yaml#/x"}]}}}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"get":{"parameters":[{"$ref":"#/components/parameters/missing"}]}}}}`,
		`{"openapi":"3.0.3","components":{"parameters":{"loop":{"$ref":"#/components/parameters/loop"}}},"paths":{}}`,
		`{"openapi":"3.0.3","paths":{"/x":null}}`,
		`{"openapi":"3.0.3","paths":{"/x/{id}":{"get":{}}}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"get":{"parameters":[{"name":"q","in":"query","style":"matrix"}]}}}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"post":{"requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object"}}}}}}}}`,
		`{"openapi":"3.0.3","security":[{"oauth":[]}],"components":{"securitySchemes":{"oauth":{"type":"oauth2"}}},"paths":{"/x":{"get":{}}}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"get":{"parameters":[{"name":"q","in":"query"},{"name":"q","in":"query"}]}}}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"get":{"operationId":"same"}},"/y":{"get":{"operationId":"same"}}}}`,
		`{"openapi":"3.0.3","paths":{"/x":{"$ref":42}}}`,
	}
	for _, raw := range cases {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("invalid input accepted: %s", raw)
		}
	}
}

func TestOpenAPIGroupedNamesStylesAndServers(t *testing.T) {
	raw := `{"openapi":"3.0.3","servers":[{"url":"http://root.test"}],"paths":{"/{id}":{"servers":[{"url":"http://path.test"}],"get":{"servers":[{"url":"http://operation.test"}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}},{"name":"id","in":"query"},{"name":"auth","in":"query"},{"name":"space","in":"query","style":"spaceDelimited"},{"name":"pipe","in":"query","style":"pipeDelimited"},{"name":"flat","in":"query","explode":false}]}}}}`
	doc, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	tools, err := NewGenerator("").Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	if tools[0].Tool.Name != "GET___id_" || SkillsToolName("test-skill") != "skill_test-skill" {
		t.Fatal("existing default names changed")
	}
	if strings.Contains(safeRESTError(&url.Error{Op: "Get", URL: "http://example.test?key=secret", Err: io.EOF}).Error(), "secret") {
		t.Fatal("request URL leaked through error")
	}
	args := map[string]any{}
	_ = json.Unmarshal([]byte(`{"path":{"id":"a/b"},"query":{"id":"q","auth":"normal"},"space":["a","b"],"pipe":["x","y"],"flat":{"b":2,"a":1}}`), &args)
	req, err := newRESTRequest(context.Background(), tools[0].RequestBinding, "", args, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Host != "operation.test" || req.URL.EscapedPath() != "/a%2Fb" || req.URL.Query().Get("id") != "q" || req.URL.Query().Get("auth") != "normal" || req.URL.Query().Get("space") != "a b" || req.URL.Query().Get("pipe") != "x|y" || req.URL.Query().Get("flat") != "a,1,b,2" {
		t.Fatal(req.URL.String())
	}
	req, err = newRESTRequest(context.Background(), tools[0].RequestBinding, "https://override.test/base", args, "", "")
	if err != nil || req.URL.Host != "override.test" {
		t.Fatalf("override: %v %v", req, err)
	}
}
