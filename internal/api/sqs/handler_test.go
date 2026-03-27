package sqs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	svc := sqssvc.NewService(cfg)
	return NewHandler(svc)
}

func TestXmlEscape_PreservesQuotesInElementText(t *testing.T) {
	input := `{"aggregateId":"agg-42","note":"it's fine"}`
	got := xmlEscape(input)
	if got != input {
		t.Fatalf("xmlEscape should not escape quotes in text nodes, got %q", got)
	}
}

func TestXmlEscape_EscapesReservedXMLChars(t *testing.T) {
	input := `<a>&</a>`
	got := xmlEscape(input)
	want := `&lt;a&gt;&amp;&lt;/a&gt;`
	if got != want {
		t.Fatalf("xmlEscape(%q) = %q, want %q", input, got, want)
	}
}

func TestDispatchUnknownXMLActionReturnsEmptyOK(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=UnknownThing"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<UnknownThingResponse") {
		t.Fatalf("expected empty OK response shape, got: %s", rec.Body.String())
	}
}

func TestDispatchUnknownJSONActionReturnsEmptyOK(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.UnknownThing")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Fatalf("expected empty json object, got: %s", rec.Body.String())
	}
}
