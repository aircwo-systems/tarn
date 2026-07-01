package stepfunctions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	stepfunctionssvc "github.com/aircwo-systems/tarn/internal/stepfunctions"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := &config.Config{Region: "us-east-1", AccountID: "000000000000", DataDir: t.TempDir()}
	svc := stepfunctionssvc.NewService(cfg, stepfunctionssvc.NewStore(cfg), nil)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	return NewHandler(svc)
}

func do(t *testing.T, h *Handler, action string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.Header.Set("X-Amz-Target", "AWSStepFunctions."+action)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestCreateAndDescribe(t *testing.T) {
	h := newTestHandler(t)
	def := `{"StartAt":"P","States":{"P":{"Type":"Pass","End":true}}}`

	rec, out := do(t, h, "CreateStateMachine", map[string]any{"name": "demo", "definition": def})
	if rec.Code != http.StatusOK {
		t.Fatalf("create code %d: %v", rec.Code, out)
	}
	arn, _ := out["stateMachineArn"].(string)
	if arn == "" {
		t.Fatalf("missing stateMachineArn: %v", out)
	}

	rec, out = do(t, h, "DescribeStateMachine", map[string]any{"stateMachineArn": arn})
	if rec.Code != http.StatusOK {
		t.Fatalf("describe code %d: %v", rec.Code, out)
	}
	if out["name"] != "demo" || out["status"] != "ACTIVE" || out["type"] != "STANDARD" {
		t.Fatalf("unexpected describe: %v", out)
	}
}

func TestInvalidDefinition(t *testing.T) {
	h := newTestHandler(t)
	rec, out := do(t, h, "CreateStateMachine", map[string]any{"name": "bad", "definition": `{"States":{}}`})
	if rec.Code != http.StatusBadRequest || out["__type"] != "InvalidDefinition" {
		t.Fatalf("expected 400 InvalidDefinition, got %d %v", rec.Code, out)
	}
}

func TestValidateStateMachineDefinition(t *testing.T) {
	h := newTestHandler(t)
	def := `{"StartAt":"P","States":{"P":{"Type":"Pass","End":true}}}`

	// AWS returns HTTP 200 with result "OK" for a valid definition (the verdict
	// is in the body, not the status).
	rec, out := do(t, h, "ValidateStateMachineDefinition", map[string]any{"definition": def})
	if rec.Code != http.StatusOK {
		t.Fatalf("validate code %d: %v", rec.Code, out)
	}
	if out["result"] != "OK" {
		t.Fatalf("expected result OK, got %v", out)
	}
}

func TestValidateStateMachineDefinitionInvalid(t *testing.T) {
	h := newTestHandler(t)
	// An invalid definition is reported as HTTP 200 with result "FAIL" plus
	// diagnostics — not as an error status — matching the AWS API the Terraform
	// provider calls during plan.
	rec, out := do(t, h, "ValidateStateMachineDefinition", map[string]any{"definition": `{"States":{}}`})
	if rec.Code != http.StatusOK {
		t.Fatalf("validate (invalid) code %d: %v", rec.Code, out)
	}
	if out["result"] != "FAIL" {
		t.Fatalf("expected result FAIL, got %v", out)
	}
	if _, ok := out["diagnostics"].([]any); !ok {
		t.Fatalf("expected diagnostics array, got %v", out["diagnostics"])
	}
}

func TestDescribeMissing(t *testing.T) {
	h := newTestHandler(t)
	rec, out := do(t, h, "DescribeStateMachine", map[string]any{
		"stateMachineArn": "arn:aws:states:us-east-1:000000000000:stateMachine:nope",
	})
	if rec.Code != http.StatusBadRequest || out["__type"] != "StateMachineDoesNotExist" {
		t.Fatalf("expected StateMachineDoesNotExist, got %d %v", rec.Code, out)
	}
}

func TestUnknownActionStub(t *testing.T) {
	h := newTestHandler(t)
	rec, out := do(t, h, "SomeFutureAction", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("stub code %d", rec.Code)
	}
	if len(out) != 0 {
		t.Fatalf("stub body should be empty, got %v", out)
	}
}
