package apigateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	apisvc "github.com/openstack-project/openstack/internal/apigateway"
	"github.com/openstack-project/openstack/internal/config"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/pkg/types"
)

func TestAPIManagementLifecycle(t *testing.T) {
	h := newTestHandler(t)

	createBody := []byte(`{"Name":"orders-http-api","ProtocolType":"HTTP","Tags":{"feature":"r10","surface":"public"}}`)
	createReq := httptest.NewRequest(http.MethodPost, "/v2/apis", bytes.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.CreateAPI(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("CreateAPI status=%d body=%s", createRec.Code, createRec.Body.String())
	}

	var created struct {
		APIID string            `json:"ApiId"`
		Name  string            `json:"Name"`
		Tags  map[string]string `json:"Tags"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created api: %v", err)
	}
	if created.Name != "orders-http-api" {
		t.Fatalf("name=%q want %q", created.Name, "orders-http-api")
	}
	if created.Tags["feature"] != "r10" {
		t.Fatalf("tags=%v want feature=r10", created.Tags)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v2/apis", nil)
	listRec := httptest.NewRecorder()
	h.ListAPIs(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("ListAPIs status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	updateBody := []byte(`{"Description":"updated"}`)
	updateReq := httptest.NewRequest(http.MethodPatch, "/v2/apis/"+created.APIID, bytes.NewReader(updateBody))
	updateReq.SetPathValue("apiId", created.APIID)
	updateRec := httptest.NewRecorder()
	h.UpdateAPI(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("UpdateAPI status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v2/apis/"+created.APIID, nil)
	deleteReq.SetPathValue("apiId", created.APIID)
	deleteRec := httptest.NewRecorder()
	h.DeleteAPI(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DeleteAPI status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestIntegrationRouteAndStageEndpoints(t *testing.T) {
	h := newTestHandler(t)

	api, err := h.svc.CreateAPI("orders-http-api", "", "HTTP", "", nil)
	if err != nil {
		t.Fatalf("create api: %v", err)
	}

	lambdaArn := "arn:aws:lambda:us-east-1:000000000000:function:orders-handler"
	integrationCreate := map[string]any{
		"IntegrationType": "AWS_PROXY",
		"IntegrationUri":  lambdaArn,
	}
	integrationBody, _ := json.Marshal(integrationCreate)
	integrationReq := httptest.NewRequest(http.MethodPost, "/v2/apis/"+api.APIID+"/integrations", bytes.NewReader(integrationBody))
	integrationReq.SetPathValue("apiId", api.APIID)
	integrationRec := httptest.NewRecorder()
	h.CreateIntegration(integrationRec, integrationReq)
	if integrationRec.Code != http.StatusCreated {
		t.Fatalf("CreateIntegration status=%d body=%s", integrationRec.Code, integrationRec.Body.String())
	}

	var integration struct {
		IntegrationID string `json:"IntegrationId"`
	}
	if err := json.NewDecoder(integrationRec.Body).Decode(&integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	routeCreate := map[string]any{
		"RouteKey": "GET /orders/{id}",
		"Target":   "integrations/" + integration.IntegrationID,
	}
	routeBody, _ := json.Marshal(routeCreate)
	routeReq := httptest.NewRequest(http.MethodPost, "/v2/apis/"+api.APIID+"/routes", bytes.NewReader(routeBody))
	routeReq.SetPathValue("apiId", api.APIID)
	routeRec := httptest.NewRecorder()
	h.CreateRoute(routeRec, routeReq)
	if routeRec.Code != http.StatusCreated {
		t.Fatalf("CreateRoute status=%d body=%s", routeRec.Code, routeRec.Body.String())
	}

	stagesReq := httptest.NewRequest(http.MethodGet, "/v2/apis/"+api.APIID+"/stages", nil)
	stagesReq.SetPathValue("apiId", api.APIID)
	stagesRec := httptest.NewRecorder()
	h.ListStages(stagesRec, stagesReq)
	if stagesRec.Code != http.StatusOK {
		t.Fatalf("ListStages status=%d body=%s", stagesRec.Code, stagesRec.Body.String())
	}

	stagePatch := []byte(`{"Description":"default stage"}`)
	stagePatchReq := httptest.NewRequest(http.MethodPatch, "/v2/apis/"+api.APIID+"/stages/$default", bytes.NewReader(stagePatch))
	stagePatchReq.SetPathValue("apiId", api.APIID)
	stagePatchReq.SetPathValue("stageName", "$default")
	stagePatchRec := httptest.NewRecorder()
	h.UpdateStage(stagePatchRec, stagePatchReq)
	if stagePatchRec.Code != http.StatusOK {
		t.Fatalf("UpdateStage status=%d body=%s", stagePatchRec.Code, stagePatchRec.Body.String())
	}
}

func TestInvokePathHandlingWithoutRoute(t *testing.T) {
	h := newTestHandler(t)

	api, err := h.svc.CreateAPI("orders-http-api", "", "HTTP", "", nil)
	if err != nil {
		t.Fatalf("create api: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_apigateway/"+api.APIID+"/$default/orders/123", nil)
	req.SetPathValue("apiId", api.APIID)
	req.SetPathValue("stage", "$default")
	req.SetPathValue("proxy", "orders/123")
	rec := httptest.NewRecorder()

	h.Invoke(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRejectUnsupportedProtocolAndIntegrationType(t *testing.T) {
	h := newTestHandler(t)

	badProtocolReq := httptest.NewRequest(http.MethodPost, "/v2/apis", bytes.NewBufferString(`{"Name":"x","ProtocolType":"WEBSOCKET"}`))
	badProtocolRec := httptest.NewRecorder()
	h.CreateAPI(badProtocolRec, badProtocolReq)
	if badProtocolRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for protocol, got %d", badProtocolRec.Code)
	}

	api, err := h.svc.CreateAPI("orders", "", "HTTP", "", nil)
	if err != nil {
		t.Fatalf("create api: %v", err)
	}
	badIntegrationReq := httptest.NewRequest(http.MethodPost, "/v2/apis/"+api.APIID+"/integrations", bytes.NewBufferString(`{"IntegrationType":"HTTP_PROXY","IntegrationUri":"https://example.com"}`))
	badIntegrationReq.SetPathValue("apiId", api.APIID)
	badIntegrationRec := httptest.NewRecorder()
	h.CreateIntegration(badIntegrationRec, badIntegrationReq)
	if badIntegrationRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for integration type, got %d", badIntegrationRec.Code)
	}
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	cfg := config.Default()
	cfg.Host = "127.0.0.1"
	cfg.Port = 4566
	cfg.DataDir = t.TempDir()

	store := lambdasvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	lambdaSvc := lambdasvc.NewService(cfg, store, nil, nil, nil)
	if err := store.SaveFunction(&types.FunctionConfig{
		FunctionName: "orders-handler",
		FunctionArn:  fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", cfg.Region, cfg.AccountID, "orders-handler"),
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/lambda-role",
		State:        types.FunctionStateActive,
			LastUpdateStatus: types.LastUpdateStatusSuccessful,
