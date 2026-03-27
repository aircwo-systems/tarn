package sns

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsSNSRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Version=2010-03-31"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !IsSNSRequest(req) {
		t.Fatal("expected request with Version=2010-03-31 to be treated as SNS request")
	}
}

func TestDispatchUnknownActionReturnsEmptyOK(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=UnknownAction"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<UnknownActionResponse") {
		t.Fatalf("expected response wrapper for unknown action, got: %s", rec.Body.String())
	}
}
