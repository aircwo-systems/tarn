package apigatewayv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apisvc "github.com/aircwo-systems/tarn/internal/apigatewayv1"
	"github.com/aircwo-systems/tarn/internal/config"
)

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
