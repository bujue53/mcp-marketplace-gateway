package mcp

import (
	"encoding/json"
	"testing"
)

func TestResponseIncludesJSONRPCVersionAndNullID(t *testing.T) {
	raw, err := json.Marshal(JSONRPCResponse{Error: &JSONRPCError{Code: -32700, Message: "parse error"}})
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	_ = json.Unmarshal(raw, &message)
	if message["jsonrpc"] != "2.0" {
		t.Fatal(string(raw))
	}
	if id, exists := message["id"]; !exists || id != nil {
		t.Fatal("missing null error ID")
	}
}
