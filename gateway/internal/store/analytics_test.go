package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnalyticsHTTPProtocolAndErrors(t *testing.T) {
	var insert string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "qa" || password != "secret" {
			t.Error("missing ClickHouse basic auth")
		}
		if r.URL.Query().Get("database") != "mcp_market" || r.URL.Query().Get("wait_end_of_query") != "1" {
			t.Error("missing query settings")
		}
		if strings.HasPrefix(r.URL.Query().Get("query"), "INSERT") {
			raw, _ := io.ReadAll(r.Body)
			insert = string(raw)
			return
		}
		if strings.HasPrefix(r.URL.Query().Get("query"), "SELECT count()") {
			_, _ = w.Write([]byte(`{"calls":2,"cost_micros":5,"errors":1,"p99_latency_ms":3}` + "\n"))
			return
		}
		if r.URL.Query().Get("query") == "bad FORMAT JSONEachRow" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("query rejected"))
			return
		}
	}))
	defer server.Close()
	analytics, err := NewAnalytics(server.URL, "qa", "secret", "mcp_market")
	if err != nil {
		t.Fatal(err)
	}
	if err := analytics.EnsureTable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := analytics.Insert(context.Background(), []AnalyticsCall{{ID: 1, ArgumentSummary: "[redacted]"}, {ID: 2}}); err != nil || strings.Count(insert, "\n") != 2 {
		t.Fatal(insert, err)
	}
	summary, err := analytics.Summary(context.Background(), true)
	if err != nil || summary["calls"] != float64(2) {
		t.Fatal(summary, err)
	}
	if _, err := analytics.Rows(context.Background(), "bad"); err == nil {
		t.Fatal("ClickHouse error hidden")
	}
	if _, err := analytics.Daily(context.Background(), 0); err == nil {
		t.Fatal("unbounded days accepted")
	}
	if _, err := NewAnalytics(server.URL, "qa", "secret", "x;DROP TABLE"); err == nil {
		t.Fatal("unsafe identifier accepted")
	}
}
