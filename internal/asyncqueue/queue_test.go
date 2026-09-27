package asyncqueue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmitReturnsBeforeTheJobRuns(t *testing.T) {
	q := New("test", 1, 4)
	defer q.Close(time.Second)
	release := make(chan struct{})
	var ran atomic.Bool
	start := time.Now()
	if err := q.Submit(func(context.Context) { <-release; ran.Store(true) }); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond || ran.Load() {
		t.Fatal("Submit waited for the job")
	}
	close(release)
	q.Drain()
	if !ran.Load() {
		t.Fatal("Drain returned before the job finished")
	}
}

func TestSubmitReportsFullBacklog(t *testing.T) {
	q := New("test", 1, 1)
	block := make(chan struct{})
	defer func() { close(block); q.Close(time.Second) }()
	started := make(chan struct{})
	_ = q.Submit(func(context.Context) { close(started); <-block }) // occupies the worker
	<-started
	if err := q.Submit(func(context.Context) {}); err != nil { // fills the backlog
		t.Fatal(err)
	}
	if err := q.Submit(func(context.Context) {}); !errors.Is(err, ErrFull) {
		t.Fatalf("err = %v, want ErrFull", err)
	}
}

func TestCloseWaitsForJobsThenRejects(t *testing.T) {
	q := New("test", 2, 8)
	var done atomic.Int32
	for range 5 {
		_ = q.Submit(func(context.Context) { time.Sleep(20 * time.Millisecond); done.Add(1) })
	}
	q.Close(5 * time.Second)
	if done.Load() != 5 {
		t.Fatalf("Close returned with %d/5 jobs done", done.Load())
	}
	if err := q.Submit(func(context.Context) {}); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestCloseCancelsJobsAfterTimeout(t *testing.T) {
	q := New("test", 1, 1)
	cancelled := make(chan struct{})
	_ = q.Submit(func(ctx context.Context) { <-ctx.Done(); close(cancelled) })
	q.Close(20 * time.Millisecond)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("job context was not cancelled after the close timeout")
	}
}

func TestPanickingJobDoesNotKillTheWorker(t *testing.T) {
	q := New("test", 1, 4)
	defer q.Close(time.Second)
	_ = q.Submit(func(context.Context) { panic("boom") })
	var ran atomic.Bool
	_ = q.Submit(func(context.Context) { ran.Store(true) })
	q.Drain()
	if !ran.Load() {
		t.Fatal("worker died after a panicking job")
	}
}

func TestNilQueueRunsInline(t *testing.T) {
	var q *Queue
	ran := false
	if err := q.Submit(func(context.Context) { ran = true }); err != nil || !ran {
		t.Fatalf("nil queue: err=%v ran=%v", err, ran)
	}
	q.Drain()
	q.Close(time.Second)
}
