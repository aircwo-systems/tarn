package eventbridge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openstack-project/openstack/internal/config"
	eventbridgesvc "github.com/openstack-project/openstack/internal/eventbridge"
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
			{"Key": "team", "Value": "openstack"},
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
