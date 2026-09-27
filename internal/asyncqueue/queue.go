// Package asyncqueue runs fire-and-forget work, such as asynchronous Lambda
// invokes and event fan-out, on a bounded pool of workers so that callers get
// their response without waiting for the work to finish.
package asyncqueue

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// ErrFull is returned by Submit when the backlog is at capacity.
var ErrFull = errors.New("async queue is full")

// ErrClosed is returned by Submit after Close.
var ErrClosed = errors.New("async queue is closed")

// Job is one unit of work. Its context is cancelled only when Close gives up
// waiting, never by the request that submitted it.
type Job func(ctx context.Context)

// Queue runs submitted jobs on a fixed number of workers.
type Queue struct {
	name string
	jobs chan Job

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex // guards closed and makes Submit and Close exclusive
	closed  bool
	pending sync.WaitGroup // submitted jobs not yet finished
	workers sync.WaitGroup
}

// New starts a queue with the given number of workers and backlog capacity.
func New(name string, workers, capacity int) *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{name: name, jobs: make(chan Job, capacity), ctx: ctx, cancel: cancel}
	for range workers {
		q.workers.Add(1)
		go q.work()
	}
	return q
}

func (q *Queue) work() {
	defer q.workers.Done()
	for job := range q.jobs {
		q.run(job)
	}
}

func (q *Queue) run(job Job) {
	defer q.pending.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[%s] async job panicked: %v", q.name, r)
		}
	}()
	job(q.ctx)
}

// Submit queues job without blocking. It returns ErrFull when the backlog is
// at capacity, so callers can report throttling instead of stalling. A nil
// Queue runs job inline, for services wired without one.
func (q *Queue) Submit(job Job) error {
	if q == nil {
		job(context.Background())
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrClosed
	}
	q.pending.Add(1)
	select {
	case q.jobs <- job:
		return nil
	default:
		q.pending.Done()
		return ErrFull
	}
}

// Drain blocks until every job submitted so far has finished.
func (q *Queue) Drain() {
	if q == nil {
		return
	}
	q.pending.Wait()
}

// Close stops accepting jobs and waits up to timeout for queued and running
// jobs to finish. After the timeout it cancels the jobs' context and returns
// without waiting further. It is safe to call more than once.
func (q *Queue) Close(timeout time.Duration) {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	close(q.jobs)
	q.mu.Unlock()

	done := make(chan struct{})
	go func() {
		q.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("[%s] async jobs still running after %s; cancelling them", q.name, timeout)
		q.cancel()
	}
}
