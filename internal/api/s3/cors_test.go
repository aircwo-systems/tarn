package s3

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testCORSConfig = `<CORSConfiguration>
  <CORSRule>
    <AllowedOrigin>http://localhost:5173</AllowedOrigin>
    <AllowedMethod>GET</AllowedMethod>
    <AllowedMethod>PUT</AllowedMethod>
    <AllowedHeader>*</AllowedHeader>
    <ExposeHeader>ETag</ExposeHeader>
    <MaxAgeSeconds>600</MaxAgeSeconds>
  </CORSRule>
  <CORSRule>
    <AllowedOrigin>*</AllowedOrigin>
    <AllowedMethod>GET</AllowedMethod>
  </CORSRule>
</CORSConfiguration>`

func newCORSBucket(t *testing.T, withConfig bool) *Handler {
	t.Helper()
	h := newTestHandler(t)
	do(t, h, http.MethodPut, "/_s3/assets", nil, "")
	do(t, h, http.MethodPut, "/_s3/assets/hello.txt", nil, "hi")
	if withConfig {
		if rec := do(t, h, http.MethodPut, "/_s3/assets?cors", nil, testCORSConfig); rec.Code != http.StatusOK {
			t.Fatalf("put cors = %d: %s", rec.Code, rec.Body.String())
		}
	}
	return h
}

func do(t *testing.T, h *Handler, method, target string, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	return rec
}

func TestS3PreflightAllowed(t *testing.T) {
	h := newCORSBucket(t, true)
	rec := do(t, h, http.MethodOptions, "/_s3/assets/upload.png", map[string]string{
		"Origin":                         "http://localhost:5173",
		"Access-Control-Request-Method":  "PUT",
		"Access-Control-Request-Headers": "content-type, x-amz-date",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":      "http://localhost:5173",
		"Access-Control-Allow-Methods":     "GET, PUT",
		"Access-Control-Allow-Headers":     "content-type, x-amz-date",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Max-Age":           "600",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestS3PreflightFallsThroughToWildcardRule(t *testing.T) {
	h := newCORSBucket(t, true)
	rec := do(t, h, http.MethodOptions, "/_s3/assets/hello.txt", map[string]string{
		"Origin":                        "https://other.test",
		"Access-Control-Request-Method": "GET",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("wildcard rule should not allow credentials, got %q", got)
	}
}

func TestS3PreflightRejected(t *testing.T) {
	cases := []struct {
		name       string
		withConfig bool
		path       string
		headers    map[string]string
		status     int
		message    string
	}{
		{
			name: "missing origin", withConfig: true, path: "/_s3/assets/hello.txt",
			headers: map[string]string{"Access-Control-Request-Method": "GET"},
			status:  http.StatusBadRequest, message: "Origin request header needed",
		},
		{
			name: "missing request method", withConfig: true, path: "/_s3/assets/hello.txt",
			headers: map[string]string{"Origin": "http://localhost:5173"},
			status:  http.StatusBadRequest, message: "Invalid Access-Control-Request-Method",
		},
		{
			name: "no cors config", withConfig: false, path: "/_s3/assets/hello.txt",
			headers: map[string]string{"Origin": "http://localhost:5173", "Access-Control-Request-Method": "GET"},
			status:  http.StatusForbidden, message: "CORS is not enabled for this bucket",
		},
		{
			name: "missing bucket", withConfig: true, path: "/_s3/nope/hello.txt",
			headers: map[string]string{"Origin": "http://localhost:5173", "Access-Control-Request-Method": "GET"},
			status:  http.StatusForbidden, message: "Bucket not found",
		},
		{
			name: "method not allowed", withConfig: true, path: "/_s3/assets/hello.txt",
			headers: map[string]string{"Origin": "http://localhost:5173", "Access-Control-Request-Method": "DELETE"},
			status:  http.StatusForbidden, message: "This CORS request is not allowed",
		},
		{
			name: "origin only matches GET rule", withConfig: true, path: "/_s3/assets/hello.txt",
			headers: map[string]string{"Origin": "https://evil.test", "Access-Control-Request-Method": "PUT"},
			status:  http.StatusForbidden, message: "This CORS request is not allowed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newCORSBucket(t, tc.withConfig)
			rec := do(t, h, http.MethodOptions, tc.path, tc.headers, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tc.status, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.message) {
				t.Errorf("body %q does not mention %q", rec.Body.String(), tc.message)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("rejected preflight carried Allow-Origin %q", got)
			}
		})
	}
}

func TestS3ActualRequestCORSHeaders(t *testing.T) {
	h := newCORSBucket(t, true)

	rec := do(t, h, http.MethodGet, "/_s3/assets/hello.txt", map[string]string{"Origin": "http://localhost:5173"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "ETag" {
		t.Errorf("Expose-Headers = %q, want ETag", got)
	}

	// Errors carry the headers too, so the browser can read the S3 error.
	rec = do(t, h, http.MethodGet, "/_s3/assets/missing.txt", map[string]string{"Origin": "http://localhost:5173"}, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing object status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("error response Allow-Origin = %q", got)
	}

	// DELETE is not allowed for any origin, so no headers are added.
	rec = do(t, h, http.MethodDelete, "/_s3/assets/hello.txt", map[string]string{"Origin": "http://localhost:5173"}, "")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed method carried Allow-Origin %q", got)
	}

	// Same-origin and server-side requests are untouched.
	rec = do(t, h, http.MethodGet, "/_s3/assets/hello.txt", nil, "")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("request without Origin carried Allow-Origin %q", got)
	}
}

func TestS3VirtualHostedPreflight(t *testing.T) {
	h := newCORSBucket(t, true)
	req := httptest.NewRequest(http.MethodOptions, "/hello.txt", nil)
	req.Host = "assets.localhost:4566"
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Allow-Origin = %q", got)
	}
}
