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

	rule, err := svc.PutRule("cron-every-minute", "rate(1 minute)", "", "ENABLED", "test", "default")
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
	rule, err := svc.PutRule("delete-guard", "rate(1 minute)", "", "ENABLED", "", "default")
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
	rule, err := svc.PutRule("scheduler", "rate(1 minute)", "", "ENABLED", "", "default")
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

	rule, err := svc.PutRule("rule-with-error", "rate(1 minute)", "", "ENABLED", "", "default")
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

func TestPutRuleWithEventPattern(t *testing.T) {
	svc, _ := newService(t)

	pattern := `{"source": ["my.app"], "detail-type": ["OrderCreated"]}`
	rule, err := svc.PutRule("order-rule", "", pattern, "ENABLED", "matches order events", "default")
	if err != nil {
		t.Fatalf("PutRule with EventPattern: %v", err)
	}
	if rule.EventPattern != pattern {
		t.Fatalf("expected EventPattern=%q, got %q", pattern, rule.EventPattern)
	}
	if rule.ScheduleExpression != "" {
		t.Fatalf("expected empty ScheduleExpression, got %q", rule.ScheduleExpression)
	}
	if rule.NextRunAt != nil {
		t.Fatalf("expected nil NextRunAt for event-pattern rule")
	}
}

func TestPutRuleRejectsBothScheduleAndPattern(t *testing.T) {
	svc, _ := newService(t)

	_, err := svc.PutRule("bad-rule", "rate(1 minute)", `{"source": ["x"]}`, "ENABLED", "", "default")
	if err == nil {
		t.Fatalf("expected error when both ScheduleExpression and EventPattern are set")
	}
}

func TestPutRuleRejectsNeitherScheduleNorPattern(t *testing.T) {
	svc, _ := newService(t)

	_, err := svc.PutRule("bad-rule", "", "", "ENABLED", "", "default")
	if err == nil {
		t.Fatalf("expected error when neither ScheduleExpression nor EventPattern is set")
	}
}

func TestPutEventsMatchesAndDispatches(t *testing.T) {
	svc, fake := newService(t)

	// Create an event-pattern rule
	pattern := `{"source": ["order.service"], "detail-type": ["OrderCreated"]}`
	rule, err := svc.PutRule("order-rule", "", pattern, "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}

	// Attach a Lambda target
	_, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:  "t1",
		Arn: "order-processor",
	}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}

	// Send a matching event
	results, failedCount, err := svc.PutEvents([]types.PutEventsEntry{{
		Source:     "order.service",
		DetailType: "OrderCreated",
		Detail:     `{"orderId": "abc123", "amount": 42.50}`,
	}})
	if err != nil {
		t.Fatalf("PutEvents: %v", err)
	}
	if failedCount != 0 {
		t.Fatalf("expected 0 failures, got %d", failedCount)
	}
	if len(results) != 1 || results[0].EventId == "" {
		t.Fatalf("unexpected results: %+v", results)
	}

	// Lambda should have been invoked
	if len(fake.invocations) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(fake.invocations))
	}
	if fake.invocations[0].FunctionName != "order-processor" {
		t.Fatalf("functionName=%q, want %q", fake.invocations[0].FunctionName, "order-processor")
	}
}

func TestPutEventsNoMatchDoesNotInvoke(t *testing.T) {
	svc, fake := newService(t)

	pattern := `{"source": ["order.service"]}`
	rule, err := svc.PutRule("order-rule", "", pattern, "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	_, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:  "t1",
		Arn: "order-processor",
	}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}

	// Send a non-matching event
	results, failedCount, err := svc.PutEvents([]types.PutEventsEntry{{
		Source:     "payment.service",
		DetailType: "PaymentProcessed",
		Detail:     `{}`,
	}})
	if err != nil {
		t.Fatalf("PutEvents: %v", err)
	}
	if failedCount != 0 {
		t.Fatalf("expected 0 failures, got %d", failedCount)
	}
	if len(results) != 1 || results[0].EventId == "" {
		t.Fatalf("unexpected results: %+v", results)
	}

	// Lambda should NOT have been invoked
	if len(fake.invocations) != 0 {
		t.Fatalf("expected 0 invocations, got %d", len(fake.invocations))
	}
}

func TestPutEventsDisabledRuleSkipped(t *testing.T) {
	svc, fake := newService(t)

	pattern := `{"source": ["test.source"]}`
	rule, err := svc.PutRule("disabled-rule", "", pattern, "DISABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	_, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:  "t1",
		Arn: "worker",
	}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}

	_, _, err = svc.PutEvents([]types.PutEventsEntry{{
		Source:     "test.source",
		DetailType: "TestEvent",
		Detail:     `{}`,
	}})
	if err != nil {
		t.Fatalf("PutEvents: %v", err)
	}
	if len(fake.invocations) != 0 {
		t.Fatalf("disabled rule should not trigger, got %d invocations", len(fake.invocations))
	}
}

func TestPutEventsMultipleRulesMatch(t *testing.T) {
	svc, fake := newService(t)

	// Create two rules that match the same event
	pattern := `{"source": ["test.source"]}`
	for _, name := range []string{"rule-a", "rule-b"} {
		rule, err := svc.PutRule(name, "", pattern, "ENABLED", "", "default")
		if err != nil {
			t.Fatalf("PutRule(%s): %v", name, err)
		}
		_, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
			ID:  "t1",
			Arn: name + "-handler",
		}})
		if err != nil {
			t.Fatalf("PutTargets(%s): %v", name, err)
		}
	}

	_, _, err := svc.PutEvents([]types.PutEventsEntry{{
		Source:     "test.source",
		DetailType: "TestEvent",
		Detail:     `{}`,
	}})
	if err != nil {
		t.Fatalf("PutEvents: %v", err)
	}

	// Both rules should have triggered
	if len(fake.invocations) != 2 {
		t.Fatalf("expected 2 invocations, got %d", len(fake.invocations))
	}
}

func TestPutEventsValidation(t *testing.T) {
	svc, _ := newService(t)

	// Missing source
	results, failedCount, err := svc.PutEvents([]types.PutEventsEntry{{
		DetailType: "Test",
		Detail:     `{}`,
	}})
	if err != nil {
		t.Fatalf("PutEvents: %v", err)
	}
	if failedCount != 1 {
		t.Fatalf("expected 1 failure, got %d", failedCount)
	}
	if results[0].ErrorCode != "ValidationException" {
		t.Fatalf("expected ValidationException, got %q", results[0].ErrorCode)
	}

	// Invalid detail JSON
	results, failedCount, err = svc.PutEvents([]types.PutEventsEntry{{
		Source:     "test",
		DetailType: "Test",
		Detail:     `not-json`,
	}})
	if err != nil {
		t.Fatalf("PutEvents: %v", err)
	}
	if failedCount != 1 {
		t.Fatalf("expected 1 failure for bad JSON, got %d", failedCount)
	}
}

func TestPutEventsBatchLimit(t *testing.T) {
	svc, _ := newService(t)

	entries := make([]types.PutEventsEntry, 11)
	for i := range entries {
		entries[i] = types.PutEventsEntry{Source: "x", DetailType: "y", Detail: "{}"}
	}
	_, _, err := svc.PutEvents(entries)
	if err == nil {
		t.Fatalf("expected error for >10 entries")
	}
}

func TestPutEventsScheduledRulesIgnored(t *testing.T) {
	svc, fake := newService(t)

	// Create a scheduled rule (should NOT be matched by PutEvents)
	rule, err := svc.PutRule("scheduled", "rate(1 minute)", "", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	_, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:  "t1",
		Arn: "worker",
	}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}

	_, _, err = svc.PutEvents([]types.PutEventsEntry{{
		Source:     "anything",
		DetailType: "Anything",
		Detail:     `{}`,
	}})
	if err != nil {
		t.Fatalf("PutEvents: %v", err)
	}

	if len(fake.invocations) != 0 {
		t.Fatalf("scheduled rules should not be triggered by PutEvents, got %d invocations", len(fake.invocations))
	}
}
