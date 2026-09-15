package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestECSServiceToSQSToLambdaViaMCP runs a long-lived ECS service that does
// real work per request, publishes the result to SQS, and has a Lambda consume
// it. Everything is verified the way an agent would see it: through `tarn mcp`
// spawned as a subprocess against the running instance.
//
// It runs in its own account, so it also proves that containers call Tarn as
// the account that owns them rather than the default one.
func TestECSServiceToSQSToLambdaViaMCP(t *testing.T) {
	const (
		account   = "333333333333"
		ecsTarget = "AmazonEC2ContainerServiceV20141113."
		container = "orders"
		image     = "node:20-alpine"
	)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	name := "e2e-svc-" + suffix
	queuePath := "/" + account + "/" + name
	queueURL := endpoint + queuePath
	queueARN := "arn:aws:sqs:us-east-1:" + account + ":" + name
	logGroup := "/ecs/" + name
	orderID := "order-" + suffix
	correlationID := "e2e-corr-" + suffix
	authorization := "AWS4-HMAC-SHA256 Credential=" + account + "/20260101/us-east-1/e2e/aws4_request, SignedHeaders=host, Signature=0"

	request := func(method, path, target, contentType string, body io.Reader) (int, []byte, error) {
		req, err := http.NewRequest(method, endpoint+path, body)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", authorization)
		if target != "" {
			req.Header.Set("X-Amz-Target", target)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		return resp.StatusCode, out, err
	}
	jsonRequest := func(method, path, target string, payload any) (int, []byte, error) {
		var body io.Reader
		contentType := "application/json"
		if target != "" {
			contentType = "application/x-amz-json-1.1"
		}
		if payload != nil {
			encoded, err := json.Marshal(payload)
			if err != nil {
				return 0, nil, err
			}
			body = bytes.NewReader(encoded)
		}
		return request(method, path, target, contentType, body)
	}
	call := func(method, path, target string, payload any, wantStatus int, out any) {
		t.Helper()
		status, body, err := jsonRequest(method, path, target, payload)
		if err != nil {
			t.Fatalf("%s %s%s: %v", method, path, target, err)
		}
		if status != wantStatus {
			t.Fatalf("%s %s%s returned %d: %s", method, path, target, status, body)
		}
		if out != nil {
			if err := json.Unmarshal(body, out); err != nil {
				t.Fatalf("decode %s %s%s: %v: %s", method, path, target, err, body)
			}
		}
	}
	ecs := func(action string, payload any, out any) {
		t.Helper()
		call(http.MethodPost, "/", ecsTarget+action, payload, http.StatusOK, out)
	}
	cleanup := func(label string, fn func() (int, []byte, error), okStatus ...int) {
		status, body, err := fn()
		if err != nil {
			t.Errorf("cleanup %s: %v", label, err)
			return
		}
		if slices.Contains(okStatus, status) {
			return
		}
		t.Errorf("cleanup %s returned %d: %s", label, status, body)
	}

	// ── Provision ───────────────────────────────────────────────────────────

	var (
		queueCreated, functionCreated, serviceCreated bool
		mappingUUID, clusterARN, taskDefinitionARN    string
	)
	t.Cleanup(func() {
		if serviceCreated {
			// Scale to zero, then a non-forced delete drains the running task.
			cleanup("UpdateService", func() (int, []byte, error) {
				return jsonRequest(http.MethodPost, "/", ecsTarget+"UpdateService", map[string]any{
					"cluster": name, "service": name, "desiredCount": 0,
				})
			}, http.StatusOK)
			cleanup("DeleteService", func() (int, []byte, error) {
				return jsonRequest(http.MethodPost, "/", ecsTarget+"DeleteService", map[string]any{
					"cluster": name, "service": name,
				})
			}, http.StatusOK)

			// Terraform's destroy polls DescribeServices until the deleted
			// service comes back INACTIVE; a MISSING failure hangs it.
			status, body, err := jsonRequest(http.MethodPost, "/", ecsTarget+"DescribeServices", map[string]any{
				"cluster": name, "services": []string{name},
			})
			var described struct {
				Services []struct {
					Status string `json:"status"`
				} `json:"services"`
				Failures []any `json:"failures"`
			}
			if err != nil || status != http.StatusOK || json.Unmarshal(body, &described) != nil {
				t.Errorf("DescribeServices after delete returned %d (%v): %s", status, err, body)
			} else if len(described.Services) != 1 || described.Services[0].Status != "INACTIVE" || len(described.Failures) != 0 {
				t.Errorf("deleted service should describe as INACTIVE with no failures, got %s", body)
			}
		}
		if taskDefinitionARN != "" {
			cleanup("DeregisterTaskDefinition", func() (int, []byte, error) {
				return jsonRequest(http.MethodPost, "/", ecsTarget+"DeregisterTaskDefinition", map[string]any{
					"taskDefinition": taskDefinitionARN,
				})
			}, http.StatusOK)
		}
		if clusterARN != "" {
			cleanup("DeleteCluster", func() (int, []byte, error) {
				return jsonRequest(http.MethodPost, "/", ecsTarget+"DeleteCluster", map[string]any{"cluster": clusterARN})
			}, http.StatusOK)
		}
		if mappingUUID != "" {
			cleanup("DeleteEventSourceMapping", func() (int, []byte, error) {
				return jsonRequest(http.MethodDelete, "/2015-03-31/event-source-mappings/"+mappingUUID, "", nil)
			}, http.StatusOK, http.StatusAccepted, http.StatusNoContent)
		}
		if functionCreated {
			cleanup("DeleteFunction", func() (int, []byte, error) {
				return jsonRequest(http.MethodDelete, "/2015-03-31/functions/"+name, "", nil)
			}, http.StatusNoContent)
		}
		if queueCreated {
			cleanup("DeleteQueue", func() (int, []byte, error) {
				form := url.Values{"Action": {"DeleteQueue"}, "QueueUrl": {queueURL}}
				return request(http.MethodPost, queuePath, "", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
			}, http.StatusOK)
		}
	})

	form := url.Values{"Action": {"CreateQueue"}, "QueueName": {name}}
	if status, body, err := request(http.MethodPost, "/", "", "application/x-www-form-urlencoded", strings.NewReader(form.Encode())); err != nil || status != http.StatusOK {
		t.Fatalf("CreateQueue returned %d (%v): %s", status, err, body)
	}
	queueCreated = true

	consumerCode := `exports.handler = async (event) => {
  for (const record of event.Records || []) {
    const order = JSON.parse(record.body);
    const attr = (record.messageAttributes || {}).correlationId || {};
    console.log("[e2e-svc] consumed " + order.id + " total=" + order.total + " correlationId=" + attr.stringValue);
  }
  return { processed: (event.Records || []).length };
};`
	call(http.MethodPost, "/2015-03-31/functions", "", map[string]any{
		"FunctionName": name,
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::" + account + ":role/" + name,
		"Timeout":      30,
		"MemorySize":   128,
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(createZip(t, map[string]string{"index.js": consumerCode})),
		},
	}, http.StatusCreated, nil)
	functionCreated = true

	var mapping struct {
		UUID string `json:"UUID"`
	}
	call(http.MethodPost, "/2015-03-31/event-source-mappings", "", map[string]any{
		"EventSourceArn":                 queueARN,
		"FunctionName":                   name,
		"BatchSize":                      1,
		"MaximumBatchingWindowInSeconds": 1,
		"Enabled":                        true,
	}, http.StatusCreated, &mapping)
	if mappingUUID = mapping.UUID; mappingUUID == "" {
		t.Fatal("CreateEventSourceMapping returned no UUID")
	}

	var cluster struct {
		Cluster struct {
			ClusterArn string `json:"clusterArn"`
		} `json:"cluster"`
	}
	ecs("CreateCluster", map[string]any{"clusterName": name}, &cluster)
	if clusterARN = cluster.Cluster.ClusterArn; !strings.Contains(clusterARN, ":"+account+":cluster/") {
		t.Fatalf("cluster ARN %q is not in account %s", clusterARN, account)
	}

	// The service prices an order, publishes it to SQS with the caller's
	// correlation ID as a message attribute, and answers with the result. It
	// signs with whatever AWS_ACCESS_KEY_ID Tarn injected, so the publish only
	// lands in this account if the container was given the right identity.
	serviceCode := `const http = require("http");
const account = process.env.AWS_ACCESS_KEY_ID;
const queue = new URL(process.env.AWS_ENDPOINT_URL);
queue.pathname = process.env.SQS_QUEUE_PATH;

function publish(order, correlationId, done) {
  const body = new URLSearchParams({
    Action: "SendMessage",
    QueueUrl: queue.toString(),
    MessageBody: JSON.stringify(order),
    "MessageAttribute.1.Name": "correlationId",
    "MessageAttribute.1.Value.DataType": "String",
    "MessageAttribute.1.Value.StringValue": correlationId,
  }).toString();
  const req = http.request(queue, {
    method: "POST",
    headers: {
      "content-type": "application/x-www-form-urlencoded",
      "content-length": Buffer.byteLength(body),
      authorization: "AWS4-HMAC-SHA256 Credential=" + account + "/20260101/us-east-1/sqs/aws4_request, SignedHeaders=host, Signature=0",
    },
  }, (res) => {
    let out = "";
    res.on("data", (c) => (out += c));
    res.on("end", () => done(res.statusCode, out));
  });
  req.on("error", (e) => done(0, String(e)));
  req.end(body);
}

http.createServer((req, res) => {
  if (req.method !== "POST" || req.url !== "/orders") {
    res.writeHead(404);
    return res.end();
  }
  let raw = "";
  req.on("data", (c) => (raw += c));
  req.on("end", () => {
    const order = JSON.parse(raw);
    order.total = order.items.reduce((sum, item) => sum + item.qty * item.price, 0);
    const correlationId = req.headers["x-correlation-id"] || process.env.TARN_CORRELATION_ID || "";
    publish(order, correlationId, (status, out) => {
      if (status !== 200) {
        console.error("[e2e-svc] publish failed " + status + " " + out);
        res.writeHead(502);
        return res.end(out);
      }
      console.log("[e2e-svc] accepted " + order.id + " total=" + order.total + " correlationId=" + correlationId);
      res.writeHead(202, { "content-type": "application/json" });
      res.end(JSON.stringify({ id: order.id, total: order.total, correlationId }));
    });
  });
}).listen(8080, () => console.log("[e2e-svc] listening as account " + account));

process.on("SIGTERM", () => process.exit(0));`

	var taskDefinition struct {
		TaskDefinition struct {
			TaskDefinitionArn string `json:"taskDefinitionArn"`
		} `json:"taskDefinition"`
	}
	ecs("RegisterTaskDefinition", map[string]any{
		"family":                  name,
		"requiresCompatibilities": []string{"FARGATE"},
		"networkMode":             "awsvpc",
		"cpu":                     "256",
		"memory":                  "512",
		"containerDefinitions": []map[string]any{{
			"name":         container,
			"image":        image,
			"essential":    true,
			"entryPoint":   []string{"node"},
			"command":      []string{"-e", serviceCode},
			"environment":  []map[string]string{{"name": "SQS_QUEUE_PATH", "value": queuePath}},
			"portMappings": []map[string]any{{"containerPort": 8080, "protocol": "tcp"}},
			"logConfiguration": map[string]any{
				"logDriver": "awslogs",
				"options":   map[string]string{"awslogs-group": logGroup},
			},
		}},
	}, &taskDefinition)
	if taskDefinitionARN = taskDefinition.TaskDefinition.TaskDefinitionArn; taskDefinitionARN == "" {
		t.Fatal("RegisterTaskDefinition returned no ARN")
	}

	ecs("CreateService", map[string]any{
		"cluster":        name,
		"serviceName":    name,
		"taskDefinition": taskDefinitionARN,
		"desiredCount":   1,
		"launchType":     "FARGATE",
		"networkConfiguration": map[string]any{
			"awsvpcConfiguration": map[string]any{"subnets": []string{"subnet-e2e"}},
		},
	}, nil)
	serviceCreated = true

	// Terraform treats these as ForceNew, so a service that does not echo them
	// is replaced on every plan.
	var describedService struct {
		Services []struct {
			SchedulingStrategy   string `json:"schedulingStrategy"`
			NetworkConfiguration struct {
				AwsvpcConfiguration struct {
					Subnets []string `json:"subnets"`
				} `json:"awsvpcConfiguration"`
			} `json:"networkConfiguration"`
		} `json:"services"`
	}
	ecs("DescribeServices", map[string]any{"cluster": name, "services": []string{name}}, &describedService)
	if len(describedService.Services) != 1 {
		t.Fatalf("DescribeServices returned %d services", len(describedService.Services))
	}
	if svc := describedService.Services[0]; svc.SchedulingStrategy != "REPLICA" ||
		!slices.Equal(svc.NetworkConfiguration.AwsvpcConfiguration.Subnets, []string{"subnet-e2e"}) {
		t.Fatalf("service does not echo schedulingStrategy/networkConfiguration: %+v", svc)
	}

	// ── Connect over MCP ────────────────────────────────────────────────────

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := exec.Command(tarnBinary, "mcp")
	cmd.Env = append(os.Environ(), "TARN_ENDPOINT="+endpoint)
	client := mcp.NewClient(&mcp.Implementation{Name: "tarn-e2e", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to tarn mcp: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list MCP tools: %v", err)
	}
	available := map[string]bool{}
	for _, tool := range tools.Tools {
		available[tool.Name] = true
	}
	for _, want := range []string{"tarn_status", "tarn_get_logs", "tarn_get_traces"} {
		if !available[want] {
			t.Fatalf("tarn mcp does not offer %s", want)
		}
	}

	callTool := func(tool string, args map[string]any, out any) {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if res.IsError {
			var text strings.Builder
			for _, content := range res.Content {
				if tc, ok := content.(*mcp.TextContent); ok {
					text.WriteString(tc.Text)
				}
			}
			t.Fatalf("%s returned a tool error: %s", tool, text.String())
		}
		encoded, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("%s: encode structured content: %v", tool, err)
		}
		if err := json.Unmarshal(encoded, out); err != nil {
			t.Fatalf("%s: decode structured content: %v: %s", tool, err, encoded)
		}
	}

	// eventually retries check until it reports done or the deadline passes,
	// returning the last reason it was not done for the failure message.
	eventually := func(what string, timeout time.Duration, check func() (bool, string)) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for {
			done, reason := check()
			if done {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out after %s waiting for %s: %s", timeout, what, reason)
			}
			time.Sleep(time.Second)
		}
	}

	// ── The service comes up: discovered through tarn_status ────────────────

	type mcpStatus struct {
		AccountID string `json:"accountId"`
		ECS       *struct {
			Services []struct {
				Name         string `json:"name"`
				Status       string `json:"status"`
				DesiredCount int    `json:"desiredCount"`
				RunningCount int    `json:"runningCount"`
			} `json:"services"`
			Tasks []struct {
				TaskArn    string `json:"taskArn"`
				Group      string `json:"group"`
				LastStatus string `json:"lastStatus"`
				Containers []struct {
					Name     string   `json:"name"`
					HostURLs []string `json:"hostUrls"`
				} `json:"containers"`
			} `json:"tasks"`
		} `json:"ecs"`
	}

	var serviceURL, taskARN string
	eventually("service task RUNNING with a published port", 2*time.Minute, func() (bool, string) {
		var status mcpStatus
		callTool("tarn_status", map[string]any{"account": account}, &status)
		if status.AccountID != account {
			return false, "status reports account " + status.AccountID
		}
		if status.ECS == nil {
			return false, "status has no ecs section"
		}
		running := false
		for _, svc := range status.ECS.Services {
			if svc.Name == name {
				running = svc.Status == "ACTIVE" && svc.DesiredCount == 1 && svc.RunningCount == 1
			}
		}
		for _, task := range status.ECS.Tasks {
			if task.Group != "service:"+name || task.LastStatus != "RUNNING" {
				continue
			}
			for _, c := range task.Containers {
				if c.Name == container && len(c.HostURLs) > 0 {
					serviceURL, taskARN = c.HostURLs[0], task.TaskArn
				}
			}
		}
		if !running || serviceURL == "" {
			return false, fmt.Sprintf("service running=%t url=%q ecs=%+v", running, serviceURL, *status.ECS)
		}
		return true, ""
	})

	type mcpLogs struct {
		Events []struct {
			Message string `json:"message"`
		} `json:"events"`
	}
	logsContain := func(args map[string]any, needle string) (bool, string) {
		var logs mcpLogs
		callTool("tarn_get_logs", args, &logs)
		for _, event := range logs.Events {
			if strings.Contains(event.Message, needle) {
				return true, ""
			}
		}
		return false, fmt.Sprintf("%d events, none containing %q", len(logs.Events), needle)
	}

	// The container sees its owning account, not the default one.
	eventually("service startup log", time.Minute, func() (bool, string) {
		return logsContain(map[string]any{"account": account, "logGroup": logGroup}, "[e2e-svc] listening as account "+account)
	})

	// ── Drive the service ───────────────────────────────────────────────────

	order := `{"id":"` + orderID + `","items":[{"sku":"a","qty":2,"price":5},{"sku":"b","qty":1,"price":7}]}`
	var accepted struct {
		ID            string `json:"id"`
		Total         int    `json:"total"`
		CorrelationID string `json:"correlationId"`
	}
	eventually("service to accept the order", 30*time.Second, func() (bool, string) {
		req, _ := http.NewRequest(http.MethodPost, strings.TrimSuffix(serviceURL, "/")+"/orders", strings.NewReader(order))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Correlation-Id", correlationID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusAccepted {
			return false, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, body)
		}
		if err := json.Unmarshal(body, &accepted); err != nil {
			t.Fatalf("decode service response: %v: %s", err, body)
		}
		return true, ""
	})
	if accepted.ID != orderID || accepted.Total != 17 || accepted.CorrelationID != correlationID {
		t.Fatalf("unexpected service response: %+v", accepted)
	}

	// ── Same service, reached through the /_ecs/ reverse proxy ─────────────
	// Cluster and service share `name` here, so the stable URL is
	// /_ecs/<account>/<name>/<name>/... . This proves the proxy resolves the
	// RUNNING task and forwards both a POST (with body/headers) and a GET to
	// the actual container, without the caller ever learning the ephemeral
	// host port serviceURL above came from.
	proxyBase := endpoint + "/_ecs/" + account + "/" + name + "/" + name
	proxyCorrelationID := "e2e-proxy-corr-" + suffix

	postReq, err := http.NewRequest(http.MethodPost, proxyBase+"/orders", strings.NewReader(order))
	if err != nil {
		t.Fatalf("build proxy POST request: %v", err)
	}
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("X-Correlation-Id", proxyCorrelationID)
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("POST via /_ecs/ proxy: %v", err)
	}
	postBody, _ := io.ReadAll(postResp.Body)
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST via /_ecs/ proxy returned %d: %s", postResp.StatusCode, postBody)
	}
	var acceptedViaProxy struct {
		ID            string `json:"id"`
		Total         int    `json:"total"`
		CorrelationID string `json:"correlationId"`
	}
	if err := json.Unmarshal(postBody, &acceptedViaProxy); err != nil {
		t.Fatalf("decode proxied service response: %v: %s", err, postBody)
	}
	if acceptedViaProxy.ID != orderID || acceptedViaProxy.Total != 17 || acceptedViaProxy.CorrelationID != proxyCorrelationID {
		t.Fatalf("unexpected proxied service response: %+v", acceptedViaProxy)
	}

	// A GET the container itself 404s (its handler only accepts POST
	// /orders); an empty body distinguishes "the container answered" from
	// Tarn's own proxy-level 404 (which carries a "service not found"/
	// "cluster not found" message body).
	getResp, err := http.Get(proxyBase + "/orders")
	if err != nil {
		t.Fatalf("GET via /_ecs/ proxy: %v", err)
	}
	getBody, _ := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET via /_ecs/ proxy returned %d, want the container's own 404: %s", getResp.StatusCode, getBody)
	}
	if len(getBody) != 0 {
		t.Fatalf("GET via /_ecs/ proxy returned a body, want the container's empty 404: %s", getBody)
	}

	// ── Verify output over MCP ──────────────────────────────────────────────

	eventually("service publish log", time.Minute, func() (bool, string) {
		return logsContain(map[string]any{"account": account, "logGroup": logGroup, "pattern": orderID},
			"[e2e-svc] accepted "+orderID+" total=17 correlationId="+correlationID)
	})
	eventually("Lambda consumer log", 2*time.Minute, func() (bool, string) {
		return logsContain(map[string]any{"account": account, "function": name, "pattern": orderID},
			"[e2e-svc] consumed "+orderID+" total=17 correlationId="+correlationID)
	})

	// ── Verify traces over MCP ──────────────────────────────────────────────

	type mcpTraces struct {
		Traces []struct {
			ID            string `json:"id"`
			CorrelationID string `json:"correlationId"`
			Status        int    `json:"status"`
			Spans         []struct {
				Kind   string            `json:"kind"`
				Name   string            `json:"name"`
				Status string            `json:"status"`
				Meta   map[string]string `json:"meta"`
			} `json:"spans"`
		} `json:"traces"`
	}

	// The SQS → Lambda delivery joins the request by the correlation ID the
	// service forwarded as a message attribute.
	eventually("queue → lambda trace for the correlation ID", time.Minute, func() (bool, string) {
		var traces mcpTraces
		callTool("tarn_get_traces", map[string]any{"account": account, "correlationId": correlationID}, &traces)
		for _, tr := range traces.Traces {
			var queueOK, lambdaOK bool
			for _, span := range tr.Spans {
				switch {
				case span.Kind == "queue" && span.Name == name:
					queueOK = span.Status == "ok"
				case span.Kind == "lambda" && span.Name == name:
					lambdaOK = span.Status == "ok"
				}
			}
			if tr.CorrelationID == correlationID && queueOK && lambdaOK {
				return true, ""
			}
		}
		return false, fmt.Sprintf("traces for %s: %+v", correlationID, traces.Traces)
	})

	// The ECS runner records the service task starting.
	eventually("ECS task trace for the service", 30*time.Second, func() (bool, string) {
		var traces mcpTraces
		callTool("tarn_get_traces", map[string]any{"account": account, "kind": "ecs", "resource": name}, &traces)
		for _, tr := range traces.Traces {
			for _, span := range tr.Spans {
				if span.Kind == "ecs" && span.Name == name && span.Meta["taskArn"] == taskARN && span.Status == "ok" {
					return true, ""
				}
			}
		}
		return false, fmt.Sprintf("ecs traces: %+v", traces.Traces)
	})
}
