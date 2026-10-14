package store

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
)

type AuditSink struct {
	st         *Store
	ch         chan *model.AuditLog
	done       chan struct{}
	acceptDone chan struct{}
	closeOnce  sync.Once
	mu         sync.RWMutex
	closed     bool
	wg         sync.WaitGroup
}

func NewAuditSink(st *Store) *AuditSink {
	sink := &AuditSink{st: st, ch: make(chan *model.AuditLog, 65536), done: make(chan struct{}), acceptDone: make(chan struct{})}
	sink.wg.Add(1)
	go sink.run()
	return sink
}
func (s *AuditSink) Submit(a *model.AuditLog) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.closed {
		select {
		case s.ch <- a:
		case <-s.acceptDone:
		}
	}
}
func (s *AuditSink) Close() {
	s.closeOnce.Do(func() { close(s.acceptDone); s.mu.Lock(); s.closed = true; close(s.done); s.mu.Unlock() })
	s.wg.Wait()
}
func (s *AuditSink) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	batch := []*model.AuditLog{}
	flush := func() {
		if len(batch) == 0 {
			return
		}
		rows := make([][]any, 0, len(batch))
		for _, a := range batch {
			var user any
			if a.UserID > 0 {
				user = a.UserID
			}
			rows = append(rows, []any{validJournalText(a.TraceID), user, validJournalText(a.Action), validJournalText(a.Resource), validJournalText(a.ResourceID), a.Detail, validJournalText(a.IP), validJournalText(a.UserAgent)})
		}
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := s.write(ctx, rows, batch)
			cancel()
			if err == nil {
				batch = batch[:0]
				return
			}
			log.Printf("audit batch will retry: %v", err)
			select {
			case <-s.done:
				log.Printf("audit shutdown failed: %d records remain", len(batch))
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	for {
		select {
		case a := <-s.ch:
			batch = append(batch, a)
			if len(batch) >= 256 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.done:
			for {
				select {
				case a := <-s.ch:
					batch = append(batch, a)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (s *AuditSink) write(ctx context.Context, rows [][]any, batch []*model.AuditLog) error {
	tx, err := s.st.P.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ids := []int64{}
	for _, entry := range batch {
		ids = append(ids, entry.UserID)
	}
	users, err := journalIDs(ctx, tx, "users", ids)
	if err != nil {
		return err
	}
	for i, entry := range batch {
		if !users[entry.UserID] {
			rows[i][1] = nil
		}
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"audit_logs"}, []string{"trace_id", "user_id", "action", "resource", "resource_id", "detail", "ip", "user_agent"}, pgx.CopyFromRows(rows))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
