package cors

import (
	"net/http"
	"testing"
)

func TestFromHTTP(t *testing.T) {
	h := http.Header{}
	if _, ok := FromHTTP(http.MethodGet, h); ok {
		t.Fatal("request without Origin should not be cross-origin")
	}

	h.Set("Origin", "http://localhost:5173")
	req, ok := FromHTTP(http.MethodGet, h)
	if !ok || req.Preflight || req.Method != http.MethodGet {
		t.Fatalf("simple request = %+v, ok=%v", req, ok)
	}

	// OPTIONS without Access-Control-Request-Method is not a preflight.
	req, _ = FromHTTP(http.MethodOptions, h)
	if req.Preflight {
		t.Fatal("OPTIONS without Access-Control-Request-Method should not be a preflight")
	}

	h.Set("Access-Control-Request-Method", "PUT")
	h.Add("Access-Control-Request-Headers", "Content-Type, X-Amz-Date")
	h.Add("Access-Control-Request-Headers", "authorization")
	req, _ = FromHTTP(http.MethodOptions, h)
	if !req.Preflight || req.Method != "PUT" {
		t.Fatalf("preflight = %+v", req)
	}
	want := []string{"content-type", "x-amz-date", "authorization"}
	if len(req.Headers) != len(want) {
		t.Fatalf("headers = %v, want %v", req.Headers, want)
	}
	for i := range want {
		if req.Headers[i] != want[i] {
			t.Fatalf("headers = %v, want %v", req.Headers, want)
		}
	}
}

func TestPolicyMatching(t *testing.T) {
	p := Policy{
		AllowOrigins: []string{"http://localhost:5173", "https://*.example.com"},
		AllowMethods: []string{"GET", "put"},
		AllowHeaders: []string{"Content-Type", "x-amz-*"},
	}

	for origin, want := range map[string]bool{
		"http://localhost:5173":    true,
		"http://localhost:3000":    false,
		"https://app.example.com":  true,
		"https://example.com":      false,
		"http://app.example.com":   false,
		"https://a.b.example.com":  true,
		"https://evil.test":        false,
		"http://localhost:5173.io": false,
	} {
		if got := p.AllowsOrigin(origin); got != want {
			t.Errorf("AllowsOrigin(%q) = %v, want %v", origin, got, want)
		}
	}

	if !p.AllowsMethod("PUT") || p.AllowsMethod("DELETE") {
		t.Error("method matching should be case-insensitive and exact")
	}
	if !p.AllowsHeaders([]string{"content-type", "x-amz-date"}) {
		t.Error("expected content-type and x-amz-date to be allowed")
	}
	if p.AllowsHeaders([]string{"content-type", "authorization"}) {
		t.Error("authorization should not be allowed")
	}
	if !p.AllowsHeaders(nil) {
		t.Error("no requested headers should always be allowed")
	}
}

func TestResponseHeadersPreflight(t *testing.T) {
	p := Policy{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"*"},
		MaxAge:           600,
		AllowCredentials: true,
	}
	req := Request{Origin: "http://localhost:5173", Method: "POST", Headers: []string{"authorization"}, Preflight: true}
	if !p.AllowsPreflight(req) {
		t.Fatal("preflight should be allowed")
	}
	h := p.ResponseHeaders(req)
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":      "http://localhost:5173",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Methods":     "GET, POST",
		"Access-Control-Allow-Headers":     "authorization",
		"Access-Control-Max-Age":           "600",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if h.Get("Access-Control-Expose-Headers") != "" {
		t.Error("preflight response should not carry Expose-Headers")
	}
}

func TestResponseHeadersWildcardOrigin(t *testing.T) {
	p := Policy{AllowOrigins: []string{"*"}, AllowMethods: []string{"*"}, ExposeHeaders: []string{"ETag", "x-amz-request-id"}}

	h := p.ResponseHeaders(Request{Origin: "https://anything.test", Method: "GET"})
	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	if got := h.Get("Access-Control-Expose-Headers"); got != "ETag, x-amz-request-id" {
		t.Errorf("Expose-Headers = %q", got)
	}

	h = p.ResponseHeaders(Request{Origin: "https://anything.test", Method: "DELETE", Preflight: true})
	if got := h.Get("Access-Control-Allow-Methods"); got != "DELETE" {
		t.Errorf("wildcard Allow-Methods = %q, want the requested method", got)
	}

	// Credentials cannot be combined with "*", so the origin is echoed.
	p.AllowCredentials = true
	h = p.ResponseHeaders(Request{Origin: "https://anything.test", Method: "GET"})
	if got := h.Get("Access-Control-Allow-Origin"); got != "https://anything.test" {
		t.Errorf("credentialed Allow-Origin = %q, want echoed origin", got)
	}
}

func TestIsCORSHeader(t *testing.T) {
	if !IsCORSHeader("access-control-allow-origin") || !IsCORSHeader("Access-Control-Max-Age") {
		t.Error("expected Access-Control-* headers to match regardless of case")
	}
	if IsCORSHeader("Content-Type") {
		t.Error("Content-Type is not a CORS header")
	}
}
