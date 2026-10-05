package apigatewayv1

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// corsHeaderParams is the integration response mapping SAM, Serverless and
// Terraform modules generate for a REST API CORS preflight.
var corsHeaderParams = map[string]string{
	"method.response.header.Access-Control-Allow-Origin":  "'http://localhost:5173'",
	"method.response.header.Access-Control-Allow-Methods": "'GET,POST,OPTIONS'",
	"method.response.header.Access-Control-Allow-Headers": "'content-type,authorization'",
}

func newMockTestService(t *testing.T, sqsSend SQSSendFunc) (*Service, string, string) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	svc := NewService(cfg, nil, sqsSend)
	api, err := svc.CreateAPI("web", "", nil)
	if err != nil {
		t.Fatalf("create api: %v", err)
	}
	res, err := svc.CreateResource(api.ID, api.RootResourceID, "orders")
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	return svc, api.ID, res.ID
}

func addMockMethod(t *testing.T, svc *Service, apiID, resID, method string, requestTemplates map[string]string) {
	t.Helper()
	if _, err := svc.PutMethod(apiID, resID, method, "NONE", nil); err != nil {
		t.Fatalf("put method: %v", err)
	}
	if _, err := svc.PutIntegration(apiID, resID, method, "MOCK", "", "", nil, requestTemplates); err != nil {
		t.Fatalf("put integration: %v", err)
	}
}

func invokeREST(t *testing.T, svc *Service, apiID, method string, headers http.Header) *InvokeOutput {
	t.Helper()
	if headers == nil {
		headers = http.Header{}
	}
	out, err := svc.Invoke(context.Background(), &InvokeInput{
		APIID: apiID, Stage: "dev", Method: method, Path: "/orders",
		Query: url.Values{}, Headers: headers,
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	return out
}

func deploy(t *testing.T, svc *Service, apiID string) {
	t.Helper()
	if _, err := svc.CreateDeployment(apiID, "", "dev"); err != nil {
		t.Fatalf("deploy: %v", err)
	}
}

func TestMockIntegrationAnswersCORSPreflight(t *testing.T) {
	svc, apiID, resID := newMockTestService(t, nil)
	addMockMethod(t, svc, apiID, resID, "OPTIONS", map[string]string{"application/json": `{"statusCode": 200}`})
	if _, err := svc.PutMethodResponse(apiID, resID, "OPTIONS", "200", nil, map[string]bool{
		"method.response.header.Access-Control-Allow-Origin": true,
	}); err != nil {
		t.Fatalf("put method response: %v", err)
	}
	if _, err := svc.PutIntegrationResponse(apiID, resID, "OPTIONS", "200", "", nil, corsHeaderParams); err != nil {
		t.Fatalf("put integration response: %v", err)
	}
	deploy(t, svc, apiID)

	out := invokeREST(t, svc, apiID, http.MethodOptions, http.Header{
		"Origin":                        {"http://localhost:5173"},
		"Access-Control-Request-Method": {"POST"},
	})
	if out.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body: %s", out.StatusCode, out.Body)
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":  "http://localhost:5173",
		"Access-Control-Allow-Methods": "GET,POST,OPTIONS",
		"Access-Control-Allow-Headers": "content-type,authorization",
	} {
		if got := out.Headers[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if len(out.Body) != 0 {
		t.Errorf("body = %q, want empty", out.Body)
	}
}

func TestMockIntegrationSelectsResponseByStatus(t *testing.T) {
	svc, apiID, resID := newMockTestService(t, nil)
	addMockMethod(t, svc, apiID, resID, "GET", map[string]string{"application/json": `{"statusCode": 404}`})
	if _, err := svc.PutIntegrationResponse(apiID, resID, "GET", "200", "", map[string]string{"application/json": `{"ok":true}`}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutIntegrationResponse(apiID, resID, "GET", "404", "4\\d{2}", map[string]string{"application/json": `{"message":"missing"}`}, nil); err != nil {
		t.Fatal(err)
	}
	deploy(t, svc, apiID)

	out := invokeREST(t, svc, apiID, http.MethodGet, nil)
	if out.StatusCode != http.StatusNotFound || string(out.Body) != `{"message":"missing"}` {
		t.Fatalf("got %d %q, want 404 with the 404 template", out.StatusCode, out.Body)
	}
	if out.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q", out.Headers["Content-Type"])
	}
}

func TestMockIntegrationWithoutResponseIsServerError(t *testing.T) {
	svc, apiID, resID := newMockTestService(t, nil)
	addMockMethod(t, svc, apiID, resID, "OPTIONS", nil)
	deploy(t, svc, apiID)

	out := invokeREST(t, svc, apiID, http.MethodOptions, nil)
	if out.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", out.StatusCode)
	}
}

func TestAWSIntegrationAppliesIntegrationResponseHeaders(t *testing.T) {
	send := func(string, string, map[string]*types.MessageAttribute, string, string) (string, string, error) {
		return "m-1", "md5", nil
	}
	svc, apiID, resID := newMockTestService(t, send)
	if _, err := svc.PutMethod(apiID, resID, "POST", "NONE", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutIntegration(apiID, resID, "POST", "AWS", "POST",
		"arn:aws:apigateway:us-east-1:sqs:path/000000000000/orders", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutIntegrationResponse(apiID, resID, "POST", "200", "", nil, map[string]string{
		"method.response.header.Access-Control-Allow-Origin": "'*'",
		"method.response.header.X-From-Integration":          "integration.response.header.X-Amzn-RequestId",
	}); err != nil {
		t.Fatal(err)
	}
	deploy(t, svc, apiID)

	out := invokeREST(t, svc, apiID, http.MethodPost, nil)
	if out.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body: %s", out.StatusCode, out.Body)
	}
	if got := out.Headers["Access-Control-Allow-Origin"]; got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	if _, ok := out.Headers["X-From-Integration"]; ok {
		t.Error("non-static mapping should be skipped")
	}
}
