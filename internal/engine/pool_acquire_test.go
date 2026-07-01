package engine

import "testing"

// These tests exercise the in-memory pool bookkeeping (AcquireIdle / Release /
// CountContainers) without Docker, so they run anywhere.

func TestAcquireIdleReleaseAndReuse(t *testing.T) {
	e := &Engine{containers: make(map[string][]*ContainerInfo)}

	if _, ok := e.AcquireIdle("fn"); ok {
		t.Fatal("expected no idle container for an empty pool")
	}
	if n := e.CountContainers("fn"); n != 0 {
		t.Fatalf("expected count 0, got %d", n)
	}

	// Two warm, running containers in the pool.
	e.containers["fn"] = []*ContainerInfo{
		{ID: "c1", FunctionName: "fn", State: "running"},
		{ID: "c2", FunctionName: "fn", State: "running"},
	}
	if n := e.CountContainers("fn"); n != 2 {
		t.Fatalf("expected count 2, got %d", n)
	}

	a, ok := e.AcquireIdle("fn")
	if !ok || a == nil {
		t.Fatal("expected to acquire a container")
	}
	if !a.Busy {
		t.Fatal("acquired container should be marked Busy")
	}

	b, ok := e.AcquireIdle("fn")
	if !ok || b.ID == a.ID {
		t.Fatalf("expected to acquire the other container, got %v", b)
	}

	// Both busy now → nothing idle.
	if _, ok := e.AcquireIdle("fn"); ok {
		t.Fatal("expected no idle container while all are busy")
	}

	// Release one and confirm it can be re-acquired.
	e.Release(a)
	if a.Busy {
		t.Fatal("released container should not be Busy")
	}
	c, ok := e.AcquireIdle("fn")
	if !ok || c.ID != a.ID {
		t.Fatalf("expected to re-acquire released container %s, got %v", a.ID, c)
	}
}

func TestAcquireIdleSkipsNonRunning(t *testing.T) {
	e := &Engine{containers: map[string][]*ContainerInfo{
		"fn": {{ID: "c1", FunctionName: "fn", State: "created"}}, // still starting
	}}
	if _, ok := e.AcquireIdle("fn"); ok {
		t.Fatal("should not acquire a container that is not yet running")
	}
}

func TestReleaseNilIsSafe(t *testing.T) {
	e := &Engine{containers: make(map[string][]*ContainerInfo)}
	e.Release(nil) // must not panic
}
