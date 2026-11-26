package eventbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const (
	testQueueARNPrefix = "arn:aws:sqs:us-east-1:000000000000:"
)

type sentMessage struct {
	queue   string
	body    string
	attrs   map[string]*types.MessageAttribute
	groupID string
	dedupID string
}

type fakeSQSSender struct {
	mu           sync.Mutex
	sent         []sentMessage
	missing      map[string]bool
	contentBased map[string]bool
}

func (f *fakeSQSSender) SendMessage(queueName, body string, _ int, attrs map[string]*types.MessageAttribute, groupID, dedupID string) (*types.SQSMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missing[queueName] {
		return nil, fmt.Errorf("queue %s not found", queueName)
	}
	f.sent = append(f.sent, sentMessage{queue: queueName, body: body, attrs: attrs, groupID: groupID, dedupID: dedupID})
	return &types.SQSMessage{MessageId: fmt.Sprintf("msg-%d", len(f.sent)), Body: body}, nil
}

func (f *fakeSQSSender) GetQueue(name string) (*types.QueueConfig, error) {
	return &types.QueueConfig{QueueName: name, FifoQueue: strings.HasSuffix(name, ".fifo"), ContentBasedDeduplication: f.contentBased[name]}, nil
}

func (f *fakeSQSSender) to(queue string) []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sentMessage
	for _, msg := range f.sent {
		if msg.queue == queue {
			out = append(out, msg)
		}
	}
	return out
}

func newSQSService(t *testing.T) (*Service, *fakeLambda, *fakeSQSSender, *tracesvc.Store) {
	t.Helper()
	svc, lambda := newService(t)
	sender := &fakeSQSSender{missing: map[string]bool{}, contentBased: map[string]bool{}}
	svc.SetSQSSender(sender)
	traces := tracesvc.NewStore()
	t.Cleanup(func() { _ = traces.Close() })
	svc.SetTraceStore(traces)
	return svc, lambda, sender, traces
}

func putPatternRule(t *testing.T, svc *Service, name string, targets ...types.EventBridgeTarget) {
	t.Helper()
	if _, err := svc.PutRule(name, "", `{"source":["app.users"]}`, "ENABLED", "", ""); err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	failed, err := svc.PutTargets(name, "", targets)
	if err != nil || len(failed) != 0 {
		t.Fatalf("PutTargets: failed=%+v err=%v", failed, err)
	}
}

func putUserEvent(t *testing.T, svc *Service) string {
	t.Helper()
	results, failed, err := svc.PutEvents([]types.PutEventsEntry{{
		Source:     "app.users",
		DetailType: "UserSignedIn",
		Detail:     `{"userId":"u-1","email":"a@example.com"}`,
	}})
	if err != nil || failed != 0 {
		t.Fatalf("PutEvents: failed=%d err=%v", failed, err)
	}
	svc.DrainDispatch()
	return results[0].EventId
}

func decodeEnvelope(t *testing.T, body string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	return envelope
}

func TestPutTargetsValidatesSQSTargets(t *testing.T) {
	svc, _, _, _ := newSQSService(t)
	if _, err := svc.PutRule("users", "", `{"source":["app.users"]}`, "ENABLED", "", ""); err != nil {
		t.Fatalf("PutRule: %v", err)
	}

	failed, err := svc.PutTargets("users", "", []types.EventBridgeTarget{
		{ID: "fifo-no-group", Arn: testQueueARNPrefix + "audit.fifo"},
		{ID: "fifo-empty-group", Arn: testQueueARNPrefix + "audit.fifo", SqsParameters: &types.SqsParameters{}},
		{ID: "standard-with-group", Arn: testQueueARNPrefix + "audit", SqsParameters: &types.SqsParameters{MessageGroupId: "g"}},
		{ID: "bad-queue-arn", Arn: "arn:aws:sqs:us-east-1:000000000000"},
		{ID: "bad-dlq", Arn: testQueueARNPrefix + "audit", DeadLetterConfig: &types.EventBridgeDeadLetterConfig{Arn: "arn:aws:sns:us-east-1:000000000000:topic"}},
		{ID: "lambda-with-sqs-params", Arn: "consumer", SqsParameters: &types.SqsParameters{MessageGroupId: "g"}},
	})
	if err != nil {
		t.Fatalf("PutTargets: %v", err)
	}
	got := map[string]FailedEntry{}
	for _, entry := range failed {
		got[entry.TargetID] = entry
	}
	for _, id := range []string{"fifo-no-group", "fifo-empty-group", "standard-with-group", "bad-queue-arn", "bad-dlq", "lambda-with-sqs-params"} {
		entry, ok := got[id]
		if !ok || entry.ErrorCode != "ValidationException" {
			t.Errorf("target %s: expected ValidationException failed entry, got %+v (ok=%v)", id, entry, ok)
		}
	}
	if targets, _, _ := svc.ListTargetsByRule("users", "", 0, ""); len(targets) != 0 {
		t.Fatalf("invalid targets were stored: %+v", targets)
	}
}

func TestPutTargetsEchoesSQSParametersDeadLetterConfigAndRetryPolicy(t *testing.T) {
	svc, _, _, _ := newSQSService(t)
	zero, age := 0, 600
	putPatternRule(t, svc, "users",
		types.EventBridgeTarget{
			ID:               "audit",
			Arn:              testQueueARNPrefix + "audit.fifo",
			SqsParameters:    &types.SqsParameters{MessageGroupId: "users"},
			DeadLetterConfig: &types.EventBridgeDeadLetterConfig{Arn: testQueueARNPrefix + "audit-dlq"},
			RetryPolicy:      &types.RetryPolicy{MaximumRetryAttempts: &zero, MaximumEventAgeInSeconds: &age},
		},
		types.EventBridgeTarget{ID: "email", Arn: testQueueARNPrefix + "email"},
	)

	targets, _, err := svc.ListTargetsByRule("users", "", 0, "")
	if err != nil {
		t.Fatalf("ListTargetsByRule: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %+v", targets)
	}
	audit := targets[0]
	if audit.Arn != testQueueARNPrefix+"audit.fifo" || audit.SqsParameters == nil || audit.SqsParameters.MessageGroupId != "users" {
		t.Fatalf("SqsParameters not echoed: %+v", audit)
	}
	if audit.DeadLetterConfig == nil || audit.DeadLetterConfig.Arn != testQueueARNPrefix+"audit-dlq" {
		t.Fatalf("DeadLetterConfig not echoed: %+v", audit.DeadLetterConfig)
	}
	if audit.RetryPolicy == nil || audit.RetryPolicy.MaximumRetryAttempts == nil || *audit.RetryPolicy.MaximumRetryAttempts != 0 || *audit.RetryPolicy.MaximumEventAgeInSeconds != 600 {
		t.Fatalf("RetryPolicy not echoed: %+v", audit.RetryPolicy)
	}

	// The stored copy must not alias the response.
	audit.SqsParameters.MessageGroupId = "mutated"
	again, _, _ := svc.ListTargetsByRule("users", "", 0, "")
	if again[0].SqsParameters.MessageGroupId != "users" {
		t.Fatal("ListTargetsByRule returned an aliased SqsParameters")
	}

	names, _, err := svc.ListRuleNamesByTarget(testQueueARNPrefix+"email", "", 0, "")
	if err != nil || len(names) != 1 || names[0] != "users" {
		t.Fatalf("ListRuleNamesByTarget = %v, %v", names, err)
	}
	if _, _, err := svc.ListRuleNamesByTarget("arn:aws:sqs:us-east-1:000000000000", "", 0, ""); err == nil {
		t.Fatal("expected invalid SQS TargetArn to be rejected")
	}
}

func TestPutEventsDeliversFullEnvelopeToStandardSQSTarget(t *testing.T) {
	svc, _, sender, traces := newSQSService(t)
	putPatternRule(t, svc, "users", types.EventBridgeTarget{ID: "email", Arn: testQueueARNPrefix + "email"})

	eventID := putUserEvent(t, svc)

	msgs := sender.to("email")
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v", sender.sent)
	}
	msg := msgs[0]
	if msg.groupID != "" || msg.dedupID != "" {
		t.Fatalf("standard queue got FIFO fields: %+v", msg)
	}
	envelope := decodeEnvelope(t, msg.body)
	if envelope["id"] != eventID || envelope["source"] != "app.users" || envelope["detail-type"] != "UserSignedIn" {
		t.Fatalf("unexpected envelope: %v", envelope)
	}
	detail, _ := envelope["detail"].(map[string]any)
	if detail["userId"] != "u-1" {
		t.Fatalf("unexpected detail: %v", envelope["detail"])
	}
	corr := msg.attrs["correlationId"]
	if corr == nil || corr.StringValue == "" {
		t.Fatalf("expected correlationId attribute, got %+v", msg.attrs)
	}

	targets, _, _ := svc.ListTargetsByRule("users", "", 0, "")
	if targets[0].LastResult != "OK" || targets[0].LastInvokedAt == nil {
		t.Fatalf("target result not recorded: %+v", targets[0])
	}

	recent := traces.Recent(10)
	if len(recent) != 1 || recent[0].CorrelationID != corr.StringValue {
		t.Fatalf("trace correlation mismatch: %+v", recent)
	}
	var sqsSpan *tracesvc.Span
	for i := range recent[0].Spans {
		if recent[0].Spans[i].Kind == "sqs" {
			sqsSpan = &recent[0].Spans[i]
		}
	}
	if sqsSpan == nil || sqsSpan.Name != "email" || sqsSpan.Status != "ok" || sqsSpan.Meta["messageId"] == "" {
		t.Fatalf("missing sqs span: %+v", recent[0].Spans)
	}
}

func TestPutEventsDeliversToFIFOTargetWithGroupAndDedup(t *testing.T) {
	svc, _, sender, _ := newSQSService(t)
	fifo := types.EventBridgeTarget{ID: "audit", Arn: testQueueARNPrefix + "audit.fifo", SqsParameters: &types.SqsParameters{MessageGroupId: "users"}}
	putPatternRule(t, svc, "users-a", fifo)
	putPatternRule(t, svc, "users-b", fifo)

	putUserEvent(t, svc)

	msgs := sender.to("audit.fifo")
	if len(msgs) != 2 {
		t.Fatalf("expected one message per rule, got %+v", msgs)
	}
	for _, msg := range msgs {
		if msg.groupID != "users" {
			t.Fatalf("MessageGroupId = %q", msg.groupID)
		}
		if msg.dedupID == "" || len(msg.dedupID) > 128 {
			t.Fatalf("invalid MessageDeduplicationId %q", msg.dedupID)
		}
	}
	if msgs[0].dedupID == msgs[1].dedupID {
		t.Fatal("two rules delivering the same event must not share a deduplication ID")
	}
}

func TestFIFOTargetOnContentBasedQueueDedupesOnBody(t *testing.T) {
	svc, _, sender, _ := newSQSService(t)
	sender.contentBased["audit.fifo"] = true
	putPatternRule(t, svc, "users", types.EventBridgeTarget{
		ID: "audit", Arn: testQueueARNPrefix + "audit.fifo",
		SqsParameters: &types.SqsParameters{MessageGroupId: "users"},
		Input:         `{"static":true}`,
	})

	putUserEvent(t, svc)
	putUserEvent(t, svc)

	msgs := sender.to("audit.fifo")
	if len(msgs) != 2 || msgs[0].dedupID != msgs[1].dedupID {
		t.Fatalf("content-based dedup IDs should match for identical bodies: %+v", msgs)
	}
}

func TestInputTransformerAndInputPathApplyToSQSTargets(t *testing.T) {
	svc, _, sender, _ := newSQSService(t)
	putPatternRule(t, svc, "users",
		types.EventBridgeTarget{
			ID:  "transformed",
			Arn: testQueueARNPrefix + "transformed",
			InputTransformer: &types.InputTransformer{
				InputPathsMap: map[string]string{"user": "$.detail.userId", "kind": "$.detail-type"},
				InputTemplate: `{"user":<user>,"kind":<kind>}`,
			},
		},
		types.EventBridgeTarget{ID: "detail-only", Arn: testQueueARNPrefix + "detail-only", InputPath: "$.detail"},
		types.EventBridgeTarget{ID: "constant", Arn: testQueueARNPrefix + "constant", Input: `{"hello":"world"}`},
	)

	putUserEvent(t, svc)

	if msgs := sender.to("transformed"); len(msgs) != 1 || msgs[0].body != `{"user":"u-1","kind":"UserSignedIn"}` {
		t.Fatalf("InputTransformer body = %+v", msgs)
	}
	if msgs := sender.to("detail-only"); len(msgs) != 1 || decodeEnvelope(t, msgs[0].body)["userId"] != "u-1" {
		t.Fatalf("InputPath body = %+v", msgs)
	}
	if msgs := sender.to("constant"); len(msgs) != 1 || msgs[0].body != `{"hello":"world"}` {
		t.Fatalf("Input body = %+v", msgs)
	}
}

func TestPutEventsFansOutToMultipleQueuesAndLambda(t *testing.T) {
	svc, lambda, sender, traces := newSQSService(t)
	putPatternRule(t, svc, "users",
		types.EventBridgeTarget{ID: "audit", Arn: testQueueARNPrefix + "audit"},
		types.EventBridgeTarget{ID: "email", Arn: testQueueARNPrefix + "email"},
		types.EventBridgeTarget{ID: "fn", Arn: "arn:aws:lambda:us-east-1:000000000000:function:last-login"},
	)

	eventID := putUserEvent(t, svc)

	for _, queue := range []string{"audit", "email"} {
		msgs := sender.to(queue)
		if len(msgs) != 1 || decodeEnvelope(t, msgs[0].body)["id"] != eventID {
			t.Fatalf("queue %s messages = %+v", queue, msgs)
		}
	}
	if len(lambda.invocations) != 1 || lambda.invocations[0].FunctionName != "last-login" {
		t.Fatalf("lambda invocations = %+v", lambda.invocations)
	}
	recent := traces.Recent(10)
	if len(recent) != 1 {
		t.Fatalf("expected one rule trace, got %d", len(recent))
	}
	kinds := map[string]int{}
	for _, span := range recent[0].Spans {
		kinds[span.Kind]++
	}
	if kinds["eventbridge"] != 1 || kinds["sqs"] != 2 || kinds["lambda"] != 1 {
		t.Fatalf("span kinds = %v", kinds)
	}
}

func TestScheduledRuleDeliversToSQSTarget(t *testing.T) {
	svc, _, sender, _ := newSQSService(t)
	rule, err := svc.PutRule("nightly", "rate(1 hour)", "", "ENABLED", "", "")
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	if failed, err := svc.PutTargets("nightly", "", []types.EventBridgeTarget{
		{ID: "q", Arn: testQueueARNPrefix + "nightly"},
		{ID: "fifo", Arn: testQueueARNPrefix + "nightly.fifo", SqsParameters: &types.SqsParameters{MessageGroupId: "nightly"}},
	}); err != nil || len(failed) != 0 {
		t.Fatalf("PutTargets: %+v %v", failed, err)
	}

	result, err := svc.fireRule("default", "nightly", true, nil)
	if err != nil {
		t.Fatalf("fireRule: %v", err)
	}
	if result.Successful != 2 || result.Failed != 0 {
		t.Fatalf("fire result = %+v", result)
	}
	for _, queue := range []string{"nightly", "nightly.fifo"} {
		msgs := sender.to(queue)
		if len(msgs) != 1 {
			t.Fatalf("queue %s messages = %+v", queue, sender.sent)
		}
		envelope := decodeEnvelope(t, msgs[0].body)
		if envelope["detail-type"] != "Scheduled Event" || envelope["source"] != "aws.events" {
			t.Fatalf("unexpected scheduled envelope: %v", envelope)
		}
		resources, _ := envelope["resources"].([]any)
		if len(resources) != 1 || resources[0] != rule.Arn {
			t.Fatalf("resources = %v", envelope["resources"])
		}
	}
}

func TestFailedSQSDeliveryGoesToDeadLetterQueue(t *testing.T) {
	svc, _, sender, traces := newSQSService(t)
	sender.missing["gone"] = true
	putPatternRule(t, svc, "users", types.EventBridgeTarget{
		ID:               "gone",
		Arn:              testQueueARNPrefix + "gone",
		DeadLetterConfig: &types.EventBridgeDeadLetterConfig{Arn: testQueueARNPrefix + "users-dlq"},
		InputTransformer: &types.InputTransformer{InputPathsMap: map[string]string{"u": "$.detail.userId"}, InputTemplate: `{"u":<u>}`},
	})

	eventID := putUserEvent(t, svc)

	dead := sender.to("users-dlq")
	if len(dead) != 1 {
		t.Fatalf("DLQ messages = %+v", sender.sent)
	}
	// AWS dead-letters the original event, not the transformed input.
	if decodeEnvelope(t, dead[0].body)["id"] != eventID {
		t.Fatalf("DLQ body is not the original event: %s", dead[0].body)
	}
	want := map[string]string{
		"ERROR_CODE": "NO_RESOURCE",
		"RULE_ARN":   ruleARN(svc.cfg, "default", "users"),
		"TARGET_ARN": testQueueARNPrefix + "gone",
	}
	for key, value := range want {
		if attr := dead[0].attrs[key]; attr == nil || attr.StringValue != value || attr.DataType != "String" {
			t.Errorf("attribute %s = %+v, want %q", key, attr, value)
		}
	}
	if attr := dead[0].attrs["ERROR_MESSAGE"]; attr == nil || !strings.Contains(attr.StringValue, "not found") {
		t.Errorf("ERROR_MESSAGE = %+v", attr)
	}
	targets, _, _ := svc.ListTargetsByRule("users", "", 0, "")
	if !strings.HasPrefix(targets[0].LastResult, "ERROR:") {
		t.Fatalf("target result should keep the delivery error, got %q", targets[0].LastResult)
	}
	spans := traces.Recent(1)[0].Spans
	if spans[len(spans)-1].Kind != "dlq" || spans[len(spans)-1].Status != "ok" {
		t.Fatalf("expected trailing dlq span, got %+v", spans)
	}
}

func TestLambdaInvokeErrorGoesToDeadLetterQueueButHandlerErrorDoesNot(t *testing.T) {
	svc, lambda, sender, _ := newSQSService(t)
	dlq := &types.EventBridgeDeadLetterConfig{Arn: testQueueARNPrefix + "fn-dlq.fifo"}
	putPatternRule(t, svc, "users", types.EventBridgeTarget{ID: "fn", Arn: "consumer", DeadLetterConfig: dlq})

	lambda.err = fmt.Errorf("Function not found: consumer")
	putUserEvent(t, svc)
	dead := sender.to("fn-dlq.fifo")
	if len(dead) != 1 || dead[0].attrs["ERROR_CODE"].StringValue != "NO_RESOURCE" {
		t.Fatalf("expected one dead-lettered event, got %+v", dead)
	}
	if dead[0].groupID == "" || dead[0].dedupID == "" {
		t.Fatalf("FIFO DLQ needs group and dedup IDs: %+v", dead[0])
	}

	// A handler that runs and throws was delivered: AWS invokes Lambda
	// targets asynchronously, so it never reaches the rule target's DLQ.
	lambda.err = nil
	handlerErr := &erroringLambda{}
	svc.lambda = handlerErr
	putUserEvent(t, svc)
	if got := sender.to("fn-dlq.fifo"); len(got) != 1 {
		t.Fatalf("handler error must not dead-letter, DLQ has %d messages", len(got))
	}
	targets, _, _ := svc.ListTargetsByRule("users", "", 0, "")
	if !strings.HasPrefix(targets[0].LastResult, "ERROR:") {
		t.Fatalf("handler error should still mark the target failed, got %q", targets[0].LastResult)
	}
}

func TestSQSTargetWithoutSenderFailsCleanly(t *testing.T) {
	svc, _ := newService(t)
	putPatternRule(t, svc, "users", types.EventBridgeTarget{ID: "q", Arn: testQueueARNPrefix + "q"})
	putUserEvent(t, svc)
	targets, _, _ := svc.ListTargetsByRule("users", "", 0, "")
	if targets[0].LastResult != "ERROR: SQS service unavailable" {
		t.Fatalf("LastResult = %q", targets[0].LastResult)
	}
}

func TestStoreRoundTripKeepsSQSTargetFields(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	rule := &types.EventBridgeRule{Name: "users", EventBusName: "default", Targets: []types.EventBridgeTarget{{
		ID: "audit", Arn: testQueueARNPrefix + "audit.fifo",
		SqsParameters:    &types.SqsParameters{MessageGroupId: "users"},
		DeadLetterConfig: &types.EventBridgeDeadLetterConfig{Arn: testQueueARNPrefix + "dlq"},
	}}}
	if err := store.SaveRule(rule); err != nil {
		t.Fatal(err)
	}
	rule.Targets[0].SqsParameters.MessageGroupId = "mutated"
	store.Flush()
	store.Close()

	reloaded := NewStore(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	got, err := reloaded.GetRule("default", "users")
	if err != nil {
		t.Fatal(err)
	}
	target := got.Targets[0]
	if target.SqsParameters == nil || target.SqsParameters.MessageGroupId != "users" || target.DeadLetterConfig == nil || target.DeadLetterConfig.Arn != testQueueARNPrefix+"dlq" {
		t.Fatalf("reloaded target = %+v", target)
	}
}

type erroringLambda struct{}

func (erroringLambda) Invoke(_ context.Context, _ *types.InvokeInput) (*types.InvokeOutput, error) {
	return &types.InvokeOutput{StatusCode: 200, FunctionError: "Unhandled"}, nil
}

func ecsTargetWithDLQ() types.EventBridgeTarget {
	return types.EventBridgeTarget{
		ID:               "worker",
		Arn:              "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		EcsParameters:    &types.EcsParameters{TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:1"},
		DeadLetterConfig: &types.EventBridgeDeadLetterConfig{Arn: testQueueARNPrefix + "ecs-dlq"},
	}
}

func TestECSRunTaskErrorGoesToDeadLetterQueue(t *testing.T) {
	svc, _, sender, _ := newSQSService(t)
	svc.SetTaskRunner(&fakeTaskRunner{err: fmt.Errorf("cluster default not found")})
	putPatternRule(t, svc, "users", ecsTargetWithDLQ())

	putUserEvent(t, svc)

	dead := sender.to("ecs-dlq")
	if len(dead) != 1 || dead[0].attrs["ERROR_CODE"].StringValue != "NO_RESOURCE" || dead[0].attrs["TARGET_ARN"].StringValue != "arn:aws:ecs:us-east-1:000000000000:cluster/default" {
		t.Fatalf("DLQ messages = %+v", dead)
	}
}

func TestECSTaskThatStartsThenFailsIsNotDeadLettered(t *testing.T) {
	svc, _, sender, _ := newSQSService(t)
	exit := int64(1)
	svc.SetTaskRunner(&fakeTaskRunner{out: &types.RunTaskOutput{Tasks: []types.Task{{
		TaskArn:       "arn:aws:ecs:us-east-1:000000000000:task/default/t1",
		LastStatus:    types.TaskStatusStopped,
		StoppedReason: "Essential container in task exited",
		Containers:    []types.TaskContainer{{Name: "app", ExitCode: &exit}},
	}}}})
	putPatternRule(t, svc, "users", ecsTargetWithDLQ())

	putUserEvent(t, svc)

	targets, _, _ := svc.ListTargetsByRule("users", "", 0, "")
	if !strings.HasPrefix(targets[0].LastResult, "ERROR:") {
		t.Fatalf("stopped task should mark the target failed, got %q", targets[0].LastResult)
	}
	if dead := sender.to("ecs-dlq"); len(dead) != 0 {
		t.Fatalf("a delivered task must not be dead-lettered: %+v", dead)
	}
}
