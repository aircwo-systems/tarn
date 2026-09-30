package stepfunctions

import (
	"fmt"
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
	if n := len(s.ListExecutions("", "", 0)); n != 1 {
		t.Fatalf("list len = %d, want 1", n)
	}
}

func testExecution(arn, machineArn string, start time.Time, status string) *types.Execution {
	return &types.Execution{
		Arn: arn, Name: arn, StateMachineArn: machineArn, Status: status,
		Input: "{}", StartDate: start,
		History: []types.HistoryEvent{{ID: 1, Type: "ExecutionStarted"}},
	}
}

func TestStoreCapsExecutionsPerMachine(t *testing.T) {
	s := NewStore(testConfig(t, false))
	base := time.Now().UTC().Add(-time.Duration(maxExecutionsPerMachine) * time.Second)

	for i := 0; i <= maxExecutionsPerMachine; i++ {
		ex := testExecution(fmt.Sprintf("arn:ex:%d", i), "arn:sm:foo",
			base.Add(time.Duration(i)*time.Second), types.ExecutionStatusSucceeded)
		if err := s.SaveExecution(ex); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	// Another machine's executions are counted separately.
	for i := 0; i < 5; i++ {
		ex := testExecution(fmt.Sprintf("arn:ex:bar:%d", i), "arn:sm:bar",
			base.Add(time.Duration(i)*time.Second), types.ExecutionStatusSucceeded)
		if err := s.SaveExecution(ex); err != nil {
			t.Fatalf("save bar %d: %v", i, err)
		}
	}

	kept := s.ListExecutions("arn:sm:foo", "", 0)
	if len(kept) != maxExecutionsPerMachine {
		t.Fatalf("kept %d executions, want %d", len(kept), maxExecutionsPerMachine)
	}
	// The oldest run is the one dropped.
	if kept[len(kept)-1].Arn != "arn:ex:1" {
		t.Fatalf("oldest kept execution = %q, want arn:ex:1", kept[len(kept)-1].Arn)
	}
	if _, err := s.GetExecution("arn:ex:0"); err == nil {
		t.Fatal("expected the oldest execution to be evicted")
	}
	if n := len(s.ListExecutions("arn:sm:bar", "", 0)); n != 5 {
		t.Fatalf("other machine kept %d executions, want 5", n)
	}
}

func TestStoreKeepsRunningExecutionsWhenCapping(t *testing.T) {
	s := NewStore(testConfig(t, false))
	base := time.Now().UTC().Add(-time.Duration(4*maxExecutionsPerMachine) * time.Second)

	save := func(i int, status string) {
		t.Helper()
		ex := testExecution(fmt.Sprintf("arn:ex:%d", i), "arn:sm:foo",
			base.Add(time.Duration(i)*time.Second), status)
		if err := s.SaveExecution(ex); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	// A run that started before everything else and is still going.
	save(0, types.ExecutionStatusRunning)
	for i := 1; i <= maxExecutionsPerMachine; i++ {
		save(i, types.ExecutionStatusSucceeded)
	}
	// Two more pushes the machine past the cap, so the oldest finished runs
	// have to go. The run that started first must not.
	for i := maxExecutionsPerMachine + 1; i <= maxExecutionsPerMachine+2; i++ {
		save(i, types.ExecutionStatusSucceeded)
	}

	running := s.ListExecutions("arn:sm:foo", types.ExecutionStatusRunning, 0)
	if len(running) != 1 || running[0].Arn != "arn:ex:0" {
		t.Fatalf("running executions = %#v, want just the oldest run kept", running)
	}
	if n := len(s.ListExecutions("arn:sm:foo", types.ExecutionStatusSucceeded, 0)); n != maxExecutionsPerMachine-1 {
		t.Fatalf("finished executions = %d, want %d", n, maxExecutionsPerMachine-1)
	}
	// The dropped ones are the oldest finished, not the newest.
	if _, err := s.GetExecution("arn:ex:1"); err == nil {
		t.Fatal("expected the oldest finished execution to be evicted")
	}
	if _, err := s.GetExecution(fmt.Sprintf("arn:ex:%d", maxExecutionsPerMachine+2)); err != nil {
		t.Fatalf("newest finished execution was evicted: %v", err)
	}
}

func TestStoreFiltersBeforeCopyingAndHonoursLimit(t *testing.T) {
	s := NewStore(testConfig(t, false))
	base := time.Now().UTC().Add(-200 * time.Second)
	for i := 0; i < 20; i++ {
		machine := "arn:sm:foo"
		if i%2 == 0 {
			machine = "arn:sm:bar"
		}
		ex := testExecution(fmt.Sprintf("arn:ex:%d", i), machine,
			base.Add(time.Duration(i)*time.Second), types.ExecutionStatusSucceeded)
		if err := s.SaveExecution(ex); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	foo := s.ListExecutions("arn:sm:foo", "", 3)
	if len(foo) != 3 {
		t.Fatalf("limited list len = %d, want 3", len(foo))
	}
	// Newest first, and only this machine's executions, which are the odd
	// numbered ones.
	if foo[0].Arn != "arn:ex:19" || foo[2].Arn != "arn:ex:15" {
		t.Fatalf("limited list = %q..%q, want arn:ex:19..arn:ex:15", foo[0].Arn, foo[2].Arn)
	}
	for _, ex := range foo {
		if ex.StateMachineArn != "arn:sm:foo" {
			t.Fatalf("list leaked another machine's execution %q", ex.Arn)
		}
	}

	// A returned execution must be a copy the caller may mutate.
	foo[0].Name = "mutated"
	again := s.ListExecutions("arn:sm:foo", "", 1)
	if again[0].Name == "mutated" {
		t.Fatal("store mutated through a returned execution")
	}
}

func TestStoreDeleteStateMachineRemovesExecutions(t *testing.T) {
	s := NewStore(testConfig(t, false))
	sm := &types.StateMachine{
		Arn: "arn:sm:foo", Name: "foo", Definition: "{}",
		Type: types.StateMachineTypeStandard, Status: types.StateMachineStatusActive,
	}
	if err := s.SaveStateMachine(sm); err != nil {
		t.Fatalf("save sm: %v", err)
	}
	if err := s.SaveExecution(testExecution("arn:ex:1", "arn:sm:foo", time.Now().UTC(), types.ExecutionStatusSucceeded)); err != nil {
		t.Fatalf("save ex: %v", err)
	}

	if err := s.DeleteStateMachine("arn:sm:foo"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetExecution("arn:ex:1"); err == nil {
		t.Fatal("expected the machine's executions to be removed with it")
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

// BenchmarkListExecutionsPerMachine measures one dashboard overview poll's
// per-state-machine execution lookup, with several machines' executions behind
// the filter.
func BenchmarkListExecutionsPerMachine(b *testing.B) {
	s := NewStore(&config.Config{
		Region: "us-east-1", AccountID: "000000000000",
		DataDir: b.TempDir(), PersistenceEnabled: false,
	})
	base := time.Now().UTC().Add(-time.Duration(maxExecutionsPerMachine*8) * time.Second)
	// 8 machines' worth of executions, each with a history to copy.
	for i := 0; i < maxExecutionsPerMachine*8; i++ {
		ex := testExecution(fmt.Sprintf("arn:ex:%d", i), fmt.Sprintf("arn:sm:%d", i%8),
			base.Add(time.Duration(i)*time.Second), types.ExecutionStatusSucceeded)
		ex.History = make([]types.HistoryEvent, 20)
		for h := range ex.History {
			ex.History[h] = types.HistoryEvent{
				ID: int64(h), Type: "TaskStateEntered", Timestamp: base,
				Details: map[string]any{"input": `{"key":"value","n":1}`},
			}
		}
		if err := s.SaveExecution(ex); err != nil {
			b.Fatalf("save %d: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := s.ListExecutions("arn:sm:0", "", 50); len(got) != 50 {
			b.Fatalf("listed %d executions, want 50", len(got))
		}
	}
}
