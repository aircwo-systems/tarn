package eventbridge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	eventbridgesvc "github.com/aircwo-systems/tarn/internal/eventbridge"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	store := eventbridgesvc.NewStore(cfg)
	svc := eventbridgesvc.NewService(cfg, store, nil)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}
	return NewHandler(svc)
}

func invoke(t *testing.T, h *Handler, action string, reqBody any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "AWSEvents."+action)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	return rec
}

func TestPutRuleDescribeAndTargetsLifecycle(t *testing.T) {
	h := newTestHandler(t)

	put := invoke(t, h, "PutRule", map[string]any{
		"Name":               "rule-a",
		"ScheduleExpression": "rate(1 minute)",
		"State":              "ENABLED",
	})
	if put.Code != http.StatusOK {
		t.Fatalf("PutRule status=%d body=%s", put.Code, put.Body.String())
	}

	desc := invoke(t, h, "DescribeRule", map[string]any{"Name": "rule-a"})
	if desc.Code != http.StatusOK {
		t.Fatalf("DescribeRule status=%d body=%s", desc.Code, desc.Body.String())
	}
	var descBody map[string]any
	if err := json.NewDecoder(desc.Body).Decode(&descBody); err != nil {
		t.Fatalf("decode describe: %v", err)
	}
	if descBody["Name"] != "rule-a" {
		t.Fatalf("describe name=%v", descBody["Name"])
	}

	putTargets := invoke(t, h, "PutTargets", map[string]any{
		"Rule": "rule-a",
		"Targets": []map[string]any{{
			"Id":  "t1",
			"Arn": "worker-a",
		}},
	})
	if putTargets.Code != http.StatusOK {
		t.Fatalf("PutTargets status=%d body=%s", putTargets.Code, putTargets.Body.String())
	}

	listTargets := invoke(t, h, "ListTargetsByRule", map[string]any{"Rule": "rule-a"})
	if listTargets.Code != http.StatusOK {
		t.Fatalf("ListTargetsByRule status=%d body=%s", listTargets.Code, listTargets.Body.String())
	}
	var listTargetsBody struct {
		Targets []map[string]any `json:"Targets"`
	}
	if err := json.NewDecoder(listTargets.Body).Decode(&listTargetsBody); err != nil {
		t.Fatalf("decode list targets: %v", err)
	}
	if len(listTargetsBody.Targets) != 1 {
		t.Fatalf("targets len=%d", len(listTargetsBody.Targets))
	}

	deleteWithTargets := invoke(t, h, "DeleteRule", map[string]any{"Name": "rule-a"})
	if deleteWithTargets.Code != http.StatusBadRequest {
		t.Fatalf("DeleteRule with targets status=%d body=%s", deleteWithTargets.Code, deleteWithTargets.Body.String())
	}

	removeTargets := invoke(t, h, "RemoveTargets", map[string]any{
		"Rule": "rule-a",
		"Ids":  []string{"t1"},
	})
	if removeTargets.Code != http.StatusOK {
		t.Fatalf("RemoveTargets status=%d body=%s", removeTargets.Code, removeTargets.Body.String())
	}

	delete := invoke(t, h, "DeleteRule", map[string]any{"Name": "rule-a"})
	if delete.Code != http.StatusOK {
		t.Fatalf("DeleteRule status=%d body=%s", delete.Code, delete.Body.String())
	}
}

func TestPutRuleRejectsInvalidSchedule(t *testing.T) {
	h := newTestHandler(t)
	resp := invoke(t, h, "PutRule", map[string]any{
		"Name":               "bad-rule",
		"ScheduleExpression": "rate(1 minutes)",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestResourceTagsLifecycle(t *testing.T) {
	h := newTestHandler(t)

	put := invoke(t, h, "PutRule", map[string]any{
		"Name":               "tagged-rule",
		"ScheduleExpression": "rate(1 minute)",
	})
	if put.Code != http.StatusOK {
		t.Fatalf("PutRule status=%d body=%s", put.Code, put.Body.String())
	}
	var putBody struct {
		RuleArn string `json:"RuleArn"`
	}
	if err := json.NewDecoder(put.Body).Decode(&putBody); err != nil {
		t.Fatalf("decode PutRule: %v", err)
	}
	if putBody.RuleArn == "" {
		t.Fatalf("expected RuleArn")
	}

	tag := invoke(t, h, "TagResource", map[string]any{
		"ResourceARN": putBody.RuleArn,
		"Tags": []map[string]string{
			{"Key": "env", "Value": "test"},
			{"Key": "team", "Value": "tarn"},
		},
	})
	if tag.Code != http.StatusOK {
		t.Fatalf("TagResource status=%d body=%s", tag.Code, tag.Body.String())
	}

	list := invoke(t, h, "ListTagsForResource", map[string]any{
		"ResourceARN": putBody.RuleArn,
	})
	if list.Code != http.StatusOK {
		t.Fatalf("ListTagsForResource status=%d body=%s", list.Code, list.Body.String())
	}
	var listBody struct {
		Tags []struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
		} `json:"Tags"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list tags: %v", err)
	}
	if len(listBody.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(listBody.Tags))
	}

	untag := invoke(t, h, "UntagResource", map[string]any{
		"ResourceARN": putBody.RuleArn,
		"TagKeys":     []string{"team"},
	})
	if untag.Code != http.StatusOK {
		t.Fatalf("UntagResource status=%d body=%s", untag.Code, untag.Body.String())
	}

	listAfter := invoke(t, h, "ListTagsForResource", map[string]any{
		"ResourceARN": putBody.RuleArn,
	})
	if listAfter.Code != http.StatusOK {
		t.Fatalf("ListTagsForResource after untag status=%d body=%s", listAfter.Code, listAfter.Body.String())
	}
	var listAfterBody struct {
		Tags []struct {
			Key string `json:"Key"`
		} `json:"Tags"`
	}
	if err := json.NewDecoder(listAfter.Body).Decode(&listAfterBody); err != nil {
		t.Fatalf("decode list tags after: %v", err)
	}
	if len(listAfterBody.Tags) != 1 {
		t.Fatalf("unexpected tags after untag: %+v", listAfterBody.Tags)
	}
	if listAfterBody.Tags[0].Key != "env" {
		t.Fatalf("expected remaining tag env, got %+v", listAfterBody.Tags[0])
	}
}

func TestUnknownActionReturnsEmptyOK(t *testing.T) {
	h := newTestHandler(t)
	rec := invoke(t, h, "UnknownAction", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "{}\n" {
		t.Fatalf("expected empty json object response, got: %q", rec.Body.String())
	}
}

func TestPutTargetsSQSWireShapeRoundTrip(t *testing.T) {
	h := newTestHandler(t)
	if rec := invoke(t, h, "PutRule", map[string]any{"Name": "users", "EventPattern": `{"source":["app.users"]}`}); rec.Code != http.StatusOK {
		t.Fatalf("PutRule status=%d body=%s", rec.Code, rec.Body.String())
	}

	put := invoke(t, h, "PutTargets", map[string]any{
		"Rule": "users",
		"Targets": []map[string]any{
			{
				"Id":               "audit",
				"Arn":              "arn:aws:sqs:us-east-1:000000000000:audit.fifo",
				"SqsParameters":    map[string]any{"MessageGroupId": "users"},
				"DeadLetterConfig": map[string]any{"Arn": "arn:aws:sqs:us-east-1:000000000000:audit-dlq"},
				"RetryPolicy":      map[string]any{"MaximumRetryAttempts": 0, "MaximumEventAgeInSeconds": 60},
			},
			{"Id": "fifo-missing-group", "Arn": "arn:aws:sqs:us-east-1:000000000000:other.fifo"},
		},
	})
	if put.Code != http.StatusOK {
		t.Fatalf("PutTargets status=%d body=%s", put.Code, put.Body.String())
	}
	var putBody struct {
		FailedEntryCount int `json:"FailedEntryCount"`
		FailedEntries    []struct {
			TargetID  string `json:"TargetId"`
			ErrorCode string `json:"ErrorCode"`
		} `json:"FailedEntries"`
	}
	if err := json.NewDecoder(put.Body).Decode(&putBody); err != nil {
		t.Fatal(err)
	}
	if putBody.FailedEntryCount != 1 || putBody.FailedEntries[0].TargetID != "fifo-missing-group" || putBody.FailedEntries[0].ErrorCode != "ValidationException" {
		t.Fatalf("unexpected failed entries: %+v", putBody)
	}

	list := invoke(t, h, "ListTargetsByRule", map[string]any{"Rule": "users"})
	var listBody struct {
		Targets []map[string]any `json:"Targets"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Targets) != 1 {
		t.Fatalf("targets = %+v", listBody.Targets)
	}
	target := listBody.Targets[0]
	if sqsParams, _ := target["SqsParameters"].(map[string]any); sqsParams["MessageGroupId"] != "users" {
		t.Fatalf("SqsParameters = %v", target["SqsParameters"])
	}
	if dlq, _ := target["DeadLetterConfig"].(map[string]any); dlq["Arn"] != "arn:aws:sqs:us-east-1:000000000000:audit-dlq" {
		t.Fatalf("DeadLetterConfig = %v", target["DeadLetterConfig"])
	}
	if retry, _ := target["RetryPolicy"].(map[string]any); retry["MaximumRetryAttempts"] != float64(0) || retry["MaximumEventAgeInSeconds"] != float64(60) {
		t.Fatalf("RetryPolicy = %v", target["RetryPolicy"])
	}

	byTarget := invoke(t, h, "ListRuleNamesByTarget", map[string]any{"TargetArn": "arn:aws:sqs:us-east-1:000000000000:audit.fifo"})
	var namesBody struct {
		RuleNames []string `json:"RuleNames"`
	}
	if err := json.NewDecoder(byTarget.Body).Decode(&namesBody); err != nil {
		t.Fatal(err)
	}
	if len(namesBody.RuleNames) != 1 || namesBody.RuleNames[0] != "users" {
		t.Fatalf("RuleNames = %v", namesBody.RuleNames)
	}
}

func TestEventBusLifecycleWireShape(t *testing.T) {
	h := newTestHandler(t)

	create := invoke(t, h, "CreateEventBus", map[string]any{
		"Name":        "domain-events",
		"Description": "identity events",
		"Tags":        []map[string]string{{"Key": "team", "Value": "identity"}},
	})
	if create.Code != http.StatusOK {
		t.Fatalf("CreateEventBus status=%d body=%s", create.Code, create.Body.String())
	}
	var createBody struct {
		EventBusArn string `json:"EventBusArn"`
	}
	if err := json.NewDecoder(create.Body).Decode(&createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.EventBusArn != "arn:aws:events:us-east-1:000000000000:event-bus/domain-events" {
		t.Fatalf("EventBusArn = %q", createBody.EventBusArn)
	}

	describe := invoke(t, h, "DescribeEventBus", map[string]any{"Name": "domain-events"})
	var describeBody map[string]any
	if err := json.NewDecoder(describe.Body).Decode(&describeBody); err != nil {
		t.Fatal(err)
	}
	if describeBody["Name"] != "domain-events" || describeBody["Arn"] != createBody.EventBusArn || describeBody["Description"] != "identity events" {
		t.Fatalf("DescribeEventBus = %v", describeBody)
	}
	if _, ok := describeBody["CreationTime"].(float64); !ok {
		t.Fatalf("CreationTime should be epoch seconds, got %T", describeBody["CreationTime"])
	}

	tags := invoke(t, h, "ListTagsForResource", map[string]any{"ResourceARN": createBody.EventBusArn})
	if !bytes.Contains(tags.Body.Bytes(), []byte(`"Key":"team"`)) {
		t.Fatalf("bus tags = %s", tags.Body.String())
	}

	if rec := invoke(t, h, "PutRule", map[string]any{"Name": "users", "EventPattern": `{"source":["app"]}`, "EventBusName": "domain-events"}); rec.Code != http.StatusOK {
		t.Fatalf("PutRule status=%d body=%s", rec.Code, rec.Body.String())
	}
	rule := invoke(t, h, "DescribeRule", map[string]any{"Name": "users", "EventBusName": "domain-events"})
	var ruleBody map[string]any
	if err := json.NewDecoder(rule.Body).Decode(&ruleBody); err != nil {
		t.Fatal(err)
	}
	if ruleBody["EventBusName"] != "domain-events" || ruleBody["Arn"] != "arn:aws:events:us-east-1:000000000000:rule/domain-events/users" {
		t.Fatalf("DescribeRule = %v", ruleBody)
	}

	list := invoke(t, h, "ListEventBuses", map[string]any{})
	var listBody struct {
		EventBuses []map[string]any `json:"EventBuses"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.EventBuses) != 2 || listBody.EventBuses[0]["Name"] != "default" || listBody.EventBuses[1]["Name"] != "domain-events" {
		t.Fatalf("ListEventBuses = %v", listBody.EventBuses)
	}

	if rec := invoke(t, h, "DeleteEventBus", map[string]any{"Name": "domain-events"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("DeleteEventBus with rules status=%d", rec.Code)
	}
	if rec := invoke(t, h, "DeleteRule", map[string]any{"Name": "users", "EventBusName": "domain-events"}); rec.Code != http.StatusOK {
		t.Fatalf("DeleteRule status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := invoke(t, h, "DeleteEventBus", map[string]any{"Name": "domain-events"}); rec.Code != http.StatusOK {
		t.Fatalf("DeleteEventBus status=%d body=%s", rec.Code, rec.Body.String())
	}
}
