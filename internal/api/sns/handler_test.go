package sns

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	snssvc "github.com/aircwo-systems/tarn/internal/sns"
)

func TestIsSNSRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Version=2010-03-31"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !IsSNSRequest(req) {
		t.Fatal("expected request with Version=2010-03-31 to be treated as SNS request")
	}
}

func TestDispatchUnknownActionReturnsEmptyOK(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=UnknownAction"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<UnknownActionResponse") {
		t.Fatalf("expected response wrapper for unknown action, got: %s", rec.Body.String())
	}
}

func TestTagResourceResponsesIncludeResult(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	h := NewHandler(snssvc.NewService(cfg, nil, nil))
	topic, err := h.svc.CreateTopic("tagged-topic", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, action := range []string{"TagResource", "UntagResource"} {
		t.Run(action, func(t *testing.T) {
			form := url.Values{"Action": {action}, "Version": {"2010-03-31"}, "ResourceArn": {topic.TopicArn}}
			if action == "TagResource" {
				form.Set("Tags.member.1.Key", "k")
				form.Set("Tags.member.1.Value", "v")
			} else {
				form.Set("TagKeys.member.1", "k")
			}
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			h.Dispatch(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "<"+action+"Result/>") {
				t.Fatalf("%sResult node missing: %s", action, rec.Body.String())
			}
			tags, err := h.svc.ListTopicTags(topic.TopicArn)
			if err != nil {
				t.Fatal(err)
			}
			if action == "TagResource" && tags["k"] != "v" {
				t.Fatalf("tag was not applied: %v", tags)
			}
			if action == "UntagResource" && len(tags) != 0 {
				t.Fatalf("tag was not removed: %v", tags)
			}
		})
	}
}
