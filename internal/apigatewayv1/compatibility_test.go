package apigatewayv1

import (
	"context"
	"errors"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
	"net/http"
	"os"
	"testing"
)

func TestSQSProxyImportAPIKeyDeliveryAndRestart(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	cfg.Region = "eu-west-2"
	cfg.AccountID = "123456789012"
	sent := []string{}
	send := func(queue, body string, _ map[string]*types.MessageAttribute, _, _ string) (string, string, error) {
		if queue != "messages" {
			t.Fatalf("queue=%s", queue)
		}
		sent = append(sent, body)
		return "id", "md5", nil
	}
	s := NewService(cfg, nil, send)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	api, err := s.CreateAPI("messages", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("testdata/sqs-proxy-openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutRestAPI(api.ID, "overwrite", body); err != nil {
		t.Fatal(err)
	}
	// Invalid imports must leave the old resource tree intact.
	if _, err := s.PutRestAPI(api.ID, "overwrite", []byte(`{"openapi":"3.0.1","paths":{"/broken":{"post":{"security":[{"unknown":[]}]}}}}`)); err == nil {
		t.Fatal("unsupported security silently accepted")
	}
	resources, err := s.ListResources(api.ID)
	if err != nil || len(resources) != 3 {
		t.Fatalf("invalid import changed resources: %v %v", resources, err)
	}
	dep, err := s.CreateDeployment(api.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateStage(api.ID, "v1", dep.ID, ""); err != nil {
		t.Fatal(err)
	}
	enabled := true
	key, err := s.CreateAPIKey(CreateAPIKeyInput{Name: "messages", Enabled: &enabled, Tags: map[string]string{"project": "events"}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.CreateUsagePlan(CreateUsagePlanInput{Name: "messages", APIStages: []APIStage{{APIID: api.ID, Stage: "v1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUsagePlanKey(plan.ID, CreateUsagePlanKeyInput{KeyID: key.ID, KeyType: "API_KEY"}); err != nil {
		t.Fatal(err)
	}
	invoke := func(keyValue string, want int) {
		t.Helper()
		out, err := s.Invoke(context.Background(), &InvokeInput{APIID: api.ID, Stage: "v1", Method: "POST", Path: "/messages/enqueue", Headers: http.Header{"Content-Type": []string{"application/json"}, "X-Api-Key": []string{keyValue}}, Body: []byte(`{"message":"a+b & c"}`)})
		if err != nil || out.StatusCode != want {
			t.Fatalf("invoke=%+v err=%v want=%d", out, err, want)
		}
	}
	invoke("", 403)
	invoke("wrong-key", 403)
	invoke(key.Value, 200)
	if len(sent) != 1 || sent[0] != `{"message":"a+b & c"}` {
		t.Fatalf("request mapping changed body: %v", sent)
	}
	s.store.Close()
	s = NewService(cfg, nil, send)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.store.Close()
	invoke(key.Value, 200)
	got, err := s.GetAPIKey(key.ID, false)
	if err != nil || got.Value != "" || got.Tags["project"] != "events" {
		t.Fatalf("key readback: %+v %v", got, err)
	}
	if _, err := s.UpdateAPIKey(key.ID, []PatchOp{{Op: "replace", Path: "/enabled", Value: "false"}}); err != nil {
		t.Fatal(err)
	}
	invoke(key.Value, 403)
	if err := s.DeleteUsagePlanKey(plan.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUsagePlan(plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAPIKey(key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAPIKey(key.ID, false); !errors.Is(err, ErrAccessNotFound) {
		t.Fatal("deleted API key still exists")
	}
}
