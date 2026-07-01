package interpreter

import (
	"reflect"
	"testing"
)

func intrinsicRoot() any {
	return map[string]any{
		"name": "Sam",
		"age":  float64(30),
		"obj":  map[string]any{"k": float64(1)},
		"str":  `{"x":5}`,
		"a":    true,
	}
}

func TestIntrinsics(t *testing.T) {
	root := intrinsicRoot()
	cases := []struct {
		name string
		expr string
		want any
	}{
		{"format", `States.Format('{} is {}', $.name, $.age)`, "Sam is 30"},
		{"json to string", `States.JsonToString($.obj)`, `{"k":1}`},
		{"string to json", `States.StringToJson($.str)`, map[string]any{"x": float64(5)}},
		{"array", `States.Array($.name, 'x', 3, $.a)`, []any{"Sam", "x", float64(3), true}},
		{"nested", `States.Array(States.Format('{}', $.age))`, []any{"30"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalIntrinsic(tc.expr, root, nil)
			if err != nil {
				t.Fatalf("evalIntrinsic(%q) error: %v", tc.expr, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("evalIntrinsic(%q) = %#v, want %#v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestIntrinsicErrors(t *testing.T) {
	root := intrinsicRoot()
	for _, expr := range []string{
		`States.Nope($.x)`,            // unsupported
		`States.Format('{} {}', $.a)`, // not enough args
		`States.JsonToString()`,       // wrong arity
		`States.Format`,               // malformed (no parens)
	} {
		if _, err := evalIntrinsic(expr, root, nil); err == nil {
			t.Errorf("expected error for %q, got nil", expr)
		}
	}
}
