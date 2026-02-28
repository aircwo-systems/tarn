package lambda

import (
	"context"
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

	_, err := svc.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "log-group-test",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/test",
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	group, err := logs.GetGroup("/aws/lambda/log-group-test")
	if err != nil {
		t.Fatalf("get log group: %v", err)
	}
	if group == nil {
		t.Fatal("expected lambda log group to be created")
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
