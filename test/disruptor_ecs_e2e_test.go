package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestECSSQSDisruptorE2E proves the SQS disruptor end to end with a realistic
// publisher: an ECS task that sends messages to a queue with production-style
// error handling (retry with backoff, give up and account for failures, never crash).
//
// Phases:
//  1. healthy   — publishes succeed, queue depth grows.
//  2. disrupted — disruptor armed at 100%: every send fails with the injected
//     error, the task still exits 0 (errors handled), queue depth unchanged.
//  3. recovered — disruptor disarmed: publishes succeed again.
func TestECSSQSDisruptorE2E(t *testing.T) {
	const (
		ecsTarget = "AmazonEC2ContainerServiceV20141113."
		container = "publisher"
		image     = "node:20-alpine"
	)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	queueName := "e2e-disruptor-" + suffix
	family := "e2e-disruptor-" + suffix
	queuePath := "/000000000000/" + queueName
	queueURL := endpoint + queuePath
	logGroup := "/ecs/" + family

	type ecsContainer struct {
		Name       string `json:"Name"`
		LastStatus string `json:"LastStatus"`
		ExitCode   *int64 `json:"ExitCode"`
	}
	type ecsTask struct {
		TaskArn       string         `json:"TaskArn"`
		LastStatus    string         `json:"LastStatus"`
		DesiredStatus string         `json:"DesiredStatus"`
		Containers    []ecsContainer `json:"Containers"`
	}

	ecsRequest := func(action string, payload any) (int, []byte, error) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", "application/x-amz-json-1.1")
		req.Header.Set("X-Amz-Target", ecsTarget+action)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		responseBody, err := io.ReadAll(resp.Body)
		return resp.StatusCode, responseBody, err
	}

	ecsCall := func(action string, payload any) []byte {
		t.Helper()
		status, body, err := ecsRequest(action, payload)
		if err != nil {
			t.Fatalf("ECS %s request failed: %v", action, err)
		}
		if status != http.StatusOK {
			t.Fatalf("ECS %s failed (%d): %s", action, status, string(body))
		}
		return body
	}

	ecsCleanup := func(action string, payload any) {
		t.Helper()
		status, body, err := ecsRequest(action, payload)
		if err != nil {
			t.Errorf("ECS cleanup %s request failed: %v", action, err)
			return
		}
		if status != http.StatusOK {
			t.Errorf("ECS cleanup %s failed (%d): %s", action, status, string(body))
		}
	}

	adminRequest := func(method, path string, payload any) (int, []byte) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal admin payload: %v", err)
			}
			body = bytes.NewReader(encoded)
		}
		req, err := http.NewRequest(method, endpoint+path, body)
		if err != nil {
			t.Fatalf("build admin request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("admin %s %s failed: %v", method, path, err)
		}
		defer resp.Body.Close()
		responseBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, responseBody
	}

	queueDepth := func() int {
		t.Helper()
		resp, err := http.PostForm(endpoint, url.Values{
			"Action":          {"GetQueueAttributes"},
			"QueueUrl":        {queueURL},
			"AttributeName.1": {"ApproximateNumberOfMessages"},
		})
		if err != nil {
			t.Fatalf("GetQueueAttributes failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GetQueueAttributes returned %d: %s", resp.StatusCode, string(body))
		}
		m := regexp.MustCompile(`<Name>ApproximateNumberOfMessages</Name>\s*<Value>(\d+)</Value>`).FindSubmatch(body)
		if len(m) != 2 {
			t.Fatalf("could not parse queue depth from: %s", string(body))
		}
		var depth int
		if _, err := fmt.Sscanf(string(m[1]), "%d", &depth); err != nil {
			t.Fatalf("parse queue depth: %v", err)
		}
		return depth
	}

	// runPublisher launches the task with a per-run RUN_TAG override, waits
	// for it to stop, and requires a clean (handled, not crashed) exit.
	runPublisher := func(taskDefinitionARN, clusterARN, runTag string) {
		t.Helper()
		var runOutput struct {
			Tasks []ecsTask `json:"Tasks"`
		}
		if err := json.Unmarshal(ecsCall("RunTask", map[string]any{
			"Cluster":        clusterARN,
			"TaskDefinition": taskDefinitionARN,
			"Count":          1,
			"LaunchType":     "FARGATE",
			"Overrides": map[string]any{
				"ContainerOverrides": []map[string]any{{
					"Name": container,
					"Environment": []map[string]string{{
						"Name":  "RUN_TAG",
						"Value": runTag,
					}},
				}},
			},
		}), &runOutput); err != nil {
			t.Fatalf("decode RunTask response: %v", err)
		}
		if len(runOutput.Tasks) != 1 {
			t.Fatalf("RunTask returned %d tasks", len(runOutput.Tasks))
		}
		taskARN := runOutput.Tasks[0].TaskArn

		deadline := time.Now().Add(120 * time.Second)
		for {
			var described struct {
				Tasks []ecsTask `json:"Tasks"`
			}
			if err := json.Unmarshal(ecsCall("DescribeTasks", map[string]any{
				"Cluster": clusterARN,
				"Tasks":   []string{taskARN},
			}), &described); err != nil {
				t.Fatalf("decode DescribeTasks response: %v", err)
			}
			if len(described.Tasks) != 1 {
				t.Fatalf("DescribeTasks returned %d tasks", len(described.Tasks))
			}
			stopped := described.Tasks[0]
			if stopped.LastStatus == "STOPPED" {
				if len(stopped.Containers) != 1 || stopped.Containers[0].Name != container {
					t.Fatalf("unexpected stopped containers: %+v", stopped.Containers)
				}
				if stopped.Containers[0].ExitCode == nil || *stopped.Containers[0].ExitCode != 0 {
					t.Fatalf("publisher task %q exited %v, want 0 (errors must be handled, not crash)",
						runTag, stopped.Containers[0].ExitCode)
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("publisher task %q did not reach STOPPED", runTag)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	// waitResult polls the task log group for the publisher's RESULT line.
	waitResult := func(runTag string, wantOK, wantFails int) {
		t.Helper()
		want := fmt.Sprintf("[disruptor-e2e] RESULT %s ok=%d fails=%d", runTag, wantOK, wantFails)
		logURL := endpoint + "/_tarn/admin/logs/events/" + url.PathEscape(logGroup) + "?limit=500"
		deadline := time.Now().Add(60 * time.Second)
		for {
			resp, err := http.Get(logURL)
			if err != nil {
				t.Fatalf("fetch ECS logs failed: %v", err)
			}
			var payload struct {
				Events []struct {
					Message string `json:"message"`
					Level   string `json:"level"`
				} `json:"events"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
			_ = resp.Body.Close()
			if decodeErr != nil {
				t.Fatalf("decode ECS log events: %v", decodeErr)
			}
			for _, event := range payload.Events {
				if strings.Contains(event.Message, want) {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for log line %q", want)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	// waitErrorLog polls until a log event at ERROR level containing substr
	// appears. It locks the stderr-level fix end to end: container stderr
	// output (e.g. Node console.error on failed publishes) must surface as
	// ERROR, not INFO.
	waitErrorLog := func(substr string) {
		t.Helper()
		logURL := endpoint + "/_tarn/admin/logs/events/" + url.PathEscape(logGroup) + "?limit=500"
		deadline := time.Now().Add(60 * time.Second)
		for {
			resp, err := http.Get(logURL)
			if err != nil {
				t.Fatalf("fetch ECS logs failed: %v", err)
			}
			var payload struct {
				Events []struct {
					Message string `json:"message"`
					Level   string `json:"level"`
				} `json:"events"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
			_ = resp.Body.Close()
			if decodeErr != nil {
				t.Fatalf("decode ECS log events: %v", decodeErr)
			}
			for _, event := range payload.Events {
				if event.Level == "ERROR" && strings.Contains(event.Message, substr) {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for ERROR log line containing %q", substr)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	var clusterARN, taskDefinitionARN string
	var queueCreated bool
	t.Cleanup(func() {
		if status, body := adminRequest(http.MethodDelete, "/_tarn/admin/sqs/disruptor?all=true", nil); status != http.StatusOK {
			t.Errorf("cleanup clear disruptor returned %d: %s", status, string(body))
		}
		if taskDefinitionARN != "" {
			ecsCleanup("DeregisterTaskDefinition", map[string]any{"TaskDefinition": taskDefinitionARN})
		}
		if clusterARN != "" {
			ecsCleanup("DeleteCluster", map[string]any{"Cluster": clusterARN})
		}
		if queueCreated {
			resp, err := http.PostForm(queueURL, url.Values{
				"Action":   {"DeleteQueue"},
				"QueueUrl": {queueURL},
			})
			if err != nil {
				t.Errorf("cleanup DeleteQueue failed: %v", err)
			} else {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("cleanup DeleteQueue returned %d: %s", resp.StatusCode, string(body))
				}
			}
		}
	})

	// Test queue.
	sqsRequest(t, url.Values{
		"Action":    {"CreateQueue"},
		"QueueName": {queueName},
	})
	queueCreated = true

	// Cluster + publisher task definition. AWS_ENDPOINT_URL is injected into
	// task containers by Tarn itself; SQS_QUEUE_PATH selects this test queue.
	publisherCode := `const http = require("http");
const runTag = process.env.RUN_TAG || "run";
const queue = new URL(process.env.AWS_ENDPOINT_URL);
queue.pathname = process.env.SQS_QUEUE_PATH;
const queueUrl = queue.toString();
const MAX_ATTEMPTS = 3;
function sendOnce(messageBody) {
  return new Promise((resolve) => {
    const payload = new URLSearchParams({ Action: "SendMessage", QueueUrl: queueUrl, MessageBody: messageBody }).toString();
    const req = http.request(queue, {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded", "content-length": Buffer.byteLength(payload) },
    }, (res) => {
      let out = "";
      res.on("data", (chunk) => { out += chunk; });
      res.on("end", () => resolve({ status: res.statusCode || 0, body: out }));
    });
    req.on("error", (err) => resolve({ status: 0, body: String((err && err.message) || err) }));
    req.setTimeout(10000, () => req.destroy(new Error("timeout")));
    req.end(payload);
  });
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
(async () => {
  let ok = 0, fails = 0;
  for (const name of ["order-1", "order-2", "order-3"]) {
    let sent = false;
    for (let attempt = 1; attempt <= MAX_ATTEMPTS && !sent; attempt++) {
      const res = await sendOnce(runTag + "-" + name);
      if (res.status === 200) {
        sent = true;
        ok++;
      } else {
        console.error("[disruptor-e2e] " + runTag + " send " + name + " attempt " + attempt + " failed: HTTP " + res.status);
        if (attempt < MAX_ATTEMPTS) await sleep(100 * attempt);
      }
    }
    if (!sent) {
      fails++;
      console.error("[disruptor-e2e] " + runTag + " giving up on " + name + " after " + MAX_ATTEMPTS + " attempts");
    }
  }
  console.log("[disruptor-e2e] RESULT " + runTag + " ok=" + ok + " fails=" + fails);
})();`

	var clusterOutput struct {
		Cluster struct {
			ClusterArn string `json:"ClusterArn"`
		} `json:"Cluster"`
	}
	if err := json.Unmarshal(ecsCall("CreateCluster", map[string]any{
		"ClusterName": family,
	}), &clusterOutput); err != nil {
		t.Fatalf("decode CreateCluster response: %v", err)
	}
	clusterARN = clusterOutput.Cluster.ClusterArn
	if clusterARN == "" {
		t.Fatal("CreateCluster returned no ARN")
	}

	var taskDefinitionOutput struct {
		TaskDefinition struct {
			TaskDefinitionArn string `json:"TaskDefinitionArn"`
		} `json:"TaskDefinition"`
	}
	if err := json.Unmarshal(ecsCall("RegisterTaskDefinition", map[string]any{
		"Family": family,
		"ContainerDefinitions": []map[string]any{{
			"Name":       container,
			"Image":      image,
			"EntryPoint": []string{"node"},
			"Command":    []string{"-e", publisherCode},
			"Environment": []map[string]string{{
				"Name":  "SQS_QUEUE_PATH",
				"Value": queuePath,
			}},
			"Essential": true,
			"LogConfiguration": map[string]any{
				"LogDriver": "awslogs",
				"Options": map[string]string{
					"awslogs-group": logGroup,
				},
			},
		}},
	}), &taskDefinitionOutput); err != nil {
		t.Fatalf("decode RegisterTaskDefinition response: %v", err)
	}
	taskDefinitionARN = taskDefinitionOutput.TaskDefinition.TaskDefinitionArn
	if taskDefinitionARN == "" {
		t.Fatal("RegisterTaskDefinition returned no ARN")
	}

	// Phase 1: healthy baseline — all publishes succeed.
	runPublisher(taskDefinitionARN, clusterARN, "healthy")
	waitResult("healthy", 3, 0)
	if depth := queueDepth(); depth != 3 {
		t.Fatalf("healthy phase queue depth = %d, want 3", depth)
	}

	// Arm the disruptor: every send to this queue fails.
	if status, body := adminRequest(http.MethodPut, "/_tarn/admin/sqs/disruptor", map[string]any{
		"queue":       queueName,
		"enabled":     true,
		"failureRate": 100,
		"code":        "ServiceUnavailable",
	}); status != http.StatusOK {
		t.Fatalf("arm disruptor returned %d: %s", status, string(body))
	}

	// Phase 2: disrupted — publisher retries, handles every failure, exits
	// cleanly, and nothing new lands on the queue.
	runPublisher(taskDefinitionARN, clusterARN, "disrupted")
	waitResult("disrupted", 0, 3)
	// The publisher logs failures via console.error (stderr): they must
	// surface at ERROR level, not INFO.
	waitErrorLog("disrupted send order-1 attempt 1 failed: HTTP 503")
	if depth := queueDepth(); depth != 3 {
		t.Fatalf("disrupted phase queue depth = %d, want 3 (no disrupted send may persist)", depth)
	}

	// Disarm and prove recovery.
	if status, body := adminRequest(http.MethodDelete, "/_tarn/admin/sqs/disruptor?queue="+url.QueryEscape(queueName), nil); status != http.StatusOK {
		t.Fatalf("disarm disruptor returned %d: %s", status, string(body))
	}
	runPublisher(taskDefinitionARN, clusterARN, "recovered")
	waitResult("recovered", 3, 0)
	if depth := queueDepth(); depth != 6 {
		t.Fatalf("recovered phase queue depth = %d, want 6", depth)
	}
}
