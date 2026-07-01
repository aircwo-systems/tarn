package interpreter

import (
	"reflect"
	"testing"
)

func TestGetPath(t *testing.T) {
	data := mustJSON(t, `{"a":{"b":[10,20,{"c":"deep"}]},"k":"v"}`)
	cases := []struct {
		path string
		want any
	}{
		{"$", data},
		{"$.k", "v"},
		{"$.a.b[0]", float64(10)},
		{"$.a.b[2].c", "deep"},
		{"$['k']", "v"},
		{"$.a.b[2]['c']", "deep"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, err := getPath(data, tc.path)
			if err != nil {
				t.Fatalf("getPath(%q) error: %v", tc.path, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("getPath(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}

func TestGetPathErrors(t *testing.T) {
	data := mustJSON(t, `{"a":[1,2]}`)
	for _, path := range []string{"$.missing", "$.a[9]", "$.a.b", "noroot", "$.a[bad]", "$.a["} {
		if _, err := getPath(data, path); err == nil {
			t.Errorf("expected error for path %q, got nil", path)
		}
	}
}

func TestSetPath(t *testing.T) {
	input := mustJSON(t, `{"a":1}`)
	result := mustJSON(t, `{"r":2}`)

	root, err := setPath(input, "$", result)
	if err != nil || !reflect.DeepEqual(root, result) {
		t.Fatalf("setPath root replace = %#v, %v", root, err)
	}

	nested, err := setPath(input, "$.out", result)
	if err != nil {
		t.Fatalf("setPath nested error: %v", err)
	}
	want := mustJSON(t, `{"a":1,"out":{"r":2}}`)
	if !reflect.DeepEqual(nested, want) {
		t.Fatalf("setPath nested = %#v, want %#v", nested, want)
	}

	// The original input must be untouched (immutability at the boundary).
	if !reflect.DeepEqual(input, mustJSON(t, `{"a":1}`)) {
		t.Fatalf("setPath mutated input: %#v", input)
	}
}
