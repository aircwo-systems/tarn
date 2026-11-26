package eventbridge

import (
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestCustomEventBusRoutesEventsOnlyToItsRules(t *testing.T) {
	svc, lambda, sender, _ := newSQSService(t)
	bus, err := svc.CreateEventBus("domain-events", "user domain events", map[string]string{"team": "identity"})
	if err != nil {
		t.Fatalf("CreateEventBus: %v", err)
	}
	if bus.Arn != "arn:aws:events:us-east-1:000000000000:event-bus/domain-events" {
		t.Fatalf("bus ARN = %s", bus.Arn)
	}

	pattern := `{"source":["app.users"]}`
	// The same rule name on two buses names two independent rules.
	custom, err := svc.PutRule("users", "", pattern, "ENABLED", "", "domain-events")
	if err != nil {
		t.Fatalf("PutRule custom: %v", err)
	}
	if custom.Arn != "arn:aws:events:us-east-1:000000000000:rule/domain-events/users" || custom.EventBusName != "domain-events" {
		t.Fatalf("custom rule = %+v", custom)
	}
	if _, err := svc.PutRule("users", "", pattern, "ENABLED", "", ""); err != nil {
		t.Fatalf("PutRule default: %v", err)
	}
	if failed, err := svc.PutTargets("users", bus.Arn, []types.EventBridgeTarget{{ID: "q", Arn: testQueueARNPrefix + "custom-q"}}); err != nil || len(failed) != 0 {
		t.Fatalf("PutTargets custom (by bus ARN): %+v %v", failed, err)
	}
	if failed, err := svc.PutTargets("users", "default", []types.EventBridgeTarget{{ID: "fn", Arn: "default-fn"}}); err != nil || len(failed) != 0 {
		t.Fatalf("PutTargets default: %+v %v", failed, err)
	}

	results, failedCount, err := svc.PutEvents([]types.PutEventsEntry{
		{Source: "app.users", DetailType: "UserSignedIn", Detail: `{}`, EventBusName: "domain-events"},
		{Source: "app.users", DetailType: "UserSignedIn", Detail: `{}`, EventBusName: "missing-bus"},
	})
	if err != nil {
		t.Fatalf("PutEvents: %v", err)
	}
	svc.DrainDispatch()
	if failedCount != 1 || results[1].ErrorCode != "ResourceNotFoundException" || results[0].EventId == "" {
		t.Fatalf("results = %+v failed=%d", results, failedCount)
	}
	if got := sender.to("custom-q"); len(got) != 1 {
		t.Fatalf("custom bus rule should receive the event once, got %d", len(got))
	}
	if len(lambda.invocations) != 0 {
		t.Fatalf("default-bus rule must not see custom-bus events, got %d invocations", len(lambda.invocations))
	}

	// And a default-bus event reaches only the default-bus rule.
	putUserEvent(t, svc)
	if len(lambda.invocations) != 1 || len(sender.to("custom-q")) != 1 {
		t.Fatalf("default event routing: invocations=%d custom=%d", len(lambda.invocations), len(sender.to("custom-q")))
	}

	rules, _, err := svc.ListRules("", "domain-events", 0, "")
	if err != nil || len(rules) != 1 || rules[0].EventBusName != "domain-events" {
		t.Fatalf("ListRules custom = %+v %v", rules, err)
	}
	names, _, err := svc.ListRuleNamesByTarget(testQueueARNPrefix+"custom-q", "", 0, "")
	if err != nil || len(names) != 0 {
		t.Fatalf("ListRuleNamesByTarget on default bus must not see custom-bus rules: %v %v", names, err)
	}
	names, _, _ = svc.ListRuleNamesByTarget(testQueueARNPrefix+"custom-q", "domain-events", 0, "")
	if len(names) != 1 {
		t.Fatalf("ListRuleNamesByTarget custom = %v", names)
	}

	tags, err := svc.ListTagsForResource(custom.Arn)
	if err != nil {
		t.Fatalf("ListTagsForResource rule on custom bus: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("rule tags = %v", tags)
	}
	if err := svc.TagResource(bus.Arn, map[string]string{"env": "local"}); err != nil {
		t.Fatalf("TagResource bus: %v", err)
	}
	busTags, err := svc.ListTagsForResource(bus.Arn)
	if err != nil || busTags["team"] != "identity" || busTags["env"] != "local" {
		t.Fatalf("bus tags = %v %v", busTags, err)
	}
	if err := svc.UntagResource(bus.Arn, []string{"team"}); err != nil {
		t.Fatal(err)
	}
	if busTags, _ = svc.ListTagsForResource(bus.Arn); busTags["team"] != "" {
		t.Fatalf("untag failed: %v", busTags)
	}
}

func TestCustomEventBusLifecycleAndValidation(t *testing.T) {
	svc, _ := newService(t)

	if _, err := svc.PutRule("r", "", `{"source":["a"]}`, "", "", "nope"); err == nil || err.(*ServiceError).Code != "ResourceNotFoundException" {
		t.Fatalf("PutRule on missing bus: %v", err)
	}
	if _, err := svc.CreateEventBus("default", "", nil); err == nil || err.(*ServiceError).Code != "ResourceAlreadyExistsException" {
		t.Fatalf("CreateEventBus default: %v", err)
	}
	if _, err := svc.CreateEventBus("orders", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateEventBus("orders", "", nil); err == nil || err.(*ServiceError).Code != "ResourceAlreadyExistsException" {
		t.Fatalf("duplicate CreateEventBus: %v", err)
	}
	if _, err := svc.PutRule("nightly", "rate(1 day)", "", "", "", "orders"); err == nil || !strings.Contains(err.Error(), "default event bus") {
		t.Fatalf("schedule on custom bus: %v", err)
	}
	if _, err := svc.PutRule("created", "", `{"source":["orders"]}`, "", "", "orders"); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnableRule("created", "orders"); err != nil {
		t.Fatalf("EnableRule on pattern rule: %v", err)
	}
	if _, err := svc.DescribeRule("created", ""); err == nil {
		t.Fatal("custom-bus rule must not be visible on the default bus")
	}

	buses, _, err := svc.ListEventBuses("", 0, "")
	if err != nil || len(buses) != 2 || buses[0].Name != "default" || buses[1].Name != "orders" {
		t.Fatalf("ListEventBuses = %+v %v", buses, err)
	}
	if described, err := svc.DescribeEventBus("arn:aws:events:us-east-1:000000000000:event-bus/orders"); err != nil || described.Name != "orders" {
		t.Fatalf("DescribeEventBus by ARN = %+v %v", described, err)
	}

	if err := svc.DeleteEventBus("orders"); err == nil {
		t.Fatal("DeleteEventBus must refuse while rules remain")
	}
	if err := svc.DeleteRule("created", "orders", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEventBus("orders"); err != nil {
		t.Fatalf("DeleteEventBus: %v", err)
	}
	if err := svc.DeleteEventBus("default"); err == nil {
		t.Fatal("deleting the default bus must fail")
	}
	if _, err := svc.DescribeEventBus("orders"); err == nil {
		t.Fatal("deleted bus still described")
	}
}

func TestCustomBusRulesAndBusesPersist(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, store, nil)
	if _, err := svc.CreateEventBus("orders", "d", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutRule("same", "", `{"source":["a"]}`, "", "", "orders"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutRule("same", "", `{"source":["b"]}`, "", "", ""); err != nil {
		t.Fatal(err)
	}
	store.Flush()
	store.Close()

	reloaded := NewStore(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if bus, err := reloaded.GetBus("orders"); err != nil || bus.Tags["k"] != "v" {
		t.Fatalf("bus = %+v %v", bus, err)
	}
	onCustom, err := reloaded.GetRule("orders", "same")
	if err != nil || onCustom.EventPattern != `{"source":["a"]}` {
		t.Fatalf("custom rule = %+v %v", onCustom, err)
	}
	onDefault, err := reloaded.GetRule("", "same")
	if err != nil || onDefault.EventPattern != `{"source":["b"]}` {
		t.Fatalf("default rule = %+v %v", onDefault, err)
	}
}
