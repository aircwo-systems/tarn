package lambda

import (
	"context"
	"strings"
	"testing"

	"github.com/openstack-project/openstack/internal/config"
	logssvc "github.com/openstack-project/openstack/internal/logs"
	"github.com/openstack-project/openstack/pkg/types"
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
