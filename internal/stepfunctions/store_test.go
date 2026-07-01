package stepfunctions

import (
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func testConfig(t *testing.T, persist bool) *config.Config {
	t.Helper()
	return &config.Config{
		Region:             "us-east-1",
		AccountID:          "000000000000",
		DataDir:            t.TempDir(),
		PersistenceEnabled: persist,
	}
}

func TestStoreStateMachineCRUD(t *testing.T) {
	s := NewStore(testConfig(t, false))
	sm := &types.StateMachine{
		Arn: "arn:sm:foo", Name: "foo", Definition: "{}",
		Type: types.StateMachineTypeStandard, Status: types.StateMachineStatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.SaveStateMachine(sm); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.GetStateMachine("arn:sm:foo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "foo" {
		t.Fatalf("name = %q, want foo", got.Name)
	}

	// Mutating the returned clone must not affect the stored copy.
	got.Name = "mutated"
	again, _ := s.GetStateMachine("arn:sm:foo")
	if again.Name != "foo" {
		t.Fatalf("store mutated through returned clone: %q", again.Name)
	}

	if n := len(s.ListStateMachines()); n != 1 {
		t.Fatalf("list len = %d, want 1", n)
	}
	if err := s.DeleteStateMachine("arn:sm:foo"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetStateMachine("arn:sm:foo"); err == nil {
		t.Fatal("expected not-found after delete")
	}
}

func TestStoreExecutionCRUD(t *testing.T) {
	s := NewStore(testConfig(t, false))
	ex := &types.Execution{
		Arn: "arn:ex:1", Name: "1", StateMachineArn: "arn:sm:foo",
		Status: types.ExecutionStatusRunning, Input: "{}", StartDate: time.Now().UTC(),
	}
	if err := s.SaveExecution(ex); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.GetExecution("arn:ex:1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != types.ExecutionStatusRunning {
		t.Fatalf("status = %q", got.Status)
	}
	if n := len(s.ListExecutions()); n != 1 {
		t.Fatalf("list len = %d, want 1", n)
	}
}

func TestStorePersistenceRoundTrip(t *testing.T) {
	cfg := testConfig(t, true)

	s := NewStore(cfg)
	sm := &types.StateMachine{
		Arn: "arn:sm:foo", Name: "foo", Definition: `{"StartAt":"A"}`,
		Type: types.StateMachineTypeStandard, Status: types.StateMachineStatusActive,
		CreatedAt: time.Now().UTC(), Tags: map[string]string{"team": "core"},
	}
	if err := s.SaveStateMachine(sm); err != nil {
		t.Fatalf("save sm: %v", err)
	}
	run := &types.Execution{
		Arn: "arn:ex:1", Name: "1", StateMachineArn: "arn:sm:foo",
		Status: types.ExecutionStatusRunning, Input: "{}", StartDate: time.Now().UTC(),
	}
	if err := s.SaveExecution(run); err != nil {
		t.Fatalf("save ex: %v", err)
	}
	s.flushToDisk()

	// A fresh store restoring the snapshot.
	s2 := NewStore(cfg)
	if err := s2.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	restored, err := s2.GetStateMachine("arn:sm:foo")
	if err != nil {
		t.Fatalf("machine not restored: %v", err)
	}
	if restored.Tags["team"] != "core" {
		t.Fatalf("tags not restored: %v", restored.Tags)
	}
	ex, err := s2.GetExecution("arn:ex:1")
	if err != nil {
		t.Fatalf("execution not restored: %v", err)
	}
	if ex.Status != types.ExecutionStatusAborted {
		t.Fatalf("RUNNING execution should restore as ABORTED, got %q", ex.Status)
	}
}
