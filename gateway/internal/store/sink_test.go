package store

import (
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
	"testing"
	"time"
)

func TestClosingFullSinksReleasesBlockedProducers(t *testing.T) {
	calls := &CallSink{ch: make(chan *model.ToolCall), done: make(chan struct{}), acceptDone: make(chan struct{})}
	audits := &AuditSink{ch: make(chan *model.AuditLog), done: make(chan struct{}), acceptDone: make(chan struct{})}
	done := make(chan struct{})
	go func() { calls.Submit(&model.ToolCall{}); close(done) }()
	calls.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("call sink shutdown deadlocked")
	}
	done = make(chan struct{})
	go func() { audits.Submit(&model.AuditLog{}); close(done) }()
	audits.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("audit sink shutdown deadlocked")
	}
	calls.Close()
	audits.Close()
}
