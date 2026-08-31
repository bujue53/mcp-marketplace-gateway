package store

import (
	"context"
	"log"
	"sync"

	"github.com/yafeng/mcp-marketplace/gateway/internal/model"
)

// CallSink 异步写入工具调用日志：提交到 channel，后台 goroutine 批量落库。
// 未来接入 ClickHouse 时仅需替换 run() 内的持久化实现。
type CallSink struct {
	st   *Store
	ch   chan *model.ToolCall
	done chan struct{}
	wg   sync.WaitGroup
}

func NewCallSink(st *Store, buf int) *CallSink {
	if buf <= 0 {
		buf = 1024
	}
	s := &CallSink{st: st, ch: make(chan *model.ToolCall, buf), done: make(chan struct{})}
	s.wg.Add(1)
	go s.run()
	return s
}

// Submit 提交一条调用记录；缓冲满或已关闭时丢弃，不阻塞热路径。
func (s *CallSink) Submit(c *model.ToolCall) {
	if s == nil || c == nil {
		return
	}
	select {
	case s.ch <- c:
	case <-s.done:
	default:
	}
}

func (s *CallSink) run() {
	defer s.wg.Done()
	for {
		select {
		case c := <-s.ch:
			s.write(c)
		case <-s.done:
			// 停止前把剩余缓冲写干净
			for {
				select {
				case c := <-s.ch:
					s.write(c)
				default:
					return
				}
			}
		}
	}
}

func (s *CallSink) write(c *model.ToolCall) {
	if c == nil {
		return
	}
	if err := s.st.InsertToolCall(context.Background(), c); err != nil {
		log.Printf("call sink insert: %v", err)
	}
}

// Close 停止接收并等待后台刷新完成。
func (s *CallSink) Close() {
	if s == nil {
		return
	}
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.wg.Wait()
}
