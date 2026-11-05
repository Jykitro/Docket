// Package events is an in-process pub/sub feeding the SSE stream.
package events

import "sync"

type Event struct {
	Type      string `json:"type"`
	TaskID    int64  `json:"task_id,omitempty"`
	ProjectID int64  `json:"project_id,omitempty"`
	Actor     string `json:"actor"`
}

type Bus struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func NewBus() *Bus { return &Bus{subs: map[chan Event]struct{}{}} }

// Subscribe returns a buffered channel and a cancel func that must be called.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Publish never blocks: a subscriber with a full buffer misses the event.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
