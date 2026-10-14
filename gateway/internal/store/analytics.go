package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Analytics struct {
	endpoint, user, password, database string
	client                             *http.Client
	cancel                             context.CancelFunc
	done                               chan struct{}
	once                               sync.Once
}
type AnalyticsCall struct {
	ID              int64     `json:"id"`
	TraceID         string    `json:"trace_id"`
	UserID          int64     `json:"user_id"`
	ToolID          int64     `json:"tool_id"`
	ToolName        string    `json:"tool_name"`
	StatusCode      int       `json:"status_code"`
	LatencyMs       int64     `json:"latency_ms"`
	TokensIn        int       `json:"tokens_in"`
	TokensOut       int       `json:"tokens_out"`
	CostMicros      int64     `json:"cost_micros"`
	ArgumentSummary string    `json:"argument_summary"`
	CreatedAt       time.Time `json:"created_at"`
}

func NewAnalytics(endpoint, user, password, database string) (*Analytics, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid CLICKHOUSE_URL")
	}
	if !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(database) {
		return nil, fmt.Errorf("invalid CLICKHOUSE_DATABASE")
	}
	return &Analytics{endpoint: strings.TrimRight(endpoint, "/"), user: user, password: password, database: database, client: &http.Client{Timeout: 5 * time.Second}, done: make(chan struct{})}, nil
}
func (a *Analytics) query(ctx context.Context, query string, payload []byte) ([]byte, error) {
	target, _ := url.Parse(a.endpoint)
	values := url.Values{"database": {a.database}, "query": {query}, "wait_end_of_query": {"1"}, "output_format_json_quote_64bit_integers": {"0"}, "date_time_input_format": {"best_effort"}}
	target.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(a.user, a.password)
	response, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ClickHouse request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("ClickHouse query failed (HTTP %d): %.300s", response.StatusCode, raw)
	}
	return raw, nil
}
func (a *Analytics) EnsureTable(ctx context.Context) error {
	_, err := a.query(ctx, `CREATE TABLE IF NOT EXISTS tool_calls (
 id UInt64,trace_id String,user_id UInt64,tool_id UInt64,tool_name String,status_code UInt16,
 latency_ms Int64,tokens_in Int64,tokens_out Int64,cost_micros Int64,argument_summary String,created_at DateTime64(3,'UTC')
 ) ENGINE=ReplacingMergeTree ORDER BY id`, nil)
	return err
}
func (a *Analytics) Insert(ctx context.Context, rows []AnalyticsCall) error {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}
	_, err := a.query(ctx, "INSERT INTO tool_calls FORMAT JSONEachRow", data.Bytes())
	return err
}
func (a *Analytics) Rows(ctx context.Context, query string) ([]map[string]any, error) {
	raw, err := a.query(ctx, query+" FORMAT JSONEachRow", nil)
	if err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		var row map[string]any
		err := decoder.Decode(&row)
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
}

// StartExporter 从 PostgreSQL 已提交记录持续同步。确认插入后推进游标，重试重复由 FINAL 去重。
func (s *Store) StartExporter(parent context.Context) {
	if s.Analytics == nil {
		return
	}
	a := s.Analytics
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	go func() {
		defer close(a.done)
		ready := false
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !ready {
				if err := a.EnsureTable(ctx); err != nil {
					log.Printf("analytics initialization: %v", err)
					continue
				}
				ready = true
			}
			for batch := 0; batch < 8; batch++ {
				count, err := s.exportCalls(ctx)
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("analytics export will retry: %v", err)
					}
					break
				}
				if count < 1000 {
					break
				}
			}
		}
	}()
}
func (a *Analytics) Close() {
	if a == nil || a.cancel == nil {
		return
	}
	a.once.Do(a.cancel)
	<-a.done
}
func (s *Store) exportCalls(ctx context.Context) (int, error) {
	var last int64
	if err := s.P.QueryRow(ctx, `INSERT INTO analytics_checkpoint(name,last_id) VALUES('clickhouse',0) ON CONFLICT(name) DO UPDATE SET name=EXCLUDED.name RETURNING last_id`).Scan(&last); err != nil {
		return 0, err
	}
	rows, err := s.P.Query(ctx, `SELECT c.id,c.trace_id,COALESCE(c.user_id,0),COALESCE(c.tool_id,0),COALESCE(t.name,''),c.status_code,c.latency_ms,c.tokens_in,c.tokens_out,c.cost_micros,c.argument_summary,c.created_at FROM tool_calls c LEFT JOIN mcp_tools t ON t.id=c.tool_id WHERE c.id>$1 ORDER BY c.id LIMIT 1000`, last)
	if err != nil {
		return 0, err
	}
	calls := []AnalyticsCall{}
	for rows.Next() {
		var c AnalyticsCall
		if err := rows.Scan(&c.ID, &c.TraceID, &c.UserID, &c.ToolID, &c.ToolName, &c.StatusCode, &c.LatencyMs, &c.TokensIn, &c.TokensOut, &c.CostMicros, &c.ArgumentSummary, &c.CreatedAt); err != nil {
			rows.Close()
			return 0, err
		}
		calls = append(calls, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(calls) == 0 {
		return 0, err
	}
	if err := s.Analytics.Insert(ctx, calls); err != nil {
		return 0, err
	}
	_, err = s.P.Exec(ctx, `UPDATE analytics_checkpoint SET last_id=$1,updated_at=now() WHERE name='clickhouse'`, calls[len(calls)-1].ID)
	return len(calls), err
}

func (a *Analytics) Summary(ctx context.Context, week bool) (map[string]any, error) {
	where := ""
	if week {
		where = " WHERE created_at>=now()-INTERVAL 7 DAY"
	}
	rows, err := a.Rows(ctx, "SELECT count() AS calls,coalesce(sum(cost_micros),0) AS cost_micros,countIf(status_code>=400) AS errors,quantileExact(0.99)(latency_ms) AS p99_latency_ms FROM tool_calls FINAL"+where)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("invalid analytics summary")
	}
	return rows[0], nil
}
func (a *Analytics) Daily(ctx context.Context, days int) (map[string]int64, error) {
	if days < 1 || days > 365 {
		return nil, fmt.Errorf("days must be 1-365")
	}
	rows, err := a.Rows(ctx, "SELECT toString(toDate(created_at,'Asia/Shanghai')) AS day,count() AS calls FROM tool_calls FINAL WHERE created_at>=now()-INTERVAL "+strconv.Itoa(days)+" DAY GROUP BY day ORDER BY day")
	if err != nil {
		return nil, err
	}
	result := map[string]int64{}
	for _, row := range rows {
		result[row["day"].(string)] = int64(row["calls"].(float64))
	}
	return result, nil
}
func (a *Analytics) Top(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("limit must be 1-100")
	}
	return a.Rows(ctx, "SELECT tool_id,anyLast(tool_name) AS name,count() AS call_count,coalesce(sum(cost_micros),0) AS cost_micros FROM tool_calls FINAL GROUP BY tool_id ORDER BY call_count DESC,tool_id LIMIT "+strconv.Itoa(limit))
}
