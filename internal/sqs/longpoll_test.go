package sqs

import (
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func newLongPollService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Stop)
	if _, err := svc.CreateQueue("jobs", nil, nil); err != nil {
		t.Fatal(err)
	}
	return svc
}

type receiveResult struct {
	msgs []*types.SQSMessage
	err  error
	took time.Duration
}

func receiveAsync(svc *Service, wait int, cancel <-chan struct{}) <-chan receiveResult {
	out := make(chan receiveResult, 1)
	go func() {
		start := time.Now()
		msgs, err := svc.ReceiveMessageUntil("jobs", 1, 30, wait, cancel)
		out <- receiveResult{msgs, err, time.Since(start)}
	}()
	return out
}

func TestLongPollWakesOnSend(t *testing.T) {
	svc := newLongPollService(t)
	res := receiveAsync(svc, 20, nil)

	time.Sleep(50 * time.Millisecond)
	if _, err := svc.SendMessage("jobs", "hello", 0, nil, "", ""); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-res:
		if r.err != nil || len(r.msgs) != 1 || r.msgs[0].Body != "hello" {
			t.Fatalf("got %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long poll did not wake when a message was sent")
	}
}

func TestLongPollWakesWhenInFlightMessageBecomesVisible(t *testing.T) {
	svc := newLongPollService(t)
	if _, err := svc.SendMessage("jobs", "retry me", 0, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	if msgs, err := svc.ReceiveMessage("jobs", 1, 1, 0); err != nil || len(msgs) != 1 {
		t.Fatalf("first receive: %v %v", msgs, err)
	}

	// Nothing signals a visibility timeout expiring; the poll must wake on
	// its own when the message is due, not wait out the whole 20 seconds.
	r := <-receiveAsync(svc, 20, nil)
	if r.err != nil || len(r.msgs) != 1 {
		t.Fatalf("got %+v", r)
	}
	if r.took > 3*time.Second {
		t.Fatalf("took %s to see a message whose visibility timeout was 1s", r.took)
	}
}

func TestLongPollCancel(t *testing.T) {
	svc := newLongPollService(t)
	cancel := make(chan struct{})
	res := receiveAsync(svc, 20, cancel)
	time.Sleep(20 * time.Millisecond)
	close(cancel)

	select {
	case r := <-res:
		if r.err != nil || len(r.msgs) != 0 {
			t.Fatalf("got %+v, want an empty result", r)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not end the long poll")
	}
}

func TestLongPollEndsWhenQueueDeleted(t *testing.T) {
	svc := newLongPollService(t)
	res := receiveAsync(svc, 20, nil)
	time.Sleep(20 * time.Millisecond)
	if err := svc.DeleteQueue("jobs"); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-res:
		if r.err == nil {
			t.Fatalf("got %+v, want queue-not-found", r)
		}
	case <-time.After(time.Second):
		t.Fatal("long poll kept waiting on a deleted queue")
	}
}

func TestEmptyReceiveDoesNotMarkStoreDirty(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q", nil, nil); err != nil {
		t.Fatal(err)
	}
	s.dirty.Store(false)

	if msgs, err := s.ReceiveMessage("q", 10, 30); err != nil || len(msgs) != 0 {
		t.Fatalf("receive: %v %v", msgs, err)
	}
	if s.dirty.Load() {
		t.Fatal("an empty receive marked the store dirty, forcing a needless state flush")
	}

	if _, err := s.SendMessage("q", "x", 0, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	s.dirty.Store(false)
	if msgs, _ := s.ReceiveMessage("q", 10, 30); len(msgs) != 1 {
		t.Fatal("expected the message")
	}
	if !s.dirty.Load() {
		t.Fatal("handing out a message must mark the store dirty")
	}
}

func TestReapCompactsInPlace(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q", nil, nil); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := s.SendMessage("q", "x", 0, nil, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	q := s.queues["q"]
	q.messages[1].Deleted = true
	backing := &q.messages[:cap(q.messages)][0]

	s.Reap()

	if len(q.messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(q.messages))
	}
	if &q.messages[:cap(q.messages)][0] != backing {
		t.Error("Reap reallocated the message slice instead of compacting it")
	}
	if tail := q.messages[:cap(q.messages)][2]; tail != nil {
		t.Error("the removed message is still referenced past the slice length")
	}
}
