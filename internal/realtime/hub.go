// Package realtime broadcasts committed changes to connected clients.
package realtime

import (
	"sync"

	"github.com/prayujt/wavelength/internal/discovery"
)

type Change interface{ change() }

type ImportChanged struct{ ID string }

func (ImportChanged) change() {}

type TrackPublished struct {
	Ref     discovery.Ref
	TrackID string
}

func (TrackPublished) change() {}

type Event struct {
	// An empty owner addresses the shared library, never private job state.
	Owner  string
	Change Change
}

type subscriber struct {
	owner  string
	events chan Event
}
type Hub struct {
	mu          sync.Mutex
	subscribers map[*subscriber]struct{}
}

func NewHub() *Hub { return &Hub{subscribers: make(map[*subscriber]struct{})} }

func (h *Hub) Subscribe(owner string) (<-chan Event, func()) {
	s := &subscriber{owner: owner, events: make(chan Event, 32)}
	h.mu.Lock()
	h.subscribers[s] = struct{}{}
	h.mu.Unlock()
	return s.events, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subscribers[s]; ok {
			delete(h.subscribers, s)
			close(s.events)
		}
	}
}

// A slow reader reconnects and refreshes its snapshot rather than silently
// missing changes or blocking the ingestion worker.
func (h *Hub) Publish(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subscribers {
		if event.Owner != "" && event.Owner != s.owner {
			continue
		}
		select {
		case s.events <- event:
		default:
			delete(h.subscribers, s)
			close(s.events)
		}
	}
}
