package persist

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlusherWritesDirtyStateInBackground(t *testing.T) {
	var dirty Dirty
	var writes atomic.Int32
	f := startFlusher(&dirty, 5*time.Millisecond, func() { writes.Add(1) })
	defer f.Close()

	dirty.Store(true)
	deadline := time.Now().Add(time.Second)
	for writes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if writes.Load() == 0 {
		t.Fatal("dirty state was never written")
	}
}

func TestFlusherCloseWritesPendingStateAndStops(t *testing.T) {
	var dirty Dirty
	var writes atomic.Int32
	f := startFlusher(&dirty, time.Hour, func() { writes.Add(1) })

	dirty.Store(true) // write acknowledged just before shutdown
	f.Close()
	if writes.Load() != 1 {
		t.Fatalf("Close wrote %d times, want 1 final write", writes.Load())
	}

	f.Close() // idempotent
	dirty.Store(true)
	time.Sleep(10 * time.Millisecond)
	if writes.Load() != 1 {
		t.Fatalf("flusher kept writing after Close: %d writes", writes.Load())
	}
}

func TestFlusherSerializesWrites(t *testing.T) {
	var dirty Dirty
	var inFlight, maxInFlight atomic.Int32
	write := func() {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		inFlight.Add(-1)
	}
	f := startFlusher(&dirty, time.Millisecond, write)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				dirty.Store(true)
				f.Flush()
			}
		}()
	}
	wg.Wait()
	f.Close()
	if maxInFlight.Load() != 1 {
		t.Fatalf("writes overlapped (max %d concurrent); an older snapshot could land last", maxInFlight.Load())
	}
}

func TestNilFlusherIsNoOp(t *testing.T) {
	var f *Flusher
	f.Flush()
	f.Close()
}

// TestCleanFlusherNeverWakes covers the idle case: a store with no changes
// must not cost periodic wakeups, and a burst of changes is one write.
func TestCleanFlusherNeverWakes(t *testing.T) {
	var dirty Dirty
	var writes atomic.Int32
	f := startFlusher(&dirty, 20*time.Millisecond, func() { writes.Add(1) })
	defer f.Close()

	time.Sleep(60 * time.Millisecond)
	if writes.Load() != 0 {
		t.Fatalf("clean store was written %d times", writes.Load())
	}

	for range 50 {
		dirty.Store(true)
	}
	time.Sleep(80 * time.Millisecond)
	if writes.Load() != 1 {
		t.Fatalf("burst of changes wrote %d times, want 1", writes.Load())
	}

	dirty.Store(true)
	time.Sleep(80 * time.Millisecond)
	if writes.Load() != 2 {
		t.Fatalf("a later change wrote %d times in total, want 2", writes.Load())
	}
}

func TestFlusherWritesStateMarkedBeforeStart(t *testing.T) {
	var dirty Dirty
	dirty.Store(true)
	var writes atomic.Int32
	f := startFlusher(&dirty, time.Millisecond, func() { writes.Add(1) })
	defer f.Close()
	time.Sleep(30 * time.Millisecond)
	if writes.Load() != 1 {
		t.Fatalf("writes = %d, want the pre-existing change written", writes.Load())
	}
}
