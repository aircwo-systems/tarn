package interpreter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MVP supports a minimal set of ASL intrinsic functions. Unsupported intrinsics
// fail with States.IntrinsicFailure rather than being silently ignored.

// evalIntrinsic parses and evaluates an intrinsic expression such as
// "States.Format('{} {}', $.a, $.b)". root is the value "$" refers to.
func evalIntrinsic(expr string, root any, ctx ContextObject) (any, error) {
	name, args, err := parseIntrinsic(expr, root, ctx)
	if err != nil {
		return nil, err
	}
	switch name {
	case "States.Format":
		return intrinsicFormat(args)
	case "States.JsonToString":
		return intrinsicJSONToString(args)
	case "States.StringToJson":
		return intrinsicStringToJSON(args)
	case "States.Array":
		return args, nil
	default:
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: fmt.Sprintf("unsupported intrinsic %q", name)}
	}
}

// parseIntrinsic splits "Name(arg, arg, ...)" and evaluates each argument.
func parseIntrinsic(expr string, root any, ctx ContextObject) (string, []any, error) {
	open := strings.IndexByte(expr, '(')
	if open < 0 || !strings.HasSuffix(expr, ")") {
		return "", nil, &StateError{Name: ErrIntrinsicFailure, Cause: fmt.Sprintf("malformed intrinsic %q", expr)}
	}
	name := expr[:open]
	rawArgs, err := splitArgs(expr[open+1 : len(expr)-1])
	if err != nil {
		return "", nil, err
	}
	args := make([]any, 0, len(rawArgs))
	for _, raw := range rawArgs {
		v, err := evalArg(strings.TrimSpace(raw), root, ctx)
		if err != nil {
			return "", nil, err
		}
		args = append(args, v)
	}
	return name, args, nil
}

// splitArgs splits an argument list on top-level commas, respecting single-quoted
// string literals and nested parentheses.
func splitArgs(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var (
		args  []string
		b     strings.Builder
		depth int
		inStr bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == '\'' {
				inStr = false
			}
		case c == '\'':
			inStr = true
			b.WriteByte(c)
		case c == '(':
			depth++
			b.WriteByte(c)
		case c == ')':
			depth--
			b.WriteByte(c)
		case c == ',' && depth == 0:
			args = append(args, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if inStr {
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: "unterminated string literal in intrinsic"}
	}
	args = append(args, b.String())
	return args, nil
}

// evalArg evaluates one intrinsic argument: a string literal, a nested intrinsic,
// a path, a number, or a JSON keyword.
func evalArg(s string, root any, ctx ContextObject) (any, error) {
	switch {
	case s == "":
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: "empty intrinsic argument"}
	case s == "null":
		return nil, nil
	case s == "true":
		return true, nil
	case s == "false":
		return false, nil
	case s[0] == '\'':
		lit, err := parseStringLiteral(s)
		return lit, err
	case strings.HasPrefix(s, "States."):
		return evalIntrinsic(s, root, ctx)
	case strings.HasPrefix(s, "$$"):
		return getPath(map[string]any(ctx), s[1:])
	case s[0] == '$':
		return getPath(root, s)
	default:
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, nil
		}
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: fmt.Sprintf("invalid intrinsic argument %q", s)}
	}
}

// parseStringLiteral unwraps a single-quoted literal, honouring \' and \\ escapes.
func parseStringLiteral(s string) (string, error) {
	if len(s) < 2 || s[len(s)-1] != '\'' {
		return "", &StateError{Name: ErrIntrinsicFailure, Cause: fmt.Sprintf("malformed string literal %q", s)}
	}
	inner := s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
		}
		b.WriteByte(inner[i])
	}
	return b.String(), nil
}

func intrinsicFormat(args []any) (any, error) {
	if len(args) == 0 {
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: "States.Format requires a template"}
	}
	tmpl, ok := args[0].(string)
	if !ok {
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: "States.Format template must be a string"}
	}
	rest := args[1:]
	var b strings.Builder
	argi := 0
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] == '{' && i+1 < len(tmpl) && tmpl[i+1] == '}' {
			if argi >= len(rest) {
				return nil, &StateError{Name: ErrIntrinsicFailure, Cause: "States.Format: not enough arguments for template"}
			}
			b.WriteString(stringify(rest[argi]))
			argi++
			i++ // skip the '}'
			continue
		}
		b.WriteByte(tmpl[i])
	}
	return b.String(), nil
}

func intrinsicJSONToString(args []any) (any, error) {
	if len(args) != 1 {
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: "States.JsonToString requires exactly one argument"}
	}
	encoded, err := json.Marshal(args[0])
	if err != nil {
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: err.Error()}
	}
	return string(encoded), nil
}

func intrinsicStringToJSON(args []any) (any, error) {
	if len(args) != 1 {
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: "States.StringToJson requires exactly one argument"}
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: "States.StringToJson requires a string argument"}
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, &StateError{Name: ErrIntrinsicFailure, Cause: err.Error()}
	}
	return v, nil
}

// stringify renders a value for States.Format interpolation.
func stringify(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		encoded, _ := json.Marshal(v)
		return string(encoded)
	}
}
