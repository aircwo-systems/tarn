package ecs

import (
	"context"
	"testing"

	"github.com/aircwo-systems/tarn/internal/engine"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// TestStopTaskHonorsContainerStopTimeout verifies the task definition's
// per-container StopTimeout reaches the engine as the SIGTERM grace period,
// with the engine default for containers that set none.
func TestStopTaskHonorsContainerStopTimeout(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	_, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "fam-stop-timeout",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "slow", Image: "example/app:latest", StopTimeout: 30},
			{Name: "plain", Image: "example/app:latest"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	ctx := context.Background()
	runOut, err := r.RunTask(ctx, &types.RunTaskInput{TaskDefinition: "fam-stop-timeout"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runOut.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(runOut.Tasks))
	}
	taskArn := runOut.Tasks[0].TaskArn

	if err := r.StopTask(ctx, "", taskArn, "test stop timeout"); err != nil {
		t.Fatalf("StopTask: %v", err)
	}

	eng.mu.Lock()
	defer eng.mu.Unlock()
	got := map[int]int{}
	for id, secs := range eng.stopTimeouts {
		t.Logf("container %s stopped with grace %ds", id, secs)
		got[secs]++
	}
	if got[30] != 1 || got[engine.DefaultStopTimeoutSec] != 1 || len(eng.stopTimeouts) != 2 {
		t.Fatalf("stop timeouts = %v, want one 30s and one %ds grace", eng.stopTimeouts, engine.DefaultStopTimeoutSec)
	}
}
