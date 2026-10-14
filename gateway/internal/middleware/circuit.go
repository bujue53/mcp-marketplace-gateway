package middleware

import (
	"sync"
	"time"

	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/metrics"
)

type cbState int

const (
	cbClosed cbState = iota
	cbOpen
	cbHalfOpen
)

type cbEntry struct {
	state    cbState
	samples  []cbSample
	openedAt time.Time
	inFlight int
}

type cbSample struct {
	at      time.Time
	success bool
}

// CircuitBreaker 每个 key 独立的熔断器：closed -> open -> halfOpen -> closed。
// 单实例内存实现（骨架阶段），接口已抽象为后续可替换为 Redis 共享状态。
type CircuitBreaker struct {
	mu         sync.Mutex
	entries    map[string]*cbEntry
	failRatio  float64
	minSamples int
	interval   time.Duration
	window     time.Duration
}

func NewCircuitBreaker(failRatio float64, minSamples int, interval, window time.Duration) *CircuitBreaker {
	if failRatio <= 0 || failRatio > 1 {
		failRatio = 0.5
	}
	if minSamples < 1 {
		minSamples = 1
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if window <= 0 {
		window = 30 * time.Second
	}
	return &CircuitBreaker{
		entries:    map[string]*cbEntry{},
		failRatio:  failRatio,
		minSamples: minSamples,
		interval:   interval,
		window:     window,
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
			e.inFlight = 1
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
		e.inFlight = 0
		if e.state == cbHalfOpen {
			e.state = cbClosed
			e.samples = nil
			metrics.SetCircuit(key, false)
			return
		}
	}

	e.inFlight = 0
	if e.state == cbHalfOpen {
		e.state = cbOpen
		e.openedAt = now
		e.samples = nil
		metrics.SetCircuit(key, true)
		return
	}
	if e.state != cbClosed {
		return
	}

	e.samples = append(e.samples, cbSample{at: now, success: ok})
	cutoff := now.Add(-c.window)
	first := 0
	for first < len(e.samples) && e.samples[first].at.Before(cutoff) {
		first++
	}
	if first > 0 {
		e.samples = append([]cbSample(nil), e.samples[first:]...)
	}
	failures := 0
	for _, sample := range e.samples {
		if !sample.success {
			failures++
		}
	}
	if len(e.samples) >= c.minSamples && float64(failures)/float64(len(e.samples)) >= c.failRatio {
		e.state = cbOpen
		e.openedAt = now
		e.samples = nil
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
