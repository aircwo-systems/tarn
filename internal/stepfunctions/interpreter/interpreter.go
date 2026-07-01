package interpreter

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/stepfunctions/asl"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// ErrAborted is returned by Execute when the context is cancelled (StopExecution).
var ErrAborted = errors.New("execution aborted")

// Run is immutable configuration for a single execution. Populate all fields
// before calling Execute; the struct must not be mutated during execution.
type Run struct {
	Machine  *asl.Machine
	Input    json.RawMessage
	Executor TaskExecutor
	Clock    Clock
	Emit     func(types.HistoryEvent)
}

// Execute drives the state machine to completion (or cancellation) and returns
// the final output JSON. It uses ctx.Done() as the abort signal; callers cancel
// the context to trigger a StopExecution abort.
func (r Run) Execute(ctx context.Context) (json.RawMessage, error) {
	em := &emitter{
		clock: r.Clock,
		sink:  r.Emit,
	}
	ex := &execution{run: r, em: em, ctx: ContextObject{}}
	return ex.run_(ctx)
}

// emitter owns the monotonically increasing event ID counter. It is shared
// across all child executions spawned by Parallel and Map states so that
// concurrent branches produce a globally ordered, race-free history.
type emitter struct {
	mu      sync.Mutex
	eventID int64
	prevID  int64
	clock   Clock
	sink    func(types.HistoryEvent)
}

// emit records one history event. Safe to call from concurrent goroutines.
func (em *emitter) emit(eventType string, details map[string]any) {
	em.mu.Lock()
	em.eventID++
	id := em.eventID
	prev := em.prevID
	em.prevID = id
	em.mu.Unlock()

	if em.sink == nil {
		return
	}
	em.sink(types.HistoryEvent{
		ID:         id,
		PreviousID: prev,
		Type:       eventType,
		Timestamp:  em.clock.Now(),
		Details:    details,
	})
}

// execution holds per-run state. Run is kept immutable so child executions
// (Parallel/Map branches) can safely share the emitter.
type execution struct {
	run Run
	em  *emitter      // shared with child executions
	ctx ContextObject // "$$" context object visible to this execution's states

	// inIter/iterIndex tag this execution (and, via childExecution, its
	// descendants) as running inside a Map iteration. When set, every history
	// event emitted through ex.emit carries the iteration index so the UI can
	// group the flat event stream by item/certificate. Root executions and
	// Parallel branches outside a Map leave inIter false.
	inIter    bool
	iterIndex int
}

// emit records one history event, stamping the current Map iteration index into
// the details when this execution is running inside a Map iteration. Use this
// instead of ex.em.emit for per-state/per-task events so grouped-history
// grouping works; top-level Execution* events are emitted on ex.em directly.
func (ex *execution) emit(eventType string, details map[string]any) {
	if ex.inIter {
		if details == nil {
			details = map[string]any{}
		}
		details["iteration"] = ex.iterIndex
	}
	ex.em.emit(eventType, details)
}

// run_ is the internal entry point. It handles the machine-level timeout,
// emits ExecutionStarted/Succeeded/Failed/Aborted, and delegates to loop().
func (ex *execution) run_(ctx context.Context) (json.RawMessage, error) {
	// Decode input once into the any-typed value model.
	var input any
	if len(ex.run.Input) > 0 {
		if err := json.Unmarshal(ex.run.Input, &input); err != nil {
			input = map[string]any{}
		}
	} else {
		input = map[string]any{}
	}

	// Determine execution timeout (AWS default: 60 s when unspecified). The
	// timeout uses real wall-clock time via the context; the injected Clock
	// governs only Wait-state delays and retry backoff, so a fake clock in tests
	// never trips the execution deadline.
	timeoutSecs := ex.run.Machine.TimeoutSeconds
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}
	loopCtx, loopCancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer loopCancel()

	ex.em.emit("ExecutionStarted", map[string]any{"input": string(ex.run.Input)})

	type result struct {
		out any
		err error
	}
	// Buffered so the loop goroutine never blocks on send even if we return early
	// via loopCtx.Done() (avoids a goroutine leak when a Task ignores ctx).
	done := make(chan result, 1)
	go func() {
		out, err := ex.loop(loopCtx, input)
		done <- result{out, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			if res.err == ErrAborted {
				return ex.finishAborted(ctx, loopCtx)
			}
			se := toStateError(res.err)
			ex.em.emit("ExecutionFailed", map[string]any{
				"error": se.Name,
				"cause": se.Cause,
			})
			return nil, res.err
		}
		encoded, err := json.Marshal(res.out)
		if err != nil {
			encoded = []byte("null")
		}
		ex.em.emit("ExecutionSucceeded", map[string]any{"output": string(encoded)})
		return encoded, nil

	case <-loopCtx.Done():
		return ex.finishAborted(ctx, loopCtx)
	}
}

// finishAborted classifies a cancelled execution as a timeout (the execution
// deadline elapsed) or an abort (the caller cancelled the parent context, e.g.
// StopExecution) and emits the matching terminal event.
func (ex *execution) finishAborted(ctx, loopCtx context.Context) (json.RawMessage, error) {
	if ctx.Err() == nil && errors.Is(loopCtx.Err(), context.DeadlineExceeded) {
		se := &StateError{Name: ErrTimeout}
		ex.em.emit("ExecutionTimedOut", map[string]any{"error": se.Name})
		return nil, se
	}
	ex.em.emit("ExecutionAborted", nil)
	return nil, ErrAborted
}

// loop is the state transition loop. It runs until a terminal state is reached
// or ctx is cancelled.
func (ex *execution) loop(ctx context.Context, input any) (any, error) {
	current := ex.run.Machine.StartAt

	for {
		select {
		case <-ctx.Done():
			return nil, ErrAborted
		default:
		}

		st, ok := ex.run.Machine.States[current]
		if !ok {
			return nil, &StateError{Name: ErrRuntime, Cause: "state not found: " + current}
		}

		ex.emit(stateEnteredEvent(st.StateType()), map[string]any{"name": current})

		out, next, err := ex.dispatchState(ctx, current, st, input)

		ex.emit(stateExitedEvent(st.StateType()), map[string]any{"name": current})

		if err != nil {
			return nil, err
		}
		if next == "" {
			// Terminal state.
			return out, nil
		}
		input = out
		current = next
	}
}

// dispatchState calls the appropriate run* method for st. It returns (output,
// nextStateName, error); nextStateName == "" signals a terminal transition.
func (ex *execution) dispatchState(
	ctx context.Context,
	name string,
	st asl.State,
	input any,
) (any, string, error) {
	switch s := st.(type) {
	case asl.PassState:
		return ex.runPass(s, input)
	case asl.TaskState:
		return ex.runTask(ctx, name, s, input)
	case asl.ChoiceState:
		return ex.runChoice(ctx, s, input)
	case asl.WaitState:
		return ex.runWait(ctx, s, input)
	case asl.SucceedState:
		return ex.runSucceed(s, input)
	case asl.FailState:
		return ex.runFail(s)
	case asl.ParallelState:
		return ex.runParallel(ctx, name, s, input)
	case asl.MapState:
		return ex.runMap(ctx, name, s, input)
	default:
		return nil, "", &StateError{Name: ErrRuntime, Cause: "unsupported state type"}
	}
}

// childExecution creates a child execution sharing this execution's emitter
// (so Parallel/Map branches emit globally ordered events) but running an
// independent sub-machine. childCtx becomes the "$$" context object visible to
// every state inside the child (e.g. Map.Item.Index/Value for a Map iteration);
// pass an empty ContextObject when the child has no additional context (e.g.
// Parallel branches).
func (ex *execution) childExecution(machine *asl.Machine, input json.RawMessage, childCtx ContextObject) *execution {
	return &execution{
		run: Run{
			Machine:  machine,
			Input:    input,
			Executor: ex.run.Executor,
			Clock:    ex.run.Clock,
			// Emit not set on child Run; the child em points at the shared emitter.
		},
		em:  ex.em,
		ctx: childCtx,
		// Inherit the iteration tag so descendants of a Map iteration (e.g. the
		// Parallel branches inside the per-certificate processor) stay grouped
		// under the same item. runItems overrides these for the iteration itself.
		inIter:    ex.inIter,
		iterIndex: ex.iterIndex,
	}
}

// stateEnteredEvent returns the history event type for entering a state.
func stateEnteredEvent(t asl.StateType) string {
	return string(t) + "StateEntered"
}

// stateExitedEvent returns the history event type for exiting a state.
func stateExitedEvent(t asl.StateType) string {
	return string(t) + "StateExited"
}
