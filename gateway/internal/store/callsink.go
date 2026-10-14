package store

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
)

// CallSink 批量写入 PostgreSQL；ClickHouse 导出器读取已提交记录，断线后继续重试。
type CallSink struct {
	st         *Store
	ch         chan *model.ToolCall
	done       chan struct{}
	acceptDone chan struct{}
	closeOnce  sync.Once
	wg         sync.WaitGroup
	mu         sync.RWMutex
	closed     bool
}

func NewCallSink(st *Store, buf int) *CallSink {
	if buf <= 0 {
		buf = 65536
	}
	sink := &CallSink{st: st, ch: make(chan *model.ToolCall, buf), done: make(chan struct{}), acceptDone: make(chan struct{})}
	sink.wg.Add(1)
	go sink.run()
	return sink
}
func (s *CallSink) Submit(call *model.ToolCall) {
	if s == nil || call == nil {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	// 容量耗尽时产生背压，不能静默丢弃已执行的调用日志。
	select {
	case s.ch <- call:
	case <-s.acceptDone:
	}
}
func (s *CallSink) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]*model.ToolCall, 0, 256)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := s.write(ctx, batch)
			cancel()
			if err == nil {
				batch = batch[:0]
				return
			}
			log.Printf("call batch will retry: %v", err)
			select {
			case <-s.done:
				log.Printf("call batch shutdown failed: %d records remain", len(batch))
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	for {
		select {
		case call := <-s.ch:
			batch = append(batch, call)
			if len(batch) == 256 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.done:
			for {
				select {
				case call := <-s.ch:
					batch = append(batch, call)
					if len(batch) == 256 {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}
func (s *CallSink) write(ctx context.Context, batch []*model.ToolCall) error {
	tx, err := s.st.P.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	userIDs, toolIDs := []int64{}, []int64{}
	for _, call := range batch {
		userIDs = append(userIDs, call.UserID)
		toolIDs = append(toolIDs, call.ToolID)
	}
	users, err := journalIDs(ctx, tx, "users", userIDs)
	if err != nil {
		return err
	}
	tools, err := journalIDs(ctx, tx, "mcp_tools", toolIDs)
	if err != nil {
		return err
	}
	rows := make([][]any, 0, len(batch))
	sensitive := map[int64]bool{}
	for _, call := range batch {
		created := call.CreatedAt
		if created.IsZero() {
			created = time.Now().UTC()
		}
		var user, tool any
		if users[call.UserID] {
			user = call.UserID
		}
		if tools[call.ToolID] {
			tool = call.ToolID
		}
		rows = append(rows, []any{validJournalText(call.TraceID), user, tool, call.StatusCode, call.LatencyMs, call.TokensIn, call.TokensOut, call.CostMicros, validJournalText(call.ArgumentSummary), created})
		if call.Sensitive {
			sensitive[call.ToolID] = true
		}
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"tool_calls"}, []string{"trace_id", "user_id", "tool_id", "status_code", "latency_ms", "tokens_in", "tokens_out", "cost_micros", "argument_summary", "created_at"}, pgx.CopyFromRows(rows))
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for id := range sensitive {
		if err := s.st.CreateSensitiveFrequencyAlert(ctx, id, 5, 60); err != nil {
			log.Printf("sensitive tool alert insert: %v", err)
		}
	}
	return nil
}
func (s *CallSink) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() { close(s.acceptDone); s.mu.Lock(); s.closed = true; close(s.done); s.mu.Unlock() })
	s.wg.Wait()
}
