// Package events is an in-process pub/sub feeding the SSE stream.
package events

import (
	"sync"
	"sync/atomic"
)

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

// Budgets for open subscriptions (one per live SSE stream). Only a visible tab or the installed app holds a stream
// (web/src/live.ts closes it when hidden), so 16 per workspace covers 2-3 devices with a few windows each plus
// connections that dropped without the server noticing yet. A Bus is one workspace and a user may create several,
// so MaxTotal bounds the whole process (about 25 KB per stream: goroutine, buffer, connection) and therefore any
// one account: 1024 is 20 streams for each of the 50 pilot accounts.
const (
	MaxPerBus = 16
	MaxTotal  = 1024
)

var total atomic.Int64

func NewBus() *Bus { return &Bus{subs: map[chan Event]struct{}{}} }

// Subscribe returns a buffered channel and a cancel func that must be called, or ok=false when the workspace or the
// process has no subscription left.
func (b *Bus) Subscribe() (_ <-chan Event, cancel func(), ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) >= MaxPerBus {
		return nil, nil, false
	}
	if total.Add(1) > MaxTotal {
		total.Add(-1)
		return nil, nil, false
	}
	ch := make(chan Event, 64)
	b.subs[ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			total.Add(-1)
		}
	}, true
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
