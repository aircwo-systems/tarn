package eventbridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
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

type fakeTaskRunner struct {
	mu      sync.Mutex
	calls   []*types.RunTaskInput
	out     *types.RunTaskOutput
	err     error
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *fakeTaskRunner) RunTask(ctx context.Context, input *types.RunTaskInput) (*types.RunTaskOutput, error) {
	f.mu.Lock()
	copied := *input
	f.calls = append(f.calls, &copied)
	f.mu.Unlock()

	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.out != nil {
		return f.out, nil
	}
	return &types.RunTaskOutput{Tasks: []types.Task{{TaskArn: "arn:aws:ecs:task/example"}}}, nil
}

func (f *fakeTaskRunner) StopTask(context.Context, string, string, string) error { return nil }

func (f *fakeTaskRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
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

// Terraform creates sibling aws_cloudwatch_event_target resources in parallel;
// concurrent PutTargets on one rule must not lose each other's writes.
func TestConcurrentPutTargetsKeepsEveryTarget(t *testing.T) {
	svc, _ := newService(t)
	rule, err := svc.PutRule("concurrent-targets", "rate(1 minute)", "", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("t%02d", i)
			if _, err := svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{ID: id, Arn: "processor"}}); err != nil {
				t.Errorf("PutTargets %s: %v", id, err)
			}
		}()
	}
	wg.Wait()

	targets, _, err := svc.ListTargetsByRule(rule.Name, "default", 0, "")
	if err != nil {
		t.Fatalf("ListTargetsByRule: %v", err)
	}
	if len(targets) != n {
		t.Fatalf("expected %d targets, got %d: %+v", n, len(targets), targets)
	}
}

// A slow dispatch must not save its pre-dispatch rule snapshot over targets
// added while it was running.
func TestFireRuleKeepsTargetsAddedDuringDispatch(t *testing.T) {
	svc, _ := newService(t)
	runner := &fakeTaskRunner{started: make(chan struct{}), release: make(chan struct{})}
	svc.SetTaskRunner(runner)

	rule, err := svc.PutRule("fire-during-put", "rate(1 minute)", "", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	if _, err := svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:            "ecs",
		Arn:           "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		EcsParameters: &types.EcsParameters{TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1"},
	}}); err != nil {
		t.Fatalf("PutTargets: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.FireRuleNow(rule.Name, nil)
		done <- err
	}()
	<-runner.started
	if _, err := svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{ID: "late", Arn: "processor"}}); err != nil {
		t.Fatalf("PutTargets during dispatch: %v", err)
	}
	close(runner.release)
	if err := <-done; err != nil {
		t.Fatalf("FireRuleNow: %v", err)
	}

	got, err := svc.DescribeRule(rule.Name, "default")
	if err != nil {
		t.Fatalf("DescribeRule: %v", err)
	}
	ids := make([]string, 0, len(got.Targets))
	for _, target := range got.Targets {
		ids = append(ids, target.ID)
		if target.ID == "ecs" && target.LastInvokedAt == nil {
			t.Fatalf("fired target lost its LastInvokedAt: %+v", target)
		}
	}
	if len(ids) != 2 || got.LastResult == "" {
		t.Fatalf("expected targets [ecs late] and a LastResult, got ids=%v lastResult=%q", ids, got.LastResult)
	}
}

func TestEventBridgeECSTargetDispatchesTransformedEvent(t *testing.T) {
	svc, _ := newService(t)
	runner := &fakeTaskRunner{out: &types.RunTaskOutput{Tasks: []types.Task{{
		TaskArn:    "arn:aws:ecs:us-east-1:000000000000:task/default/task-1",
		LastStatus: types.TaskStatusRunning,
	}}}}
	svc.SetTaskRunner(runner)
	traceStore := tracesvc.NewStore()
	svc.SetTraceStore(traceStore)

	rule, err := svc.PutRule("ecs-target-rule", "rate(1 minute)", "", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	target := types.EventBridgeTarget{
		ID:  "ecs-target",
		Arn: "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		InputTransformer: &types.InputTransformer{
			InputPathsMap: map[string]string{"source": "$.source"},
			InputTemplate: `{"source": <source>}`,
		},
		EcsParameters: &types.EcsParameters{
			TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/order-worker:1",
			TaskCount:         2,
			LaunchType:        types.LaunchTypeFargate,
			ContainerOverrides: []types.ContainerOverride{{
				Name:        "app",
				Command:     []string{"worker"},
				Environment: []types.KeyValuePair{{Name: "STATIC", Value: "yes"}},
			}},
		},
	}
	failed, err := svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{target})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("expected ECS target to be accepted, got failures: %+v", failed)
	}

	result, err := svc.FireRuleNow(rule.Name, nil)
	if err != nil {
		t.Fatalf("FireRuleNow: %v", err)
	}
	if result.Successful != 1 || result.Failed != 0 {
		t.Fatalf("unexpected fire result: %+v", result)
	}
	if runner.callCount() != 1 {
		t.Fatalf("expected one RunTask call, got %d", runner.callCount())
	}

	runner.mu.Lock()
	input := runner.calls[0]
	runner.mu.Unlock()
	if input.Cluster != target.Arn || input.TaskDefinition != target.EcsParameters.TaskDefinitionArn || input.Count != 2 || input.LaunchType != types.LaunchTypeFargate {
		t.Fatalf("unexpected RunTask input: %+v", input)
	}
	if input.Overrides == nil || len(input.Overrides.ContainerOverrides) != 1 {
		t.Fatalf("expected one container override, got %+v", input.Overrides)
	}
	container := input.Overrides.ContainerOverrides[0]
	if container.Name != "app" || len(container.Command) != 1 || container.Command[0] != "worker" {
		t.Fatalf("unexpected container override: %+v", container)
	}
	var eventPayload string
	for _, env := range container.Environment {
		if env.Name == "EVENT_PAYLOAD" {
			eventPayload = env.Value
		}
	}
	if eventPayload != `{"source": "aws.events"}` {
		t.Fatalf("EVENT_PAYLOAD=%q, want transformed event", eventPayload)
	}

	traces := traceStore.Recent(1)
	if len(traces) != 1 {
		t.Fatalf("expected one trace, got %d", len(traces))
	}
	var ecsSpan *tracesvc.Span
	for i := range traces[0].Spans {
		if traces[0].Spans[i].Kind == "ecs" {
			ecsSpan = &traces[0].Spans[i]
			break
		}
	}
	if ecsSpan == nil || ecsSpan.Name != "order-worker" || ecsSpan.Status != "ok" || ecsSpan.DurationMs < 0 {
		t.Fatalf("unexpected ECS span: %+v", ecsSpan)
	}
	if ecsSpan.Meta["taskDefinitionFamily"] != "order-worker" {
		t.Fatalf("unexpected ECS span metadata: %+v", ecsSpan.Meta)
	}
}

func TestEventBridgeECSTargetReadsContainerOverridesFromTargetInput(t *testing.T) {
	svc, _ := newService(t)
	runner := &fakeTaskRunner{}
	svc.SetTaskRunner(runner)

	rule, err := svc.PutRule("ecs-input-target", "rate(1 minute)", "", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	input := `{"containerOverrides":[{"name":"app","command":["process"],"environment":[{"name":"STATIC","value":"yes"}]}]}`
	failed, err := svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:    "ecs-input",
		Arn:   "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		Input: input,
		EcsParameters: &types.EcsParameters{
			TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1",
		},
	}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("expected target to be accepted, got failures: %+v", failed)
	}

	result, err := svc.FireRuleNow(rule.Name, nil)
	if err != nil {
		t.Fatalf("FireRuleNow: %v", err)
	}
	if result.Successful != 1 || result.Failed != 0 {
		t.Fatalf("unexpected fire result: %+v", result)
	}
	if runner.callCount() != 1 {
		t.Fatalf("expected one RunTask call, got %d", runner.callCount())
	}
	runner.mu.Lock()
	got := runner.calls[0]
	runner.mu.Unlock()
	if got.Overrides == nil || len(got.Overrides.ContainerOverrides) != 1 {
		t.Fatalf("expected overrides from target Input, got %+v", got.Overrides)
	}
	override := got.Overrides.ContainerOverrides[0]
	if override.Name != "app" || len(override.Command) != 1 || override.Command[0] != "process" {
		t.Fatalf("unexpected decoded override: %+v", override)
	}
	// The target's Input document was consumed as the ECS containerOverrides
	// document, so EVENT_PAYLOAD must carry the original matched event, not
	// that override document -- otherwise the container receives its own
	// launch instructions instead of "the event."
	if got.EventPayload == nil || string(got.EventPayload) == input {
		t.Fatalf("event payload = %q, want the original event, not the containerOverrides document", got.EventPayload)
	}
	if !strings.Contains(string(got.EventPayload), `"source":"aws.events"`) {
		t.Fatalf("event payload = %q, want the scheduled event envelope", got.EventPayload)
	}
	var eventEnv string
	for _, env := range override.Environment {
		if env.Name == "EVENT_PAYLOAD" {
			eventEnv = env.Value
		}
	}
	if eventEnv != string(got.EventPayload) {
		t.Fatalf("EVENT_PAYLOAD env=%q, want it to match RunTaskInput.EventPayload=%q", eventEnv, got.EventPayload)
	}
}

// TestBuildECSTaskInputLegacyContainerOverridesKeepsPayloadAsEvent covers the
// "otherwise" half of the EVENT_PAYLOAD fix: when overrides come from the
// EcsParameters.ContainerOverrides compatibility fallback (not parsed out of
// payload), payload was never consumed as an override document, so it must
// still be delivered as EVENT_PAYLOAD even if payload happens to also
// contain a containerOverrides-shaped key.
func TestBuildECSTaskInputLegacyContainerOverridesKeepsPayloadAsEvent(t *testing.T) {
	target := &types.EventBridgeTarget{
		Arn: "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		EcsParameters: &types.EcsParameters{
			TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1",
			ContainerOverrides: []types.ContainerOverride{
				{Name: "app", Command: []string{"legacy"}},
			},
		},
	}
	payload := []byte(`{"containerOverrides":[{"name":"other","command":["ignored"]}]}`)
	eventPayload := []byte(`{"source":"aws.events","detail-type":"Scheduled Event"}`)

	runInput, err := buildECSTaskInput(target, payload, eventPayload)
	if err != nil {
		t.Fatalf("buildECSTaskInput: %v", err)
	}
	if runInput.Overrides == nil || len(runInput.Overrides.ContainerOverrides) != 1 || runInput.Overrides.ContainerOverrides[0].Name != "app" {
		t.Fatalf("expected the legacy EcsParameters override, got %+v", runInput.Overrides)
	}
	if string(runInput.EventPayload) != string(payload) {
		t.Fatalf("EventPayload=%q, want the target payload %q (legacy overrides never consume payload)", runInput.EventPayload, payload)
	}
}

// TestBuildECSTaskInputOverrideDocumentSwapsInOriginalEvent is the direct
// unit-level counterpart to the containerOverrides-from-Input dispatch test:
// when payload is parsed as the override document, EVENT_PAYLOAD and
// RunTaskInput.EventPayload must both carry eventPayload, not payload.
func TestBuildECSTaskInputOverrideDocumentSwapsInOriginalEvent(t *testing.T) {
	target := &types.EventBridgeTarget{
		Arn: "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		EcsParameters: &types.EcsParameters{
			TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1",
		},
	}
	payload := []byte(`{"containerOverrides":[{"name":"app","command":["process"]}]}`)
	eventPayload := []byte(`{"source":"aws.events","detail-type":"Scheduled Event"}`)

	runInput, err := buildECSTaskInput(target, payload, eventPayload)
	if err != nil {
		t.Fatalf("buildECSTaskInput: %v", err)
	}
	if string(runInput.EventPayload) != string(eventPayload) {
		t.Fatalf("EventPayload=%q, want the original event %q", runInput.EventPayload, eventPayload)
	}
}

func TestPutTargetsValidatesECSTargetARNAndTaskDefinition(t *testing.T) {
	svc, _ := newService(t)
	rule, err := svc.PutRule("ecs-validation", "rate(1 minute)", "", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}

	failed, err := svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:  "bad-cluster",
		Arn: "arn:aws:ecs:us-east-1:000000000000:service/not-a-cluster",
		EcsParameters: &types.EcsParameters{
			TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1",
		},
	}})
	if err != nil {
		t.Fatalf("PutTargets bad cluster: %v", err)
	}
	if len(failed) != 1 || !strings.Contains(failed[0].ErrorMessage, "ECS cluster") {
		t.Fatalf("expected cluster validation failure, got %+v", failed)
	}

	failed, err = svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:  "bad-task-definition",
		Arn: "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		EcsParameters: &types.EcsParameters{
			TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:service/worker",
		},
	}})
	if err != nil {
		t.Fatalf("PutTargets bad task definition: %v", err)
	}
	if len(failed) != 1 || !strings.Contains(failed[0].ErrorMessage, "TaskDefinitionArn") {
		t.Fatalf("expected task-definition validation failure, got %+v", failed)
	}
}

// TestPutTargetsEchoesECSNetworkConfiguration guards the third Terraform
// bug: aws_cloudwatch_event_target's ecs_target.network_configuration is
// ForceNew, so ListTargetsByRule must echo back exactly what PutTargets was
// given or every subsequent plan sees drift and replaces the target.
func TestPutTargetsEchoesECSNetworkConfiguration(t *testing.T) {
	svc, _ := newService(t)
	rule, err := svc.PutRule("ecs-network-config", "rate(1 minute)", "", "ENABLED", "", "default")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}

	netCfg := &types.EcsNetworkConfiguration{
		AwsvpcConfiguration: &types.EcsAwsVpcConfiguration{
			Subnets:        []string{"subnet-1", "subnet-2"},
			SecurityGroups: []string{"sg-1"},
			AssignPublicIp: "ENABLED",
		},
	}
	failed, err := svc.PutTargets(rule.Name, "default", []types.EventBridgeTarget{{
		ID:  "ecs-network-target",
		Arn: "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		EcsParameters: &types.EcsParameters{
			TaskDefinitionArn:    "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1",
			LaunchType:           types.LaunchTypeFargate,
			NetworkConfiguration: netCfg,
		},
	}})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("expected PutTargets to succeed, got failures: %+v", failed)
	}

	targets, _, err := svc.ListTargetsByRule(rule.Name, "default", 0, "")
	if err != nil {
		t.Fatalf("ListTargetsByRule: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	params := targets[0].EcsParameters
	if params == nil || params.NetworkConfiguration == nil || params.NetworkConfiguration.AwsvpcConfiguration == nil {
		t.Fatalf("expected NetworkConfiguration to be echoed back, got %+v", params)
	}
	avc := params.NetworkConfiguration.AwsvpcConfiguration
	if len(avc.Subnets) != 2 || avc.Subnets[0] != "subnet-1" || len(avc.SecurityGroups) != 1 || avc.SecurityGroups[0] != "sg-1" || avc.AssignPublicIp != "ENABLED" {
		t.Fatalf("network configuration not echoed exactly, got %+v", avc)
	}

	// Mutating the caller's input after PutTargets must not retroactively
	// change the stored target (PutTargets must deep-copy, not alias).
	netCfg.AwsvpcConfiguration.Subnets[0] = "mutated"
	targetsAgain, _, err := svc.ListTargetsByRule(rule.Name, "default", 0, "")
	if err != nil {
		t.Fatalf("ListTargetsByRule: %v", err)
	}
	if targetsAgain[0].EcsParameters.NetworkConfiguration.AwsvpcConfiguration.Subnets[0] != "subnet-1" {
		t.Fatal("stored target aliases the caller's NetworkConfiguration slice instead of cloning it")
	}
}

func TestEventBridgeECSTargetConcurrencyLimit(t *testing.T) {
	svc, _ := newService(t)
	runner := &fakeTaskRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc.SetTaskRunner(runner)
	svc.ecsLimitMu.Lock()
	svc.ecsLimit = 1
	svc.ecsLimitMu.Unlock()

	target := types.EventBridgeTarget{
		ID:  "ecs-target",
		Arn: "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		EcsParameters: &types.EcsParameters{
			TaskDefinitionArn:  "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1",
			ContainerOverrides: []types.ContainerOverride{{Name: "app"}},
		},
	}
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		success, _ := svc.dispatchTarget(&target, []byte(`{"event":"first"}`), targetDispatchOptions{ruleKey: "rule"})
		if !success {
			t.Errorf("first ECS dispatch failed: %s", target.LastResult)
		}
	}()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("first RunTask call did not start")
	}

	second := target
	second.ID = "ecs-target-2"
	success, _ := svc.dispatchTarget(&second, []byte(`{"event":"second"}`), targetDispatchOptions{ruleKey: "rule"})
	if success {
		t.Fatal("expected second ECS dispatch to be throttled")
	}
	if !strings.Contains(second.LastResult, "ThrottlingException") {
		t.Fatalf("expected clear throttling result, got %q", second.LastResult)
	}
	if runner.callCount() != 1 {
		t.Fatalf("throttled dispatch reached runner: %d calls", runner.callCount())
	}

	close(runner.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first ECS dispatch did not finish")
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
	_, failedCount, err = svc.PutEvents([]types.PutEventsEntry{{
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
