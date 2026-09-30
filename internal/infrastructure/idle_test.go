package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestProbingPausesWithoutViewers covers an idle laptop: with no dashboard
// reading results, targets stop being dialled, and a read resumes probing.
func TestProbingPausesWithoutViewers(t *testing.T) {
	origInterval, origIdle := probeInterval, probeIdleAfter
	probeInterval, probeIdleAfter = 10*time.Millisecond, 50*time.Millisecond
	defer func() { probeInterval, probeIdleAfter = origInterval, origIdle }()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	s := NewService("", true)
	s.targets = []ProbeTarget{{Name: "web", Kind: "http", URL: srv.URL}}
	s.Start(context.Background())
	defer s.Stop()

	// Past the idle window with nobody reading, probing stops.
	time.Sleep(150 * time.Millisecond)
	settled := hits.Load()
	time.Sleep(100 * time.Millisecond)
	if extra := hits.Load() - settled; extra != 0 {
		t.Fatalf("probed %d more times with no dashboard open", extra)
	}

	// Reading results resumes probing straight away.
	s.Results()
	deadline := time.Now().Add(time.Second)
	for hits.Load() == settled && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hits.Load() == settled {
		t.Fatal("reading results did not resume probing")
	}
}
