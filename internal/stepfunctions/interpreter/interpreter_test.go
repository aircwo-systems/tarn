package interpreter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/stepfunctions/asl"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// ---- Test doubles ----

// fakeClock is a Clock whose After always returns an already-closed channel, so
// tests never actually sleep. Now returns a fixed timestamp.
type fakeClock struct {
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) After(_ time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

// fakeExecutor is a configurable TaskExecutor for tests.
type fakeExecutor struct {
	// failTimes: fail this many times before succeeding.
	failTimes int
	// calls counts how many times RunTask was invoked.
	calls atomic.Int32
	// result is returned on success.
	result json.RawMessage
	// err is the error to return when failing (nil → generic error).
	err error
}

func (f *fakeExecutor) RunTask(_ context.Context, _ TaskRequest) (TaskResult, error) {
	n := int(f.calls.Add(1))
	if n <= f.failTimes {
		if f.err != nil {
			return TaskResult{}, f.err
		}
		return TaskResult{}, errors.New("transient failure")
	}
	return TaskResult{Output: f.result}, nil
}

// alwaysFailExecutor always returns a fixed error.
type alwaysFailExecutor struct {
	err error
}

func (a *alwaysFailExecutor) RunTask(_ context.Context, _ TaskRequest) (TaskResult, error) {
	return TaskResult{}, a.err
}

// recordingEmitter collects emitted history events. The interpreter invokes the
// sink outside the emitter's lock and concurrently across Parallel/Map branches,
// so the recorder must guard its own slice (mirroring the production sink, which
// appends under its service mutex).
type recordingEmitter struct {
	mu     sync.Mutex
	events []types.HistoryEvent
}

func (r *recordingEmitter) emit(ev types.HistoryEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

// ---- Helpers ----

// runASL parses definition, runs Execute with the given input JSON string, and
// returns the decoded output (any) and error.
func runASL(t *testing.T, definition, inputJSON string, exec TaskExecutor) (any, error) {
	t.Helper()
	m, err := asl.Parse(definition)
	if err != nil {
		t.Fatalf("asl.Parse: %v", err)
	}
	if inputJSON == "" {
		inputJSON = "{}"
	}
	rec := &recordingEmitter{}
	r := Run{
		Machine:  m,
		Input:    json.RawMessage(inputJSON),
		Executor: exec,
		Clock:    newFakeClock(),
		Emit:     rec.emit,
	}
	raw, runErr := r.Execute(context.Background())
	if runErr != nil {
		return nil, runErr
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("output unmarshal: %v", err)
	}
	return out, nil
}

// ---- Tests ----

// TestPassState verifies a single Pass state with a fixed Result.
func TestPassState(t *testing.T) {
	def := `{
		"StartAt": "P",
		"States": {
			"P": {"Type":"Pass","Result":{"hello":"world"},"End":true}
		}
	}`
	out, err := runASL(t, def, `{}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"hello": "world"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestLinearPassTaskSucceed verifies Pass → Task → Succeed.
func TestLinearPassTaskSucceed(t *testing.T) {
	def := `{
		"StartAt": "Init",
		"States": {
			"Init": {"Type":"Pass","Result":{"n":1},"Next":"Call"},
			"Call": {"Type":"Task","Resource":"arn:aws:lambda:::function:f","Next":"Done"},
			"Done": {"Type":"Succeed"}
		}
	}`
	exec := &fakeExecutor{result: json.RawMessage(`{"n":2}`)}
	out, err := runASL(t, def, `{}`, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"n": float64(2)}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestChoiceMatch verifies that a matching Choice rule routes to the correct state.
func TestChoiceMatch(t *testing.T) {
	def := `{
		"StartAt": "Branch",
		"States": {
			"Branch": {
				"Type":"Choice",
				"Choices":[
					{"Variable":"$.x","NumericEquals":1,"Next":"A"},
					{"Variable":"$.x","NumericEquals":2,"Next":"B"}
				],
				"Default":"B"
			},
			"A": {"Type":"Pass","Result":{"got":"A"},"End":true},
			"B": {"Type":"Pass","Result":{"got":"B"},"End":true}
		}
	}`
	out, err := runASL(t, def, `{"x":1}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"got": "A"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestChoiceDefault verifies that the Default branch fires when no rule matches.
func TestChoiceDefault(t *testing.T) {
	def := `{
		"StartAt": "Branch",
		"States": {
			"Branch": {
				"Type":"Choice",
				"Choices":[{"Variable":"$.x","NumericEquals":99,"Next":"A"}],
				"Default":"B"
			},
			"A": {"Type":"Pass","Result":{"got":"A"},"End":true},
			"B": {"Type":"Pass","Result":{"got":"B"},"End":true}
		}
	}`
	out, err := runASL(t, def, `{"x":1}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"got": "B"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestChoiceNoMatch verifies States.NoChoiceMatched when no rule nor Default.
func TestChoiceNoMatch(t *testing.T) {
	def := `{
		"StartAt": "Branch",
		"States": {
			"Branch": {
				"Type":"Choice",
				"Choices":[{"Variable":"$.x","NumericEquals":99,"Next":"A"}]
			},
			"A": {"Type":"Succeed"}
		}
	}`
	_, err := runASL(t, def, `{"x":1}`, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*StateError)
	if !ok {
		t.Fatalf("expected *StateError, got %T: %v", err, err)
	}
	if se.Name != ErrNoChoiceMatched {
		t.Errorf("got %q, want %q", se.Name, ErrNoChoiceMatched)
	}
}

// TestWait verifies that Wait returns the input after the (fake) delay.
func TestWait(t *testing.T) {
	def := `{
		"StartAt": "W",
		"States": {
			"W": {"Type":"Wait","Seconds":5,"Next":"Done"},
			"Done": {"Type":"Succeed"}
		}
	}`
	out, err := runASL(t, def, `{"key":"val"}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"key": "val"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestFail verifies that a Fail state returns a StateError.
func TestFail(t *testing.T) {
	def := `{
		"StartAt": "F",
		"States": {
			"F": {"Type":"Fail","Error":"MyError","Cause":"bad thing"}
		}
	}`
	_, err := runASL(t, def, `{}`, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*StateError)
	if !ok {
		t.Fatalf("expected *StateError, got %T: %v", err, err)
	}
	if se.Name != "MyError" {
		t.Errorf("Name: got %q, want %q", se.Name, "MyError")
	}
	if se.Cause != "bad thing" {
		t.Errorf("Cause: got %q, want %q", se.Cause, "bad thing")
	}
}

// TestRetrySucceeds verifies that an executor that fails twice then succeeds
// is retried the correct number of times.
func TestRetrySucceeds(t *testing.T) {
	def := `{
		"StartAt": "T",
		"States": {
			"T": {
				"Type":"Task",
				"Resource":"arn:aws:lambda:::function:f",
				"Retry":[{"ErrorEquals":["States.ALL"],"MaxAttempts":3}],
				"End":true
			}
		}
	}`
	exec := &fakeExecutor{
		failTimes: 2,
		result:    json.RawMessage(`{"ok":true}`),
	}
	out, err := runASL(t, def, `{}`, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"ok": true}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
	if int(exec.calls.Load()) != 3 {
		t.Errorf("expected 3 calls, got %d", exec.calls.Load())
	}
}

// TestRetryExhausted verifies that after exhausting retries the error propagates.
func TestRetryExhausted(t *testing.T) {
	def := `{
		"StartAt": "T",
		"States": {
			"T": {
				"Type":"Task",
				"Resource":"arn:aws:lambda:::function:f",
				"Retry":[{"ErrorEquals":["States.ALL"],"MaxAttempts":2}],
				"End":true
			}
		}
	}`
	exec := &fakeExecutor{
		failTimes: 999,
		err:       &StateError{Name: ErrTaskFailed, Cause: "always fails"},
	}
	_, err := runASL(t, def, `{}`, exec)
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*StateError)
	if !ok {
		t.Fatalf("expected *StateError, got %T: %v", err, err)
	}
	if se.Name != ErrTaskFailed {
		t.Errorf("got %q, want %q", se.Name, ErrTaskFailed)
	}
	// MaxAttempts=2 means 1 initial + 2 retries = 3 total calls.
	if int(exec.calls.Load()) != 3 {
		t.Errorf("expected 3 calls, got %d", exec.calls.Load())
	}
}

// TestCatch verifies that a Catcher routes to the correct next state and places
// the error object at ResultPath.
func TestCatch(t *testing.T) {
	def := `{
		"StartAt": "T",
		"States": {
			"T": {
				"Type":"Task",
				"Resource":"arn:aws:lambda:::function:f",
				"Catch":[{
					"ErrorEquals":["States.TaskFailed"],
					"ResultPath":"$.err",
					"Next":"Recover"
				}],
				"End":true
			},
			"Recover": {"Type":"Succeed"}
		}
	}`
	exec := &alwaysFailExecutor{err: &StateError{Name: ErrTaskFailed, Cause: "oops"}}
	out, err := runASL(t, def, `{"x":1}`, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The output should be the original input with $.err = {Error, Cause}.
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	if m["x"] != float64(1) {
		t.Errorf("$.x = %v, want 1", m["x"])
	}
	errObj, ok := m["err"].(map[string]any)
	if !ok {
		t.Fatalf("$.err not a map: %v", m["err"])
	}
	if errObj["Error"] != ErrTaskFailed {
		t.Errorf("$.err.Error = %q, want %q", errObj["Error"], ErrTaskFailed)
	}
	if errObj["Cause"] != "oops" {
		t.Errorf("$.err.Cause = %q, want %q", errObj["Cause"], "oops")
	}
}

// TestParallel verifies that Parallel returns an ordered array of branch outputs.
func TestParallel(t *testing.T) {
	def := `{
		"StartAt": "Par",
		"States": {
			"Par": {
				"Type":"Parallel",
				"Branches":[
					{
						"StartAt":"A",
						"States":{"A":{"Type":"Pass","Result":{"branch":1},"End":true}}
					},
					{
						"StartAt":"B",
						"States":{"B":{"Type":"Pass","Result":{"branch":2},"End":true}}
					}
				],
				"End":true
			}
		}
	}`
	out, err := runASL(t, def, `{}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(arr))
	}
	want0 := map[string]any{"branch": float64(1)}
	want1 := map[string]any{"branch": float64(2)}
	if !reflect.DeepEqual(arr[0], want0) {
		t.Errorf("branch[0] = %v, want %v", arr[0], want0)
	}
	if !reflect.DeepEqual(arr[1], want1) {
		t.Errorf("branch[1] = %v, want %v", arr[1], want1)
	}
}

// TestMap verifies that Map runs the processor for each item and preserves order.
func TestMap(t *testing.T) {
	def := `{
		"StartAt": "M",
		"States": {
			"M": {
				"Type":"Map",
				"ItemProcessor":{
					"StartAt":"Double",
					"States":{
						"Double":{"Type":"Pass","End":true}
					}
				},
				"End":true
			}
		}
	}`
	out, err := runASL(t, def, `[{"v":1},{"v":2},{"v":3}]`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	for i, v := range []float64{1, 2, 3} {
		m, ok := arr[i].(map[string]any)
		if !ok {
			t.Fatalf("item[%d] not a map: %T", i, arr[i])
		}
		if m["v"] != v {
			t.Errorf("item[%d].v = %v, want %v", i, m["v"], v)
		}
	}
}

// TestMapItemSelector verifies that ItemSelector reshapes each item and that
// "$$.Map.Item.Index" / "$$.Map.Item.Value" resolve inside the ItemProcessor.
func TestMapItemSelector(t *testing.T) {
	def := `{
		"StartAt": "M",
		"States": {
			"M": {
				"Type":"Map",
				"ItemSelector": {
					"name.$": "$.name",
					"index.$": "$$.Map.Item.Index",
					"original.$": "$$.Map.Item.Value"
				},
				"ItemProcessor":{
					"StartAt":"Pass",
					"States":{
						"Pass":{"Type":"Pass","End":true}
					}
				},
				"End":true
			}
		}
	}`
	out, err := runASL(t, def, `[{"name":"a","extra":1},{"name":"b","extra":2}]`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := out.([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("expected 2-element array, got %v", out)
	}

	m0, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("item[0] not a map: %T", arr[0])
	}
	if m0["name"] != "a" {
		t.Errorf("item[0].name = %v, want %q", m0["name"], "a")
	}
	if m0["index"] != float64(0) {
		t.Errorf("item[0].index = %v, want 0", m0["index"])
	}
	orig0, ok := m0["original"].(map[string]any)
	if !ok || orig0["name"] != "a" || orig0["extra"] != float64(1) {
		t.Errorf("item[0].original = %v, want the raw first item", m0["original"])
	}

	m1, ok := arr[1].(map[string]any)
	if !ok {
		t.Fatalf("item[1] not a map: %T", arr[1])
	}
	if m1["name"] != "b" {
		t.Errorf("item[1].name = %v, want %q", m1["name"], "b")
	}
	if m1["index"] != float64(1) {
		t.Errorf("item[1].index = %v, want 1", m1["index"])
	}
}

// TestMapNoItemSelectorPassesItemsThrough is a regression test: a Map with no
// ItemSelector must pass each item to the ItemProcessor unchanged, exactly as
// before ItemSelector support was added.
func TestMapNoItemSelectorPassesItemsThrough(t *testing.T) {
	def := `{
		"StartAt": "M",
		"States": {
			"M": {
				"Type":"Map",
				"ItemProcessor":{
					"StartAt":"Pass",
					"States":{
						"Pass":{"Type":"Pass","End":true}
					}
				},
				"End":true
			}
		}
	}`
	out, err := runASL(t, def, `[{"v":1},{"v":2},{"v":3}]`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := out.([]any)
	if !ok || len(arr) != 3 {
		t.Fatalf("expected 3-element array, got %v", out)
	}
	for i, v := range []float64{1, 2, 3} {
		m, ok := arr[i].(map[string]any)
		if !ok {
			t.Fatalf("item[%d] not a map: %T", i, arr[i])
		}
		if len(m) != 1 || m["v"] != v {
			t.Errorf("item[%d] = %v, want {\"v\":%v} unchanged", i, m, v)
		}
	}
}

// TestMapMaxConcurrency verifies that MaxConcurrency is respected (smoke test
// with fake clock; real concurrency tested by -race).
func TestMapMaxConcurrency(t *testing.T) {
	def := `{
		"StartAt": "M",
		"States": {
			"M": {
				"Type":"Map",
				"MaxConcurrency":1,
				"ItemProcessor":{
					"StartAt":"Passthru",
					"States":{
						"Passthru":{"Type":"Pass","End":true}
					}
				},
				"End":true
			}
		}
	}`
	out, err := runASL(t, def, `["a","b","c"]`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := out.([]any)
	if !ok || len(arr) != 3 {
		t.Errorf("expected 3-element array, got %v", out)
	}
}

// TestCtxCancellation verifies that cancelling the context aborts execution.
func TestCtxCancellation(t *testing.T) {
	// Use a real executor that blocks until context is cancelled.
	def := `{
		"StartAt": "T",
		"States": {
			"T": {"Type":"Task","Resource":"arn:aws:lambda:::function:f","End":true}
		}
	}`
	m, err := asl.Parse(def)
	if err != nil {
		t.Fatalf("asl.Parse: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	blockingExec := &blockingTaskExecutor{cancel: cancel}
	rec := &recordingEmitter{}
	r := Run{
		Machine:  m,
		Input:    json.RawMessage(`{}`),
		Executor: blockingExec,
		Clock:    newFakeClock(),
		Emit:     rec.emit,
	}
	_, runErr := r.Execute(ctx)
	if !errors.Is(runErr, ErrAborted) {
		// Also accept a *StateError wrapping the aborted sentinel.
		if runErr == nil {
			t.Fatal("expected ErrAborted, got nil")
		}
	}
}

// blockingTaskExecutor cancels the context when called, simulating a slow task.
type blockingTaskExecutor struct {
	cancel context.CancelFunc
}

func (b *blockingTaskExecutor) RunTask(ctx context.Context, _ TaskRequest) (TaskResult, error) {
	b.cancel()
	<-ctx.Done()
	return TaskResult{}, ctx.Err()
}

// TestSucceed verifies the Succeed state returns the effective input.
func TestSucceed(t *testing.T) {
	def := `{
		"StartAt": "S",
		"States": {
			"S": {"Type":"Succeed"}
		}
	}`
	out, err := runASL(t, def, `{"a":1}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"a": float64(1)}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestChoiceStringOperators exercises string comparison operators.
func TestChoiceStringOperators(t *testing.T) {
	def := `{
		"StartAt": "C",
		"States": {
			"C": {
				"Type":"Choice",
				"Choices":[
					{"Variable":"$.s","StringEquals":"hello","Next":"Match"}
				],
				"Default":"NoMatch"
			},
			"Match":   {"Type":"Pass","Result":{"m":true},"End":true},
			"NoMatch": {"Type":"Pass","Result":{"m":false},"End":true}
		}
	}`
	out, err := runASL(t, def, `{"s":"hello"}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"m": true}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestChoiceBooleanEquals exercises BooleanEquals operator.
func TestChoiceBooleanEquals(t *testing.T) {
	def := `{
		"StartAt": "C",
		"States": {
			"C": {
				"Type":"Choice",
				"Choices":[
					{"Variable":"$.flag","BooleanEquals":true,"Next":"Yes"}
				],
				"Default":"No"
			},
			"Yes": {"Type":"Pass","Result":{"answer":"yes"},"End":true},
			"No":  {"Type":"Pass","Result":{"answer":"no"},"End":true}
		}
	}`
	out, err := runASL(t, def, `{"flag":true}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"answer": "yes"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestChoiceIsPresent exercises the IsPresent operator.
func TestChoiceIsPresent(t *testing.T) {
	def := `{
		"StartAt": "C",
		"States": {
			"C": {
				"Type":"Choice",
				"Choices":[
					{"Variable":"$.missing","IsPresent":false,"Next":"Absent"}
				],
				"Default":"Present"
			},
			"Absent":  {"Type":"Pass","Result":{"r":"absent"},"End":true},
			"Present": {"Type":"Pass","Result":{"r":"present"},"End":true}
		}
	}`
	out, err := runASL(t, def, `{"other":1}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"r": "absent"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestHistoryEvents verifies that events are emitted in order with increasing IDs.
func TestHistoryEvents(t *testing.T) {
	def := `{
		"StartAt": "P",
		"States": {
			"P": {"Type":"Pass","Result":{"x":1},"End":true}
		}
	}`
	m, err := asl.Parse(def)
	if err != nil {
		t.Fatalf("asl.Parse: %v", err)
	}
	rec := &recordingEmitter{}
	r := Run{
		Machine:  m,
		Input:    json.RawMessage(`{}`),
		Executor: nil,
		Clock:    newFakeClock(),
		Emit:     rec.emit,
	}
	if _, err := r.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(rec.events) == 0 {
		t.Fatal("no events emitted")
	}
	for i, ev := range rec.events {
		if ev.ID != int64(i+1) {
			t.Errorf("event[%d].ID = %d, want %d", i, ev.ID, i+1)
		}
		wantPrev := int64(i)
		if ev.PreviousID != wantPrev {
			t.Errorf("event[%d].PreviousID = %d, want %d", i, ev.PreviousID, wantPrev)
		}
	}
	if rec.events[0].Type != "ExecutionStarted" {
		t.Errorf("first event type = %q, want ExecutionStarted", rec.events[0].Type)
	}
	last := rec.events[len(rec.events)-1]
	if last.Type != "ExecutionSucceeded" {
		t.Errorf("last event type = %q, want ExecutionSucceeded", last.Type)
	}
}

// TestPassResultPath verifies ResultPath merges the result into the original input.
func TestPassResultPath(t *testing.T) {
	def := `{
		"StartAt": "P",
		"States": {
			"P": {"Type":"Pass","Result":42,"ResultPath":"$.answer","End":true}
		}
	}`
	out, err := runASL(t, def, `{"question":"everything"}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"question": "everything", "answer": float64(42)}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

// TestWaitSecondsPath verifies SecondsPath resolves the delay from the state data.
func TestWaitSecondsPath(t *testing.T) {
	def := `{
		"StartAt": "W",
		"States": {
			"W": {"Type":"Wait","SecondsPath":"$.delay","Next":"Done"},
			"Done": {"Type":"Succeed"}
		}
	}`
	out, err := runASL(t, def, `{"delay":10,"v":7}`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	if m["v"] != float64(7) {
		t.Errorf("$.v = %v, want 7", m["v"])
	}
}

// TestParallelBranchFailure verifies that a failing branch propagates as States.BranchFailed.
func TestParallelBranchFailure(t *testing.T) {
	def := `{
		"StartAt": "Par",
		"States": {
			"Par": {
				"Type":"Parallel",
				"Branches":[
					{
						"StartAt":"OK",
						"States":{"OK":{"Type":"Succeed"}}
					},
					{
						"StartAt":"Bad",
						"States":{"Bad":{"Type":"Fail","Error":"Boom","Cause":"nope"}}
					}
				],
				"End":true
			}
		}
	}`
	_, err := runASL(t, def, `{}`, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*StateError)
	if !ok {
		t.Fatalf("expected *StateError, got %T: %v", err, err)
	}
	if se.Name != ErrBranchFailed {
		t.Errorf("Name = %q, want %q", se.Name, ErrBranchFailed)
	}
}

// ---- HTTP Task interpreter tests (fake executor) ----

// httpSuccessExecutor is a fakeExecutor that returns an http:invoke-shaped
// success result: {StatusCode,Headers,ResponseBody}.
type httpSuccessExecutor struct {
	statusCode int
	body       any
	headers    map[string]string
}

func (h *httpSuccessExecutor) RunTask(_ context.Context, _ TaskRequest) (TaskResult, error) {
	result := map[string]any{
		"StatusCode":   float64(h.statusCode),
		"Headers":      h.headers,
		"ResponseBody": h.body,
	}
	raw, _ := json.Marshal(result)
	return TaskResult{Output: json.RawMessage(raw)}, nil
}

// httpFailExecutor always returns States.Http.StatusCodeFailure.
type httpFailExecutor struct {
	statusCode int
	calls      atomic.Int32
}

func (h *httpFailExecutor) RunTask(_ context.Context, _ TaskRequest) (TaskResult, error) {
	h.calls.Add(1)
	return TaskResult{}, &StateError{
		Name:  ErrHTTPStatusCode,
		Cause: fmt.Sprintf("%d server error", h.statusCode),
	}
}

// httpSocketExecutor always returns States.Http.SocketException.
type httpSocketExecutor struct{}

func (h *httpSocketExecutor) RunTask(_ context.Context, _ TaskRequest) (TaskResult, error) {
	return TaskResult{}, &StateError{Name: ErrHTTPSocket, Cause: "connection refused"}
}

// TestHTTPTaskSuccessEvents verifies that a successful http:invoke Task emits
// TaskScheduled, TaskStarted, TaskSucceeded (not Lambda* events) and that the
// result has the {StatusCode,Headers,ResponseBody} shape.
func TestHTTPTaskSuccessEvents(t *testing.T) {
	def := `{
		"StartAt": "Call",
		"States": {
			"Call": {
				"Type":"Task",
				"Resource":"arn:aws:states:::http:invoke",
				"Parameters":{
					"ApiEndpoint":"http://localhost/v1/resource",
					"Method":"DELETE"
				},
				"End":true
			}
		}
	}`
	exec := &httpSuccessExecutor{
		statusCode: 200,
		body:       map[string]any{"deleted": true},
		headers:    map[string]string{"Content-Type": "application/json"},
	}

	m, err := asl.Parse(def)
	if err != nil {
		t.Fatalf("asl.Parse: %v", err)
	}
	rec := &recordingEmitter{}
	r := Run{
		Machine:  m,
		Input:    json.RawMessage(`{}`),
		Executor: exec,
		Clock:    newFakeClock(),
		Emit:     rec.emit,
	}
	raw, runErr := r.Execute(context.Background())
	if runErr != nil {
		t.Fatalf("Execute: %v", runErr)
	}

	// Verify output shape.
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("output unmarshal: %v", err)
	}
	if out["StatusCode"] != float64(200) {
		t.Errorf("StatusCode = %v, want 200", out["StatusCode"])
	}
	if out["ResponseBody"] == nil {
		t.Error("ResponseBody should not be nil")
	}

	// Verify event names: must be Task* not Lambda*.
	eventTypes := make([]string, 0, len(rec.events))
	for _, ev := range rec.events {
		eventTypes = append(eventTypes, ev.Type)
	}
	wantScheduled := "TaskScheduled"
	wantStarted := "TaskStarted"
	wantSucceeded := "TaskSucceeded"
	found := map[string]bool{}
	for _, et := range eventTypes {
		found[et] = true
	}
	for _, want := range []string{wantScheduled, wantStarted, wantSucceeded} {
		if !found[want] {
			t.Errorf("expected event %q in history, got: %v", want, eventTypes)
		}
	}
	for _, bad := range []string{"LambdaFunctionScheduled", "LambdaFunctionSucceeded"} {
		if found[bad] {
			t.Errorf("unexpected Lambda event %q for http:invoke task", bad)
		}
	}
}

// TestHTTPTaskNonTwxxRetryThenCatch verifies that States.Http.StatusCodeFailure
// is retried by Retry.ErrorEquals and then caught by Catch.
func TestHTTPTaskNonTwxxRetryThenCatch(t *testing.T) {
	def := `{
		"StartAt": "Call",
		"States": {
			"Call": {
				"Type":"Task",
				"Resource":"arn:aws:states:::http:invoke",
				"Parameters":{
					"ApiEndpoint":"http://localhost/v1/resource",
					"Method":"GET"
				},
				"Retry":[{
					"ErrorEquals":["States.Http.StatusCodeFailure"],
					"MaxAttempts":2
				}],
				"Catch":[{
					"ErrorEquals":["States.Http.StatusCodeFailure"],
					"ResultPath":"$.httpErr",
					"Next":"Recover"
				}],
				"End":true
			},
			"Recover": {"Type":"Succeed"}
		}
	}`
	exec := &httpFailExecutor{statusCode: 503}
	out, runErr := runASL(t, def, `{"id":"abc"}`, exec)
	if runErr != nil {
		t.Fatalf("expected Catch to handle error, got: %v", runErr)
	}
	// MaxAttempts=2 means 1 initial + 2 retries = 3 calls, then Catch fires.
	if int(exec.calls.Load()) != 3 {
		t.Errorf("expected 3 calls (1 + 2 retries), got %d", exec.calls.Load())
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	errObj, ok := m["httpErr"].(map[string]any)
	if !ok {
		t.Fatalf("$.httpErr not a map: %v", m["httpErr"])
	}
	if errObj["Error"] != ErrHTTPStatusCode {
		t.Errorf("$.httpErr.Error = %q, want %q", errObj["Error"], ErrHTTPStatusCode)
	}
}

// TestHTTPTaskSocketException verifies that States.Http.SocketException is
// catchable by its exact error name.
func TestHTTPTaskSocketException(t *testing.T) {
	def := `{
		"StartAt": "Call",
		"States": {
			"Call": {
				"Type":"Task",
				"Resource":"arn:aws:states:::http:invoke",
				"Parameters":{
					"ApiEndpoint":"http://localhost/v1/resource",
					"Method":"GET"
				},
				"Catch":[{
					"ErrorEquals":["States.Http.SocketException"],
					"ResultPath":"$.sockErr",
					"Next":"Recover"
				}],
				"End":true
			},
			"Recover": {"Type":"Succeed"}
		}
	}`
	exec := &httpSocketExecutor{}
	out, runErr := runASL(t, def, `{"id":"x"}`, exec)
	if runErr != nil {
		t.Fatalf("expected Catch to handle SocketException, got: %v", runErr)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	errObj, ok := m["sockErr"].(map[string]any)
	if !ok {
		t.Fatalf("$.sockErr not a map: %v", m["sockErr"])
	}
	if errObj["Error"] != ErrHTTPSocket {
		t.Errorf("$.sockErr.Error = %q, want %q", errObj["Error"], ErrHTTPSocket)
	}
}
