package secretsproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestHandlerRejectsInvalidToken(t *testing.T) {
	h := NewHandler(Options{
		UpstreamURL:  "http://example.invalid",
		SessionToken: "expected-token",
		RequireToken: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/secretsmanager/get?secretId=my-secret", nil)
	req.Header.Set(tokenHeader, "wrong-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandlerForwardsGetSecret(t *testing.T) {
	var targetHeader string
	var requestPayload map[string]string
	httpClient := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			targetHeader = r.Header.Get("X-Amz-Target")
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &requestPayload)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"ARN":"arn:aws:secretsmanager:us-east-1:000000000000:secret:my-secret","Name":"my-secret","SecretString":"top-secret"}`)),
			}, nil
		}),
	}

	h := NewHandler(Options{
		UpstreamURL:  "http://openstack.local",
		SessionToken: "expected-token",
		RequireToken: true,
		HTTPClient:   httpClient,
	})

	req := httptest.NewRequest(http.MethodGet, "/secretsmanager/get?secretId=my-secret&versionStage=AWSCURRENT", nil)
	req.Header.Set(tokenHeader, "expected-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if targetHeader != "secretsmanager.GetSecretValue" {
		t.Fatalf("X-Amz-Target = %q, want %q", targetHeader, "secretsmanager.GetSecretValue")
	}
	if got := requestPayload["SecretId"]; got != "my-secret" {
		t.Fatalf("SecretId = %q, want %q", got, "my-secret")
	}
	if got := requestPayload["VersionStage"]; got != "AWSCURRENT" {
		t.Fatalf("VersionStage = %q, want %q", got, "AWSCURRENT")
	}
	if !strings.Contains(rec.Body.String(), `"SecretString":"top-secret"`) {
		t.Fatalf("response did not contain forwarded secret value, got: %s", rec.Body.String())
	}
}
