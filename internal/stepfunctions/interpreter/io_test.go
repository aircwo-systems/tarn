package interpreter

import (
	"reflect"
	"testing"
)

func TestApplyInputPath(t *testing.T) {
	in := mustJSON(t, `{"a":{"b":5}}`)
	cases := []struct {
		path string
		want any
	}{
		{"$", in},
		{"$.a", mustJSON(t, `{"b":5}`)},
		{"", map[string]any{}},
	}
	for _, tc := range cases {
		got, err := applyInputPath(in, tc.path)
		if err != nil {
			t.Fatalf("applyInputPath(%q) error: %v", tc.path, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("applyInputPath(%q) = %#v, want %#v", tc.path, got, tc.want)
		}
	}
}

func TestApplyParameters(t *testing.T) {
	input := mustJSON(t, `{"name":"Sam","age":30,"nested":{"k":"v"}}`)
	ctx := ContextObject{"Execution": map[string]any{"Id": "exec-1"}}
	params := mustJSON(t, `{
		"greeting":"hi",
		"who.$":"$.name",
		"execId.$":"$$.Execution.Id",
		"msg.$":"States.Format('{} is {}', $.name, $.age)",
		"deep":{"copy.$":"$.nested.k"}
	}`)
	got, err := applyParameters(input, ctx, params)
	if err != nil {
		t.Fatalf("applyParameters error: %v", err)
	}
	want := mustJSON(t, `{"greeting":"hi","who":"Sam","execId":"exec-1","msg":"Sam is 30","deep":{"copy":"v"}}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("applyParameters = %#v, want %#v", got, want)
	}
}

func TestApplyParametersNilPassthrough(t *testing.T) {
	input := mustJSON(t, `{"a":1}`)
	got, err := applyParameters(input, nil, nil)
	if err != nil || !reflect.DeepEqual(got, input) {
		t.Fatalf("nil Parameters should pass input through: got %#v, err %v", got, err)
	}
}

func TestApplyResultSelector(t *testing.T) {
	result := mustJSON(t, `{"Payload":{"x":7},"StatusCode":200}`)
	sel := mustJSON(t, `{"x.$":"$.Payload.x","ok":true}`)
	got, err := applyResultSelector(result, nil, sel)
	if err != nil {
		t.Fatalf("applyResultSelector error: %v", err)
	}
	want := mustJSON(t, `{"x":7,"ok":true}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("applyResultSelector = %#v, want %#v", got, want)
	}
}

func TestApplyResultPath(t *testing.T) {
	input := mustJSON(t, `{"a":1}`)
	result := mustJSON(t, `{"r":2}`)
	cases := []struct {
		path string
		want any
	}{
		{"$", result},
		{"", input},
		{"$.output", mustJSON(t, `{"a":1,"output":{"r":2}}`)},
	}
	for _, tc := range cases {
		got, err := applyResultPath(input, result, tc.path)
		if err != nil {
			t.Fatalf("applyResultPath(%q) error: %v", tc.path, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("applyResultPath(%q) = %#v, want %#v", tc.path, got, tc.want)
		}
	}
}
