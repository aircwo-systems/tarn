package lambda

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openstack-project/openstack/internal/config"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/pkg/types"
)

func newLambdaHandler(t *testing.T) *Handler {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := lambdasvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	svc := lambdasvc.NewService(cfg, store, nil, nil, nil)
	return NewHandler(svc, nil)
}

// ensure the configuration endpoint always returns a LastUpdateStatus field,
// and that the HTTP wrapper (with "Configuration" envelope) also exposes it.
func TestGetFunctionConfigurationIncludesLastUpdateStatus(t *testing.T) {
	h := newLambdaHandler(t)

	// create a function via service directly
	_, err := h.svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "foo",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	// call /configuration
	req := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/foo/configuration", nil)
	req.SetPathValue("name", "foo")
	rec := httptest.NewRecorder()
	h.GetFunctionConfiguration(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cfg types.FunctionConfig
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if cfg.LastUpdateStatus == "" {
		t.Fatalf("expected non-empty LastUpdateStatus")
	}

	// call the wrapped GET and verify envelope contains a LastUpdateStatus
	req2 := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/foo", nil)
	req2.SetPathValue("name", "foo")
	rec2 := httptest.NewRecorder()
	h.GetFunction(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("wrapper status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var resp struct {
		Configuration types.FunctionConfig `json:"Configuration"`
		Code          struct {
			RepositoryType string `json:"RepositoryType"`
		} `json:"Code"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode wrapper: %v", err)
	}
	if resp.Configuration.LastUpdateStatus == "" {
		t.Fatalf("expected envelope LastUpdateStatus")
	}
	if resp.Code.RepositoryType == "" {
		t.Fatalf("expected non-empty Code.RepositoryType")
	}
}

// the handler should surface the transition from Pending -> Active on
// successive fetches, just like the service.
func TestFunctionStateTransitionsViaHandler(t *testing.T) {
	h := newLambdaHandler(t)

	_, err := h.svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "bar",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	// first request should report Pending
	req := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/bar/configuration", nil)
	req.SetPathValue("name", "bar")
	rec := httptest.NewRecorder()
	h.GetFunctionConfiguration(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first status=%d", rec.Code)
	}
	var cfg types.FunctionConfig
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if cfg.State != types.FunctionStatePending {
		t.Fatalf("expected pending state first, got %s", cfg.State)
	}

	// second call should now return active
	rec2 := httptest.NewRecorder()
	h.GetFunctionConfiguration(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second status=%d", rec2.Code)
	}
	var cfg2 types.FunctionConfig
	if err := json.NewDecoder(rec2.Body).Decode(&cfg2); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if cfg2.State != types.FunctionStateActive {
		t.Fatalf("expected active on second fetch, got %s", cfg2.State)
	}
}

func TestGetFunctionWrapperIncludesLastUpdateStatus(t *testing.T) {
	h := newLambdaHandler(t)

	_, err := h.svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "baz",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/baz", nil)
	req.SetPathValue("name", "baz")
	// First call triggers Pending → Active transition (saved asynchronously).
	h.GetFunction(httptest.NewRecorder(), req)
	// Second call should observe Active state.
	rec := httptest.NewRecorder()
	h.GetFunction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Configuration types.FunctionConfig `json:"Configuration"`
		Code          struct {
			RepositoryType string `json:"RepositoryType"`
		} `json:"Code"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Configuration.LastUpdateStatus != types.LastUpdateStatusSuccessful {
		t.Fatalf("expected LastUpdateStatus Successful, got %q", resp.Configuration.LastUpdateStatus)
	}
	if resp.Code.RepositoryType == "" {
		t.Fatalf("expected non-empty Code.RepositoryType")
	}
}

func TestListVersionsByFunction(t *testing.T) {
	h := newLambdaHandler(t)

	_, err := h.svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "ver",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/ver/versions", nil)
	req.SetPathValue("name", "ver")
	rec := httptest.NewRecorder()
	h.ListVersionsByFunction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Versions []types.FunctionConfig `json:"Versions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(resp.Versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(resp.Versions))
	}
	if resp.Versions[0].FunctionName != "ver" {
		t.Fatalf("expected function name ver, got %q", resp.Versions[0].FunctionName)
	}
	if resp.Versions[0].Version == "" {
		t.Fatalf("expected non-empty version")
	}
}

func TestPermissionLifecycle(t *testing.T) {
	h := newLambdaHandler(t)
	_, err := h.svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "perm-fn",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	addReq := httptest.NewRequest(http.MethodPost, "/2015-03-31/functions/perm-fn/policy", strings.NewReader(`{"StatementId":"AllowEventBridgeInvoke","Action":"lambda:InvokeFunction","Principal":"events.amazonaws.com","SourceArn":"arn:aws:events:us-east-1:000000000000:rule/demo"}`))
	addReq.SetPathValue("name", "perm-fn")
	addRec := httptest.NewRecorder()
	h.AddPermission(addRec, addReq)
	if addRec.Code != http.StatusOK {
		t.Fatalf("AddPermission status=%d body=%s", addRec.Code, addRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/perm-fn/policy", nil)
	getReq.SetPathValue("name", "perm-fn")
	getRec := httptest.NewRecorder()
	h.GetPolicy(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GetPolicy status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var getBody struct {
		Policy string `json:"Policy"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&getBody); err != nil {
		t.Fatalf("decode policy body: %v", err)
	}
	if !strings.Contains(getBody.Policy, "AllowEventBridgeInvoke") {
		t.Fatalf("expected statement in policy, got %s", getBody.Policy)
	}

	removeReq := httptest.NewRequest(http.MethodDelete, "/2015-03-31/functions/perm-fn/policy/AllowEventBridgeInvoke", nil)
	removeReq.SetPathValue("name", "perm-fn")
	removeReq.SetPathValue("statementId", "AllowEventBridgeInvoke")
	removeRec := httptest.NewRecorder()
	h.RemovePermission(removeRec, removeReq)
	if removeRec.Code != http.StatusNoContent {
		t.Fatalf("RemovePermission status=%d body=%s", removeRec.Code, removeRec.Body.String())
	}

	getAfterRec := httptest.NewRecorder()
	h.GetPolicy(getAfterRec, getReq)
	if getAfterRec.Code != http.StatusNotFound {
		t.Fatalf("GetPolicy after remove status=%d body=%s", getAfterRec.Code, getAfterRec.Body.String())
	}
}

func TestCreateFunctionWithLayersReturnsAWSLayerObjects(t *testing.T) {
	h := newLambdaHandler(t)

	layerArn := "arn:aws:lambda:us-east-1:177933569100:layer:AWS-Parameters-and-Secrets-Lambda-Extension:12"
	body := `{
		"FunctionName":"layered-fn",
		"Runtime":"nodejs20.x",
		"Handler":"index.handler",
		"Role":"arn:aws:iam::000000000000:role/test",
		"Layers":["` + layerArn + `"]
	}`

	req := httptest.NewRequest(http.MethodPost, "/2015-03-31/functions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateFunction(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		FunctionName string `json:"FunctionName"`
		Layers       []struct {
			Arn      string `json:"Arn"`
			CodeSize int64  `json:"CodeSize"`
		} `json:"Layers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.FunctionName != "layered-fn" {
		t.Fatalf("unexpected function name %q", resp.FunctionName)
	}
	if len(resp.Layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(resp.Layers))
	}
	if resp.Layers[0].Arn != layerArn {
		t.Fatalf("unexpected layer arn %q", resp.Layers[0].Arn)
	}
}
