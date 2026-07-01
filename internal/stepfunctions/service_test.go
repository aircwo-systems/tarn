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
