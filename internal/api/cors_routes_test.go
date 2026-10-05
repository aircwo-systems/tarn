package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
)

// TestCORSPreflightRouting checks that browser preflights reach S3 and API
// Gateway, with and without the dashboard's catch-all route registered.
func TestCORSPreflightRouting(t *testing.T) {
	for _, uiEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "ui off", true: "ui on"}[uiEnabled], func(t *testing.T) {
			cfg := config.Default()
			cfg.DataDir = t.TempDir()
			cfg.UIEnabled = uiEnabled
			handler := newTestHTTPHandlerWithConfig(t, cfg)

			send := func(method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, target, strings.NewReader(body))
				for k, v := range headers {
					req.Header.Set(k, v)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				return rec
			}
			preflight := map[string]string{
				"Origin":                        "http://localhost:5173",
				"Access-Control-Request-Method": "PUT",
			}

			send(http.MethodPut, "/assets", "", nil)
			send(http.MethodPut, "/assets?cors", `<CORSConfiguration><CORSRule><AllowedOrigin>http://localhost:5173</AllowedOrigin><AllowedMethod>PUT</AllowedMethod></CORSRule></CORSConfiguration>`, nil)

			for _, target := range []string{"/assets/photos/cat.png", "/assets", "/_s3/assets/cat.png"} {
				rec := send(http.MethodOptions, target, "", preflight)
				if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
					t.Errorf("OPTIONS %s: status=%d allow-origin=%q body=%s", target, rec.Code, rec.Header().Get("Access-Control-Allow-Origin"), rec.Body.String())
				}
			}

			rec := send(http.MethodPost, "/v2/apis", `{"name":"web","corsConfiguration":{"allowOrigins":["http://localhost:5173"],"allowMethods":["PUT"]}}`, nil)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create api: %d %s", rec.Code, rec.Body.String())
			}
			apiID := between(rec.Body.String(), `"apiId":"`, `"`)
			rec = send(http.MethodOptions, "/_apigateway/"+apiID+"/$default/orders", "", preflight)
			if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
				t.Errorf("API Gateway preflight: status=%d allow-origin=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
			}

			rec = send(http.MethodDelete, "/v2/apis/"+apiID+"/cors", "", nil)
			if rec.Code != http.StatusNoContent {
				t.Errorf("DeleteCorsConfiguration: status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func between(s, start, end string) string {
	_, after, _ := strings.Cut(s, start)
	value, _, _ := strings.Cut(after, end)
	return value
}
