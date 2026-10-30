package dynamodb

import (
	"errors"
	"testing"
)

func evalCond(t *testing.T, expr string, item map[string]any, names map[string]string, values map[string]any) bool {
	t.Helper()
	ok, err := evaluateConditionExpression(expr, item, names, values)
	if err != nil {
		t.Fatalf("evaluate %q: %v", expr, err)
	}
	return ok
}

func exprRequireValidation(t *testing.T, err error) {
	t.Helper()
	var se *ServiceError
	if !errors.As(err, &se) || se.Code != "ValidationException" {
		t.Fatalf("want ValidationException, got %v", err)
	}
}

func sAttr(v string) map[string]any { return map[string]any{"S": v} }
func nAttr(v string) map[string]any { return map[string]any{"N": v} }

func TestExprSizeFunction(t *testing.T) {
	item := map[string]any{
		"s":    sAttr("hello"),
		"l":    map[string]any{"L": []any{sAttr("a"), sAttr("b")}},
		"m":    map[string]any{"M": map[string]any{"x": sAttr("1"), "y": sAttr("2"), "z": sAttr("3")}},
		"ss":   map[string]any{"SS": []any{"a", "b"}},
		"n":    nAttr("12345"),
		"list": map[string]any{"L": []any{sAttr("a"), sAttr("b"), sAttr("c")}},
	}
	values := map[string]any{
		":three": nAttr("3"),
		":five":  nAttr("5"),
		":two":   nAttr("2"),
		":zero":  nAttr("0"),
		":n":     nAttr("1"),
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"size(s) = :five", true},
		{"size(l) = :two", true},
		{"size(m) = :three", true},
		{"size(ss) = :two", true},
		{"size(#l) > :n", true},
		{"size(n) > :zero", false},
	}
	names := map[string]string{"#l": "list"}
	for _, tc := range cases {
		if got := evalCond(t, tc.expr, item, names, values); got != tc.want {
			t.Errorf("%q = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestExprContains(t *testing.T) {
	item := map[string]any{
		"s":  sAttr("hello world"),
		"ss": map[string]any{"SS": []any{"a", "b"}},
		"ns": map[string]any{"NS": []any{"1", "2"}},
		"l":  map[string]any{"L": []any{sAttr("x"), nAttr("7")}},
	}
	values := map[string]any{
		":sub":  sAttr("lo w"),
		":miss": sAttr("zz"),
		":a":    sAttr("a"),
		":n1":   nAttr("1.0"),
		":x":    sAttr("x"),
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"contains(s, :sub)", true},
		{"contains(s, :miss)", false},
		{"contains(ss, :a)", true},
		{"contains(ns, :n1)", true},
		{"contains(l, :x)", true},
		{"contains(missing, :a)", false},
	}
	for _, tc := range cases {
		if got := evalCond(t, tc.expr, item, nil, values); got != tc.want {
			t.Errorf("%q = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestExprAttributeType(t *testing.T) {
	item := map[string]any{"a": sAttr("x")}
	names := map[string]string{"#a": "a"}
	if !evalCond(t, "attribute_type(#a, :t)", item, names, map[string]any{":t": sAttr("S")}) {
		t.Error("attribute_type S = S should be true")
	}
	if evalCond(t, "attribute_type(#a, :t)", item, names, map[string]any{":t": sAttr("N")}) {
		t.Error("attribute_type S = N should be false")
	}
}

func TestExprCrossTypeComparisonFalse(t *testing.T) {
	item := map[string]any{"a": sAttr("5")}
	if evalCond(t, "a < :v", item, nil, map[string]any{":v": nAttr("6")}) {
		t.Error(`{"S":"5"} < {"N":"6"} should be false`)
	}
}

func TestExprSetEqualityOrderIndependent(t *testing.T) {
	item := map[string]any{"a": map[string]any{"SS": []any{"a", "b"}}}
	values := map[string]any{":v": map[string]any{"SS": []any{"b", "a"}}}
	if !evalCond(t, "a = :v", item, nil, values) {
		t.Error("SS equality should ignore order")
	}
}

func TestExprNotEqualMissingAttributeTrue(t *testing.T) {
	item := map[string]any{}
	if !evalCond(t, "a <> :v", item, nil, map[string]any{":v": sAttr("x")}) {
		t.Error("missing a <> :v should be true")
	}
}

func TestExprBeginsWith(t *testing.T) {
	item := map[string]any{"s": sAttr("prefix-body"), "n": nAttr("123")}
	values := map[string]any{":pre": sAttr("prefix"), ":other": sAttr("xx"), ":num": nAttr("12")}
	if !evalCond(t, "begins_with(s, :pre)", item, nil, values) {
		t.Error("begins_with S true case failed")
	}
	if evalCond(t, "begins_with(s, :other)", item, nil, values) {
		t.Error("begins_with S false case returned true")
	}
	if evalCond(t, "begins_with(n, :num)", item, nil, values) {
		t.Error("begins_with on N should be false")
	}
}

func TestUpdateListAppendIfNotExistsTwice(t *testing.T) {
	item := map[string]any{}
	names := map[string]string{"#l": "list"}
	values := map[string]any{
		":empty": map[string]any{"L": []any{}},
		":v":     map[string]any{"L": []any{sAttr("x")}},
	}
	for i := 0; i < 2; i++ {
		if err := applyUpdateExpression(item, "SET #l = list_append(if_not_exists(#l, :empty), :v)", names, values); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	l, ok := item["list"].(map[string]any)["L"].([]any)
	if !ok || len(l) != 2 {
		t.Errorf("list = %v, want 2 elements", item["list"])
	}
}

func TestUpdateIfNotExistsIncrementTwice(t *testing.T) {
	item := map[string]any{}
	names := map[string]string{"#c": "count"}
	values := map[string]any{":zero": nAttr("0"), ":one": nAttr("1")}
	for i := 0; i < 2; i++ {
		if err := applyUpdateExpression(item, "SET #c = if_not_exists(#c, :zero) + :one", names, values); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	if got := item["count"]; !attributeValueEqual(got, nAttr("2")) {
		t.Errorf("count = %v, want N 2", got)
	}
}

func TestUpdateIfNotExistsSumOperands(t *testing.T) {
	item := map[string]any{"b": nAttr("10")}
	names := map[string]string{"#a": "a", "#b": "b"}
	values := map[string]any{":x": nAttr("1"), ":y": nAttr("0")}
	if err := applyUpdateExpression(item, "SET #a = if_not_exists(#a, :x) + if_not_exists(#b, :y)", names, values); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := item["a"]; !attributeValueEqual(got, nAttr("11")) {
		t.Errorf("a = %v, want N 11", got)
	}
}

func TestUpdateListAppendNonListRejected(t *testing.T) {
	item := map[string]any{"s": sAttr("text")}
	names := map[string]string{"#s": "s", "#l": "l"}
	values := map[string]any{":v": map[string]any{"L": []any{sAttr("x")}}}
	err := applyUpdateExpression(item, "SET #l = list_append(#s, :v)", names, values)
	exprRequireValidation(t, err)
}

func TestUpdatePathFromMissingAttributeRejected(t *testing.T) {
	item := map[string]any{}
	names := map[string]string{"#a": "a", "#b": "b"}
	err := applyUpdateExpression(item, "SET #a = #b", names, nil)
	exprRequireValidation(t, err)
}
