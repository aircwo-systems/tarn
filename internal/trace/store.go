package trace

import (
	"sync"
	"time"
)

const maxTraces = 200

// Store is a thread-safe ring buffer of recent request traces.
type Store struct {
	mu    sync.RWMutex
	buf   [maxTraces]*Trace
	head  int
	count int
}

// Trace records the full request lifecycle for one invocation.
type Trace struct {
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"startedAt"`
	DurationMs  int64     `json:"durationMs"`
	Status      int       `json:"status"`
	Method      string    `json:"method,omitempty"`
	Path        string    `json:"path,omitempty"`
	GatewayID   string    `json:"gatewayId,omitempty"`
	GatewayName string    `json:"gatewayName,omitempty"`
	Spans       []Span    `json:"spans"`
}

// Span is one hop in the trace path.
type Span struct {
	Kind       string            `json:"kind"`       // "gateway", "lambda", "queue"
	Name       string            `json:"name"`       // resource name
	DurationMs int64             `json:"durationMs"` // wall time for this span
	Status     string            `json:"status"`     // "ok", "error", "client_error"
	Meta       map[string]string `json:"meta,omitempty"`
}

// NewStore creates a new trace store.
func NewStore() *Store { return &Store{} }

// Add records a trace, evicting the oldest entry when the buffer is full.
func (s *Store) Add(t *Trace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf[s.head] = t
	s.head = (s.head + 1) % maxTraces
	if s.count < maxTraces {
		s.count++
	}
}

// Recent returns up to n of the most recent traces, newest first.
func (s *Store) Recent(n int) []*Trace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n > s.count {
		n = s.count
	}
	out := make([]*Trace, n)
	for i := 0; i < n; i++ {
		idx := ((s.head - 1 - i) + maxTraces) % maxTraces
		out[i] = s.buf[idx]
	}
	return out
}
