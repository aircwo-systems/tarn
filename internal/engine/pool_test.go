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
	pool := NewWarmPool(e, 1000)

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
	NewWarmPool(e, 1000).evictIdle()

	if <-acquired {
		t.Fatal("a container being removed was handed to an invoke")
	}
}
