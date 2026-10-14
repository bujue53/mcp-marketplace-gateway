package api

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestArgumentSummaryRedactsOpenAPICredentials(t *testing.T) {
	result := summarizeArgs(json.RawMessage(`{"id":"visible","auth":{"token":"secret-bearer"},"headers":{"X-API-Key":"secret-key"},"cookies":{"session":"secret-session"}}`))
	for _, secret := range []string{"secret-bearer", "secret-key", "secret-session"} {
		if strings.Contains(result, secret) {
			t.Fatal("credential leaked to call summary")
		}
	}
	if !strings.Contains(result, "visible") || !strings.Contains(result, "[redacted]") {
		t.Fatal(result)
	}
}

func TestArgumentSummaryTruncatesAtUTF8Boundary(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"text": strings.Repeat("中文", 100)})
	result := summarizeArgs(raw)
	if !utf8.ValidString(result) || len(result) > 256 {
		t.Fatal("summary is invalid UTF-8 or exceeds limit")
	}
}
