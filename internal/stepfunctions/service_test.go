package stepfunctions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/stepfunctions/interpreter"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// fakeLambda is a configurable LambdaInterface for tests.
type fakeLambda struct {
	mu       sync.Mutex
	lastName string
	result   json.RawMessage
	funcErr  string
	err      error
}

func (f *fakeLambda) Invoke(_ context.Context, in *types.InvokeInput) (*types.InvokeOutput, error) {
	f.mu.Lock()
	f.lastName = in.FunctionName
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.funcErr != "" {
		return &types.InvokeOutput{StatusCode: 200, FunctionError: f.funcErr, Payload: f.result}, nil
	}
	return &types.InvokeOutput{StatusCode: 200, Payload: f.result}, nil
}

func (f *fakeLambda) name() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastName
}

func newService(t *testing.T, lambda LambdaInterface) *Service {
	t.Helper()
	cfg := &config.Config{
		Region: "us-east-1", AccountID: "000000000000",
		DataDir: t.TempDir(), PersistenceEnabled: false,
	}
	svc := NewService(cfg, NewStore(cfg), lambda)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	return svc
}

func waitForTerminal(t *testing.T, svc *Service, arn string) *types.Execution {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ex, err := svc.DescribeExecution(arn)
		if err != nil {
			t.Fatalf("describe: %v", err)
		}
		if ex.Status != types.ExecutionStatusRunning {
			return ex
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("execution %s did not reach a terminal state in time", arn)
	return nil
}

type fakeECSTaskIntegration struct {
	mu         sync.Mutex
	input      *types.RunTaskInput
	task       types.Task
	exitCode   *int64
	stopOnPoll int
	polls      int
	stopCalls  []string
	stopReason []string
	failures   []types.Failure
	runErr     error
	notFound   bool
}

func (f *fakeECSTaskIntegration) RunTask(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	f.mu.Lock()
	copyInput := *in
	f.input = &copyInput
	task := f.task
	failures := append([]types.Failure(nil), f.failures...)
	runErr := f.runErr
	f.mu.Unlock()
	return &types.RunTaskOutput{Tasks: []types.Task{task}, Failures: failures}, runErr
}

func (f *fakeECSTaskIntegration) StopTask(_ context.Context, _, taskArn, reason string) error {
	f.mu.Lock()
	f.stopCalls = append(f.stopCalls, taskArn)
	f.stopReason = append(f.stopReason, reason)
	f.mu.Unlock()
	return nil
}

func (f *fakeECSTaskIntegration) GetTask(taskRef string) (*types.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	if f.notFound {
		return nil, fmt.Errorf("task %s not found", taskRef)
	}
	task := f.task
	if task.TaskArn != taskRef {
		return nil, fmt.Errorf("unexpected task reference %q", taskRef)
	}
	if f.stopOnPoll > 0 && f.polls >= f.stopOnPoll {
		task.LastStatus = types.TaskStatusStopped
		for i := range task.Containers {
			task.Containers[i].LastStatus = types.TaskStatusStopped
			task.Containers[i].ExitCode = f.exitCode
		}
	}
	return &task, nil
}

func (f *fakeECSTaskIntegration) recordedInput() *types.RunTaskInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.input == nil {
		return nil
	}
	copyInput := *f.input
	return &copyInput
}

func (f *fakeECSTaskIntegration) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.stopCalls)
}

func (f *fakeECSTaskIntegration) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

func ecsFakeTask() types.Task {
	return types.Task{
		TaskArn:           "arn:aws:ecs:us-east-1:000000000000:task/default/task-1",
		ClusterArn:        "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1",
		LastStatus:        types.TaskStatusRunning,
		DesiredStatus:     types.TaskDesiredStatusRunning,
		Containers: []types.TaskContainer{{
			Name:         "worker",
			ContainerArn: "arn:aws:ecs:us-east-1:000000000000:container/default/task-1/worker",
			LastStatus:   types.TaskStatusRunning,
		}},
	}
}

func TestECSRunTaskSyncReturnsStoppedTask(t *testing.T) {
	zero := int64(0)
	fake := &fakeECSTaskIntegration{
		task:       ecsFakeTask(),
		exitCode:   &zero,
		stopOnPoll: 2,
	}
	exec := &taskExecutor{ecsRunner: fake, ecsLookup: fake}

	result, err := exec.RunTask(context.Background(), interpreter.TaskRequest{
		Resource: ecsRunTaskSyncResource,
		Payload: json.RawMessage(`{
			"Cluster":"arn:aws:ecs:us-east-1:000000000000:cluster/default",
			"TaskDefinition":"arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1",
			"LaunchType":"FARGATE",
			"Overrides":{"ContainerOverrides":[{"Name":"worker","Command":["hello"]}]}
		}`),
	})
	if err != nil {
		t.Fatalf("run ECS task: %v", err)
	}

	var out types.RunTaskOutput
	if err := json.Unmarshal(result.Output, &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].LastStatus != types.TaskStatusStopped {
		t.Fatalf("unexpected completed tasks: %+v", out.Tasks)
	}
	if out.Tasks[0].Containers[0].ExitCode == nil || *out.Tasks[0].Containers[0].ExitCode != 0 {
		t.Fatalf("unexpected exit code: %+v", out.Tasks[0].Containers[0].ExitCode)
	}
	input := fake.recordedInput()
	if input == nil || input.Cluster == "" || input.TaskDefinition == "" || input.LaunchType != types.LaunchTypeFargate {
		t.Fatalf("ECS input was not forwarded: %+v", input)
	}
	if input.Overrides == nil || len(input.Overrides.ContainerOverrides) != 1 {
		t.Fatalf("ECS overrides were not forwarded: %+v", input.Overrides)
	}
}

func TestECSRunTaskSyncFailureReturnsStatesTaskFailed(t *testing.T) {
	failed := int64(17)
	fake := &fakeECSTaskIntegration{
		task:       ecsFakeTask(),
		exitCode:   &failed,
		stopOnPoll: 1,
	}
	exec := &taskExecutor{ecsRunner: fake, ecsLookup: fake}

	_, err := exec.RunTask(context.Background(), interpreter.TaskRequest{
		Resource: ecsRunTaskSyncResource,
		Payload:  json.RawMessage(`{"TaskDefinition":"worker:1"}`),
	})
	var stateErr *interpreter.StateError
	if !errors.As(err, &stateErr) || stateErr.Name != interpreter.ErrTaskFailed {
		t.Fatalf("expected States.TaskFailed, got %v", err)
	}
	if !strings.Contains(stateErr.Cause, "17") {
		t.Fatalf("failure cause does not include exit code: %q", stateErr.Cause)
	}
}

func TestECSRunTaskSyncCancellationStopsTask(t *testing.T) {
	fake := &fakeECSTaskIntegration{task: ecsFakeTask()}
	exec := &taskExecutor{ecsRunner: fake, ecsLookup: fake}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	_, err := exec.RunTask(ctx, interpreter.TaskRequest{
		Resource: ecsRunTaskSyncResource,
		Payload:  json.RawMessage(`{"Cluster":"default","TaskDefinition":"worker:1"}`),
	})
	if !errors.Is(err, interpreter.ErrAborted) {
		t.Fatalf("expected cancellation to abort task, got %v", err)
	}
	if fake.stopCount() == 0 {
		t.Fatal("expected cancellation to stop the ECS task")
	}
}

func TestECSRunTaskSyncTaskTimeoutStopsTask(t *testing.T) {
	fake := &fakeECSTaskIntegration{task: ecsFakeTask()}
	exec := &taskExecutor{ecsRunner: fake, ecsLookup: fake}

	_, err := exec.RunTask(context.Background(), interpreter.TaskRequest{
		Resource:       ecsRunTaskSyncResource,
		Payload:        json.RawMessage(`{"Cluster":"default","TaskDefinition":"worker:1"}`),
		TimeoutSeconds: 1,
	})
	var stateErr *interpreter.StateError
	if !errors.As(err, &stateErr) || stateErr.Name != interpreter.ErrTimeout {
		t.Fatalf("expected States.Timeout, got %v", err)
	}
	if fake.stopCount() == 0 {
		t.Fatal("expected task timeout to stop the ECS task")
	}
}

// TestECSTaskContextNoTimeoutHasNoDeadline pins the fix for the wrong
// hard-coded 60s default: real Step Functions imposes no ceiling on
// ecs:runTask.sync when TimeoutSeconds is omitted, so the derived context
// must not carry a deadline either.
func TestECSTaskContextNoTimeoutHasNoDeadline(t *testing.T) {
	ctx, cancel := ecsTaskContext(context.Background(), 0)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("expected no deadline when TimeoutSeconds is unset")
	}
}

// TestECSTaskContextNoTimeoutCancelsWithParent confirms the unbounded wait
// still ends when the execution is aborted (parent context cancelled), the
// same way the timeout path already does.
func TestECSTaskContextNoTimeoutCancelsWithParent(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	ctx, cancel := ecsTaskContext(parent, 0)
	defer cancel()
	parentCancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected the derived context to be cancelled with its parent")
	}
}

func TestECSTaskContextWithTimeoutHasDeadline(t *testing.T) {
	ctx, cancel := ecsTaskContext(context.Background(), 5)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected a deadline when TimeoutSeconds is set")
	}
}

// TestECSRunTaskSyncWithoutTimeoutWaitsPastOldDefault confirms a
// long-running task (several polls beyond what the old 60s default would
// tolerate in spirit) still succeeds once it stops, with no TimeoutSeconds
// set on the request.
func TestECSRunTaskSyncWithoutTimeoutWaitsPastOldDefault(t *testing.T) {
	zero := int64(0)
	fake := &fakeECSTaskIntegration{
		task:       ecsFakeTask(),
		exitCode:   &zero,
		stopOnPoll: 5,
	}
	exec := &taskExecutor{ecsRunner: fake, ecsLookup: fake}

	result, err := exec.RunTask(context.Background(), interpreter.TaskRequest{
		Resource: ecsRunTaskSyncResource,
		Payload:  json.RawMessage(`{"Cluster":"default","TaskDefinition":"worker:1"}`),
	})
	if err != nil {
		t.Fatalf("run ECS task: %v", err)
	}
	var out types.RunTaskOutput
	if err := json.Unmarshal(result.Output, &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].LastStatus != types.TaskStatusStopped {
		t.Fatalf("unexpected completed tasks: %+v", out.Tasks)
	}
}

// TestECSRunTaskSyncGetTaskNotFoundIsTerminal pins waitECSTasks' existing
// (and easy to accidentally regress) behavior: once GetTask reports the
// task record is gone, e.g. pruned after STOPPED, that is a terminal
// failure on the first observation, never an infinite poll loop.
func TestECSRunTaskSyncGetTaskNotFoundIsTerminal(t *testing.T) {
	fake := &fakeECSTaskIntegration{task: ecsFakeTask(), notFound: true}
	exec := &taskExecutor{ecsRunner: fake, ecsLookup: fake}

	_, err := exec.RunTask(context.Background(), interpreter.TaskRequest{
		Resource: ecsRunTaskSyncResource,
		Payload:  json.RawMessage(`{"Cluster":"default","TaskDefinition":"worker:1"}`),
	})
	var stateErr *interpreter.StateError
	if !errors.As(err, &stateErr) || stateErr.Name != interpreter.ErrTaskFailed {
		t.Fatalf("expected States.TaskFailed, got %v", err)
	}
	if fake.pollCount() != 1 {
		t.Fatalf("expected exactly one GetTask poll before terminal failure, got %d", fake.pollCount())
	}
}

func TestECSRunTaskSyncStateUsesResolvedParameters(t *testing.T) {
	zero := int64(0)
	fake := &fakeECSTaskIntegration{
		task:       ecsFakeTask(),
		exitCode:   &zero,
		stopOnPoll: 1,
	}
	svc := newService(t, nil)
	svc.SetECSTaskRunner(fake, fake)

	definition := `{
		"StartAt":"Run",
		"States":{
			"Run":{
				"Type":"Task",
				"Resource":"arn:aws:states:::ecs:runTask.sync",
				"Parameters":{
					"Cluster.$":"$.cluster",
					"TaskDefinition.$":"$.taskDefinition"
				},
				"End":true
			}
		}
	}`
	sm, err := svc.CreateStateMachine("ecs-sync", definition, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	execution, err := svc.StartExecution(sm.Arn, "run1", `{
		"cluster":"arn:aws:ecs:us-east-1:000000000000:cluster/default",
		"taskDefinition":"arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1"
	}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, execution.Arn)
	if done.Status != types.ExecutionStatusSucceeded {
		t.Fatalf("status = %q, cause = %q", done.Status, done.Cause)
	}
	var out types.RunTaskOutput
	if err := json.Unmarshal([]byte(done.Output), &out); err != nil {
		t.Fatalf("decode execution output: %v", err)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].LastStatus != types.TaskStatusStopped {
		t.Fatalf("unexpected execution output: %+v", out.Tasks)
	}
	input := fake.recordedInput()
	if input == nil || input.Cluster == "" || input.TaskDefinition == "" {
		t.Fatalf("resolved Parameters were not forwarded: %+v", input)
	}
}

func TestCreateAndDescribeStateMachine(t *testing.T) {
	svc := newService(t, nil)
	def := `{"StartAt":"P","States":{"P":{"Type":"Pass","End":true}}}`
	sm, err := svc.CreateStateMachine("hello", def, "arn:role", "", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sm.Arn != "arn:aws:states:us-east-1:000000000000:stateMachine:hello" {
		t.Fatalf("arn = %q", sm.Arn)
	}
	got, err := svc.DescribeStateMachine(sm.Arn)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if got.Status != types.StateMachineStatusActive || got.Tags["k"] != "v" {
		t.Fatalf("unexpected machine: %+v", got)
	}
	if n := len(svc.ListStateMachines()); n != 1 {
		t.Fatalf("list len = %d", n)
	}

	// Duplicate create is rejected.
	if _, err := svc.CreateStateMachine("hello", def, "", "", nil); err == nil {
		t.Fatal("expected StateMachineAlreadyExists")
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	svc := newService(t, nil)

	_, err := svc.CreateStateMachine("bad", `{"StartAt":"X","States":{}}`, "", "", nil)
	var se *ServiceError
	if !errors.As(err, &se) || se.Code != "InvalidDefinition" {
		t.Fatalf("expected InvalidDefinition, got %v", err)
	}

	valid := `{"StartAt":"P","States":{"P":{"Type":"Pass","End":true}}}`
	_, err = svc.CreateStateMachine("express", valid, "", types.StateMachineTypeExpress, nil)
	if !errors.As(err, &se) || se.Code != "ValidationException" {
		t.Fatalf("expected ValidationException for EXPRESS, got %v", err)
	}
}

func TestStartExecutionPass(t *testing.T) {
	svc := newService(t, nil)
	def := `{"StartAt":"P","States":{"P":{"Type":"Pass","Result":{"hello":"world"},"End":true}}}`
	sm, err := svc.CreateStateMachine("pass", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusSucceeded {
		t.Fatalf("status = %q, cause = %q", done.Status, done.Cause)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(done.Output), &out); err != nil {
		t.Fatalf("output decode: %v", err)
	}
	if out["hello"] != "world" {
		t.Fatalf("output = %v", out)
	}
	if len(done.History) == 0 {
		t.Fatal("expected non-empty history")
	}
}

func TestStartExecutionLambdaTask(t *testing.T) {
	fl := &fakeLambda{result: json.RawMessage(`{"ok":true}`)}
	svc := newService(t, fl)
	def := `{"StartAt":"T","States":{"T":{"Type":"Task","Resource":"arn:aws:lambda:us-east-1:000000000000:function:hello","End":true}}}`
	sm, err := svc.CreateStateMachine("task", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{"in":1}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusSucceeded {
		t.Fatalf("status = %q, cause = %q", done.Status, done.Cause)
	}
	if fl.name() != "hello" {
		t.Fatalf("lambda invoked with %q, want hello", fl.name())
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(done.Output), &out)
	if out["ok"] != true {
		t.Fatalf("output = %v", out)
	}
}

func TestExecutionCatchOnLambdaError(t *testing.T) {
	fl := &fakeLambda{funcErr: "Unhandled", result: json.RawMessage(`{"errorMessage":"boom"}`)}
	svc := newService(t, fl)
	def := `{
		"StartAt":"T",
		"States":{
			"T":{
				"Type":"Task",
				"Resource":"arn:aws:lambda:us-east-1:000000000000:function:hello",
				"Catch":[{"ErrorEquals":["States.TaskFailed"],"ResultPath":"$.error","Next":"Done"}],
				"End":true
			},
			"Done":{"Type":"Succeed"}
		}
	}`
	sm, err := svc.CreateStateMachine("catch", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{"x":1}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusSucceeded {
		t.Fatalf("status = %q, cause = %q", done.Status, done.Cause)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(done.Output), &out); err != nil {
		t.Fatalf("output decode: %v", err)
	}
	if out["x"] != float64(1) {
		t.Fatalf("expected original input preserved, got %v", out)
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok || errObj["Error"] != "States.TaskFailed" {
		t.Fatalf("expected caught error at $.error, got %v", out["error"])
	}
}

func TestStopExecution(t *testing.T) {
	svc := newService(t, nil)
	// A long Wait gives us time to stop the execution before it completes.
	def := `{"StartAt":"W","States":{"W":{"Type":"Wait","Seconds":30,"Next":"D"},"D":{"Type":"Succeed"}}}`
	sm, err := svc.CreateStateMachine("stoppable", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // let it enter the Wait state
	if _, err := svc.StopExecution(exec.Arn); err != nil {
		t.Fatalf("stop: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusAborted {
		t.Fatalf("status = %q, want ABORTED", done.Status)
	}
}

// ---- HTTP Task service-level tests (httptest.Server) ----

// newServiceWithEndpoint creates a Service whose config Endpoint matches the
// given host:port string so isTarnHost allows test server requests.
func newServiceWithEndpoint(t *testing.T, serverHost string) *Service {
	t.Helper()
	// httptest.Server listens on 127.0.0.1 which is always allowed as loopback;
	// we don't need to set a special endpoint.
	cfg := &config.Config{
		Region: "us-east-1", AccountID: "111111111111",
		Host: "127.0.0.1", Port: 4566,
		DataDir: t.TempDir(), PersistenceEnabled: false,
	}
	_ = serverHost // loopback is always allowed
	svc := NewService(cfg, NewStore(cfg), nil)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	return svc
}

// TestHTTPTaskServiceSuccess verifies that runHTTP calls the test server,
// returns {StatusCode,Headers,ResponseBody}, and emits Task* events.
func TestHTTPTaskServiceSuccess(t *testing.T) {
	// Stand up a test HTTP server that returns a JSON body.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"deleted":true}`)
	}))
	defer ts.Close()

	svc := newServiceWithEndpoint(t, ts.Listener.Addr().String())
	def := fmt.Sprintf(`{
		"StartAt":"Call",
		"States":{
			"Call":{
				"Type":"Task",
				"Resource":"arn:aws:states:::http:invoke",
				"Parameters":{
					"ApiEndpoint":"%s/v1/resource",
					"Method":"DELETE"
				},
				"End":true
			}
		}
	}`, ts.URL)

	sm, err := svc.CreateStateMachine("http-task", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusSucceeded {
		t.Fatalf("status = %q, cause = %q", done.Status, done.Cause)
	}

	// Verify output shape.
	var out map[string]any
	if err := json.Unmarshal([]byte(done.Output), &out); err != nil {
		t.Fatalf("output decode: %v", err)
	}
	if out["StatusCode"] != float64(200) {
		t.Errorf("StatusCode = %v, want 200", out["StatusCode"])
	}
	rb, ok := out["ResponseBody"].(map[string]any)
	if !ok {
		t.Fatalf("ResponseBody not a map: %T %v", out["ResponseBody"], out["ResponseBody"])
	}
	if rb["deleted"] != true {
		t.Errorf("ResponseBody.deleted = %v, want true", rb["deleted"])
	}

	// Verify Task* events in history.
	found := map[string]bool{}
	for _, ev := range done.History {
		found[ev.Type] = true
	}
	for _, want := range []string{"TaskScheduled", "TaskStarted", "TaskSucceeded"} {
		if !found[want] {
			t.Errorf("expected history event %q, got types: %v", want, historyTypes(done.History))
		}
	}
}

// TestHTTPTaskServiceNonTwxx verifies non-2xx → States.Http.StatusCodeFailure.
func TestHTTPTaskServiceNonTwxx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "internal error")
	}))
	defer ts.Close()

	svc := newServiceWithEndpoint(t, ts.Listener.Addr().String())
	def := fmt.Sprintf(`{
		"StartAt":"Call",
		"States":{
			"Call":{
				"Type":"Task",
				"Resource":"arn:aws:states:::http:invoke",
				"Parameters":{
					"ApiEndpoint":"%s/v1/resource",
					"Method":"GET"
				},
				"Catch":[{
					"ErrorEquals":["%s"],
					"ResultPath":"$.httpErr",
					"Next":"Recover"
				}],
				"End":true
			},
			"Recover":{"Type":"Succeed"}
		}
	}`, ts.URL, interpreter.ErrHTTPStatusCode)

	sm, err := svc.CreateStateMachine("http-task-fail", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{"id":"x"}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusSucceeded {
		t.Fatalf("status = %q, cause = %q (Catch should have handled non-2xx)", done.Status, done.Cause)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(done.Output), &out); err != nil {
		t.Fatalf("output decode: %v", err)
	}
	errObj, ok := out["httpErr"].(map[string]any)
	if !ok {
		t.Fatalf("$.httpErr not a map: %v", out["httpErr"])
	}
	if errObj["Error"] != interpreter.ErrHTTPStatusCode {
		t.Errorf("$.httpErr.Error = %q, want %q", errObj["Error"], interpreter.ErrHTTPStatusCode)
	}
}

// TestHTTPTaskServiceSocketException verifies that a transport error
// (no server listening) → States.Http.SocketException.
func TestHTTPTaskServiceSocketException(t *testing.T) {
	// Point at a port with nothing listening on loopback.
	svc := newServiceWithEndpoint(t, "127.0.0.1:1")
	def := `{
		"StartAt":"Call",
		"States":{
			"Call":{
				"Type":"Task",
				"Resource":"arn:aws:states:::http:invoke",
				"Parameters":{
					"ApiEndpoint":"http://127.0.0.1:1/v1/resource",
					"Method":"GET"
				},
				"Catch":[{
					"ErrorEquals":["States.Http.SocketException"],
					"ResultPath":"$.sockErr",
					"Next":"Recover"
				}],
				"End":true
			},
			"Recover":{"Type":"Succeed"}
		}
	}`
	sm, err := svc.CreateStateMachine("http-socket", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{"id":"y"}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusSucceeded {
		t.Fatalf("status = %q, cause = %q (Catch should have handled SocketException)", done.Status, done.Cause)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(done.Output), &out); err != nil {
		t.Fatalf("output decode: %v", err)
	}
	errObj, ok := out["sockErr"].(map[string]any)
	if !ok {
		t.Fatalf("$.sockErr not a map: %v", out["sockErr"])
	}
	if errObj["Error"] != interpreter.ErrHTTPSocket {
		t.Errorf("$.sockErr.Error = %q, want %q", errObj["Error"], interpreter.ErrHTTPSocket)
	}
}

// TestHTTPTaskServiceSSRFGuard verifies that a non-Tarn external host is rejected.
func TestHTTPTaskServiceSSRFGuard(t *testing.T) {
	svc := newServiceWithEndpoint(t, "")
	def := `{
		"StartAt":"Call",
		"States":{
			"Call":{
				"Type":"Task",
				"Resource":"arn:aws:states:::http:invoke",
				"Parameters":{
					"ApiEndpoint":"http://evil.example.com/steal",
					"Method":"GET"
				},
				"End":true
			}
		}
	}`
	sm, err := svc.CreateStateMachine("http-ssrf", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusFailed {
		t.Fatalf("expected FAILED (SSRF guard), got status=%q cause=%q", done.Status, done.Cause)
	}
	if !strings.Contains(done.Cause, "not an allowed Tarn endpoint") {
		t.Errorf("expected SSRF cause message, got %q", done.Cause)
	}
}

// TestHTTPTaskServiceAccountHeader verifies that the Authorization header
// carrying the account credential is set on requests to Tarn-hosted surfaces.
func TestHTTPTaskServiceAccountHeader(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"ok":true}`)
	}))
	defer ts.Close()

	svc := newServiceWithEndpoint(t, ts.Listener.Addr().String())
	def := fmt.Sprintf(`{
		"StartAt":"Call",
		"States":{
			"Call":{
				"Type":"Task",
				"Resource":"arn:aws:states:::http:invoke",
				"Parameters":{
					"ApiEndpoint":"%s/v1/resource",
					"Method":"GET"
				},
				"End":true
			}
		}
	}`, ts.URL)

	sm, err := svc.CreateStateMachine("http-auth", def, "", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec, err := svc.StartExecution(sm.Arn, "run1", `{}`)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := waitForTerminal(t, svc, exec.Arn)
	if done.Status != types.ExecutionStatusSucceeded {
		t.Fatalf("status = %q, cause = %q", done.Status, done.Cause)
	}
	wantAuth := "AWS4-HMAC-SHA256 Credential=111111111111/20000101/us-east-1/tarn/aws4_request, SignedHeaders=host, Signature=0"
	if gotAuth != wantAuth {
		t.Errorf("Authorization header:\n  got:  %q\n  want: %q", gotAuth, wantAuth)
	}
}

// historyTypes is a helper that extracts the event type strings from history.
func historyTypes(events []types.HistoryEvent) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Type
	}
	return out
}
