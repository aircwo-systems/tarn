package interpreter

import (
	"context"
	"math"
	"time"

	"github.com/aircwo-systems/tarn/internal/stepfunctions/asl"
)

// matchesErrorEquals reports whether name is matched by the ErrorEquals slice.
// States.ALL is the wildcard that matches any error name.
func matchesErrorEquals(errorEquals []string, name string) bool {
	for _, e := range errorEquals {
		if e == ErrAll || e == name {
			return true
		}
	}
	return false
}

// defaultMaxAttempts is the AWS default when MaxAttempts is not specified.
const defaultMaxAttempts = 3

// defaultIntervalSeconds is the AWS default when IntervalSeconds is 0.
const defaultIntervalSeconds = 1

// defaultBackoffRate is the AWS default when BackoffRate is 0.
const defaultBackoffRate = 2.0

// retryDelay returns the wait duration for attempt n (1-based) of a retrier.
func retryDelay(r asl.Retrier, attempt int) time.Duration {
	interval := r.IntervalSeconds
	if interval == 0 {
		interval = defaultIntervalSeconds
	}
	rate := r.BackoffRate
	if rate == 0 {
		rate = defaultBackoffRate
	}
	secs := float64(interval) * math.Pow(rate, float64(attempt-1))
	return time.Duration(secs * float64(time.Second))
}

// maxAttempts returns the effective MaxAttempts for a retrier (AWS default: 3).
func maxAttempts(r asl.Retrier) int {
	if r.MaxAttempts == nil {
		return defaultMaxAttempts
	}
	return *r.MaxAttempts
}

// runWithRetryCatch runs work repeatedly according to retriers/catchers on the
// execution, returning the effective output any and the next state name (empty
// string means the error propagates or was terminal). effIn is the effective
// input (post-InputPath+Parameters) used for Catch ResultPath placement.
//
// It returns (result, nextState, nil) on success or after a Catch redirect, or
// (nil, "", err) when all retriers are exhausted and no Catcher matched.
func (ex *execution) runWithRetryCatch(
	ctx context.Context,
	effIn any,
	retriers []asl.Retrier,
	catchers []asl.Catcher,
	work func() (any, error),
) (any, string, error) {
	// Track per-retrier attempt counts (index matches retriers slice).
	attempts := make([]int, len(retriers))

	for {
		result, err := work()
		if err == nil {
			return result, "", nil
		}

		se := toStateError(err)

		// --- Retry phase ---
		retried := false
		for i, r := range retriers {
			if !matchesErrorEquals(r.ErrorEquals, se.Name) {
				continue
			}
			attempts[i]++
			if attempts[i] <= maxAttempts(r) {
				d := retryDelay(r, attempts[i])
				select {
				case <-ctx.Done():
					return nil, "", ErrAborted
				case <-ex.run.Clock.After(d):
				}
				retried = true
				break
			}
		}
		if retried {
			continue
		}

		// --- Catch phase ---
		for _, c := range catchers {
			if !matchesErrorEquals(c.ErrorEquals, se.Name) {
				continue
			}
			errObj := map[string]any{
				"Error": se.Name,
				"Cause": se.Cause,
			}
			out, pathErr := applyResultPath(effIn, errObj, c.ResultPath.Or("$"))
			if pathErr != nil {
				return nil, "", pathErr
			}
			return out, c.Next, nil
		}

		// No retry or catch matched: propagate.
		return nil, "", se
	}
}

// toStateError coerces an error to *StateError, wrapping non-StateError values
// as States.TaskFailed.
func toStateError(err error) *StateError {
	if se, ok := err.(*StateError); ok {
		return se
	}
	return &StateError{Name: ErrTaskFailed, Cause: err.Error()}
}
