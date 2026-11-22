package apigatewayv1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	apisvc "github.com/aircwo-systems/tarn/internal/apigatewayv1"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

type testLambda struct{ output types.InvokeOutput }

func (l *testLambda) Invoke(context.Context, *types.InvokeInput) (*types.InvokeOutput, error) {
	return &l.output, nil
}

func TestRESTLambdaProxyResponsePreservesMultiValueHeaders(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	lambda := &testLambda{output: types.InvokeOutput{StatusCode: http.StatusOK, Payload: []byte(
		`{"statusCode":201,"headers":{"set-cookie":"session=abc","x-result":"ok"},"multiValueHeaders":{"Set-Cookie":["session=abc","theme=dark"],"x-result":["saved"]},"body":"created"}`,
	)}}
	svc := apisvc.NewService(cfg, lambda, nil)
	api, err := svc.CreateAPI("sessions", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutMethod(api.ID, api.RootResourceID, http.MethodPost, "NONE", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutIntegration(api.ID, api.RootResourceID, http.MethodPost, "AWS_PROXY", http.MethodPost,
		"arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/arn:aws:lambda:us-east-1:000000000000:function:sessions/invocations", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateDeployment(api.ID, "", "dev"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /_aws/execute-api/{apiId}/{stage}", NewHandler(svc).Invoke)
	invoke := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/_aws/execute-api/"+api.ID+"/dev", nil))
		return response
	}

	response := invoke()
	if response.Code != http.StatusCreated || response.Body.String() != "created" {
		t.Fatalf("got %d %q, want 201 created", response.Code, response.Body)
	}
	if got := response.Result().Header.Values("Set-Cookie"); !slices.Equal(got, []string{"session=abc", "theme=dark"}) {
		t.Errorf("Set-Cookie = %q, want both cookies once", got)
	}
	if got := response.Result().Header.Values("X-Result"); !slices.Equal(got, []string{"saved", "ok"}) {
		t.Errorf("X-Result = %q, want both distinct values", got)
	}

	lambda.output = types.InvokeOutput{StatusCode: http.StatusOK, FunctionError: "Unhandled", Payload: []byte(`{"errorType":"Runtime.ExitError"}`)}
	response = invoke()
	if response.Code != http.StatusBadGateway || response.Body.String() != `{"message":"Internal server error"}` {
		t.Fatalf("runtime exit got %d %q, want generic 502 response", response.Code, response.Body)
	}
}

func TestRestAPIReportsAvailableToTerraform(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	h := NewHandler(apisvc.NewService(cfg, nil, nil))

	createReq := httptest.NewRequest(http.MethodPost, "/restapis", bytes.NewBufferString(`{"name":"terraform-api"}`))
	createRec := httptest.NewRecorder()
	h.CreateRestAPI(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("CreateRestAPI status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ID        string `json:"id"`
		APIStatus string `json:"apiStatus"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.APIStatus != "AVAILABLE" {
		t.Fatalf("CreateRestAPI response = %+v, want id and AVAILABLE", created)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/restapis/"+created.ID, nil)
	getReq.SetPathValue("restApiId", created.ID)
	getRec := httptest.NewRecorder()
	h.GetRestAPI(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GetRestAPI status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var got struct {
		APIStatus string `json:"apiStatus"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.APIStatus != "AVAILABLE" {
		t.Fatalf("GetRestAPI apiStatus = %q, want AVAILABLE", got.APIStatus)
	}
}

func TestResponseParametersRoundTrip(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	svc := apisvc.NewService(cfg, nil, nil)
	h := NewHandler(svc)

	api, err := svc.CreateAPI("web", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutMethod(api.ID, api.RootResourceID, "OPTIONS", "NONE", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutIntegration(api.ID, api.RootResourceID, "OPTIONS", "MOCK", "", "", nil, nil); err != nil {
		t.Fatal(err)
	}

	call := func(handler http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/", bytes.NewBufferString(body))
		req.SetPathValue("restApiId", api.ID)
		req.SetPathValue("resourceId", api.RootResourceID)
		req.SetPathValue("httpMethod", "OPTIONS")
		req.SetPathValue("statusCode", "200")
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	call(h.PutMethodResponse, http.MethodPut, `{"responseParameters":{"method.response.header.Access-Control-Allow-Origin":true}}`)
	call(h.PutIntegrationResponse, http.MethodPut, `{"responseParameters":{"method.response.header.Access-Control-Allow-Origin":"'*'"}}`)

	var mr struct {
		ResponseParameters map[string]bool `json:"responseParameters"`
	}
	if err := json.Unmarshal(call(h.GetMethodResponse, http.MethodGet, "").Body.Bytes(), &mr); err != nil {
		t.Fatal(err)
	}
	if !mr.ResponseParameters["method.response.header.Access-Control-Allow-Origin"] {
		t.Errorf("method response parameters = %v", mr.ResponseParameters)
	}

	var ir struct {
		ResponseParameters map[string]string `json:"responseParameters"`
	}
	if err := json.Unmarshal(call(h.GetIntegrationResponse, http.MethodGet, "").Body.Bytes(), &ir); err != nil {
		t.Fatal(err)
	}
	if got := ir.ResponseParameters["method.response.header.Access-Control-Allow-Origin"]; got != "'*'" {
		t.Errorf("integration response parameter = %q, want '*' with quotes", got)
	}
}
