package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// AWS protocol and configuration readback regressions through the HTTP router.
func TestTerraformCompatibility(t *testing.T) {
	request := compatibilityRequest
	t.Run("CloudWatchLogs", func(t *testing.T) {
		w := request(newTestHTTPHandler(t), "POST", "/", `{}`, "Logs_20140328.DescribeLogGroups")
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("DescribeLogGroups status=%d body=%s", w.Code, w.Body)
		}
	})
	t.Run("LambdaReadback", func(t *testing.T) {
		h := newTestHTTPHandler(t)
		w := request(h, "POST", "/2015-03-31/functions", `{"FunctionName":"audit","Runtime":"nodejs20.x","Handler":"index.handler","Architectures":["arm64"],"Environment":{"Variables":{"MODE":"local"}},"Tags":{"Project":"audit"}}`, "")
		if w.Code != 201 {
			t.Fatal(w.Body)
		}
		w = request(h, "GET", "/2015-03-31/functions/audit", "", "")
		var body struct {
			Configuration struct {
				Environment   struct{ Variables map[string]string }
				Architectures []string
			}
			Tags map[string]string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Configuration.Environment.Variables["MODE"] != "local" {
			t.Errorf("Environment.Variables missing: %s", w.Body)
		}
		if len(body.Configuration.Architectures) != 1 || body.Configuration.Architectures[0] != "arm64" {
			t.Errorf("Architectures missing")
		}
		if body.Tags["Project"] != "audit" {
			t.Errorf("GetFunction top-level Tags missing")
		}
	})
	t.Run("EventMappingReadback", func(t *testing.T) {
		w := request(newTestHTTPHandler(t), "POST", "/2015-03-31/event-source-mappings", `{"FunctionName":"audit","EventSourceArn":"arn:aws:sqs:us-east-1:000000000000:audit","Enabled":false,"FunctionResponseTypes":["ReportBatchItemFailures"],"Tags":{"Project":"audit"}}`, "")
		var body struct {
			EventSourceMappingArn string
			FunctionResponseTypes []string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 201 || body.EventSourceMappingArn == "" || len(body.FunctionResponseTypes) != 1 {
			t.Fatalf("mapping status=%d body=%s", w.Code, w.Body)
		}
	})
	t.Run("S3QueueNotifications", func(t *testing.T) {
		h := newTestHTTPHandler(t)
		w := request(h, "PUT", "/audit-bucket", "", "")
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		w = request(h, "PUT", "/audit-bucket?notification", `<NotificationConfiguration><QueueConfiguration><Id>audit</Id><Queue>arn:aws:sqs:us-east-1:000000000000:audit</Queue><Event>s3:ObjectCreated:*</Event></QueueConfiguration></NotificationConfiguration>`, "")
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		w = request(h, "GET", "/audit-bucket?notification", "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "<QueueConfiguration>") {
			t.Fatalf("notification status=%d body=%s", w.Code, w.Body)
		}
	})
	t.Run("RestAPIImport", func(t *testing.T) {
		h := newTestHTTPHandler(t)
		w := request(h, "POST", "/restapis", `{"name":"audit"}`, "")
		var api struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &api); err != nil {
			t.Fatal(err)
		}
		w = request(h, "PUT", "/restapis/"+api.ID+"?mode=overwrite", `{"openapi":"3.0.1","info":{"title":"audit","version":"1"},"paths":{}}`, "")
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("PutRestApi status=%d body=%s", w.Code, w.Body)
		}
	})
	t.Run("APIKeys", func(t *testing.T) {
		h := newTestHTTPHandler(t)
		created := request(h, "POST", "/apikeys", `{"name":"audit","enabled":true}`, "")
		if created.Code != 201 {
			t.Fatal(created.Body)
		}
		listed := request(h, "GET", "/apikeys?includeValues=true", "", "")
		var body struct {
			Item []struct {
				ID    string
				Value string
			} `json:"item"`
		}
		if err := json.Unmarshal(listed.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if listed.Code != 200 || len(body.Item) != 1 || body.Item[0].Value == "" {
			t.Fatalf("SDK listing shape: %s", listed.Body)
		}
	})
	t.Run("IAMRoleTags", func(t *testing.T) {
		h := newTestHTTPHandler(t)
		request(h, "POST", "/?Version=2010-05-08&Action=CreateRole&RoleName=audit&Tags.member.1.Key=Project&Tags.member.1.Value=audit", "", "")
		w := request(h, "POST", "/?Version=2010-05-08&Action=ListRoleTags&RoleName=audit", "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "<Key>Project</Key>") {
			t.Fatalf("ListRoleTags status=%d body=%s", w.Code, w.Body)
		}
	})
}

func compatibilityRequest(h http.Handler, method, path, body, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if target != "" {
		r.Header.Set("X-Amz-Target", target)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRestAPILegacyInvokeURL(t *testing.T) {
	h := newTestHTTPHandler(t)
	create := compatibilityRequest(h, "POST", "/restapis", `{"name":"messages","endpointConfiguration":{"types":["REGIONAL"]}}`, "")
	var api struct {
		ID                    string
		EndpointConfiguration struct{ Types []string }
	}
	if err := json.Unmarshal(create.Body.Bytes(), &api); err != nil {
		t.Fatal(err)
	}
	if len(api.EndpointConfiguration.Types) != 1 || api.EndpointConfiguration.Types[0] != "REGIONAL" {
		t.Fatalf("endpoint config lost: %s", create.Body)
	}
	body := `{"openapi":"3.0.1","paths":{"/ping":{"post":{"responses":{"200":{"description":"OK"}},"x-amazon-apigateway-integration":{"type":"mock","requestTemplates":{"application/json":"{\"statusCode\":200}"},"responses":{"default":{"statusCode":"200","responseTemplates":{"application/json":"{\"pong\":true}"}}}}}}}}`
	imported := compatibilityRequest(h, "PUT", "/restapis/"+api.ID+"?mode=overwrite", body, "")
	if imported.Code != 200 {
		t.Fatal(imported.Body)
	}
	deployed := compatibilityRequest(h, "POST", "/restapis/"+api.ID+"/deployments", `{"stageName":"v1"}`, "")
	if deployed.Code != 201 {
		t.Fatal(deployed.Body)
	}
	invoked := compatibilityRequest(h, "POST", "/restapis/"+api.ID+"/v1/_user_request_/ping", `{}`, "")
	if invoked.Code != 200 || !strings.Contains(invoked.Body.String(), `"pong":true`) {
		t.Fatalf("invoke status=%d body=%s", invoked.Code, invoked.Body)
	}
}
