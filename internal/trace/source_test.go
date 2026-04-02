package trace

import (
	"net/http"
	"testing"
)

func TestExternalSpanFromRequest_OriginBrowser(t *testing.T) {
	headers := http.Header{}
	headers.Set("Origin", "http://localhost:5173")
	headers.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X)")

	span := ExternalSpanFromRequest(headers, "127.0.0.1:60321")
	if span == nil {
		t.Fatal("span = nil, want external span")
	}
	if span.Kind != "external" {
		t.Fatalf("kind = %q, want %q", span.Kind, "external")
	}
	if span.Name != "localhost:5173" {
		t.Fatalf("name = %q, want %q", span.Name, "localhost:5173")
	}
	if got := span.Meta["sourceKind"]; got != "frontend" {
		t.Fatalf("sourceKind = %q, want %q", got, "frontend")
	}
	if got := span.Meta["clientIp"]; got != "127.0.0.1" {
		t.Fatalf("clientIp = %q, want %q", got, "127.0.0.1")
	}
}

func TestExternalSpanFromRequest_Postman(t *testing.T) {
	headers := http.Header{}
	headers.Set("User-Agent", "PostmanRuntime/7.43.0")

	span := ExternalSpanFromRequest(headers, "")
	if span == nil {
		t.Fatal("span = nil, want external span")
	}
	if span.Name != "Postman" {
		t.Fatalf("name = %q, want %q", span.Name, "Postman")
	}
	if got := span.Meta["sourceKind"]; got != "postman" {
		t.Fatalf("sourceKind = %q, want %q", got, "postman")
	}
}

func TestExternalSpanFromRequest_Empty(t *testing.T) {
	if span := ExternalSpanFromRequest(http.Header{}, ""); span != nil {
		t.Fatalf("span = %+v, want nil", span)
	}
}
