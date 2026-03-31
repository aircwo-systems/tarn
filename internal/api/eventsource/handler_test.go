package eventsource

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	eventsourcesvc "github.com/aircwo-systems/tarn/internal/eventsource"
)

const (
	eventSourceMappingsPath = "/2015-03-31/event-source-mappings"
	eventSourceMappingUUID  = eventSourceMappingsPath + "/"
	statusCodeMismatch      = "status = %d, want %d"
)

type mappingResponse struct {
	UUID         string  `json:"UUID"`
	FunctionName string  `json:"FunctionName"`
	BatchSize    int     `json:"BatchSize"`
	LastModified float64 `json:"LastModified"`
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := eventsourcesvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	svc := eventsourcesvc.NewService(cfg, store, nil, nil, nil)
	return NewHandler(svc)
}

func TestCreateAndGetMapping(t *testing.T) {
	h := newTestHandler(t)

	body := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:orders","FunctionName":"process-order","BatchSize":5,"Enabled":false}`
	req := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var mapping mappingResponse
	if err := json.NewDecoder(rec.Body).Decode(&mapping); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if mapping.FunctionName != "process-order" {
		t.Fatalf("functionName = %q, want %q", mapping.FunctionName, "process-order")
	}
	if mapping.BatchSize != 5 {
		t.Fatalf("batchSize = %d, want 5", mapping.BatchSize)
	}
	if mapping.UUID == "" {
		t.Fatal("expected non-empty UUID")
	}
	if mapping.LastModified == 0 {
		t.Fatal("expected numeric LastModified")
	}

	// GET
	req2 := httptest.NewRequest(http.MethodGet, eventSourceMappingUUID+mapping.UUID, nil)
	req2.SetPathValue("uuid", mapping.UUID)
	rec2 := httptest.NewRecorder()

	h.Get(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", rec2.Code, http.StatusOK)
	}
}

func TestCreateDuplicateMappingIsIdempotent(t *testing.T) {
	h := newTestHandler(t)

	body1 := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:orders","FunctionName":"process-order","BatchSize":5,"Enabled":true}`
	req1 := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body1))
	rec1 := httptest.NewRecorder()
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want %d, body: %s", rec1.Code, http.StatusCreated, rec1.Body.String())
	}

	body2 := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:orders","FunctionName":"process-order","BatchSize":3,"Enabled":true}`
	req2 := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body2))
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("second create status = %d, want %d, body: %s", rec2.Code, http.StatusCreated, rec2.Body.String())
	}

	var second mappingResponse
	if err := json.NewDecoder(rec2.Body).Decode(&second); err != nil {
		t.Fatalf("decode second create: %v", err)
	}
	if second.BatchSize != 3 {
		t.Fatalf("second create batchSize = %d, want 3", second.BatchSize)
	}

	listReq := httptest.NewRequest(http.MethodGet, eventSourceMappingsPath, nil)
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRec.Code, http.StatusOK)
	}
	var listBody struct {
		EventSourceMappings []mappingResponse `json:"EventSourceMappings"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.EventSourceMappings) != 1 {
		t.Fatalf("mapping count = %d, want 1", len(listBody.EventSourceMappings))
	}
}

func TestCreateMissingEventSourceArn(t *testing.T) {
	h := newTestHandler(t)

	body := `{"FunctionName":"fn"}`
	req := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf(statusCodeMismatch, rec.Code, http.StatusBadRequest)
	}
}

func TestCreateMissingFunctionName(t *testing.T) {
	h := newTestHandler(t)

	body := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:q"}`
	req := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf(statusCodeMismatch, rec.Code, http.StatusBadRequest)
	}
}

func TestListMappings(t *testing.T) {
	h := newTestHandler(t)

	body := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:q1","FunctionName":"fn1","Enabled":false}`
	req := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	// List all
	req2 := httptest.NewRequest(http.MethodGet, eventSourceMappingsPath, nil)
	rec2 := httptest.NewRecorder()
	h.List(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf(statusCodeMismatch, rec2.Code, http.StatusOK)
	}

	var result struct {
		EventSourceMappings []mappingResponse `json:"EventSourceMappings"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.EventSourceMappings) != 1 {
		t.Fatalf("mappings len = %d, want 1", len(result.EventSourceMappings))
	}
}

func TestDeleteMapping(t *testing.T) {
	h := newTestHandler(t)

	body := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:q1","FunctionName":"fn1","Enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/2015-03-31/event-source-mappings", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	var mapping mappingResponse
	json.NewDecoder(rec.Body).Decode(&mapping)

	// Delete
	delReq := httptest.NewRequest(http.MethodDelete, eventSourceMappingUUID+mapping.UUID, nil)
	delReq.SetPathValue("uuid", mapping.UUID)
	delRec := httptest.NewRecorder()
	h.Delete(delRec, delReq)

	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", delRec.Code, http.StatusNoContent)
	}

	// Verify gone
	getReq := httptest.NewRequest(http.MethodGet, eventSourceMappingUUID+mapping.UUID, nil)
	getReq.SetPathValue("uuid", mapping.UUID)
	getRec := httptest.NewRecorder()
	h.Get(getRec, getReq)

	if getRec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want %d", getRec.Code, http.StatusNotFound)
	}
}

func TestGetNotFound(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/2015-03-31/event-source-mappings/nonexistent", nil)
	req.SetPathValue("uuid", "nonexistent")
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	var body struct {
		Type string `json:"__type"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Type != "ResourceNotFoundException" {
		t.Fatalf("__type = %q, want %q", body.Type, "ResourceNotFoundException")
	}
}

func TestUpdateMapping(t *testing.T) {
	h := newTestHandler(t)

	body := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:q1","FunctionName":"fn1","BatchSize":5,"Enabled":false}`
	req := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	var mapping mappingResponse
	json.NewDecoder(rec.Body).Decode(&mapping)

	// Update batch size
	updateBody := `{"BatchSize":3}`
	updateReq := httptest.NewRequest(http.MethodPut, eventSourceMappingUUID+mapping.UUID, bytes.NewBufferString(updateBody))
	updateReq.SetPathValue("uuid", mapping.UUID)
	updateRec := httptest.NewRecorder()
	h.Update(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d", updateRec.Code, http.StatusOK)
	}

	var updated mappingResponse
	json.NewDecoder(updateRec.Body).Decode(&updated)
	if updated.BatchSize != 3 {
		t.Fatalf("batchSize = %d, want 3", updated.BatchSize)
	}
}

func TestListFilterByFunctionName(t *testing.T) {
	h := newTestHandler(t)

	body1 := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:q1","FunctionName":"fn-a","Enabled":false}`
	req1 := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body1))
	h.Create(httptest.NewRecorder(), req1)

	body2 := `{"EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:q2","FunctionName":"fn-b","Enabled":false}`
	req2 := httptest.NewRequest(http.MethodPost, eventSourceMappingsPath, bytes.NewBufferString(body2))
	h.Create(httptest.NewRecorder(), req2)

	// Filter by FunctionName
	listReq := httptest.NewRequest(http.MethodGet, eventSourceMappingsPath+"?FunctionName=fn-a", nil)
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)

	var result struct {
		EventSourceMappings []mappingResponse `json:"EventSourceMappings"`
	}
	json.NewDecoder(listRec.Body).Decode(&result)

	if len(result.EventSourceMappings) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(result.EventSourceMappings))
	}
	if result.EventSourceMappings[0].FunctionName != "fn-a" {
		t.Fatalf("functionName = %q, want %q", result.EventSourceMappings[0].FunctionName, "fn-a")
	}
}
