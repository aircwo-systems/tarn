package eventbridge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

type fakeLambda struct {
	invocations []*types.InvokeInput
	err         error
}

func (f *fakeLambda) Invoke(_ context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	copied := *input
	f.invocations = append(f.invocations, &copied)
	if f.err != nil {
		return nil, f.err
	}
	return &types.InvokeOutput{StatusCode: 202}, nil
}

func newService(t *testing.T) (*Service, *fakeLambda) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	lambda := &fakeLambda{}
	store := NewStore(cfg)
	svc := NewService(cfg, store, lambda)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}
	return svc, lambda
}

func TestPutRuleAndPutTargetsAndFire(t *testing.T) {
	svc, fake := newService(t)

	rule, err := svc.PutRule("cron-every-minute", "rate(1 minute)", "ENABLED", "test", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	if rule.Arn == "" || rule.NextRunAt == nil {
		t.Fatalf("unexpected rule shape: %+v", rule)
	}

	failed, err := svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:  "t1",
		Arn: "processor",
	}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("expected 0 failed entries, got %d", len(failed))
	}

	fire, err := svc.FireRuleNow(rule.Name, map[string]string{"raceSession": "abc123"})
	if err != nil {
		t.Fatalf("FireRuleNow: %v", err)
	}
	if fire.Targets != 1 || fire.Successful != 1 || fire.Failed != 0 {
		t.Fatalf("unexpected fire result: %+v", fire)
	}
	if len(fake.invocations) != 1 {
		t.Fatalf("expected 1 lambda invocation, got %d", len(fake.invocations))
	}
	if fake.invocations[0].FunctionName != "processor" {
		t.Fatalf("functionName=%q want %q", fake.invocations[0].FunctionName, "processor")
	}
}

func TestDeleteRuleRequiresTargetsRemoved(t *testing.T) {
	svc, _ := newService(t)
	rule, err := svc.PutRule("delete-guard", "rate(1 minute)", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	_, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{ID: "t1", Arn: "worker"}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}

	if err := svc.DeleteRule(rule.Name, "default", false); err == nil {
		t.Fatalf("expected DeleteRule to fail when targets exist")
	}

	failed, err := svc.RemoveTargets(rule.Name, "default", []string{"t1"})
	if err != nil {
		t.Fatalf("RemoveTargets: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("expected no failed removals")
	}
	if err := svc.DeleteRule(rule.Name, "default", false); err != nil {
		t.Fatalf("DeleteRule after RemoveTargets: %v", err)
	}
}

func TestExecuteDueRulesAdvancesNextRun(t *testing.T) {
	svc, fake := newService(t)
	rule, err := svc.PutRule("scheduler", "rate(1 minute)", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	_, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{ID: "t1", Arn: "worker"}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	rule, err = svc.DescribeRule(rule.Name, "default")
	if err != nil {
		t.Fatalf("DescribeRule: %v", err)
	}
	rule.NextRunAt = &now
	if err := svc.store.SaveRule(rule); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}

	svc.executeDueRules(now)
	if len(fake.invocations) == 0 {
		t.Fatalf("expected scheduler to trigger invocation")
	}

	updated, err := svc.DescribeRule(rule.Name, "default")
	if err != nil {
		t.Fatalf("DescribeRule(updated): %v", err)
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.After(now) {
		t.Fatalf("expected next run after now, got %+v", updated.NextRunAt)
	}
}

func TestFireRulePersistsInvokeErrorDetails(t *testing.T) {
	svc, fake := newService(t)
	fake.err = fmt.Errorf("failed to resolve layers: layer code not found")

	rule, err := svc.PutRule("rule-with-error", "rate(1 minute)", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	_, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{ID: "t1", Arn: "worker"}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}

	fire, err := svc.FireRuleNow(rule.Name, nil)
	if err != nil {
		t.Fatalf("FireRuleNow: %v", err)
	}
	if fire.Failed != 1 || fire.Successful != 0 {
		t.Fatalf("unexpected fire result: %+v", fire)
	}

	updated, err := svc.DescribeRule(rule.Name, "default")
	if err != nil {
		t.Fatalf("DescribeRule: %v", err)
	}
	if len(updated.Targets) != 1 {
		t.Fatalf("expected one target, got %d", len(updated.Targets))
	}
	if !strings.Contains(updated.Targets[0].LastResult, "failed to resolve layers") {
		t.Fatalf("expected detailed LastResult, got %q", updated.Targets[0].LastResult)
	}
}
