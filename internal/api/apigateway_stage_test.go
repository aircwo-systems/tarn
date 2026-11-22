package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apigatewayhandler "github.com/aircwo-systems/tarn/internal/api/apigateway"
	"github.com/aircwo-systems/tarn/internal/apigateway"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

type httpAPITestLambda func(context.Context, *types.InvokeInput) (*types.InvokeOutput, error)

func (f httpAPITestLambda) Invoke(ctx context.Context, in *types.InvokeInput) (*types.InvokeOutput, error) {
	return f(ctx, in)
}

func (httpAPITestLambda) GetFunction(name string) (*types.FunctionConfig, error) {
	return &types.FunctionConfig{FunctionName: name}, nil
}

func newHTTPAPITestHandler(t *testing.T, lambda httpAPITestLambda) (http.Handler, *apigateway.Service) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	svc := apigateway.NewService(cfg, lambda, nil)
	bundle := NewAccountBundle(&HandlerSet{APIGateway: apigatewayhandler.NewHandler(svc)}, nil)
	registry := NewHandlerRegistry(func(string) (*AccountBundle, error) { return bundle, nil })
	mux := http.NewServeMux()
	NewServer(cfg, registry, nil, nil).registerRoutes(mux)
	return mux, svc
}

func TestHTTPAPIStageLifecycle(t *testing.T) {
	calls := 0
	h, svc := newHTTPAPITestHandler(t, func(_ context.Context, in *types.InvokeInput) (*types.InvokeOutput, error) {
		calls++
		var event struct {
			RawPath        string
			RequestContext struct{ Stage string }
		}
		if err := json.Unmarshal(in.Payload, &event); err != nil || event.RawPath != "/orders" || event.RequestContext.Stage != "poc" {
			t.Errorf("unexpected Lambda event %s, error %v", in.Payload, err)
		}
		return &types.InvokeOutput{StatusCode: 200, Payload: []byte(`{"statusCode":200,"body":"reached orders"}`)}, nil
	})
	api, err := svc.CreateAPI("orders", "", "HTTP", "", nil, &types.APIGatewayCORS{
		AllowOrigins: []string{"https://staff.example"},
		AllowMethods: []string{"GET"},
	})
	if err != nil {
		t.Fatal(err)
	}
	integration, err := svc.CreateIntegration(api.APIID, apigateway.IntegrationCreateInput{
		IntegrationURI: "arn:aws:lambda:us-east-1:000000000000:function:orders",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRoute(api.APIID, apigateway.RouteCreateInput{
		RouteKey: "GET /orders",
		Target:   "integrations/" + integration.IntegrationID,
	}); err != nil {
		t.Fatal(err)
	}

	stages := "/v2/apis/" + api.APIID + "/stages"
	invoke := "/_apigateway/" + api.APIID + "/poc/orders"
	request := func(method, path, body string, status int, errType string) string {
		t.Helper()
		response := compatibilityRequest(h, method, path, body, "")
		if response.Code != status {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, response.Code, status, response.Body)
		}
		if errType != "" {
			var fault struct {
				Type string `json:"__type"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &fault); err != nil || fault.Type != errType || response.Header().Get("X-Amzn-ErrorType") != errType {
				t.Fatalf("%s %s returned %s, want %s", method, path, response.Body, errType)
			}
		}
		return response.Body.String()
	}
	preflight := func(want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodOptions, invoke, nil)
		req.Header.Set("Origin", "https://staff.example")
		req.Header.Set("Access-Control-Request-Method", "GET")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("preflight returned %d, want %d: %s", response.Code, want, response.Body)
		}
	}

	request("GET", invoke, "", 404, "NotFoundException")
	stageJSON := request("POST", stages, `{"stageName":"poc","description":"POC stage","autoDeploy":true}`, 201, "")
	var stage types.APIGatewayStage
	if err := json.Unmarshal([]byte(stageJSON), &stage); err != nil || stage.StageName != "poc" ||
		stage.Description != "POC stage" || !stage.AutoDeploy || !strings.HasSuffix(stage.InvokeURL, "/"+api.APIID+"/poc") ||
		stage.CreatedDate.IsZero() || stage.LastUpdatedDate.IsZero() {
		t.Fatalf("unexpected stage response %s, error %v", stageJSON, err)
	}
	request("POST", stages, `{"stageName":"poc"}`, 409, "ConflictException")
	request("POST", stages, `{"stageName":"$default"}`, 409, "ConflictException")
	request("POST", stages, `{"stageName":"bad/name"}`, 400, "BadRequestException")
	request("POST", "/v2/apis/unknown/stages", `{"stageName":"poc"}`, 404, "NotFoundException")
	request("GET", stages+"/poc", "", 200, "")
	preflight(http.StatusNoContent)
	if got := request("GET", invoke, "", 200, ""); got != "reached orders" {
		t.Fatalf("invoke body = %q", got)
	}
	if body := request("DELETE", stages+"/poc", "", 204, ""); body != "" {
		t.Fatalf("delete returned a body: %q", body)
	}
	request("GET", invoke, "", 404, "NotFoundException")
	preflight(http.StatusNotFound)
	request("DELETE", stages+"/poc", "", 404, "NotFoundException")
	request("DELETE", "/v2/apis/unknown/stages/poc", "", 404, "NotFoundException")
	request("GET", stages+"/$default", "", 200, "")
	if calls != 1 {
		t.Fatalf("Lambda invokes = %d, want 1", calls)
	}
}
