package ecs

import (
	"context"
	"testing"
	"time"

	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// runAppWithSidecar registers and runs a task with an essential "app" and a
// non-essential "sidecar", returning the task ARN and each container's ID.
func runAppWithSidecar(t *testing.T, r *Runner, svc *Service, family string) (string, map[string]string) {
	t.Helper()
	notEssential := false
	if _, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: family,
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "app", Image: "example/app:latest"},
			{Name: "sidecar", Image: "example/sidecar:latest", Essential: &notEssential},
		},
	}); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("RunTask returned %d tasks, failures=%+v", len(out.Tasks), out.Failures)
	}
	ids := make(map[string]string, 2)
	for _, c := range out.Tasks[0].Containers {
		ids[c.Name] = c.ContainerID
	}
	if ids["app"] == "" || ids["sidecar"] == "" {
		t.Fatalf("missing container IDs: %+v", ids)
	}
	return out.Tasks[0].TaskArn, ids
}

func TestEssentialContainerExitStopsTaskAndSiblings(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	store := tracesvc.NewStore()
	defer store.Close()
	r.SetTraceStore(store)

	taskArn, ids := runAppWithSidecar(t, r, svc, "fam-essential-exit")

	// Siblings stopped by Tarn exit non-zero, like a SIGTERM'd process.
	eng.mu.Lock()
	eng.stopExitCode = 143
	eng.mu.Unlock()

	eng.finish(ids["app"], 0, nil)
	waitForCall(t, eng, "stop:"+ids["sidecar"])
	waitForTaskStopped(t, svc, taskArn)

	task, err := svc.GetTask(taskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.StopCode != types.TaskStopCodeEssentialContainerExited {
		t.Fatalf("StopCode = %q, want EssentialContainerExited", task.StopCode)
	}
	if task.StoppedReason != "Essential container in task exited" {
		t.Fatalf("StoppedReason = %q, want AWS's essential container reason", task.StoppedReason)
	}
	if task.DesiredStatus != types.TaskDesiredStatusStopped {
		t.Fatalf("DesiredStatus = %q, want STOPPED", task.DesiredStatus)
	}
	if indexOf(eng.callLog(), "stop:"+ids["app"]) >= 0 {
		t.Fatalf("the exited essential container should not be stopped again: %v", eng.callLog())
	}

	waitForCall(t, eng, "remove:"+ids["sidecar"])
	waitForCall(t, eng, "remove:"+ids["app"])
	r.Stop()

	// The essential container exited 0, so the sidecar's 143 must not turn
	// the task's STOPPED trace into an error.
	for _, tr := range store.Recent(50) {
		if tr.Method != "ECS" {
			continue
		}
		for _, span := range tr.Spans {
			if span.Meta["taskArn"] != taskArn {
				continue
			}
			if code, ok := span.Meta["exitCode"]; ok {
				if code != "0" || tr.Status != 200 {
					t.Fatalf("stopped trace = status %d exitCode %q, want 200 / 0", tr.Status, code)
				}
				return
			}
		}
	}
	t.Fatal("no STOPPED trace recorded for task")
}

func TestNonEssentialContainerExitKeepsTaskRunning(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	taskArn, ids := runAppWithSidecar(t, r, svc, "fam-sidecar-exit")

	eng.finish(ids["sidecar"], 1, nil)
	waitForContainerStopped(t, svc, taskArn, "sidecar")
	waitForCall(t, eng, "remove:"+ids["sidecar"])

	task, err := svc.GetTask(taskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.LastStatus != types.TaskStatusRunning || task.DesiredStatus != types.TaskDesiredStatusRunning || task.StopCode != "" {
		t.Fatalf("task = last %q desired %q stopCode %q, want RUNNING/RUNNING/none", task.LastStatus, task.DesiredStatus, task.StopCode)
	}
	if indexOf(eng.callLog(), "stop:"+ids["app"]) >= 0 {
		t.Fatalf("essential app must keep running after a non-essential exit: %v", eng.callLog())
	}

	finishAllContainers(eng, 0, nil)
	waitForTaskStopped(t, svc, taskArn)
	r.Stop()
}

func TestStopTaskIsNotReportedAsEssentialContainerExit(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, _, _ := newTestRunner(t)
	taskArn, _ := runAppWithSidecar(t, r, svc, "fam-essential-user-stop")

	if err := r.StopTask(context.Background(), "", taskArn, "user stop"); err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	waitForTaskStopped(t, svc, taskArn)

	task, err := svc.GetTask(taskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.StopCode != types.TaskStopCodeUserInitiated || task.StoppedReason != "user stop" {
		t.Fatalf("task = stopCode %q reason %q, want UserInitiated / user stop", task.StopCode, task.StoppedReason)
	}
	r.Stop()
}
