package engine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/client"
)

func TestWarmPoolEvictsUnusedContainerDespiteOtherInvokes(t *testing.T) {
	removed := make(chan string, 2)
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/stop"), "/v1.44/containers/")
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			removed <- id
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer docker.Close()

	cli, err := client.NewClientWithOpts(client.WithHost(docker.URL), client.WithVersion("1.44"))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	active := &ContainerInfo{ID: "active-container", FunctionName: "fn", State: "running", Busy: true, LastInvoked: time.Now()}
	unused := &ContainerInfo{ID: "unused-container", FunctionName: "fn", State: "running", LastInvoked: time.Now().Add(-time.Minute)}
	e := &Engine{client: cli, containers: map[string][]*ContainerInfo{poolKey("a", "fn"): {active, unused}}}
	pool := NewWarmPool(e, 1000, 1000)

	// Completing an invocation on one container must not extend another's life.
	e.Release(active)
	pool.evictIdle()

	select {
	case id := <-removed:
		if id != unused.ID {
			t.Fatalf("removed %s, want %s", id, unused.ID)
		}
	default:
		t.Fatal("unused warm container was not removed from Docker")
	}
	if got := e.CountContainers("a", "fn"); got != 1 {
		t.Fatalf("pool still tracks %d containers, want 1", got)
	}
}

// TestWarmPoolEvictsBurstContainersSoonerThanBaseline covers the two keep-alive
// windows: a function's first container is a baseline worth keeping, but the
// extra containers a burst of concurrent invokes started only have to outlast
// the burst. Reaping both on the full keep-alive leaves several GB of JVM
// containers resident for ten minutes after the load is gone.
func TestWarmPoolEvictsBurstContainersSoonerThanBaseline(t *testing.T) {
	e, fake := newFakeEngine(t, 4566)
	baseline := &ContainerInfo{ID: "baseline", FunctionName: "fn", State: "running", LastInvoked: time.Now().Add(-time.Minute)}
	burst := &ContainerInfo{ID: "burst", FunctionName: "fn", State: "running", LastInvoked: time.Now().Add(-time.Minute)}
	busy := &ContainerInfo{ID: "busy", FunctionName: "fn", State: "running", Busy: true, LastInvoked: time.Now().Add(-time.Minute)}
	e.containers[poolKey("a", "fn")] = []*ContainerInfo{baseline, burst, busy}

	// Idle for a minute: well past the 2s burst window, well inside the
	// baseline's ten.
	NewWarmPool(e, 600000, 2000).evictIdle()

	if got := e.CountContainers("a", "fn"); got != 2 {
		t.Fatalf("pool tracks %d containers, want the baseline and the busy one", got)
	}
	if got := strings.Join(fake.removed, ","); got != "burst" {
		t.Fatalf("removed %q from Docker, want only the burst container", got)
	}
}

// TestWarmPoolKeepsBaselineOnTheFullWindow checks the baseline window on its
// own: a function's only container is never a burst container, so it stays warm
// for the whole keep-alive.
func TestWarmPoolKeepsBaselineOnTheFullWindow(t *testing.T) {
	e, fake := newFakeEngine(t, 4566)
	e.containers[poolKey("a", "fn")] = []*ContainerInfo{
		{ID: "baseline", FunctionName: "fn", State: "running", LastInvoked: time.Now().Add(-time.Minute)},
	}

	NewWarmPool(e, 600000, 2000).evictIdle()

	if got := e.CountContainers("a", "fn"); got != 1 {
		t.Fatalf("pool tracks %d containers, want the baseline kept for the full keep-alive", got)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("removed %v, want the baseline left warm", fake.removed)
	}
}

func TestNewWarmPoolFallsBackToKeepAliveForOverflow(t *testing.T) {
	tests := []struct {
		name             string
		keepAlive, burst int
		want             time.Duration
	}{
		{"shorter than baseline", 1000, 500, 500 * time.Millisecond},
		{"unset", 1000, 0, time.Second},
		{"longer than baseline", 1000, 5000, time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewWarmPool(&Engine{}, tt.keepAlive, tt.burst).overflowKeepAlive; got != tt.want {
				t.Fatalf("overflow keepalive = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestWarmPoolUntracksBeforeRemoving covers an invoke arriving while the
// reaper removes a container: once chosen for eviction the container must no
// longer be acquirable.
func TestWarmPoolUntracksBeforeRemoving(t *testing.T) {
	acquired := make(chan bool, 1)
	var e *Engine
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			_, ok := e.AcquireIdle("a", "fn")
			acquired <- ok
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer docker.Close()

	cli, err := client.NewClientWithOpts(client.WithHost(docker.URL), client.WithVersion("1.44"))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	idle := &ContainerInfo{ID: "idle", FunctionName: "fn", State: "running", LastInvoked: time.Now().Add(-time.Minute)}
	e = &Engine{client: cli, containers: map[string][]*ContainerInfo{poolKey("a", "fn"): {idle}}}
	NewWarmPool(e, 1000, 1000).evictIdle()

	if <-acquired {
		t.Fatal("a container being removed was handed to an invoke")
	}
}
