package wrapper

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestSkeletonEscapesMetadataAndProducesStableProjects(t *testing.T) {
	doc, err := Parse([]byte(`{"openapi":"3.0.3","info":{"title":"quoted \"name\"\n世界","version":"1.0.0"},"servers":[{"url":"http://example.test"}],"paths":{"/b":{"get":{"operationId":"b","summary":"quoted \"description\"\nline"}},"/a":{"get":{"operationId":"a"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"go", "ts"} {
		first, err := GenerateSkeletonProject(doc, lang, "")
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 20; i++ {
			next, err := GenerateSkeletonProject(doc, lang, "")
			if err != nil || next.Code != first.Code {
				t.Fatalf("%s output depends on map iteration order: %v", lang, err)
			}
		}
		if lang == "go" {
			if _, err := parser.ParseFile(token.NewFileSet(), "main.go", first.Files["main.go"], parser.AllErrors); err != nil {
				t.Fatalf("OpenAPI text broke generated Go syntax: %v", err)
			}
		}
	}
}

func TestSkeletonRejectsAmbiguousNamesAndUnsupportedLanguages(t *testing.T) {
	doc := &OpenAPI{Paths: map[string]*PathItem{
		"/a": {Get: &Operation{OperationID: "duplicate"}},
		"/b": {Post: &Operation{OperationID: "duplicate"}},
	}}
	if _, err := GenerateSkeletonProject(doc, "go", ""); err == nil {
		t.Fatal("ambiguous tool names were accepted")
	}
	if _, err := GenerateSkeletonProject(&OpenAPI{}, "python", ""); err == nil {
		t.Fatal("unsupported language silently generated Go")
	}
	if _, err := GenerateSkeletonProject(nil, "go", ""); err == nil {
		t.Fatal("nil OpenAPI document was accepted")
	}
}
