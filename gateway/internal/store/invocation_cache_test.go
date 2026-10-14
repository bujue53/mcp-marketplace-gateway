package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvocationCacheCoalescesRefreshAndInvalidates(t *testing.T) {
	cache := invocationCache[int]{}
	var loads atomic.Int64
	load := func(context.Context) (*int, error) { loads.Add(1); value := 7; return &value, nil }
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := cache.get(context.Background(), "one", load)
			if err != nil || *value != 7 {
				t.Error("invalid cached value")
			}
		}()
	}
	wg.Wait()
	if loads.Load() != 1 {
		t.Fatal("duplicate concurrent refresh")
	}
	cache.mu.Lock()
	entry := cache.entries["one"]
	entry.expires = time.Now().Add(-time.Second)
	cache.entries["one"] = entry
	cache.mu.Unlock()
	_, _ = cache.get(context.Background(), "one", load)
	cache.clear()
	_, _ = cache.get(context.Background(), "one", load)
	if loads.Load() != 3 {
		t.Fatal("TTL or invalidation ignored")
	}
}
