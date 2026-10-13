//go:build !lite

package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testUIFS(t *testing.T) fstest.MapFS {
	return fstest.MapFS{
		"200.html":                     {Data: []byte("<html>shell</html>")},
		"favicon.svg":                  {Data: []byte("<svg/>")},
		"_app/immutable/app.abc.js.gz": {Data: gzipBytes(t, "console.log('app')")},
		"_app/immutable/font.woff2":    {Data: []byte("woff2")},
	}
}

func serveUI(t *testing.T, target, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	newUIHandler(testUIFS(t)).ServeHTTP(rec, req)
	return rec
}

func TestUIHandler_ServesGzippedAssetToGzipClient(t *testing.T) {
	rec := serveUI(t, "/_app/immutable/app.abc.js", "gzip, deflate, br")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("Content-Type = %q, want javascript", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("Cache-Control = %q, want immutable", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	body, _ := io.ReadAll(zr)
	if string(body) != "console.log('app')" {
		t.Fatalf("body = %q", body)
	}
}

func TestUIHandler_InflatesGzippedAssetForPlainClient(t *testing.T) {
	for _, ae := range []string{"", "identity", "gzip;q=0"} {
		rec := serveUI(t, "/_app/immutable/app.abc.js", ae)

		if rec.Code != http.StatusOK {
			t.Fatalf("Accept-Encoding %q: status = %d, want 200", ae, rec.Code)
		}
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Accept-Encoding %q: Content-Encoding = %q, want none", ae, got)
		}
		if got := rec.Body.String(); got != "console.log('app')" {
			t.Fatalf("Accept-Encoding %q: body = %q", ae, got)
		}
	}
}

func TestUIHandler_ServesUncompressedAssetUnchanged(t *testing.T) {
	rec := serveUI(t, "/_app/immutable/font.woff2", "gzip")

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	if got := rec.Body.String(); got != "woff2" {
		t.Fatalf("body = %q", got)
	}
}

func TestUIHandler_SPAFallbackRevalidates(t *testing.T) {
	for _, target := range []string{"/", "/some/route", "/_app/immutable/missing.js"} {
		rec := serveUI(t, target, "gzip")

		if got := rec.Body.String(); got != "<html>shell</html>" {
			t.Fatalf("%s: body = %q, want SPA shell", target, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s: Cache-Control = %q, want no-cache", target, got)
		}
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":                 false,
		"identity":         false,
		"gzip":             true,
		"GZIP":             true,
		"br, gzip":         true,
		"gzip;q=0":         false,
		"gzip; q=0.5":      true,
		"*":                true,
		"deflate, *;q=0.1": true,
	}
	for header, want := range cases {
		if got := acceptsGzip(header); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}
