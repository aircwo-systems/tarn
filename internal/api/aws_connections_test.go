package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/awsconnection"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/secrets"
)

const ownerTestBase = "http://127.0.0.1:4566"
const ownerTestCredentials = `{"name":"Development","region":"eu-west-2","credentials":{"kind":"temporary","accessKeyId":"ASIAEXAMPLE1234567890","secretAccessKey":"example-secret-key","sessionToken":"example-session-token"}}`

func testAWSOwner(t *testing.T) (*AWSConnections, http.Handler) {
	t.Helper()
	cfg := config.Default()
	cfg.Host = "127.0.0.1"
	cfg.DataDir = t.TempDir()
	vault, err := secrets.LoadOrCreateVault(filepath.Join(t.TempDir(), "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	a := NewAWSConnections(cfg, vault, func(context.Context, string, awsconnection.Credentials) (awsconnection.Identity, error) {
		return awsconnection.Identity{AccountID: "123456789012", ARN: "arn:aws:iam::123456789012:user/developer"}, nil
	})
	if ok, reason := a.Availability(); !ok {
		t.Fatal(reason)
	}
	t.Cleanup(a.Close)
	s := NewServer(cfg, NewHandlerRegistry(nil), nil, nil)
	s.SetAWSConnections(a)
	return a, s.httpServer.Handler
}

func ownerRequest(h http.Handler, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, ownerTestBase+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	if method != "GET" {
		r.Header.Set("Origin", ownerTestBase)
	}
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-Tarn-CSRF", csrf)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func unlockTestOwner(t *testing.T, a *AWSConnections, h http.Handler) (*http.Cookie, string) {
	t.Helper()
	r := httptest.NewRequest("POST", ownerTestBase+"/_tarn/owner/bootstrap", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Authorization", "Bearer "+a.control.Token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("bootstrap: %d %s", w.Code, w.Body.String())
	}
	var link struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	token := strings.Split(link.URL, "aws-unlock=")[1]
	r = httptest.NewRequest("POST", ownerTestBase+"/_tarn/owner/session", strings.NewReader(`{"token":"`+token+`"}`))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Origin", ownerTestBase)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tarn-Owner-Request", "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("session: %d %s", w.Code, w.Body.String())
	}
	var session struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("missing owner cookie")
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/_tarn/" {
		t.Fatal("unsafe owner cookie")
	}
	replay := httptest.NewRecorder()
	r.Body = io.NopCloser(strings.NewReader(`{"token":"` + token + `"}`))
	h.ServeHTTP(replay, r)
	if replay.Code != 403 {
		t.Fatal("single-use token was replayed")
	}
	return cookie, session.CSRF
}

func TestAWSOwnerConnectionLifecycle(t *testing.T) {
	a, h := testAWSOwner(t)
	cookie, csrf := unlockTestOwner(t, a, h)
	created := ownerRequest(h, "POST", "/_tarn/admin/aws-connections", ownerTestCredentials, cookie, csrf)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var m awsconnection.Metadata
	if err := json.Unmarshal(created.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/_tarn/owner/status", "/_tarn/admin/aws-connections"} {
		w := ownerRequest(h, "GET", path, "", cookie, "")
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("metadata response: %d", w.Code)
		}
		for _, secret := range []string{"ASIAEXAMPLE1234567890", "example-secret-key", "example-session-token"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("credential leaked into response")
			}
		}
	}
	base := "/_tarn/admin/aws-connections/" + m.ID
	if w := ownerRequest(h, "PATCH", base, `{"name":"Renamed","region":"eu-west-2"}`, cookie, csrf); w.Code != 200 {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	if w := ownerRequest(h, "POST", base+"/test", "", cookie, csrf); w.Code != 200 {
		t.Fatalf("test: %d", w.Code)
	}
	if w := ownerRequest(h, "DELETE", base, "", cookie, csrf); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if len(a.service.List()) != 0 {
		t.Fatal("connection not deleted")
	}
	if w := ownerRequest(h, "DELETE", "/_tarn/owner/session", "", cookie, csrf); w.Code != 204 {
		t.Fatal("lock failed")
	}
	if w := ownerRequest(h, "GET", "/_tarn/admin/aws-connections", "", cookie, ""); w.Code != 401 {
		t.Fatal("locked cookie accepted")
	}
}

func TestAWSOwnerRejectsUnauthenticatedAndCrossOriginRequests(t *testing.T) {
	a, h := testAWSOwner(t)
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		w := ownerRequest(h, method, "/_tarn/admin/aws-connections", ownerTestCredentials, nil, "")
		if w.Code != 401 {
			t.Errorf("unauthenticated %s: %d", method, w.Code)
		}
	}
	cookie, csrf := unlockTestOwner(t, a, h)
	for _, test := range []struct {
		name   string
		change func(*http.Request)
	}{
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }},
		{"null origin", func(r *http.Request) { r.Header.Set("Origin", "null") }},
		{"missing origin", func(r *http.Request) { r.Header.Del("Origin") }},
		{"foreign host", func(r *http.Request) { r.Host = "evil.example:4566" }},
		{"missing csrf", func(r *http.Request) { r.Header.Del("X-Tarn-CSRF") }},
		{"cross-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{"remote peer", func(r *http.Request) { r.RemoteAddr = "192.0.2.10:1234"; r.Header.Set("X-Forwarded-For", "127.0.0.1") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", ownerTestBase+"/_tarn/admin/aws-connections", strings.NewReader(ownerTestCredentials))
			r.RemoteAddr = "127.0.0.1:12345"
			r.Header.Set("Origin", ownerTestBase)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Tarn-CSRF", csrf)
			r.AddCookie(cookie)
			test.change(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("expected forbidden: %d %s", w.Code, w.Body.String())
			}
		})
	}
	if len(a.service.List()) != 0 {
		t.Fatal("forbidden operation created a record")
	}
}

func TestAWSOwnerProtocolHeadersCannotBypassAuthentication(t *testing.T) {
	_, h := testAWSOwner(t)
	r := httptest.NewRequest("POST", ownerTestBase+"/_tarn/admin/aws-connections", strings.NewReader(ownerTestCredentials))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=123456789012/test, Signature=0")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("protocol bypass: %d", w.Code)
	}
}

func TestAWSOwnerBootstrapExpiresAndRequiresLocalCapability(t *testing.T) {
	a, h := testAWSOwner(t)
	if w := ownerRequest(h, "POST", "/_tarn/owner/bootstrap", "", nil, ""); w.Code != 403 {
		t.Fatal("bootstrap issued without local capability")
	}
	a.bootstrap = "expired"
	a.bootstrapOrigin = ownerTestBase
	a.bootstrapExpires = time.Now().Add(-time.Second)
	r := httptest.NewRequest("POST", ownerTestBase+"/_tarn/owner/session", strings.NewReader(`{"token":"expired"}`))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Origin", ownerTestBase)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tarn-Owner-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("expired token accepted")
	}
}

func TestAWSOwnerDisabledOnPublicBind(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "::"} {
		cfg := config.Default()
		cfg.Host = host
		cfg.DataDir = t.TempDir()
		a := NewAWSConnections(cfg, nil, nil)
		if ok, _ := a.Availability(); ok {
			t.Fatal("connections enabled without encryption or loopback")
		}
		if _, err := os.Stat(OwnerControlPath(cfg.DataDir)); !os.IsNotExist(err) {
			t.Fatal("disabled owner capability created")
		}
		if w := ownerRequest(a, "POST", "/_tarn/owner/session", `{"token":"anything"}`, nil, ""); w.Code != 503 {
			t.Fatal("disabled session accepted")
		}
	}
}

func TestAWSOwnerProfilesWorkWithoutVault(t *testing.T) {
	cfg := config.Default()
	cfg.Host, cfg.DataDir = "127.0.0.1", t.TempDir()
	a := NewAWSConnections(cfg, nil, func(_ context.Context, _ string, c awsconnection.Credentials) (awsconnection.Identity, error) {
		if c.Kind != "profile" {
			t.Error("manual credentials reached validation without encryption")
		}
		return awsconnection.Identity{AccountID: "123456789012", ARN: "arn:aws:iam::123456789012:user/developer"}, nil
	})
	t.Cleanup(a.Close)
	cookie, csrf := unlockTestOwner(t, a, a)
	w := ownerRequest(a, "POST", "/_tarn/admin/aws-connections", `{"name":"Company","region":"eu-west-2","credentials":{"kind":"profile","profile":"company-dev"}}`, cookie, csrf)
	if w.Code != 201 {
		t.Fatalf("profile creation: %d %s", w.Code, w.Body.String())
	}
	w = ownerRequest(a, "POST", "/_tarn/admin/aws-connections", ownerTestCredentials, cookie, csrf)
	if w.Code != 400 {
		t.Fatalf("manual credentials without Vault: %d", w.Code)
	}
	w = ownerRequest(a, "GET", "/_tarn/owner/status", "", cookie, "")
	if !strings.Contains(w.Body.String(), `"manualCredentialsAvailable":false`) {
		t.Fatal("missing manual credential availability")
	}
}

func TestAWSOwnerRejectsUnsupportedDashboardOrigins(t *testing.T) {
	for _, origin := range []string{"http://evil.example:4173", "https://127.0.0.1:4173", "http://127.0.0.1:4173/path", "http://user@127.0.0.1:4173", "null"} {
		cfg := config.Default()
		cfg.Host, cfg.DataDir, cfg.AWSUIOrigin = "127.0.0.1", t.TempDir(), origin
		a := NewAWSConnections(cfg, nil, nil)
		if ok, reason := a.Availability(); ok || !strings.Contains(reason, "TARN_AWS_UI_ORIGIN") {
			t.Errorf("unsupported origin %q accepted: %s", origin, reason)
		}
	}
}
