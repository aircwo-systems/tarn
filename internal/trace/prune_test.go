package trace

import (
	"fmt"
	"testing"
	"time"
)

// TestAddPrunesWhileRunning covers a long-lived process: retention must hold
// without a restart, since prune otherwise only ran when the store opened.
func TestAddPrunesWhileRunning(t *testing.T) {
	s := NewStore()
	s.Add(&Trace{ID: "old", StartedAt: time.Now().Add(-2 * defaultRetention)})
	for i := range pruneEvery {
		s.Add(&Trace{ID: fmt.Sprintf("t%d", i), StartedAt: time.Now()})
	}
	if got := s.Count(); got != pruneEvery {
		t.Fatalf("count = %d, want %d: the expired trace should have been pruned", got, pruneEvery)
	}
}
