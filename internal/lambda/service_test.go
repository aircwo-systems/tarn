package lambda

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	logssvc "github.com/aircwo-systems/tarn/internal/logs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestCreateFunctionCreatesLambdaLogGroup(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	logs := logssvc.NewService(cfg)
	svc := NewService(cfg, store, nil, nil, logs)

	fn, err := svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "log-group-test",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}
	// newly created functions start pending; the caller should see that.
	if fn.State != types.FunctionStatePending {
		t.Fatalf("expected pending state, got %q", fn.State)
	}
	if fn.LastUpdateStatus != types.LastUpdateStatusPending {
		t.Fatalf("expected last update status pending, got %q", fn.LastUpdateStatus)
	}
	// First fetch triggers the Pending → Active transition (Active state saved asynchronously).
	if _, err := svc.GetFunction("log-group-test"); err != nil {
		t.Fatalf("get function (first): %v", err)
	}
	// Second fetch should observe the Active state.
	retrieved, err := svc.GetFunction("log-group-test")
	if err != nil {
		t.Fatalf("get function (second): %v", err)
	}
	if retrieved.State != types.FunctionStateActive {
		t.Fatalf("expected active state after second fetch, got %q", retrieved.State)
	}
	if retrieved.LastUpdateStatus != types.LastUpdateStatusSuccessful {
		t.Fatalf("expected successful update status after second fetch, got %q", retrieved.LastUpdateStatus)
	}
}

// additional tests for update functions to ensure LastUpdateStatus remains populated
func TestUpdateFunctionsPopulateLastUpdateStatus(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	svc := NewService(cfg, store, nil, nil, nil)

	_, err := svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "update-test",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	// update code
	updated, err := svc.UpdateFunctionCode(context.Background(), "update-test", []byte("zip"))
	if err != nil {
		t.Fatalf("update code: %v", err)
	}
	if updated.LastUpdateStatus != types.LastUpdateStatusSuccessful {
		t.Fatalf("last update status after code update = %q", updated.LastUpdateStatus)
	}

	// update configuration
	delta := &types.FunctionConfigUpdate{Description: new(string)}
	*delta.Description = "foo"
	conf, err := svc.UpdateFunctionConfiguration(context.Background(), "update-test", delta)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if conf.LastUpdateStatus != types.LastUpdateStatusSuccessful {
		t.Fatalf("last update status after config update = %q", conf.LastUpdateStatus)
	}
}

func TestConsumeNewContainerLogs(t *testing.T) {
	svc := &Service{
		logCursors: make(map[string]int),
	}

	first := svc.consumeNewContainerLogs("container-1", "line-1\nline-2\n")
	if first != "line-1\nline-2\n" {
		t.Fatalf("first consume = %q, want full log payload", first)
	}

	second := svc.consumeNewContainerLogs("container-1", "line-1\nline-2\n")
	if second != "" {
		t.Fatalf("second consume = %q, want empty delta", second)
	}

	third := svc.consumeNewContainerLogs("container-1", "line-1\nline-2\nline-3\n")
	if third != "line-3\n" {
		t.Fatalf("third consume = %q, want only appended payload", third)
	}

	reset := svc.consumeNewContainerLogs("container-1", "line-a\n")
	if reset != "line-a\n" {
		t.Fatalf("reset consume = %q, want full payload after truncation", reset)
	}
}

func TestInvokePromotesPendingFunctionToActive(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	svc := NewService(cfg, store, nil, nil, nil)

	_, err := svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "invoke-pending",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	_, err = svc.Invoke(context.Background(), &types.InvokeInput{
		FunctionName:   "invoke-pending",
		InvocationType: "DryRun",
	})
	if err == nil {
		t.Fatalf("expected invoke to fail when execution engine is not configured")
	}
	if !strings.Contains(err.Error(), "lambda execution engine is not configured") {
		t.Fatalf("unexpected invoke error: %v", err)
	}

	fn, err := store.GetFunction("invoke-pending")
	if err != nil {
		t.Fatalf("get function: %v", err)
	}
	if fn.State != types.FunctionStateActive {
		t.Fatalf("expected active after invoke path promotion, got %q", fn.State)
	}
	if fn.LastUpdateStatus != types.LastUpdateStatusSuccessful {
		t.Fatalf("expected successful last update status after invoke promotion, got %q", fn.LastUpdateStatus)
	}
}

func TestIsTransientRIEInvokeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "transient eof",
			err:  fmt.Errorf("RIE invocation failed: Post http://127.0.0.1:12345/2015-03-31/functions/function/invocations: EOF"),
			want: true,
		},
		{
			name: "transient connection refused",
			err:  fmt.Errorf("RIE invocation failed: Post http://127.0.0.1:12345/2015-03-31/functions/function/invocations: dial tcp 127.0.0.1:12345: connect: connection refused"),
			want: true,
		},
		{
			name: "non transient runtime error",
			err:  fmt.Errorf("RIE invocation failed: runtime exited with status 1"),
			want: false,
		},
		{
			name: "non rie error",
			err:  fmt.Errorf("failed to create request: invalid method"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isTransientRIEInvokeError(tc.err)
			if got != tc.want {
				t.Fatalf("isTransientRIEInvokeError(%v)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestResolveLayerDirsSkipsUnavailableExternalManagedLayer(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	svc := NewService(cfg, store, nil, nil, nil)
	externalLayer := "arn:aws:lambda:us-east-1:177933569100:layer:AWS-Parameters-and-Secrets-Lambda-Extension:12"

	dirs, skipped, err := svc.ResolveLayerDirs([]string{externalLayer})
	if err != nil {
		t.Fatalf("ResolveLayerDirs returned error for unavailable external layer: %v", err)
	}
	if len(dirs) != 0 {
		t.Fatalf("expected 0 resolved dirs, got %d", len(dirs))
	}
	if len(skipped) != 1 || skipped[0] != externalLayer {
		t.Fatalf("unexpected skipped layers: %+v", skipped)
	}
}

func TestResolveLayerDirsFailsForUnavailableLocalAccountLayer(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	svc := NewService(cfg, store, nil, nil, nil)
	localAccountLayer := fmt.Sprintf("arn:aws:lambda:%s:%s:layer:missing-local-layer:1", cfg.Region, cfg.AccountID)

	_, _, err := svc.ResolveLayerDirs([]string{localAccountLayer})
	if err == nil {
		t.Fatalf("expected ResolveLayerDirs to fail for missing local-account layer")
	}
}
