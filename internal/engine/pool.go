package engine

import (
	"context"
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

// Touch updates the last invoked time for a function's container.
func (wp *WarmPool) Touch(functionName string) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.engine.mu.Lock()
	defer wp.engine.mu.Unlock()

	if info, ok := wp.engine.containers[functionName]; ok {
		info.LastInvoked = time.Now()
	}
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

	wp.engine.mu.Lock()
	toEvict := make([]*ContainerInfo, 0)
	for _, info := range wp.engine.containers {
		if time.Since(info.LastInvoked) > wp.keepAlive {
			toEvict = append(toEvict, info)
		}
	}
	wp.engine.mu.Unlock()

	ctx := context.Background()
	for _, info := range toEvict {
		log.Printf("[warm-pool] evicting idle container for %s (idle %s)", info.FunctionName, time.Since(info.LastInvoked))
		if err := wp.engine.StopContainer(ctx, info.ID); err != nil {
			log.Printf("[warm-pool] error stopping container %s: %v", info.ID[:12], err)
		}
		if err := wp.engine.RemoveContainer(ctx, info.ID); err != nil {
			log.Printf("[warm-pool] error removing container %s: %v", info.ID[:12], err)
		}

		wp.engine.mu.Lock()
		delete(wp.engine.containers, info.FunctionName)
		wp.engine.mu.Unlock()
	}
}
