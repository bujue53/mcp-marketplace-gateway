package middleware

import (
	"sync"
	"time"

	"github.com/yafeng/mcp-marketplace/gateway/internal/metrics"
)

type cbState int

const (
	cbClosed cbState = iota
	cbOpen
	cbHalfOpen
)

type cbEntry struct {
	state    cbState
	failures int
	success  int
	openedAt time.Time
	inFlight int
}

// CircuitBreaker 每个 key 独立的熔断器：closed -> open -> halfOpen -> closed。
// 单实例内存实现（骨架阶段），接口已抽象为后续可替换为 Redis 共享状态。
type CircuitBreaker struct {
	mu         sync.Mutex
	entries    map[string]*cbEntry
	failRatio  float64
	minSamples int
	interval   time.Duration
}

func NewCircuitBreaker(failRatio float64, minSamples int, interval time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		entries:    map[string]*cbEntry{},
		failRatio:  failRatio,
		minSamples: minSamples,
		interval:   interval,
	}
}

// Allow 判断指定 key 当前是否允许发起调用。
func (c *CircuitBreaker) Allow(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.get(key)
	now := time.Now()
	switch e.state {
	case cbClosed:
		metrics.SetCircuit(key, false)
		return true
	case cbOpen:
		if now.Sub(e.openedAt) >= c.interval {
			e.state = cbHalfOpen
			e.inFlight = 0
			metrics.SetCircuit(key, false)
			return true
		}
		metrics.SetCircuit(key, true)
		return false
	default: // halfOpen: 只放行一个探针
		if e.inFlight == 0 {
			e.inFlight = 1
			return true
		}
		return false
	}
}

// Record 记录调用结果（成功/失败）。
func (c *CircuitBreaker) Record(key string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.get(key)
	now := time.Now()

	if ok {
		e.success++
		e.inFlight = 0
		if e.state == cbHalfOpen || (e.state == cbClosed && e.failures > 0) {
			e.state = cbClosed
			e.failures = 0
			e.success = 0
		}
		metrics.SetCircuit(key, false)
		return
	}

	e.failures++
	e.inFlight = 0
	total := e.failures + e.success
	if e.state == cbHalfOpen {
		e.state = cbOpen
		e.openedAt = now
		metrics.SetCircuit(key, true)
		return
	}
	if total >= c.minSamples && float64(e.failures)/float64(total) >= c.failRatio {
		e.state = cbOpen
		e.openedAt = now
		metrics.SetCircuit(key, true)
	}
}

func (c *CircuitBreaker) get(key string) *cbEntry {
	e, ok := c.entries[key]
	if !ok {
		e = &cbEntry{state: cbClosed}
		c.entries[key] = e
	}
	return e
}

