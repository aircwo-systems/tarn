package interpreter

import (
	"time"

	"github.com/aircwo-systems/tarn/internal/stepfunctions/asl"
)

// evalRule reports whether a ChoiceRule matches data. It returns (matched,
// error); a missing variable is not an error for IsPresent but is a non-match
// for comparison operators.
func evalRule(rule asl.ChoiceRule, data any) (bool, error) {
	// --- Combinators ---
	if len(rule.And) > 0 {
		for _, sub := range rule.And {
			ok, err := evalRule(sub, data)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
	if len(rule.Or) > 0 {
		for _, sub := range rule.Or {
			ok, err := evalRule(sub, data)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if rule.Not != nil {
		ok, err := evalRule(*rule.Not, data)
		if err != nil {
			return false, err
		}
		return !ok, nil
	}

	// --- Variable resolution ---
	varVal, varErr := getPath(data, rule.Variable)
	present := varErr == nil

	// --- Type-check operators ---
	if rule.IsPresent != nil {
		return present == *rule.IsPresent, nil
	}
	if rule.IsNull != nil {
		if !present {
			return false, nil
		}
		return (varVal == nil) == *rule.IsNull, nil
	}
	if rule.IsNumeric != nil {
		if !present {
			return false, nil
		}
		_, ok := varVal.(float64)
		return ok == *rule.IsNumeric, nil
	}
	if rule.IsString != nil {
		if !present {
			return false, nil
		}
		_, ok := varVal.(string)
		return ok == *rule.IsString, nil
	}
	if rule.IsBoolean != nil {
		if !present {
			return false, nil
		}
		_, ok := varVal.(bool)
		return ok == *rule.IsBoolean, nil
	}
	if rule.IsTimestamp != nil {
		if !present {
			return false, nil
		}
		s, ok := varVal.(string)
		if !ok {
			return !*rule.IsTimestamp, nil
		}
		_, err := time.Parse(time.RFC3339, s)
		isTS := err == nil
		return isTS == *rule.IsTimestamp, nil
	}

	// For all comparison operators, a missing variable is a non-match.
	if !present {
		return false, nil
	}

	// --- String comparisons ---
	if rule.StringEquals != nil {
		s, ok := varVal.(string)
		return ok && s == *rule.StringEquals, nil
	}
	if rule.StringLessThan != nil {
		s, ok := varVal.(string)
		return ok && s < *rule.StringLessThan, nil
	}
	if rule.StringGreaterThan != nil {
		s, ok := varVal.(string)
		return ok && s > *rule.StringGreaterThan, nil
	}
	if rule.StringLessThanEquals != nil {
		s, ok := varVal.(string)
		return ok && s <= *rule.StringLessThanEquals, nil
	}
	if rule.StringGreaterThanEquals != nil {
		s, ok := varVal.(string)
		return ok && s >= *rule.StringGreaterThanEquals, nil
	}

	// --- String *Path variants ---
	if rule.StringEqualsPath != nil {
		cmp, err := resolvePathVal(data, *rule.StringEqualsPath)
		if err != nil {
			return false, nil
		}
		s, ok := varVal.(string)
		cs, cok := cmp.(string)
		return ok && cok && s == cs, nil
	}
	if rule.StringLessThanPath != nil {
		cmp, err := resolvePathVal(data, *rule.StringLessThanPath)
		if err != nil {
			return false, nil
		}
		s, ok := varVal.(string)
		cs, cok := cmp.(string)
		return ok && cok && s < cs, nil
	}
	if rule.StringGreaterThanPath != nil {
		cmp, err := resolvePathVal(data, *rule.StringGreaterThanPath)
		if err != nil {
			return false, nil
		}
		s, ok := varVal.(string)
		cs, cok := cmp.(string)
		return ok && cok && s > cs, nil
	}
	if rule.StringLessThanEqualsPath != nil {
		cmp, err := resolvePathVal(data, *rule.StringLessThanEqualsPath)
		if err != nil {
			return false, nil
		}
		s, ok := varVal.(string)
		cs, cok := cmp.(string)
		return ok && cok && s <= cs, nil
	}
	if rule.StringGreaterThanEqualsPath != nil {
		cmp, err := resolvePathVal(data, *rule.StringGreaterThanEqualsPath)
		if err != nil {
			return false, nil
		}
		s, ok := varVal.(string)
		cs, cok := cmp.(string)
		return ok && cok && s >= cs, nil
	}

	// --- Numeric comparisons ---
	if rule.NumericEquals != nil {
		n, ok := varVal.(float64)
		return ok && n == *rule.NumericEquals, nil
	}
	if rule.NumericLessThan != nil {
		n, ok := varVal.(float64)
		return ok && n < *rule.NumericLessThan, nil
	}
	if rule.NumericGreaterThan != nil {
		n, ok := varVal.(float64)
		return ok && n > *rule.NumericGreaterThan, nil
	}
	if rule.NumericLessThanEquals != nil {
		n, ok := varVal.(float64)
		return ok && n <= *rule.NumericLessThanEquals, nil
	}
	if rule.NumericGreaterThanEquals != nil {
		n, ok := varVal.(float64)
		return ok && n >= *rule.NumericGreaterThanEquals, nil
	}

	// --- Numeric *Path variants ---
	if rule.NumericEqualsPath != nil {
		cmp, err := resolvePathVal(data, *rule.NumericEqualsPath)
		if err != nil {
			return false, nil
		}
		n, ok := varVal.(float64)
		cn, cok := cmp.(float64)
		return ok && cok && n == cn, nil
	}
	if rule.NumericLessThanPath != nil {
		cmp, err := resolvePathVal(data, *rule.NumericLessThanPath)
		if err != nil {
			return false, nil
		}
		n, ok := varVal.(float64)
		cn, cok := cmp.(float64)
		return ok && cok && n < cn, nil
	}
	if rule.NumericGreaterThanPath != nil {
		cmp, err := resolvePathVal(data, *rule.NumericGreaterThanPath)
		if err != nil {
			return false, nil
		}
		n, ok := varVal.(float64)
		cn, cok := cmp.(float64)
		return ok && cok && n > cn, nil
	}
	if rule.NumericLessThanEqualsPath != nil {
		cmp, err := resolvePathVal(data, *rule.NumericLessThanEqualsPath)
		if err != nil {
			return false, nil
		}
		n, ok := varVal.(float64)
		cn, cok := cmp.(float64)
		return ok && cok && n <= cn, nil
	}
	if rule.NumericGreaterThanEqualsPath != nil {
		cmp, err := resolvePathVal(data, *rule.NumericGreaterThanEqualsPath)
		if err != nil {
			return false, nil
		}
		n, ok := varVal.(float64)
		cn, cok := cmp.(float64)
		return ok && cok && n >= cn, nil
	}

	// --- Boolean comparisons ---
	if rule.BooleanEquals != nil {
		b, ok := varVal.(bool)
		return ok && b == *rule.BooleanEquals, nil
	}
	if rule.BooleanEqualsPath != nil {
		cmp, err := resolvePathVal(data, *rule.BooleanEqualsPath)
		if err != nil {
			return false, nil
		}
		b, ok := varVal.(bool)
		cb, cok := cmp.(bool)
		return ok && cok && b == cb, nil
	}

	// --- Timestamp comparisons ---
	if rule.TimestampEquals != nil {
		ts, ok := parseTS(varVal)
		cmp, cerr := time.Parse(time.RFC3339, *rule.TimestampEquals)
		return ok && cerr == nil && ts.Equal(cmp), nil
	}
	if rule.TimestampLessThan != nil {
		ts, ok := parseTS(varVal)
		cmp, cerr := time.Parse(time.RFC3339, *rule.TimestampLessThan)
		return ok && cerr == nil && ts.Before(cmp), nil
	}
	if rule.TimestampGreaterThan != nil {
		ts, ok := parseTS(varVal)
		cmp, cerr := time.Parse(time.RFC3339, *rule.TimestampGreaterThan)
		return ok && cerr == nil && ts.After(cmp), nil
	}
	if rule.TimestampLessThanEquals != nil {
		ts, ok := parseTS(varVal)
		cmp, cerr := time.Parse(time.RFC3339, *rule.TimestampLessThanEquals)
		return ok && cerr == nil && !ts.After(cmp), nil
	}
	if rule.TimestampGreaterThanEquals != nil {
		ts, ok := parseTS(varVal)
		cmp, cerr := time.Parse(time.RFC3339, *rule.TimestampGreaterThanEquals)
		return ok && cerr == nil && !ts.Before(cmp), nil
	}

	// No operator matched — treat as non-match.
	return false, nil
}

// resolvePathVal resolves a *Path comparison value from data using getPath.
func resolvePathVal(data any, path string) (any, error) {
	return getPath(data, path)
}

// parseTS attempts to parse a timestamp from an any value (must be a string in
// RFC3339). Returns the parsed time and whether it succeeded.
func parseTS(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}
