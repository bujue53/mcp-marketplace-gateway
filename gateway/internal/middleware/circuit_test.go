package middleware

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreakerOpensAndRecoversWithSingleProbe(t *testing.T) {
	cb := NewCircuitBreaker(0.5, 2, 20*time.Millisecond, time.Second)
	key := "test:recover"
	cb.Record(key, false)
	cb.Record(key, false)

	if cb.Allow(key) {
		t.Fatal("circuit should reject calls after reaching the failure threshold")
	}
	time.Sleep(25 * time.Millisecond)

	const callers = 24
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.Allow(key) {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 1 {
		t.Fatalf("half-open circuit allowed %d probes; want exactly 1", allowed)
	}

	cb.Record(key, true)
	if !cb.Allow(key) {
		t.Fatal("successful probe should close the circuit")
	}
}

func TestCircuitBreakerReopensWhenProbeFails(t *testing.T) {
	cb := NewCircuitBreaker(0.5, 1, 10*time.Millisecond, time.Second)
	key := "test:probe-failure"
	cb.Record(key, false)
	if cb.Allow(key) {
		t.Fatal("circuit should be open after the first failure")
	}
	time.Sleep(15 * time.Millisecond)
	if !cb.Allow(key) {
		t.Fatal("circuit should allow a probe after the cooldown")
	}
	cb.Record(key, false)
	if cb.Allow(key) {
		t.Fatal("failed probe should reopen the circuit")
	}
}

func TestCircuitBreakerUsesConfiguredWindow(t *testing.T) {
	cb := NewCircuitBreaker(0.5, 2, time.Second, 15*time.Millisecond)
	key := "test:window"
	cb.Record(key, false)
	time.Sleep(20 * time.Millisecond)
	cb.Record(key, true)
	cb.Record(key, true)
	if !cb.Allow(key) {
		t.Fatal("expired failures should not open the circuit")
	}
}
