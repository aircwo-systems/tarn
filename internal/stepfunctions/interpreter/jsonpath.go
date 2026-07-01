package interpreter

import (
	"fmt"
	"strconv"
	"strings"
)

// JSON reference paths are single-valued selectors over decoded JSON values
// (map[string]any, []any, and scalars). This is deliberately a scoped subset of
// JSONPath — just what ASL InputPath/OutputPath/ResultPath/Variable need — with
// zero external dependencies. Supported forms: "$", "$.a.b", "$['a']", "$[0]",
// and combinations like "$.a[2].b".

// getPath evaluates a reference path against data and returns the selected value.
// A missing key or out-of-range index is a States.Runtime failure.
func getPath(data any, path string) (any, error) {
	segs, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	cur := data
	for _, seg := range segs {
		switch s := seg.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("path %q: %q is not an object", path, s)}
			}
			v, ok := m[s]
			if !ok {
				return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("path %q: key %q not found", path, s)}
			}
			cur = v
		case int:
			arr, ok := cur.([]any)
			if !ok {
				return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("path %q: not an array", path)}
			}
			if s < 0 || s >= len(arr) {
				return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("path %q: index %d out of range", path, s)}
			}
			cur = arr[s]
		}
	}
	return cur, nil
}

// setPath returns a copy of data with value inserted at path. "$" replaces the
// root entirely. Only object key paths are supported (ASL ResultPath semantics).
func setPath(data any, path string, value any) (any, error) {
	if path == "$" {
		return value, nil
	}
	segs, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	return setSegs(data, segs, value)
}

func setSegs(data any, segs []any, value any) (any, error) {
	if len(segs) == 0 {
		return value, nil
	}
	key, ok := segs[0].(string)
	if !ok {
		return nil, &StateError{Name: ErrResultPathMatchFailure, Cause: "ResultPath does not support array indices"}
	}
	m, ok := data.(map[string]any)
	if !ok {
		// ASL merges the result into the effective input; if that input is not an
		// object, it is replaced by a new object holding the result.
		m = map[string]any{}
	} else {
		m = cloneMap(m)
	}
	child, err := setSegs(m[key], segs[1:], value)
	if err != nil {
		return nil, err
	}
	m[key] = child
	return m, nil
}

// parsePath tokenizes a reference path into a sequence of string keys and int
// indices. The leading "$" is required; "$" alone yields no segments.
func parsePath(path string) ([]any, error) {
	if path == "" || path[0] != '$' {
		return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("invalid reference path %q", path)}
	}
	rest := path[1:]
	var segs []any
	for i := 0; i < len(rest); {
		switch rest[i] {
		case '.':
			i++
			start := i
			for i < len(rest) && rest[i] != '.' && rest[i] != '[' {
				i++
			}
			if start == i {
				return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("invalid reference path %q", path)}
			}
			segs = append(segs, rest[start:i])
		case '[':
			end := strings.IndexByte(rest[i:], ']')
			if end < 0 {
				return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("unbalanced brackets in path %q", path)}
			}
			inner := strings.TrimSpace(rest[i+1 : i+end])
			i += end + 1
			seg, err := bracketSegment(inner, path)
			if err != nil {
				return nil, err
			}
			segs = append(segs, seg)
		default:
			return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("unexpected character in path %q", path)}
		}
	}
	return segs, nil
}

// bracketSegment parses the contents of a [...] accessor: a quoted key or an
// integer index.
func bracketSegment(inner, path string) (any, error) {
	if len(inner) >= 2 && (inner[0] == '\'' || inner[0] == '"') {
		quote := inner[0]
		if inner[len(inner)-1] != quote {
			return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("unterminated key in path %q", path)}
		}
		return inner[1 : len(inner)-1], nil
	}
	idx, err := strconv.Atoi(inner)
	if err != nil {
		return nil, &StateError{Name: ErrRuntime, Cause: fmt.Sprintf("invalid array index %q in path %q", inner, path)}
	}
	return idx, nil
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
