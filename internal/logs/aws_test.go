package logs

import (
	"encoding/json"
	"fmt"
	"github.com/aircwo-systems/tarn/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAWSLogGroupLifecycleTagsAndRetention(t *testing.T) {
	cfg := config.Default()
	cfg.Region = "eu-west-2"
	cfg.AccountID = "123456789012"
	svc := NewService(cfg)
	call := func(action, body string, status int) map[string]json.RawMessage {
		t.Helper()
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("X-Amz-Target", "Logs_20140328."+action)
		w := httptest.NewRecorder()
		svc.DispatchAWS(w, r)
		if w.Code != status {
			t.Fatalf("%s status=%d body=%s", action, w.Code, w.Body)
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	call("CreateLogGroup", `{"logGroupName":"/aws/lambda/audit","tags":{"project":"events"}}`, 200)
	call("CreateLogGroup", `{"logGroupName":"/aws/lambda/audit"}`, 400)
	call("PutRetentionPolicy", `{"logGroupName":"/aws/lambda/audit","retentionInDays":7}`, 200)
	call("PutRetentionPolicy", `{"logGroupName":"/aws/lambda/audit","retentionInDays":8}`, 400)
	arn := "arn:aws:logs:eu-west-2:123456789012:log-group:/aws/lambda/audit"
	out := call("DescribeLogGroups", `{"logGroupNamePrefix":"/aws/lambda/"}`, 200)
	var groups []struct {
		ARN             string
		LogGroupArn     string
		RetentionInDays int
		CreationTime    int64
	}
	if err := json.Unmarshal(out["logGroups"], &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ARN != arn+":*" || groups[0].LogGroupArn != arn || groups[0].RetentionInDays != 7 || groups[0].CreationTime == 0 {
		t.Fatalf("groups=%+v", groups)
	}
	out = call("ListTagsForResource", fmt.Sprintf(`{"resourceArn":%q}`, arn), 200)
	if string(out["tags"]) != `{"project":"events"}` {
		t.Fatalf("tags=%s", out["tags"])
	}
	call("TagResource", fmt.Sprintf(`{"resourceArn":%q,"tags":{"team":"infra"}}`, arn), 200)
	call("UntagResource", fmt.Sprintf(`{"resourceArn":%q,"tagKeys":["project"]}`, arn), 200)
	out = call("ListTagsForResource", fmt.Sprintf(`{"resourceArn":%q}`, arn), 200)
	if string(out["tags"]) != `{"team":"infra"}` {
		t.Fatalf("tags=%s", out["tags"])
	}
	svc.PutLogEvents("/aws/lambda/audit", "invocation", []LogEvent{{Timestamp: time.Now().Add(-8 * 24 * time.Hour), Message: "expired"}, {Timestamp: time.Now(), Message: "keep"}})
	if n := svc.PruneOlderThan(time.Now().Add(-30 * 24 * time.Hour)); n != 1 {
		t.Fatalf("retention pruned %d, want 1", n)
	}
	call("DeleteRetentionPolicy", `{"logGroupName":"/aws/lambda/audit"}`, 200)
	call("DeleteLogGroup", `{"logGroupName":"/aws/lambda/audit"}`, 200)
	out = call("DescribeLogGroups", `{"logGroupNamePrefix":"/aws/lambda/"}`, 200)
	if string(out["logGroups"]) != "[]" {
		t.Fatalf("deleted group remains: %s", out["logGroups"])
	}
	call("DeleteLogGroup", `{"logGroupName":"/aws/lambda/audit"}`, 400)
}
