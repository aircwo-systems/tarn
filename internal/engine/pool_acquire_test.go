package engine

import "testing"

// These tests exercise the in-memory pool bookkeeping (AcquireIdle / Release /
// CountContainers) without Docker, so they run anywhere.

func TestAcquireIdleReleaseAndReuse(t *testing.T) {
	e := &Engine{containers: make(map[string][]*ContainerInfo)}

	if _, ok := e.AcquireIdle("a", "fn"); ok {
		t.Fatal("expected no idle container for an empty pool")
	}
	if n := e.CountContainers("a", "fn"); n != 0 {
		t.Fatalf("expected count 0, got %d", n)
	}

	// Two warm, running containers in the pool.
	e.containers[poolKey("a", "fn")] = []*ContainerInfo{
		{ID: "c1", FunctionName: "fn", State: "running"},
		{ID: "c2", FunctionName: "fn", State: "running"},
	}
	if n := e.CountContainers("a", "fn"); n != 2 {
		t.Fatalf("expected count 2, got %d", n)
	}

	a, ok := e.AcquireIdle("a", "fn")
	if !ok || a == nil {
		t.Fatal("expected to acquire a container")
	}
	if !a.Busy {
		t.Fatal("acquired container should be marked Busy")
	}

	b, ok := e.AcquireIdle("a", "fn")
	if !ok || b.ID == a.ID {
		t.Fatalf("expected to acquire the other container, got %v", b)
	}

	// Both busy now → nothing idle.
	if _, ok := e.AcquireIdle("a", "fn"); ok {
		t.Fatal("expected no idle container while all are busy")
	}

	// Release one and confirm it can be re-acquired.
	e.Release(a)
	if a.Busy {
		t.Fatal("released container should not be Busy")
	}
	c, ok := e.AcquireIdle("a", "fn")
	if !ok || c.ID != a.ID {
		t.Fatalf("expected to re-acquire released container %s, got %v", a.ID, c)
	}
}

func TestAcquireIdleSkipsNonRunning(t *testing.T) {
	e := &Engine{containers: map[string][]*ContainerInfo{
		poolKey("a", "fn"): {{ID: "c1", FunctionName: "fn", State: "created"}}, // still starting
	}}
	if _, ok := e.AcquireIdle("a", "fn"); ok {
		t.Fatal("should not acquire a container that is not yet running")
	}
}

func TestReleaseNilIsSafe(t *testing.T) {
	e := &Engine{containers: make(map[string][]*ContainerInfo)}
	e.Release(nil) // must not panic
}

// TestPoolsAreIsolatedByAccount covers two accounts with a function of the
// same name: each container carries its own account's code, environment and
// credentials, so one account must never be handed the other's container.
func TestPoolsAreIsolatedByAccount(t *testing.T) {
	e := &Engine{containers: map[string][]*ContainerInfo{
		poolKey("111111111111", "orders"): {{ID: "a1", AccountID: "111111111111", FunctionName: "orders", State: "running"}},
	}}

	if _, ok := e.AcquireIdle("222222222222", "orders"); ok {
		t.Fatal("account 222222222222 acquired account 111111111111's container")
	}
	if n := e.CountContainers("222222222222", "orders"); n != 0 {
		t.Fatalf("other account's pool counted: %d", n)
	}
	if c, ok := e.AcquireIdle("111111111111", "orders"); !ok || c.ID != "a1" {
		t.Fatalf("owning account could not acquire its container: %v", c)
	}
}
