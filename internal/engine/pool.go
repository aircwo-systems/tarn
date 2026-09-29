package engine

import (
	"log"
	"sync"
	"time"
)

// WarmPool manages a pool of warm Lambda containers for fast invocation.
type WarmPool struct {
	engine    *Engine
	keepAlive time.Duration
	mu        sync.Mutex
	stopCh    chan struct{}
}

// NewWarmPool creates a warm pool with the given keep-alive duration.
func NewWarmPool(engine *Engine, keepAliveMS int) *WarmPool {
	return &WarmPool{
		engine:    engine,
		keepAlive: time.Duration(keepAliveMS) * time.Millisecond,
		stopCh:    make(chan struct{}),
	}
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
	ticker := time.NewTicker(30 * time.Second)
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

func (wp *WarmPool) evictIdle() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	// Untrack idle containers past the keep-alive window while holding the
	// engine lock, so AcquireIdle can never hand one out after it has been
	// chosen for removal. Busy containers are mid-invocation and never reaped.
	e := wp.engine
	var toEvict []*ContainerInfo
	e.mu.Lock()
	for key, pool := range e.containers {
		kept := pool[:0]
		for _, info := range pool {
			if !info.Busy && time.Since(info.LastInvoked) > wp.keepAlive {
				toEvict = append(toEvict, info)
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

	for _, info := range toEvict {
		log.Printf("[warm-pool] evicting idle container for %s (idle %s)", info.FunctionName, time.Since(info.LastInvoked).Round(time.Second))
	}
	e.forceRemoveAll(toEvict)
}
