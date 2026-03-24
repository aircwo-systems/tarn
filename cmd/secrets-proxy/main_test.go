package main

import "testing"

func TestIsInternalLambdaRuntime(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "1", want: true},
		{input: "true", want: true},
		{input: "TRUE", want: true},
		{input: "yes", want: true},
		{input: "on", want: true},
		{input: "0", want: false},
		{input: "false", want: false},
		{input: "", want: false},
	}

	for _, tt := range tests {
		if got := isInternalLambdaRuntime(tt.input); got != tt.want {
			t.Fatalf("isInternalLambdaRuntime(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
