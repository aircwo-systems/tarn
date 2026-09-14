package ecs

import (
	"context"
	"strings"
	"testing"
	"time"

	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// TestRunnerInjectsAccountIDAndCorrelationIDIntoContainerSpec verifies every
// container the runner starts carries the account's own ID (so SDK calls
// from inside it are attributed to this account, not the default one — see
// internal/account) and a per-task correlation ID (exposed to the container
// as TARN_CORRELATION_ID by the engine), minted fresh when RunTask supplies
// none.
func TestRunnerInjectsAccountIDAndCorrelationIDIntoContainerSpec(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-env")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	specs := eng.createdSpecs()
	if len(specs) != 1 {
		t.Fatalf("created specs = %d, want 1", len(specs))
	}
	spec := specs[0]
	if spec.AccountID != "000000000000" {
		t.Fatalf("spec.AccountID = %q, want 000000000000", spec.AccountID)
	}
	if spec.CorrelationID == "" {
		t.Fatalf("spec.CorrelationID was not set")
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
	_ = out
}

// TestRunnerUsesUpstreamCorrelationIDWhenProvided verifies RunTaskInput's
// CorrelationID (the field an upstream dispatcher such as EventBridge or
// Step Functions sets) is used verbatim instead of a freshly minted one.
func TestRunnerUsesUpstreamCorrelationIDWhenProvided(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-corr")

	_, err := r.RunTask(context.Background(), &types.RunTaskInput{
		TaskDefinition: td.Family,
		CorrelationID:  "upstream-corr-id",
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	specs := eng.createdSpecs()
	if len(specs) != 1 || specs[0].CorrelationID != "upstream-corr-id" {
		t.Fatalf("expected spec.CorrelationID = upstream-corr-id, got %+v", specs)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestRunnerContainerOverrideEnvironmentReachesSpec verifies a task
// definition's own AWS_ACCESS_KEY_ID/TARN_CORRELATION_ID (set as ordinary
// container environment) flows through to the spec's Env map unchanged —
// this is what lets it win over the runner-assigned defaults once the
// engine builds the final container environment (see
// internal/engine.buildTaskContainerEnv, which applies spec.Env last).
func TestRunnerContainerOverrideEnvironmentReachesSpec(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "fam-override-env",
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:  "app",
				Image: "example/app:latest",
				Environment: []types.KeyValuePair{
					{Name: "AWS_ACCESS_KEY_ID", Value: "custom-key"},
					{Name: "TARN_CORRELATION_ID", Value: "custom-corr"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	_, err = r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	specs := eng.createdSpecs()
	if len(specs) != 1 {
		t.Fatalf("created specs = %d, want 1", len(specs))
	}
	if specs[0].Env["AWS_ACCESS_KEY_ID"] != "custom-key" {
		t.Fatalf("spec.Env[AWS_ACCESS_KEY_ID] = %q, want custom-key", specs[0].Env["AWS_ACCESS_KEY_ID"])
	}
	if specs[0].Env["TARN_CORRELATION_ID"] != "custom-corr" {
		t.Fatalf("spec.Env[TARN_CORRELATION_ID] = %q, want custom-corr", specs[0].Env["TARN_CORRELATION_ID"])
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestRunnerRecordsRunningAndStoppedTracesWithMatchingCorrelationID verifies
// a task reaching RUNNING and then STOPPED each produce a trace, both
// carrying the same CorrelationID, Method "ECS", and a Path scoped to the
// cluster/task-definition-family.
func TestRunnerRecordsRunningAndStoppedTracesWithMatchingCorrelationID(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	store := tracesvc.NewStore()
	defer store.Close()
	r.SetTraceStore(store)

	td := registerSingleContainerTaskDef(t, svc, "fam-trace")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(out.Tasks))
	}
	taskArn := out.Tasks[0].TaskArn

	finishAllContainers(eng, 0, nil)
	waitForCall(t, eng, "remove:container-1")
	r.Stop()

	traces := store.Recent(50)
	var running, stopped *tracesvc.Trace
	for _, tr := range traces {
		if tr.Method != "ECS" {
			continue
		}
		for _, span := range tr.Spans {
			if span.Meta["taskArn"] != taskArn {
				continue
			}
			if span.Status == "ok" && running == nil && tr.Status == 200 {
				// Distinguish RUNNING (no stoppedReason/exitCode meta) from
				// STOPPED (which always adds exitCode for a clean exit).
				if _, hasExit := span.Meta["exitCode"]; !hasExit {
					running = tr
					continue
				}
			}
			if _, hasExit := span.Meta["exitCode"]; hasExit {
				stopped = tr
			}
		}
	}

	if running == nil {
		t.Fatalf("no RUNNING trace found among %d traces: %+v", len(traces), traces)
	}
	if stopped == nil {
		t.Fatalf("no STOPPED trace found among %d traces: %+v", len(traces), traces)
	}
	if running.CorrelationID == "" || running.CorrelationID != stopped.CorrelationID {
		t.Fatalf("correlation IDs do not match: running=%q stopped=%q", running.CorrelationID, stopped.CorrelationID)
	}
	if !strings.HasPrefix(running.Path, "/ecs/") || !strings.HasSuffix(running.Path, "/fam-trace") {
		t.Fatalf("running.Path = %q, want prefix /ecs/ and suffix /fam-trace", running.Path)
	}
	if stopped.Path != running.Path {
		t.Fatalf("stopped.Path = %q, want %q", stopped.Path, running.Path)
	}
	if stopped.Status != 200 {
		t.Fatalf("stopped.Status = %d, want 200 for a clean exit", stopped.Status)
	}
	if stopped.Spans[0].Meta["exitCode"] != "0" {
		t.Fatalf("stopped exitCode meta = %q, want 0", stopped.Spans[0].Meta["exitCode"])
	}
}

// TestRunnerRecordsErrorStoppedTraceOnNonZeroExit verifies a non-zero exit
// code on the (essential-by-default) container marks the STOPPED trace's
// span status "error" and its Trace.Status 500.
func TestRunnerRecordsErrorStoppedTraceOnNonZeroExit(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	store := tracesvc.NewStore()
	defer store.Close()
	r.SetTraceStore(store)

	td := registerSingleContainerTaskDef(t, svc, "fam-trace-error")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	taskArn := out.Tasks[0].TaskArn

	finishAllContainers(eng, 1, nil)
	waitForCall(t, eng, "remove:container-1")
	r.Stop()

	traces := store.Recent(50)
	var stopped *tracesvc.Trace
	for _, tr := range traces {
		if tr.Method != "ECS" {
			continue
		}
		for _, span := range tr.Spans {
			if span.Meta["taskArn"] == taskArn {
				if _, hasExit := span.Meta["exitCode"]; hasExit {
					stopped = tr
				}
			}
		}
	}
	if stopped == nil {
		t.Fatalf("no STOPPED trace found among %d traces", len(traces))
	}
	if stopped.Status != 500 {
		t.Fatalf("stopped.Status = %d, want 500 for a non-zero exit", stopped.Status)
	}
	if stopped.Spans[0].Status != "error" {
		t.Fatalf("stopped span status = %q, want error", stopped.Spans[0].Status)
	}
	if stopped.Spans[0].Meta["exitCode"] != "1" {
		t.Fatalf("stopped exitCode meta = %q, want 1", stopped.Spans[0].Meta["exitCode"])
	}
}

// TestRunnerSkipsTraceRecordingWithNoTraceStore verifies trace recording is
// a nil-safe no-op when SetTraceStore was never called — the default state
// for a Runner in tests/production paths that don't wire one up.
func TestRunnerSkipsTraceRecordingWithNoTraceStore(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-no-trace")

	if _, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family}); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
	// No assertion beyond "did not panic": r.traceStore is nil throughout.
}

// TestRunnerRecordsOKStoppedTraceWhenStopWasRequested verifies that a task
// stopped on purpose is not reported as a failure: the SIGKILL exit code a
// container returns when it is told to stop is the expected outcome.
func TestRunnerRecordsOKStoppedTraceWhenStopWasRequested(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	store := tracesvc.NewStore()
	defer store.Close()
	r.SetTraceStore(store)

	eng.mu.Lock()
	eng.stopExitCode = 137
	eng.mu.Unlock()
	td := registerSingleContainerTaskDef(t, svc, "fam-trace-stop")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	taskArn := out.Tasks[0].TaskArn

	if err := r.StopTask(context.Background(), "", taskArn, "scale down"); err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	waitForContainerStopped(t, svc, taskArn, "app")

	var stopped *tracesvc.Trace
	deadline := time.Now().Add(2 * time.Second)
	for stopped == nil && time.Now().Before(deadline) {
		for _, tr := range store.Recent(50) {
			if tr.Method == "ECS" && tr.Spans[0].Meta["taskArn"] == taskArn && tr.Spans[0].Meta["stopRequested"] == "true" {
				stopped = tr
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if stopped == nil {
		t.Fatal("no STOPPED trace marked stopRequested")
	}
	if stopped.Spans[0].Meta["exitCode"] != "137" {
		t.Fatalf("exitCode meta = %q, want 137", stopped.Spans[0].Meta["exitCode"])
	}
	if stopped.Status != 200 || stopped.Spans[0].Status != "ok" {
		t.Fatalf("stopped trace status = %d/%q, want 200/ok (exitCode=%q)",
			stopped.Status, stopped.Spans[0].Status, stopped.Spans[0].Meta["exitCode"])
	}
}
