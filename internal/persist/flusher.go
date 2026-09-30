// Package persist holds the background write-behind loop shared by Tarn's
// JSON-snapshot stores.
package persist

import (
	"sync"
	"sync/atomic"
	"time"
)

// FlushInterval is how long after a store is first marked dirty its snapshot
// is written. Changes made in that window are coalesced into one write.
const FlushInterval = 250 * time.Millisecond

// Dirty is a store's "has unsaved changes" flag. Marking it dirty wakes the
// store's Flusher, so a clean store costs no timer wakeups at all. The zero
// value is ready to use.
type Dirty struct {
	flag atomic.Bool
	kick atomic.Pointer[chan struct{}]
}

// Store sets the flag. Setting it on a clean store wakes the flusher.
func (d *Dirty) Store(v bool) {
	if !v {
		d.flag.Store(false)
		return
	}
	if d.flag.Swap(true) {
		return // already dirty: a write is already scheduled
	}
	if c := d.kick.Load(); c != nil {
		select {
		case *c <- struct{}{}:
		default:
		}
	}
}

// Load reports whether there are unsaved changes.
func (d *Dirty) Load() bool { return d.flag.Load() }

// Flusher writes a store's snapshot to disk shortly after it is marked dirty.
// Writes are serialized, so a slower, older snapshot can never be renamed over
// a newer one, and Close performs a final write so acknowledged changes made
// just before shutdown are not lost. A nil *Flusher is a valid no-op, for
// stores running with persistence disabled.
type Flusher struct {
	dirty *Dirty
	write func()

	mu   sync.Mutex // serializes write
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// StartFlusher writes the store FlushInterval after it becomes dirty. The
// store marks itself dirty through *dirty; write must snapshot and persist it.
func StartFlusher(dirty *Dirty, write func()) *Flusher {
	return startFlusher(dirty, FlushInterval, write)
}

func startFlusher(dirty *Dirty, delay time.Duration, write func()) *Flusher {
	f := &Flusher{
		dirty: dirty,
		write: write,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	kick := make(chan struct{}, 1)
	dirty.kick.Store(&kick)
	if dirty.Load() {
		kick <- struct{}{} // marked before the flusher existed
	}
	go f.run(kick, delay)
	return f
}

// run sleeps until the store is marked dirty, waits delay so a burst of
// changes lands in one write, then writes.
func (f *Flusher) run(kick <-chan struct{}, delay time.Duration) {
	defer close(f.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-kick:
		}
		timer.Reset(delay)
		select {
		case <-f.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
		if f.dirty.Load() {
			f.Flush()
		}
	}
}

// Flush writes the current state now, regardless of the dirty flag.
func (f *Flusher) Flush() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Clear before writing: a change made during the write re-marks the store
	// and schedules another write.
	f.dirty.Store(false)
	f.write()
}

// Close stops the background loop and writes any pending state. It is safe to
// call more than once.
func (f *Flusher) Close() {
	if f == nil {
		return
	}
	f.once.Do(func() {
		close(f.stop)
		<-f.done
		if f.dirty.Load() {
			f.Flush()
		}
	})
}
