package apigateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apisvc "github.com/aircwo-systems/tarn/internal/apigateway"
	"github.com/aircwo-systems/tarn/internal/config"
	lambdasvc "github.com/aircwo-systems/tarn/internal/lambda"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const testCORSConfig = `{
	"allowOrigins": ["http://localhost:5173"],
	"allowMethods": ["GET", "POST"],
	"allowHeaders": ["content-type", "authorization"],
	"exposeHeaders": ["x-request-id"],
	"maxAge": 600,
	"allowCredentials": true
}`

// newCORSTestHandler builds a handler whose AWS integrations send to a stub
// SQS queue, so invokes succeed without a Lambda runtime.
func newCORSTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	store := lambdasvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	send := func(string, string, map[string]*types.MessageAttribute, string, string) (string, string, error) {
		return "msg-1", "md5", nil
	}
	return NewHandler(apisvc.NewService(cfg, lambdasvc.NewService(cfg, store, nil, nil, nil), send))
}

func createCORSAPI(t *testing.T, h *Handler, corsJSON string) string {
	t.Helper()
	body := `{"name":"web","protocolType":"HTTP"`
	if corsJSON != "" {
		body += `,"corsConfiguration":` + corsJSON
	}
	body += `}`
	rec := httptest.NewRecorder()
	h.CreateAPI(rec, httptest.NewRequest(http.MethodPost, "/v2/apis", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("CreateAPI status=%d body=%s", rec.Code, rec.Body.String())
	}
	var api types.APIGatewayAPI
	if err := json.Unmarshal(rec.Body.Bytes(), &api); err != nil {
		t.Fatalf("decode api: %v", err)
	}

	integration, err := h.svc.CreateIntegration(api.APIID, apisvc.IntegrationCreateInput{
		IntegrationType: "AWS",
		IntegrationURI:  "arn:aws:sqs:us-east-1:000000000000:orders",
	})
	if err != nil {
		t.Fatalf("create integration: %v", err)
	}
	if _, err := h.svc.CreateRoute(api.APIID, apisvc.RouteCreateInput{
		RouteKey: "POST /orders",
		Target:   "integrations/" + integration.IntegrationID,
	}); err != nil {
		t.Fatalf("create route: %v", err)
	}
	return api.APIID
}

func invoke(h *Handler, apiID, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/_apigateway/"+apiID+"/$default"+path, bytes.NewBufferString(`{"id":1}`))
	req.SetPathValue("apiId", apiID)
	req.SetPathValue("stage", "$default")
	req.SetPathValue("proxy", strings.TrimPrefix(path, "/"))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.Invoke(rec, req)
	return rec
}

func TestCORSConfigurationRoundTrip(t *testing.T) {
	h := newCORSTestHandler(t)
	apiID := createCORSAPI(t, h, testCORSConfig)

	get := func() *types.APIGatewayCORS {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v2/apis/"+apiID, nil)
		req.SetPathValue("apiId", apiID)
		h.GetAPI(rec, req)
		var api types.APIGatewayAPI
		if err := json.Unmarshal(rec.Body.Bytes(), &api); err != nil {
			t.Fatalf("decode api: %v", err)
		}
		return api.CorsConfiguration
	}

	got := get()
	if got == nil || got.MaxAge != 600 || !got.AllowCredentials ||
		strings.Join(got.AllowOrigins, ",") != "http://localhost:5173" ||
		strings.Join(got.AllowHeaders, ",") != "content-type,authorization" {
		t.Fatalf("corsConfiguration after create = %+v", got)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/v2/apis/"+apiID, strings.NewReader(`{"corsConfiguration":{"allowOrigins":["*"],"allowMethods":["*"]}}`))
	req.SetPathValue("apiId", apiID)
	h.UpdateAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("UpdateAPI status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := get(); got == nil || strings.Join(got.AllowOrigins, ",") != "*" || got.MaxAge != 0 {
		t.Fatalf("corsConfiguration after update = %+v", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/v2/apis/"+apiID+"/cors", nil)
	req.SetPathValue("apiId", apiID)
	h.DeleteCORSConfiguration(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteCorsConfiguration status=%d", rec.Code)
	}
	if got := get(); got != nil {
		t.Fatalf("corsConfiguration after delete = %+v", got)
	}
}

func TestCORSConfigurationValidation(t *testing.T) {
	h := newCORSTestHandler(t)
	rec := httptest.NewRecorder()
	h.CreateAPI(rec, httptest.NewRequest(http.MethodPost, "/v2/apis", strings.NewReader(
		`{"name":"web","corsConfiguration":{"allowOrigins":["*"],"allowCredentials":true}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("credentials with * origin: status=%d, want 400", rec.Code)
	}
}

func TestCORSPreflightAnsweredByGateway(t *testing.T) {
	h := newCORSTestHandler(t)
	apiID := createCORSAPI(t, h, testCORSConfig)

	// No OPTIONS route exists; API Gateway answers the preflight itself.
	rec := invoke(h, apiID, http.MethodOptions, "/orders", map[string]string{
		"Origin":                         "http://localhost:5173",
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "content-type,authorization",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status=%d body=%s", rec.Code, rec.Body.String())
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":      "http://localhost:5173",
		"Access-Control-Allow-Methods":     "GET, POST",
		"Access-Control-Allow-Headers":     "content-type, authorization",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Max-Age":           "600",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	// A disallowed origin still gets 204, but without CORS headers, so the
	// browser blocks the request.
	rec = invoke(h, apiID, http.MethodOptions, "/orders", map[string]string{
		"Origin":                        "https://evil.test",
		"Access-Control-Request-Method": "POST",
	})
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed preflight status=%d allow-origin=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSHeadersOnResponses(t *testing.T) {
	h := newCORSTestHandler(t)
	apiID := createCORSAPI(t, h, testCORSConfig)
	origin := map[string]string{"Origin": "http://localhost:5173"}

	rec := invoke(h, apiID, http.MethodPost, "/orders", origin)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "x-request-id" {
		t.Errorf("Expose-Headers = %q", got)
	}

	// API Gateway's own errors carry the headers too.
	rec = invoke(h, apiID, http.MethodGet, "/missing", origin)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing route status=%d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("404 Allow-Origin = %q", got)
	}

	rec = invoke(h, apiID, http.MethodPost, "/orders", map[string]string{"Origin": "https://evil.test"})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got Allow-Origin %q", got)
	}
}

func TestOptionsRoutedWithoutCORSConfiguration(t *testing.T) {
	h := newCORSTestHandler(t)
	apiID := createCORSAPI(t, h, "")

	// Without corsConfiguration the preflight is routed like any request;
	// there is no OPTIONS route, so it is a 404 with no CORS headers.
	rec := invoke(h, apiID, http.MethodOptions, "/orders", map[string]string{
		"Origin":                        "http://localhost:5173",
		"Access-Control-Request-Method": "POST",
	})
	if rec.Code != http.StatusNotFound || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("status=%d allow-origin=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}
