package registry

import (
	"sync"
	"time"
)

// ServerStatusEvent describes a real online/offline transition.
type ServerStatusEvent struct {
	ServerID       int64     `json:"server_id"`
	ServerName     string    `json:"server_name"`
	PreviousStatus string    `json:"previous_status"`
	Status         string    `json:"status"`
	Healthy        bool      `json:"healthy"`
	ChangedAt      time.Time `json:"changed_at"`
}

// StatusBroadcaster fans status changes out to connected SSE clients.
type StatusBroadcaster struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]chan ServerStatusEvent
}

func NewStatusBroadcaster() *StatusBroadcaster {
	return &StatusBroadcaster{subscribers: make(map[uint64]chan ServerStatusEvent)}
}

func (b *StatusBroadcaster) Subscribe() (<-chan ServerStatusEvent, func()) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	ch := make(chan ServerStatusEvent, 16)
	b.subscribers[id] = ch
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, id)
			close(ch)
			b.mu.Unlock()
		})
	}
}

func (b *StatusBroadcaster) Publish(event ServerStatusEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Keep the newest state if a slow consumer fills its buffer.
			select {
			case <-ch:
			default:
			}
			ch <- event
		}
	}
}
