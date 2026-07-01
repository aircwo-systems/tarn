package interpreter

import (
	"fmt"
	"strings"
)

// ContextObject is the ASL "$$" context made available to Parameters and
// ResultSelector. The service shell populates it with execution metadata.
type ContextObject map[string]any

// The five functions below implement ASL's state I/O processing. They are the
// single source of truth for the ordering applied around every state's work:
//
//	raw input
//	  -> applyInputPath        (select a slice of the input)
//	  -> applyParameters       (construct the effective input)
//	  -> <state work>
//	  -> applyResultSelector   (reshape the result)
//	  -> applyResultPath       (merge the result back into the input)
//	  -> applyOutputPath       (select a slice of the output)
//	-> state output
//
// Each takes already-resolved path strings ("$" default, "" meaning an explicit
// null/discard) so callers never repeat the default logic.

// applyInputPath selects the portion of input addressed by path.
func applyInputPath(input any, path string) (any, error) {
	switch path {
	case "":
		return map[string]any{}, nil
	case "$":
		return input, nil
	default:
		return getPath(input, path)
	}
}

// applyOutputPath selects the portion of output addressed by path.
func applyOutputPath(output any, path string) (any, error) {
	switch path {
	case "":
		return map[string]any{}, nil
	case "$":
		return output, nil
	default:
		return getPath(output, path)
	}
}

// applyParameters builds the effective input from a Parameters template,
// resolving ".$" fields against the input and context. A nil template is a
// pass-through.
func applyParameters(input any, ctx ContextObject, params any) (any, error) {
	if params == nil {
		return input, nil
	}
	return resolvePayload(params, input, ctx)
}

// applyResultSelector reshapes a state's raw result using a ResultSelector
// template, resolving ".$" fields against the result and context. A nil template
// is a pass-through.
func applyResultSelector(result any, ctx ContextObject, sel any) (any, error) {
	if sel == nil {
		return result, nil
	}
	return resolvePayload(sel, result, ctx)
}

// applyResultPath merges result into input at path. "$" replaces the input with
// the result; "" (explicit null) discards the result and keeps the input.
func applyResultPath(input, result any, path string) (any, error) {
	switch path {
	case "$":
		return result, nil
	case "":
		return input, nil
	default:
		return setPath(input, path, result)
	}
}

// resolvePayload evaluates a Parameters/ResultSelector template. A map key ending
// in ".$" has its string value evaluated (as a path or intrinsic) against root
// and the context; every other value is copied, recursing into nested objects
// and arrays.
func resolvePayload(template, root any, ctx ContextObject) (any, error) {
	switch t := template.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			if strings.HasSuffix(k, ".$") {
				expr, ok := v.(string)
				if !ok {
					return nil, &StateError{Name: ErrParameterPathFailure, Cause: fmt.Sprintf("%q must be a string", k)}
				}
				val, err := evalExpr(expr, root, ctx)
				if err != nil {
					return nil, err
				}
				out[strings.TrimSuffix(k, ".$")] = val
				continue
			}
			rv, err := resolvePayload(v, root, ctx)
			if err != nil {
				return nil, err
			}
			out[k] = rv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, v := range t {
			rv, err := resolvePayload(v, root, ctx)
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	default:
		return template, nil
	}
}

// evalExpr resolves the string value of a ".$" field: an intrinsic invocation
// ("States.*(...)"), a context path ("$$..."), or an input path ("$...").
func evalExpr(expr string, root any, ctx ContextObject) (any, error) {
	switch {
	case strings.HasPrefix(expr, "States."):
		return evalIntrinsic(expr, root, ctx)
	case strings.HasPrefix(expr, "$$"):
		return getPath(map[string]any(ctx), expr[1:])
	case strings.HasPrefix(expr, "$"):
		return getPath(root, expr)
	default:
		return nil, &StateError{Name: ErrParameterPathFailure, Cause: fmt.Sprintf("invalid path expression %q", expr)}
	}
}
