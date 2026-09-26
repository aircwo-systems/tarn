// Package persist holds the background write-behind loop shared by Tarn's
// JSON-snapshot stores.
package persist

import (
	"sync"
	"sync/atomic"
	"time"
)

// FlushInterval is how often a dirty store is written to disk.
const FlushInterval = 250 * time.Millisecond

// Flusher writes a store's snapshot to disk shortly after it is marked dirty.
// Writes are serialized, so a slower, older snapshot can never be renamed over
// a newer one, and Close performs a final write so acknowledged changes made
// just before shutdown are not lost. A nil *Flusher is a valid no-op, for
// stores running with persistence disabled.
type Flusher struct {
	dirty *atomic.Bool
	write func()

	mu   sync.Mutex // serializes write
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// StartFlusher starts writing dirty state every FlushInterval. The store marks
// itself dirty by setting *dirty; write must snapshot and persist the store.
func StartFlusher(dirty *atomic.Bool, write func()) *Flusher {
	return startFlusher(dirty, FlushInterval, write)
}

func startFlusher(dirty *atomic.Bool, interval time.Duration, write func()) *Flusher {
	f := &Flusher{
		dirty: dirty,
		write: write,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go f.run(interval)
	return f
}

func (f *Flusher) run(interval time.Duration) {
	defer close(f.done)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-t.C:
			if f.dirty.Load() {
				f.Flush()
			}
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
	// and is picked up by the next tick.
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
