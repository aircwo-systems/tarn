package eventsource

import "testing"

func TestNormalizeLambdaFunctionName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "order-logger", want: "order-logger"},
		{in: "arn:aws:lambda:us-east-1:000000000000:function:order-logger", want: "order-logger"},
		{in: "arn:aws:lambda:us-east-1:000000000000:function:order-logger:live", want: "order-logger"},
		{in: " arn:aws:lambda:us-east-1:000000000000:function:order-logger ", want: "order-logger"},
		{in: "", want: ""},
	}

	for _, tc := range tests {
		if got := normalizeLambdaFunctionName(tc.in); got != tc.want {
			t.Fatalf("normalizeLambdaFunctionName(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}
