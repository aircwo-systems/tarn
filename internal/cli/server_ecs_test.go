package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/api"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/infrastructure"
	"github.com/aircwo-systems/tarn/internal/trace"
)

func TestInitAccountBundleWiresECSRunner(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	cfg.InfraProbeEnabled = false

	traceStore := trace.NewStore()
	shared := &sharedDeps{
		traceStore: traceStore,
		collector:  trace.NewCollector(),
		infraSvc:   infrastructure.NewService("", false),
	}
	bundle, err := initAccountBundle(cfg, shared)
	if err != nil {
		t.Fatalf("initAccountBundle returned error: %v", err)
	}
	registry := api.NewHandlerRegistry(func(string) (*api.AccountBundle, error) {
		return bundle, nil
	})
	if _, err := registry.PreInit(cfg.AccountID); err != nil {
		t.Fatalf("register account bundle: %v", err)
	}
	t.Cleanup(func() {
		registry.StopAll()
		_ = traceStore.Close()
	})

	if bundle.Handlers().ECS == nil {
		t.Fatal("expected account bundle to expose an ECS handler")
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"TaskDefinition":"missing"}`))
	req.Header.Set("X-Amz-Target", "AmazonEC2ContainerServiceV20141113.RunTask")
	rec := httptest.NewRecorder()
	bundle.Handlers().ECS.Dispatch(rec, req)

	if rec.Code != http.StatusBadRequest {
		body, _ := io.ReadAll(rec.Result().Body)
		t.Fatalf("expected wired runner to resolve the request and return 400, got %d: %s", rec.Code, body)
	}
	if strings.Contains(rec.Body.String(), "task runner is not configured") {
		t.Fatalf("expected ECS handler to have a task runner, got: %s", rec.Body.String())
	}
}
