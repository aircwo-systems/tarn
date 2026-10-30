package secrets

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	secretssvc "github.com/aircwo-systems/tarn/internal/secrets"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	svc := secretssvc.NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init secrets: %v", err)
	}
	return NewHandler(cfg, svc)
}

func TestGetResourcePolicyNoPolicy(t *testing.T) {
	h := newTestHandler(t)

	createReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"Name":"test-secret","SecretString":"value"}`))
	createReq.Header.Set("X-Amz-Target", "secretsmanager.CreateSecret")
	createReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	createRec := httptest.NewRecorder()
	h.Dispatch(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d, body: %s", createRec.Code, http.StatusOK, createRec.Body.String())
	}

	policyReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"SecretId":"test-secret"}`))
	policyReq.Header.Set("X-Amz-Target", "secretsmanager.GetResourcePolicy")
	policyReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	policyRec := httptest.NewRecorder()
	h.Dispatch(policyRec, policyReq)

	if policyRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", policyRec.Code, http.StatusOK, policyRec.Body.String())
	}
	if !strings.Contains(policyRec.Body.String(), "TarnDefaultSecretPolicy") {
		t.Fatalf("expected default policy document, got: %s", policyRec.Body.String())
	}
}

func TestUnknownActionReturnsEmptyOK(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set("X-Amz-Target", "secretsmanager.UnknownAction")
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Fatalf("expected empty JSON object response, got: %s", rec.Body.String())
	}
}

func dispatchSecrets(t *testing.T, h *Handler, target, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Amz-Target", "secretsmanager."+target)
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, body: %s", target, rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s decode: %v", target, err)
	}
	return resp
}

// TestKmsKeyIdRoundTrips guards Terraform's kms_key_id: Tarn encrypts with
// its own vault, but the ID must read back or every plan shows a diff.
func TestKmsKeyIdRoundTrips(t *testing.T) {
	h := newTestHandler(t)

	dispatchSecrets(t, h, "CreateSecret", `{"Name":"kms-secret","SecretString":"v","KmsKeyId":"alias/app"}`)
	if got := dispatchSecrets(t, h, "DescribeSecret", `{"SecretId":"kms-secret"}`)["KmsKeyId"]; got != "alias/app" {
		t.Fatalf("KmsKeyId after create = %v, want alias/app", got)
	}

	dispatchSecrets(t, h, "UpdateSecret", `{"SecretId":"kms-secret","SecretString":"v2"}`)
	if got := dispatchSecrets(t, h, "DescribeSecret", `{"SecretId":"kms-secret"}`)["KmsKeyId"]; got != "alias/app" {
		t.Fatalf("KmsKeyId after value update = %v, want alias/app", got)
	}

	dispatchSecrets(t, h, "UpdateSecret", `{"SecretId":"kms-secret","KmsKeyId":"alias/other"}`)
	list := dispatchSecrets(t, h, "ListSecrets", `{}`)["SecretList"].([]any)
	if got := list[0].(map[string]any)["KmsKeyId"]; got != "alias/other" {
		t.Fatalf("ListSecrets KmsKeyId = %v, want alias/other", got)
	}
}
