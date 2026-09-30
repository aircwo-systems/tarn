package engine

import (
	"log"
	"sync"
	"time"
)

// reaperInterval is how often the warm pool looks for containers to evict. It
// bounds how long past its keep-alive a container lingers, so the shorter burst
// window still lands within one to two ticks.
const reaperInterval = 30 * time.Second

// WarmPool manages a pool of warm Lambda containers for fast invocation.
type WarmPool struct {
	engine    *Engine
	keepAlive time.Duration
	// overflowKeepAlive applies to every container after the first in a
	// function's pool. Those only exist to absorb a burst, so they are released
	// once the burst is over rather than held for a whole keep-alive window.
	overflowKeepAlive time.Duration
	mu                sync.Mutex
	stopCh            chan struct{}
}

// NewWarmPool creates a warm pool with the given keep-alive durations, both in
// milliseconds. An overflowKeepAliveMS that is unset or not shorter than
// keepAliveMS falls back to keepAliveMS, leaving a single window.
func NewWarmPool(engine *Engine, keepAliveMS, overflowKeepAliveMS int) *WarmPool {
	wp := &WarmPool{
		engine:            engine,
		keepAlive:         time.Duration(keepAliveMS) * time.Millisecond,
		overflowKeepAlive: time.Duration(overflowKeepAliveMS) * time.Millisecond,
		stopCh:            make(chan struct{}),
	}
	if wp.overflowKeepAlive <= 0 || wp.overflowKeepAlive > wp.keepAlive {
		wp.overflowKeepAlive = wp.keepAlive
	}
	return wp
}

// Start begins the warm pool reaper goroutine that cleans up idle containers.
func (wp *WarmPool) Start() {
	go wp.reaper()
}

// Stop halts the warm pool reaper.
func (wp *WarmPool) Stop() {
	close(wp.stopCh)
}

// reaper periodically checks for idle containers and removes them.
func (wp *WarmPool) reaper() {
	ticker := time.NewTicker(reaperInterval)
	defer ticker.Stop()

	for {
		select {
		case <-wp.stopCh:
			return
		case <-ticker.C:
			wp.evictIdle()
		}
	}
}

// idleEviction is a container the reaper has chosen to remove.
type idleEviction struct {
	info     *ContainerInfo
	overflow bool
}

func (wp *WarmPool) evictIdle() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	// Untrack idle containers past the keep-alive window while holding the
	// engine lock, so AcquireIdle can never hand one out after it has been
	// chosen for removal. Busy containers are mid-invocation and never reaped.
	e := wp.engine
	var toEvict []idleEviction
	e.mu.Lock()
	for key, pool := range e.containers {
		kept := pool[:0]
		for i, info := range pool {
			// Pools are append-ordered by creation and preserved by this
			// filter, so index 0 is the function's baseline environment.
			overflow := i > 0
			limit := wp.keepAlive
			if overflow {
				limit = wp.overflowKeepAlive
			}
			if !info.Busy && time.Since(info.LastInvoked) > limit {
				toEvict = append(toEvict, idleEviction{info: info, overflow: overflow})
				continue
			}
			kept = append(kept, info)
		}
		clear(pool[len(kept):])
		if len(kept) == 0 {
			delete(e.containers, key)
		} else {
			e.containers[key] = kept
		}
	}
	e.mu.Unlock()

	for _, ev := range toEvict {
		kind := "baseline"
		if ev.overflow {
			kind = "burst"
		}
		log.Printf("[warm-pool] evicting idle %s container for %s (idle %s)", kind, ev.info.FunctionName, time.Since(ev.info.LastInvoked).Round(time.Second))
	}
	e.forceRemoveAll(containersOf(toEvict))
}

func containersOf(evictions []idleEviction) []*ContainerInfo {
	out := make([]*ContainerInfo, 0, len(evictions))
	for _, ev := range evictions {
		out = append(out, ev.info)
	}
	return out
}
