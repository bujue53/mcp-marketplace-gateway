// 验收工具：真实数据库/报表 MCP 服务，以及固定速率的网关调用压测客户端。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"inputSchema"`
}

func schema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required}
}
func tools() []tool {
	return []tool{
		{"acceptance_ping", "Fast native MCP tool for measuring gateway forwarding", schema(map[string]any{"run_id": map[string]any{"type": "string"}})},
		{"acceptance_query", "Read-only query against the real PostgreSQL service", schema(map[string]any{"sql": map[string]any{"type": "string"}}, "sql")},
		{"acceptance_report_write", "Save a Markdown report in the acceptance data directory", schema(map[string]any{"name": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}}, "name", "text")},
		{"acceptance_report_read", "Read a previously saved report", schema(map[string]any{"name": map[string]any{"type": "string"}}, "name")},
	}
}
func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: acceptance backend|load")
	}
	switch os.Args[1] {
	case "backend":
		backend()
	case "load":
		load(os.Args[2:])
	default:
		log.Fatal("unknown command")
	}
}
func backend() {
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	root := os.Getenv("REPORT_DIR")
	if root == "" {
		root = "/reports"
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var message struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&message); err != nil {
			w.WriteHeader(400)
			return
		}
		if message.ID == nil {
			w.WriteHeader(202)
			return
		}
		var result any
		switch message.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "acceptance-backend", "version": "1.0.0"}}
		case "tools/list":
			result = map[string]any{"tools": tools()}
		case "ping":
			result = map[string]any{}
		case "tools/call":
			value, err := invokeBackend(r.Context(), pool, root, message.Params.Name, message.Params.Arguments)
			text := ""
			if err != nil {
				text = err.Error()
			} else {
				raw, _ := json.Marshal(value)
				text = string(raw)
			}
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": err != nil}
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32601, "message": "unknown method"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result})
	})
	server := &http.Server{Addr: ":9006", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
func invokeBackend(ctx context.Context, pool *pgxpool.Pool, root, name string, args map[string]any) (any, error) {
	switch name {
	case "acceptance_ping":
		return map[string]any{"ok": true, "run_id": args["run_id"]}, nil
	case "acceptance_query":
		sql, _ := args["sql"].(string)
		if len(sql) > 10000 || sql == "" {
			return nil, fmt.Errorf("SQL is required and limited to 10000 bytes")
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		rows, err := tx.Query(ctx, sql)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		records := []map[string]any{}
		fields := rows.FieldDescriptions()
		for rows.Next() {
			if len(records) >= 1000 {
				return nil, fmt.Errorf("query exceeds 1000 rows")
			}
			values, err := rows.Values()
			if err != nil {
				return nil, err
			}
			record := map[string]any{}
			for i, field := range fields {
				record[field.Name] = values[i]
			}
			records = append(records, record)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return map[string]any{"rows": records}, nil
	case "acceptance_report_write", "acceptance_report_read":
		filename, _ := args["name"].(string)
		if filename == "" || filepath.Base(filename) != filename || strings.ContainsAny(filename, "/\\:") || !strings.HasSuffix(filename, ".md") {
			return nil, fmt.Errorf("report name must be a local .md filename")
		}
		target := filepath.Join(root, filename)
		if name == "acceptance_report_write" {
			text, ok := args["text"].(string)
			if !ok || len(text) > 1<<20 {
				return nil, fmt.Errorf("report text is required and limited to 1 MiB")
			}
			if err := os.WriteFile(target, []byte(text), 0600); err != nil {
				return nil, err
			}
			return map[string]any{"name": filename, "bytes": len(text)}, nil
		}
		content, err := os.ReadFile(target)
		if err != nil {
			return nil, err
		}
		return map[string]any{"name": filename, "text": string(content)}, nil
	default:
		return nil, fmt.Errorf("unknown tool")
	}
}

type sample struct {
	Success           bool
	Status            int
	Latency, Overhead float64
	Error             string
}

func load(arguments []string) {
	flags := flag.NewFlagSet("load", flag.ExitOnError)
	base := flags.String("base", "http://gateway:8080", "gateway URL")
	rps := flags.Int("rps", 2100, "offered requests/second")
	seconds := flags.Int("seconds", 20, "duration")
	workers := flags.Int("workers", 128, "concurrency")
	toolName := flags.String("tool", "acceptance_ping", "tool")
	runID := flags.String("run", "acceptance-load", "run ID")
	_ = flags.Parse(arguments)
	if *rps < 1 || *rps > 20000 || *seconds < 1 || *workers < 1 {
		log.Fatal("invalid load parameters")
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{MaxIdleConns: 1024, MaxIdleConnsPerHost: 1024, MaxConnsPerHost: 1024}}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": env("ADMIN_PASS", "admin123")})
	response, err := client.Post(*base+"/api/auth/login", "application/json", bytes.NewReader(login))
	if err != nil {
		log.Fatal(err)
	}
	var authentication struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(response.Body).Decode(&authentication)
	response.Body.Close()
	if authentication.Token == "" {
		log.Fatal("login failed")
	}
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": *toolName, "arguments": map[string]string{"run_id": *runID}}})
	invoke := func(due time.Time) sample {
		req, _ := http.NewRequest("POST", *base+"/mcp", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+authentication.Token)
		req.Header.Set("User-Agent", "acceptance-load/"+*runID)
		response, err := client.Do(req)
		if err != nil {
			return sample{Latency: float64(time.Since(due)) / float64(time.Millisecond), Error: err.Error()}
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		overhead, _ := strconv.ParseFloat(response.Trailer.Get("X-Gateway-Overhead-Ms"), 64)
		var message struct {
			JSONRPC string `json:"jsonrpc"`
			Result  struct {
				IsError bool  `json:"isError"`
				Content []any `json:"content"`
			} `json:"result"`
			Error any `json:"error"`
		}
		decodeErr := json.Unmarshal(raw, &message)
		valid := err == nil && decodeErr == nil && response.StatusCode == 200 && message.JSONRPC == "2.0" && message.Error == nil && !message.Result.IsError && len(message.Result.Content) > 0 && response.Trailer.Get("X-Gateway-Overhead-Ms") != ""
		return sample{Success: valid, Status: response.StatusCode, Latency: float64(time.Since(due)) / float64(time.Millisecond), Overhead: overhead, Error: string(raw)}
	}
	// 先建立连接、编译 Redis 脚本并预热 SQL/工具路由。
	for i := 0; i < 100; i++ {
		_ = invoke(time.Now())
	}
	jobs := make(chan time.Time, *rps)
	samples := make(chan sample, *rps**seconds)
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for due := range jobs {
				samples <- invoke(due)
			}
		}()
	}
	start := time.Now()
	offered := *rps * *seconds
	scheduled := 0
	dropped := 0
	ticker := time.NewTicker(5 * time.Millisecond)
	for scheduled < offered {
		now := <-ticker.C
		expected := int(now.Sub(start).Seconds() * float64(*rps))
		if expected > offered {
			expected = offered
		}
		for scheduled < expected {
			due := start.Add(time.Duration(float64(scheduled) * float64(time.Second) / float64(*rps)))
			select {
			case jobs <- due:
			default:
				dropped++
			}
			scheduled++
		}
	}
	ticker.Stop()
	close(jobs)
	wg.Wait()
	close(samples)
	elapsed := time.Since(start).Seconds()
	latencies, overheads := []float64{}, []float64{}
	successes := 0
	statuses := map[string]int{}
	failures := []string{}
	for sample := range samples {
		statuses[strconv.Itoa(sample.Status)]++
		latencies = append(latencies, sample.Latency)
		if sample.Success {
			successes++
			overheads = append(overheads, sample.Overhead)
		} else if len(failures) < 3 {
			failures = append(failures, sample.Error)
		}
	}
	sort.Float64s(latencies)
	sort.Float64s(overheads)
	percentile := func(values []float64, p float64) float64 {
		if len(values) == 0 {
			return 0
		}
		return values[min(len(values)-1, int(float64(len(values)-1)*p))]
	}
	completed := len(latencies)
	successfulQPS := float64(successes) / elapsed
	result := map[string]any{"run_id": *runID, "offered_rps": *rps, "seconds": *seconds, "workers": *workers, "offered": offered, "completed": completed, "dropped": dropped, "successes": successes, "elapsed_seconds": elapsed, "successful_qps": successfulQPS, "success_ratio": float64(successes) / float64(offered), "status_counts": statuses, "latency_ms": map[string]float64{"p50": percentile(latencies, .50), "p95": percentile(latencies, .95), "p99": percentile(latencies, .99)}, "gateway_overhead_ms": map[string]float64{"p50": percentile(overheads, .50), "p95": percentile(overheads, .95), "p99": percentile(overheads, .99)}, "failure_examples": failures}
	result["passed"] = successfulQPS >= 2000 && successes == offered && dropped == 0 && percentile(overheads, .99) < 5
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
