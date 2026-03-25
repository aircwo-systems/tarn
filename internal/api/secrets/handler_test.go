package secrets

import (
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
