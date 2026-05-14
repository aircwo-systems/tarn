package account

import (
	"net/http"
	"testing"
)

func TestFromRequest(t *testing.T) {
	const def = "000000000000"

	tests := []struct {
		name string
		auth string
		want string
	}{
		{
			name: "12-digit AKID used as account ID",
			auth: "AWS4-HMAC-SHA256 Credential=111111111111/20240101/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=abc",
			want: "111111111111",
		},
		{
			name: "standard alphanumeric AKID falls back to default",
			auth: "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20240101/us-east-1/s3/aws4_request",
			want: def,
		},
		{
			name: "no Authorization header falls back to default",
			auth: "",
			want: def,
		},
		{
			name: "non-SigV4 Authorization falls back to default",
			auth: "Bearer sometoken",
			want: def,
		},
		{
			name: "11-digit AKID (too short) falls back to default",
			auth: "AWS4-HMAC-SHA256 Credential=11111111111/20240101/us-east-1/s3/aws4_request",
			want: def,
		},
		{
			name: "13-digit AKID (too long) falls back to default",
			auth: "AWS4-HMAC-SHA256 Credential=1111111111111/20240101/us-east-1/s3/aws4_request",
			want: def,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest(http.MethodGet, "/", nil)
			if tt.auth != "" {
				r.Header.Set("Authorization", tt.auth)
			}
			got := FromRequest(r, def)
			if got != tt.want {
				t.Errorf("FromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}
