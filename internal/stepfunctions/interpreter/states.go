package interpreter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/stepfunctions/asl"
)

// decodeParams unmarshals a json.RawMessage Parameters/ResultSelector field
// into the any-typed template model. Returns nil when the field is absent.
func decodeParams(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, &StateError{Name: ErrRuntime, Cause: "cannot decode parameters: " + err.Error()}
	}
	return v, nil
}

// buildCtx returns the "$$" ContextObject for this execution. Top-level
// executions and Parallel branches carry an empty context; Map iterations
// carry a Map.Item.Index/Value context set by runItems and threaded through
// childExecution.
func (ex *execution) buildCtx() ContextObject {
	if ex.ctx != nil {
		return ex.ctx
	}
	return ContextObject{}
}

// ---- Pass ----

func (ex *execution) runPass(s asl.PassState, input any) (any, string, error) {
	// stateInput (post-InputPath) is the base ResultPath merges into; the result
	// is the Parameters-shaped value (or a static Result).
	stateInput, err := applyInputPath(input, s.InputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	params, err := decodeParams(s.Parameters)
	if err != nil {
		return nil, "", err
	}
	effIn, err := applyParameters(stateInput, ex.buildCtx(), params)
	if err != nil {
		return nil, "", err
	}

	// Result field overrides effective input as the state's "result".
	result := effIn
	if len(s.Result) > 0 {
		if err := json.Unmarshal(s.Result, &result); err != nil {
			return nil, "", &StateError{Name: ErrRuntime, Cause: "cannot decode Result: " + err.Error()}
		}
	}

	merged, err := applyResultPath(stateInput, result, s.ResultPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	out, err := applyOutputPath(merged, s.OutputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	return out, transition(s.Next, s.End), nil
}

// ---- Task ----

// taskEventNames returns the scheduled/started/succeeded/failed history-event
// type strings for a given Task resource. Lambda resources keep the legacy
// LambdaFunction* names for backwards-compatibility; all other resources
// (including :::http:invoke) use the generic Task* names that AWS emits for
// SDK/optimised integrations.
func taskEventNames(resource string) (scheduled, started, succeeded, failed string) {
	if isLambdaTaskResource(resource) {
		return "LambdaFunctionScheduled", "", "LambdaFunctionSucceeded", "LambdaFunctionFailed"
	}
	return "TaskScheduled", "TaskStarted", "TaskSucceeded", "TaskFailed"
}

// isLambdaTaskResource returns true when the resource is a bare Lambda ARN or
// the :::lambda:invoke optimised-integration ARN.
func isLambdaTaskResource(resource string) bool {
	if strings.Contains(resource, ":lambda:invoke") {
		return true
	}
	// Bare Lambda function ARN: arn:aws:lambda:…:function:<name>
	if strings.HasPrefix(resource, "arn:aws:lambda:") {
		return true
	}
	// Bare function name (no arn: prefix)
	if !strings.HasPrefix(resource, "arn:") {
		return true
	}
	return false
}

func (ex *execution) runTask(ctx context.Context, name string, s asl.TaskState, input any) (any, string, error) {
	// stateInput is the input after InputPath. Per ASL, ResultPath and Catch merge
	// the result/error into THIS value — Parameters only shapes what the resource
	// receives, it does not replace the state input. taskPayload is the
	// Parameters-shaped value sent to the resource.
	stateInput, err := applyInputPath(input, s.InputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	params, err := decodeParams(s.Parameters)
	if err != nil {
		return nil, "", err
	}
	taskPayload, err := applyParameters(stateInput, ex.buildCtx(), params)
	if err != nil {
		return nil, "", err
	}

	sel, err := decodeParams(s.ResultSelector)
	if err != nil {
		return nil, "", err
	}

	evScheduled, evStarted, evSucceeded, evFailed := taskEventNames(s.Resource)

	work := func() (any, error) {
		payload, merr := json.Marshal(taskPayload)
		if merr != nil {
			return nil, &StateError{Name: ErrRuntime, Cause: "cannot marshal task input: " + merr.Error()}
		}
		ex.emit(evScheduled, map[string]any{
			"stateName": name,
			"resource":  s.Resource,
		})
		// For non-Lambda resources emit TaskStarted just before the call.
		if evStarted != "" {
			ex.emit(evStarted, map[string]any{"stateName": name})
		}
		res, rerr := ex.run.Executor.RunTask(ctx, TaskRequest{
			Resource: s.Resource,
			Payload:  json.RawMessage(payload),
		})
		if rerr != nil {
			ex.emit(evFailed, withTaskLogRefs(map[string]any{
				"stateName": name,
				"error":     toStateError(rerr).Name,
			}, res))
			return nil, rerr
		}
		ex.emit(evSucceeded, withTaskLogRefs(map[string]any{"stateName": name}, res))
		raw := res.Output
		var result any
		if len(raw) > 0 {
			if uerr := json.Unmarshal(raw, &result); uerr != nil {
				return nil, &StateError{Name: ErrRuntime, Cause: "cannot unmarshal task result: " + uerr.Error()}
			}
		}
		result, rerr = applyResultSelector(result, ex.buildCtx(), sel)
		if rerr != nil {
			return nil, rerr
		}
		return result, nil
	}

	result, next, err := ex.runWithRetryCatch(ctx, stateInput, s.Retry, s.Catch, work)
	if err != nil {
		return nil, "", err
	}
	if next != "" {
		// Came from a Catcher; result already has the error object merged in.
		out, oerr := applyOutputPath(result, s.OutputPath.Or("$"))
		if oerr != nil {
			return nil, "", oerr
		}
		return out, next, nil
	}

	merged, err := applyResultPath(stateInput, result, s.ResultPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	out, err := applyOutputPath(merged, s.OutputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	return out, transition(s.Next, s.End), nil
}

// withTaskLogRefs adds the invocation's log group/stream and request id to a
// Lambda history event's details when the executor reported them, so the UI can
// deep-link to the exact log stream. Absent fields are simply omitted.
func withTaskLogRefs(details map[string]any, res TaskResult) map[string]any {
	if res.LogGroup != "" {
		details["logGroup"] = res.LogGroup
	}
	if res.LogStream != "" {
		details["logStream"] = res.LogStream
	}
	if res.RequestID != "" {
		details["requestId"] = res.RequestID
	}
	return details
}

// ---- Choice ----

func (ex *execution) runChoice(ctx context.Context, s asl.ChoiceState, input any) (any, string, error) {
	data, err := applyInputPath(input, s.InputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}

	for _, rule := range s.Choices {
		matched, err := evalRule(rule, data)
		if err != nil {
			return nil, "", err
		}
		if matched {
			out, err := applyOutputPath(data, s.OutputPath.Or("$"))
			if err != nil {
				return nil, "", err
			}
			return out, rule.Next, nil
		}
	}

	if s.Default != "" {
		out, err := applyOutputPath(data, s.OutputPath.Or("$"))
		if err != nil {
			return nil, "", err
		}
		return out, s.Default, nil
	}

	return nil, "", &StateError{Name: ErrNoChoiceMatched}
}

// ---- Wait ----

func (ex *execution) runWait(ctx context.Context, s asl.WaitState, input any) (any, string, error) {
	data, err := applyInputPath(input, s.InputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}

	delay, err := computeWaitDelay(ex.run.Clock.Now(), s, data)
	if err != nil {
		return nil, "", err
	}

	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, "", ErrAborted
		case <-ex.run.Clock.After(delay):
		}
	}

	out, err := applyOutputPath(data, s.OutputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	return out, transition(s.Next, s.End), nil
}

// computeWaitDelay resolves the Wait state's delay from its fields.
func computeWaitDelay(now time.Time, s asl.WaitState, data any) (time.Duration, error) {
	switch {
	case s.Seconds != nil:
		return time.Duration(*s.Seconds) * time.Second, nil

	case s.Timestamp != "":
		t, err := time.Parse(time.RFC3339, s.Timestamp)
		if err != nil {
			return 0, &StateError{Name: ErrRuntime, Cause: "invalid Timestamp: " + err.Error()}
		}
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, nil

	case s.SecondsPath != "":
		v, err := getPath(data, s.SecondsPath)
		if err != nil {
			return 0, err
		}
		n, ok := v.(float64)
		if !ok {
			return 0, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("SecondsPath %q must resolve to a number", s.SecondsPath)}
		}
		return time.Duration(n) * time.Second, nil

	case s.TimestampPath != "":
		v, err := getPath(data, s.TimestampPath)
		if err != nil {
			return 0, err
		}
		str, ok := v.(string)
		if !ok {
			return 0, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("TimestampPath %q must resolve to a string", s.TimestampPath)}
		}
		t, err := time.Parse(time.RFC3339, str)
		if err != nil {
			return 0, &StateError{Name: ErrRuntime, Cause: "invalid TimestampPath value: " + err.Error()}
		}
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, nil

	default:
		return 0, &StateError{Name: ErrRuntime, Cause: "Wait state has no delay specifier"}
	}
}

// ---- Succeed ----

func (ex *execution) runSucceed(s asl.SucceedState, input any) (any, string, error) {
	data, err := applyInputPath(input, s.InputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	out, err := applyOutputPath(data, s.OutputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	return out, "", nil // empty next = terminal
}

// ---- Fail ----

func (ex *execution) runFail(s asl.FailState) (any, string, error) {
	errName := s.Error
	if errName == "" {
		errName = "States.Failed"
	}
	return nil, "", &StateError{Name: errName, Cause: s.Cause}
}

// ---- Parallel ----

func (ex *execution) runParallel(ctx context.Context, _ string, s asl.ParallelState, input any) (any, string, error) {
	// stateInput (post-InputPath) is the base ResultPath/Catch merge into; effIn
	// (post-Parameters) is what the branches receive.
	stateInput, err := applyInputPath(input, s.InputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	params, err := decodeParams(s.Parameters)
	if err != nil {
		return nil, "", err
	}
	effIn, err := applyParameters(stateInput, ex.buildCtx(), params)
	if err != nil {
		return nil, "", err
	}

	sel, err := decodeParams(s.ResultSelector)
	if err != nil {
		return nil, "", err
	}

	work := func() (any, error) {
		return ex.runBranches(ctx, s.Branches, effIn)
	}

	result, next, err := ex.runWithRetryCatch(ctx, stateInput, s.Retry, s.Catch, work)
	if err != nil {
		return nil, "", err
	}
	if next != "" {
		out, oerr := applyOutputPath(result, s.OutputPath.Or("$"))
		if oerr != nil {
			return nil, "", oerr
		}
		return out, next, nil
	}

	result, err = applyResultSelector(result, ex.buildCtx(), sel)
	if err != nil {
		return nil, "", err
	}
	merged, err := applyResultPath(stateInput, result, s.ResultPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	out, err := applyOutputPath(merged, s.OutputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	return out, transition(s.Next, s.End), nil
}

// runBranches runs each branch concurrently and returns the ordered results.
func (ex *execution) runBranches(ctx context.Context, branches []*asl.Machine, input any) (any, error) {
	results := make([]any, len(branches))
	errs := make([]error, len(branches))

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, &StateError{Name: ErrRuntime, Cause: "cannot marshal parallel input: " + err.Error()}
	}

	var wg sync.WaitGroup
	for i, branch := range branches {
		i, branch := i, branch
		wg.Add(1)
		go func() {
			defer wg.Done()
			child := ex.childExecution(branch, inputJSON, ContextObject{})
			out, cerr := child.loop(ctx, input)
			results[i] = out
			errs[i] = cerr
		}()
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			se := toStateError(e)
			return nil, &StateError{Name: ErrBranchFailed, Cause: se.Error()}
		}
	}
	return results, nil
}

// ---- Map ----

func (ex *execution) runMap(ctx context.Context, _ string, s asl.MapState, input any) (any, string, error) {
	// stateInput (post-InputPath) is the base ResultPath/Catch merge into; effIn
	// (post-Parameters) is what ItemsPath resolves against / items are derived from.
	stateInput, err := applyInputPath(input, s.InputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	params, err := decodeParams(s.Parameters)
	if err != nil {
		return nil, "", err
	}
	effIn, err := applyParameters(stateInput, ex.buildCtx(), params)
	if err != nil {
		return nil, "", err
	}

	sel, err := decodeParams(s.ResultSelector)
	if err != nil {
		return nil, "", err
	}

	work := func() (any, error) {
		return ex.runItems(ctx, s, effIn)
	}

	result, next, err := ex.runWithRetryCatch(ctx, stateInput, s.Retry, s.Catch, work)
	if err != nil {
		return nil, "", err
	}
	if next != "" {
		out, oerr := applyOutputPath(result, s.OutputPath.Or("$"))
		if oerr != nil {
			return nil, "", oerr
		}
		return out, next, nil
	}

	result, err = applyResultSelector(result, ex.buildCtx(), sel)
	if err != nil {
		return nil, "", err
	}
	merged, err := applyResultPath(stateInput, result, s.ResultPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	out, err := applyOutputPath(merged, s.OutputPath.Or("$"))
	if err != nil {
		return nil, "", err
	}
	return out, transition(s.Next, s.End), nil
}

// runItems runs the Map processor once per item, honouring MaxConcurrency. For
// each item i, it builds an iteration ContextObject shaped
// {"Map":{"Item":{"Index":i,"Value":item}}} so "$$.Map.Item.Index/Value"
// resolve inside the processor's states. When ItemSelector is present it
// renders the ItemProcessor input from the item + iteration context; otherwise
// the item is passed through unchanged (existing behaviour).
func (ex *execution) runItems(ctx context.Context, s asl.MapState, effIn any) (any, error) {
	proc := s.Processor()
	if proc == nil {
		return nil, &StateError{Name: ErrRuntime, Cause: "Map state has no processor"}
	}

	itemSelector, err := decodeParams(s.ItemSelector)
	if err != nil {
		return nil, err
	}

	// Resolve the items array.
	itemsPath := s.ItemsPath
	if itemsPath == "" {
		itemsPath = "$"
	}
	raw, err := getPath(effIn, itemsPath)
	if err != nil {
		return nil, &StateError{Name: ErrRuntime, Cause: "cannot resolve ItemsPath: " + err.Error()}
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, &StateError{Name: ErrRuntime, Cause: "ItemsPath must resolve to an array"}
	}

	results := make([]any, len(items))
	errs := make([]error, len(items))

	// MaxConcurrency == 0 means unlimited.
	concurrency := s.MaxConcurrency
	if concurrency <= 0 || concurrency > len(items) {
		concurrency = len(items)
	}
	if concurrency == 0 {
		// Empty items array.
		return results, nil
	}

	// Semaphore channel limits concurrent goroutines.
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	for i, item := range items {
		i, item := i, item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			iterCtx := ContextObject{
				"Map": map[string]any{
					"Item": map[string]any{
						"Index": float64(i),
						"Value": item,
					},
				},
			}

			procInput := item
			if itemSelector != nil {
				sel, serr := applyParameters(item, iterCtx, itemSelector)
				if serr != nil {
					errs[i] = serr
					return
				}
				procInput = sel
			}

			itemJSON, merr := json.Marshal(procInput)
			if merr != nil {
				errs[i] = &StateError{Name: ErrRuntime, Cause: "cannot marshal map item: " + merr.Error()}
				return
			}
			// Boundary marker so the UI can group this iteration's events and
			// label the group from the item (e.g. certificateId). The index also
			// lets it demultiplex interleaved events at MaxConcurrency > 1.
			ex.em.emit("MapIterationStarted", map[string]any{
				"iteration": float64(i),
				"input":     procInput,
			})

			child := ex.childExecution(proc, itemJSON, iterCtx)
			child.inIter = true
			child.iterIndex = i
			out, cerr := child.loop(ctx, procInput)
			results[i] = out
			errs[i] = cerr

			if cerr != nil {
				ex.em.emit("MapIterationFailed", map[string]any{
					"iteration": float64(i),
					"error":     toStateError(cerr).Name,
				})
			} else {
				ex.em.emit("MapIterationSucceeded", map[string]any{"iteration": float64(i)})
			}
		}()
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return nil, toStateError(e)
		}
	}
	return results, nil
}

// transition returns the next state name. An empty string signals End == true.
func transition(next string, end bool) string {
	if end {
		return ""
	}
	return next
}
