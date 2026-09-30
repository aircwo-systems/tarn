package ecs

import (
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func (f *fakeEngine) listCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.listSelectors)
}

func shrinkReconcileIntervals(t *testing.T) {
	t.Helper()
	tick, maxI, idle := reconcileTickInterval, reconcileMaxInterval, reconcileIdleInterval
	reconcileTickInterval, reconcileMaxInterval, reconcileIdleInterval = 5*time.Millisecond, 40*time.Millisecond, time.Hour
	t.Cleanup(func() { reconcileTickInterval, reconcileMaxInterval, reconcileIdleInterval = tick, maxI, idle })
}

// TestReconcileIdlesWithoutServices covers an account with no ECS services:
// after one pass the loop must stop asking Docker for containers, and a new
// service must wake it at once rather than on the idle interval.
func TestReconcileIdlesWithoutServices(t *testing.T) {
	shrinkReconcileIntervals(t)
	r, svc, eng, _ := newTestRunner(t)
	r.Start()
	defer r.Stop()

	// Startup recovery lists containers once; count from after it.
	time.Sleep(20 * time.Millisecond)
	base := eng.listCount()
	time.Sleep(60 * time.Millisecond)
	if n := eng.listCount() - base; n != 0 {
		t.Fatalf("idle loop listed containers %d times with no services", n)
	}

	td := registerSingleContainerTaskDef(t, svc, "fam-idle")
	if _, err := svc.CreateService(&types.CreateServiceInput{ServiceName: "web", TaskDefinition: td.Family, DesiredCount: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for eng.listCount() == base && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if eng.listCount() == base {
		t.Fatal("creating a service did not wake the idle reconcile loop")
	}
}

// TestReconcileBacksOffWhileSteady covers a running service: passes slow
// towards reconcileMaxInterval instead of listing containers every tick.
func TestReconcileBacksOffWhileSteady(t *testing.T) {
	shrinkReconcileIntervals(t)
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-steady")
	if _, err := svc.CreateService(&types.CreateServiceInput{ServiceName: "web", TaskDefinition: td.Family, DesiredCount: 0}); err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()

	time.Sleep(400 * time.Millisecond)
	// At a fixed 5ms tick this would be ~80 passes; backing off to 40ms
	// keeps it near 12.
	if n := eng.listCount(); n == 0 || n > 20 {
		t.Fatalf("listed containers %d times in 400ms, want a backed-off handful", n)
	}
}
