package eventbridge

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MatchEventPattern checks whether a JSON event matches an AWS EventBridge
// event pattern. The pattern language supports:
//   - Exact value match:        {"source": ["aws.s3"]}
//   - Prefix match:             {"source": [{"prefix": "aws."}]}
//   - Suffix match:             {"source": [{"suffix": ".s3"}]}
//   - Anything-but:             {"source": [{"anything-but": ["internal"]}]}
//   - Anything-but with prefix: {"source": [{"anything-but": {"prefix": "internal."}}]}
//   - Numeric comparisons:      {"detail": {"size": [{"numeric": [">", 0, "<=", 100]}]}}
//   - Exists / not-exists:      {"detail": {"key": [{"exists": true}]}}
//   - Wildcard:                 {"source": [{"wildcard": "aws.*"}]}
//   - Empty pattern {}:         matches everything
//
// Arrays in the pattern are OR'd; all keys in a pattern object are AND'd.
func MatchEventPattern(patternJSON, eventJSON []byte) (bool, error) {
	var pattern map[string]any
	if err := json.Unmarshal(patternJSON, &pattern); err != nil {
		return false, fmt.Errorf("invalid event pattern: %w", err)
	}
	var event map[string]any
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		return false, fmt.Errorf("invalid event: %w", err)
	}
	return matchObject(pattern, event), nil
}

// matchObject checks that every key in pattern exists in event and its
// constraint is satisfied. All keys are AND'd.
func matchObject(pattern, event map[string]any) bool {
	for key, constraint := range pattern {
		eventVal, exists := event[key]
		if !matchField(constraint, eventVal, exists) {
			return false
		}
	}
	return true
}

// matchField dispatches a single pattern constraint against an event value.
// constraint is the pattern value (array of matchers, or nested object).
func matchField(constraint any, eventVal any, exists bool) bool {
	switch c := constraint.(type) {
	case []any:
		return matchArray(c, eventVal, exists)
	case map[string]any:
		// Nested object: the event value must also be an object
		eventObj, ok := eventVal.(map[string]any)
		if !ok {
			return false
		}
		return matchObject(c, eventObj)
	default:
		return false
	}
}

// matchArray checks if the event value matches ANY element in the pattern array (OR logic).
func matchArray(matchers []any, eventVal any, exists bool) bool {
	for _, matcher := range matchers {
		if matchSingleMatcher(matcher, eventVal, exists) {
			return true
		}
	}
	return false
}

// matchSingleMatcher handles one element in a pattern array.
// It can be a literal (string/number/bool/null) or a matcher object
// (prefix, suffix, anything-but, numeric, exists, wildcard).
func matchSingleMatcher(matcher any, eventVal any, exists bool) bool {
	switch m := matcher.(type) {
	case map[string]any:
		return matchOperator(m, eventVal, exists)
	default:
		// Literal comparison
		return literalEqual(m, eventVal) && exists
	}
}

// matchOperator handles operator objects: {prefix, suffix, anything-but, numeric, exists, wildcard}.
func matchOperator(op map[string]any, eventVal any, exists bool) bool {
	if v, ok := op["exists"]; ok {
		wantExists, isBool := v.(bool)
		if !isBool {
			return false
		}
		return wantExists == exists
	}

	if !exists {
		return false
	}

	if v, ok := op["prefix"]; ok {
		s, isStr := v.(string)
		ev, evStr := eventVal.(string)
		return isStr && evStr && strings.HasPrefix(ev, s)
	}

	if v, ok := op["suffix"]; ok {
		s, isStr := v.(string)
		ev, evStr := eventVal.(string)
		return isStr && evStr && strings.HasSuffix(ev, s)
	}

	if v, ok := op["wildcard"]; ok {
		pat, isStr := v.(string)
		ev, evStr := eventVal.(string)
		if !isStr || !evStr {
			return false
		}
		return matchWildcard(pat, ev)
	}

	if v, ok := op["anything-but"]; ok {
		return matchAnythingBut(v, eventVal)
	}

	if v, ok := op["numeric"]; ok {
		arr, isArr := v.([]any)
		if !isArr {
			return false
		}
		return matchNumeric(arr, eventVal)
	}

	if v, ok := op["cidr"]; ok {
		// cidr matching is uncommon in local dev; treat as literal prefix for now
		_ = v
		return false
	}

	return false
}

// matchAnythingBut returns true if eventVal does NOT match the anything-but constraint.
func matchAnythingBut(constraint any, eventVal any) bool {
	switch c := constraint.(type) {
	case []any:
		// anything-but: [val1, val2, ...] — true if eventVal is NOT any of them
		for _, item := range c {
			if literalEqual(item, eventVal) {
				return false
			}
		}
		return true
	case string:
		return !literalEqual(c, eventVal)
	case float64:
		return !literalEqual(c, eventVal)
	case map[string]any:
		// anything-but with operator, e.g. {"anything-but": {"prefix": "internal."}}
		if pfx, ok := c["prefix"]; ok {
			s, isStr := pfx.(string)
			ev, evStr := eventVal.(string)
			if isStr && evStr {
				return !strings.HasPrefix(ev, s)
			}
			return true
		}
		if sfx, ok := c["suffix"]; ok {
			s, isStr := sfx.(string)
			ev, evStr := eventVal.(string)
			if isStr && evStr {
				return !strings.HasSuffix(ev, s)
			}
			return true
		}
		if wc, ok := c["wildcard"]; ok {
			pat, isStr := wc.(string)
			ev, evStr := eventVal.(string)
			if isStr && evStr {
				return !matchWildcard(pat, ev)
			}
			return true
		}
		return false
	default:
		return true
	}
}

// matchNumeric evaluates numeric comparison operators.
// The array is pairs: [">", 0, "<=", 100] means > 0 AND <= 100.
func matchNumeric(ops []any, eventVal any) bool {
	num, ok := toFloat64(eventVal)
	if !ok {
		return false
	}
	if len(ops)%2 != 0 {
		return false
	}
	for i := 0; i < len(ops); i += 2 {
		opStr, isStr := ops[i].(string)
		if !isStr {
			return false
		}
		threshold, thOk := toFloat64(ops[i+1])
		if !thOk {
			return false
		}
		switch opStr {
		case "=":
			if num != threshold {
				return false
			}
		case ">":
			if num <= threshold {
				return false
			}
		case ">=":
			if num < threshold {
				return false
			}
		case "<":
			if num >= threshold {
				return false
			}
		case "<=":
			if num > threshold {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// matchWildcard matches a pattern containing '*' wildcards against a string.
// '*' matches zero or more characters. Literal '*' is not escapable (AWS behavior).
func matchWildcard(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}

	// First part must be a prefix
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	rest := s[len(parts[0]):]

	// Middle parts must appear in order
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(rest, parts[i])
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(parts[i]):]
	}

	// Last part must be a suffix
	return strings.HasSuffix(rest, parts[len(parts)-1])
}

// literalEqual compares a pattern literal against an event value.
// JSON numbers decode as float64; strings and bools are compared directly.
func literalEqual(pattern, event any) bool {
	if pattern == nil && event == nil {
		return true
	}
	if pattern == nil || event == nil {
		return false
	}

	// Both numbers
	pf, pIsNum := toFloat64(pattern)
	ef, eIsNum := toFloat64(event)
	if pIsNum && eIsNum {
		return pf == ef
	}

	// Both strings
	ps, pIsStr := pattern.(string)
	es, eIsStr := event.(string)
	if pIsStr && eIsStr {
		return ps == es
	}

	// Both bools
	pb, pIsBool := pattern.(bool)
	eb, eIsBool := event.(bool)
	if pIsBool && eIsBool {
		return pb == eb
	}

	return false
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// ValidateEventPattern checks that a pattern string is valid JSON and uses
// only supported operators. Returns nil if valid.
func ValidateEventPattern(patternJSON string) error {
	patternJSON = strings.TrimSpace(patternJSON)
	if patternJSON == "" {
		return fmt.Errorf("event pattern must not be empty")
	}
	var pattern map[string]any
	if err := json.Unmarshal([]byte(patternJSON), &pattern); err != nil {
		return fmt.Errorf("invalid JSON in event pattern: %w", err)
	}
	return validatePatternObject(pattern)
}

func validatePatternObject(obj map[string]any) error {
	for key, val := range obj {
		switch v := val.(type) {
		case []any:
			if err := validatePatternArray(key, v); err != nil {
				return err
			}
		case map[string]any:
			if err := validatePatternObject(v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("pattern value for key %q must be an array or object, got %T", key, val)
		}
	}
	return nil
}

func validatePatternArray(key string, arr []any) error {
	if len(arr) == 0 {
		return fmt.Errorf("pattern array for key %q must not be empty", key)
	}
	for _, elem := range arr {
		switch e := elem.(type) {
		case string, float64, bool:
			// literal match — ok
		case nil:
			// null match — ok
		case map[string]any:
			if err := validateOperatorObject(key, e); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported matcher type %T in pattern key %q", elem, key)
		}
	}
	return nil
}

var validOperators = map[string]bool{
	"prefix":       true,
	"suffix":       true,
	"anything-but": true,
	"numeric":      true,
	"exists":       true,
	"wildcard":     true,
	"cidr":         true,
}

func validateOperatorObject(key string, op map[string]any) error {
	if len(op) == 0 {
		return fmt.Errorf("empty matcher object in pattern key %q", key)
	}
	for opKey := range op {
		if !validOperators[opKey] {
			return fmt.Errorf("unsupported operator %q in pattern key %q", opKey, key)
		}
	}
	return nil
}
