package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// e2eClient signs requests as one account and posts JSON-protocol calls.
type e2eClient struct {
	t       *testing.T
	account string
}

func (c e2eClient) do(method, path, target, contentType string, payload any) (int, http.Header, []byte) {
	c.t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			c.t.Fatalf("encode %s: %v", target, err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, endpoint+path, body)
	if err != nil {
		c.t.Fatalf("new request %s: %v", target, err)
	}
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.account+"/20260101/us-east-1/e2e/aws4_request, SignedHeaders=host, Signature=0")
	if target != "" {
		req.Header.Set("X-Amz-Target", target)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read %s: %v", target, err)
	}
	return resp.StatusCode, resp.Header, out
}

// ecs calls an ECS JSON 1.1 action and decodes a 200 response into out.
func (c e2eClient) ecs(action string, payload, out any) {
	c.t.Helper()
	status, _, body := c.do(http.MethodPost, "/", "AmazonEC2ContainerServiceV20141113."+action, "application/x-amz-json-1.1", payload)
	if status != http.StatusOK {
		c.t.Fatalf("%s returned %d: %s", action, status, body)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			c.t.Fatalf("decode %s: %v: %s", action, err, body)
		}
	}
}

type e2eTask struct {
	TaskArn       string `json:"taskArn"`
	LastStatus    string `json:"lastStatus"`
	DesiredStatus string `json:"desiredStatus"`
	StopCode      string `json:"stopCode"`
	StoppedReason string `json:"stoppedReason"`
	Containers    []struct {
		Name       string `json:"name"`
		LastStatus string `json:"lastStatus"`
		ExitCode   *int64 `json:"exitCode"`
	} `json:"containers"`
}

func (c e2eClient) describeTask(cluster, taskArn string) e2eTask {
	c.t.Helper()
	var out struct {
		Tasks []e2eTask `json:"tasks"`
	}
	c.ecs("DescribeTasks", map[string]any{"cluster": cluster, "tasks": []string{taskArn}}, &out)
	if len(out.Tasks) != 1 {
		c.t.Fatalf("DescribeTasks %s returned %d tasks", taskArn, len(out.Tasks))
	}
	return out.Tasks[0]
}

func waitUntil(t *testing.T, what string, timeout time.Duration, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		done, reason := check()
		if done {
			return
		}
		last = reason
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %s", what, last)
}

// TestECSStopSemanticsE2E checks AWS task stop semantics against real Docker:
// the essential-container rule, non-essential exits, stop codes for launch
// failures and user stops, and that tarn_status (MCP) reports them.
func TestECSStopSemanticsE2E(t *testing.T) {
	const (
		account = "555555555555"
		image   = "node:20-alpine"
	)
	c := e2eClient{t: t, account: account}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	cluster := "e2e-stop-" + suffix

	c.ecs("CreateCluster", map[string]any{"clusterName": cluster}, nil)
	var started []string
	t.Cleanup(func() {
		for _, arn := range started {
			c.do(http.MethodPost, "/", "AmazonEC2ContainerServiceV20141113.StopTask", "application/x-amz-json-1.1",
				map[string]any{"cluster": cluster, "task": arn, "reason": "e2e cleanup"})
		}
	})

	runTask := func(family string, containers []map[string]any) e2eTask {
		t.Helper()
		c.ecs("RegisterTaskDefinition", map[string]any{"family": family, "containerDefinitions": containers}, nil)
		var out struct {
			Tasks    []e2eTask `json:"tasks"`
			Failures []any     `json:"failures"`
		}
		c.ecs("RunTask", map[string]any{"cluster": cluster, "taskDefinition": family}, &out)
		if len(out.Tasks) != 1 || len(out.Failures) != 0 {
			t.Fatalf("RunTask %s: tasks=%+v failures=%+v", family, out.Tasks, out.Failures)
		}
		started = append(started, out.Tasks[0].TaskArn)
		return out.Tasks[0]
	}
	// A sidecar that ignores SIGTERM (node as PID 1) with a short stopTimeout,
	// so Tarn's forced stop of it is observable but quick.
	sidecar := func(essential bool, script string) map[string]any {
		return map[string]any{
			"name": "sidecar", "image": image, "essential": essential, "stopTimeout": 2,
			"command": []string{"node", "-e", script},
		}
	}

	// ── Essential container exits → task stops, sibling is stopped ─────────
	essentialFamily := "e2e-essential-" + suffix
	essentialTask := runTask(essentialFamily, []map[string]any{
		{"name": "app", "image": image, "essential": true, "command": []string{"node", "-e", "setTimeout(()=>process.exit(3),1500)"}},
		sidecar(false, "setInterval(()=>{},1000)"),
	})
	var stopped e2eTask
	waitUntil(t, "essential exit to stop the task", 90*time.Second, func() (bool, string) {
		stopped = c.describeTask(cluster, essentialTask.TaskArn)
		return stopped.LastStatus == "STOPPED", fmt.Sprintf("%+v", stopped)
	})
	if stopped.StopCode != "EssentialContainerExited" || stopped.StoppedReason != "Essential container in task exited" || stopped.DesiredStatus != "STOPPED" {
		t.Fatalf("essential exit task = %+v, want STOPPED with EssentialContainerExited", stopped)
	}
	for _, container := range stopped.Containers {
		if container.LastStatus != "STOPPED" {
			t.Fatalf("container %s still %s after essential exit", container.Name, container.LastStatus)
		}
		if container.Name == "app" && (container.ExitCode == nil || *container.ExitCode != 3) {
			t.Fatalf("app exit code = %v, want 3", container.ExitCode)
		}
	}

	// ── Non-essential container exits → task keeps running ──────────────────
	sidecarFamily := "e2e-sidecar-" + suffix
	sidecarTask := runTask(sidecarFamily, []map[string]any{
		{"name": "app", "image": image, "essential": true, "stopTimeout": 2, "command": []string{"node", "-e", "setInterval(()=>{},1000)"}},
		sidecar(false, "setTimeout(()=>process.exit(0),1500)"),
	})
	waitUntil(t, "non-essential sidecar to exit", 90*time.Second, func() (bool, string) {
		task := c.describeTask(cluster, sidecarTask.TaskArn)
		for _, container := range task.Containers {
			if container.Name == "sidecar" && container.LastStatus == "STOPPED" {
				return true, ""
			}
		}
		return false, fmt.Sprintf("%+v", task)
	})
	time.Sleep(2 * time.Second)
	if task := c.describeTask(cluster, sidecarTask.TaskArn); task.LastStatus != "RUNNING" || task.DesiredStatus != "RUNNING" || task.StopCode != "" {
		t.Fatalf("task after non-essential exit = %+v, want still RUNNING", task)
	}

	// ── StopTask → UserInitiated ────────────────────────────────────────────
	c.ecs("StopTask", map[string]any{"cluster": cluster, "task": sidecarTask.TaskArn, "reason": "e2e user stop"}, nil)
	waitUntil(t, "StopTask to finish", 60*time.Second, func() (bool, string) {
		task := c.describeTask(cluster, sidecarTask.TaskArn)
		return task.LastStatus == "STOPPED", fmt.Sprintf("%+v", task)
	})
	if task := c.describeTask(cluster, sidecarTask.TaskArn); task.StopCode != "UserInitiated" || task.StoppedReason != "e2e user stop" {
		t.Fatalf("user-stopped task = %+v, want UserInitiated / e2e user stop", task)
	}

	// ── Launch failure → task returned STOPPED, not a Failures entry ────────
	failFamily := "e2e-launchfail-" + suffix
	failed := runTask(failFamily, []map[string]any{{
		"name": "app", "image": image, "essential": true,
		"command": []string{"node", "-e", "console.log('unreachable')"},
		"secrets": []map[string]any{{"name": "MISSING", "valueFrom": "e2e-secret-does-not-exist-" + suffix}},
	}})
	if failed.LastStatus != "STOPPED" || failed.DesiredStatus != "STOPPED" || failed.StopCode != "TaskFailedToStart" ||
		!strings.Contains(failed.StoppedReason, "ResourceInitializationError") {
		t.Fatalf("launch failure task = %+v, want STOPPED / TaskFailedToStart / ResourceInitializationError", failed)
	}

	// ── tarn_status over MCP reports the stop codes and reasons ─────────────
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.Command(tarnBinary, "mcp")
	cmd.Env = append(os.Environ(), "TARN_ENDPOINT="+endpoint)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "tarn-e2e", Version: "test"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to tarn mcp: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "tarn_status", Arguments: map[string]any{"account": account}})
	if err != nil || res.IsError {
		t.Fatalf("tarn_status: err=%v result=%+v", err, res)
	}
	encoded, _ := json.Marshal(res.StructuredContent)
	var status struct {
		ECS struct {
			Tasks []struct {
				TaskArn       string `json:"taskArn"`
				StopCode      string `json:"stopCode"`
				StoppedReason string `json:"stoppedReason"`
				Containers    []struct {
					Name     string `json:"name"`
					ExitCode *int64 `json:"exitCode"`
					Reason   string `json:"reason"`
				} `json:"containers"`
			} `json:"tasks"`
		} `json:"ecs"`
	}
	if err := json.Unmarshal(encoded, &status); err != nil {
		t.Fatalf("decode tarn_status: %v: %s", err, encoded)
	}
	wantCodes := map[string]string{
		essentialTask.TaskArn: "EssentialContainerExited",
		sidecarTask.TaskArn:   "UserInitiated",
		failed.TaskArn:        "TaskFailedToStart",
	}
	for _, task := range status.ECS.Tasks {
		want, ok := wantCodes[task.TaskArn]
		if !ok {
			continue
		}
		if task.StopCode != want || task.StoppedReason == "" {
			t.Errorf("tarn_status task %s = stopCode %q reason %q, want %s with a reason", task.TaskArn, task.StopCode, task.StoppedReason, want)
		}
		if task.TaskArn == failed.TaskArn {
			if len(task.Containers) != 1 || !strings.Contains(task.Containers[0].Reason, "ResourceInitializationError") {
				t.Errorf("tarn_status launch-failure containers = %+v, want the container reason", task.Containers)
			}
		}
		delete(wantCodes, task.TaskArn)
	}
	if len(wantCodes) != 0 {
		t.Fatalf("tarn_status did not report tasks: %v (payload %s)", wantCodes, encoded)
	}
}

// TestTerraformCompatFixesE2E covers the provider-facing fixes over real HTTP:
// Lambda's code-signing-config route, SQS's query-compatible missing-queue
// error, and concurrent EventBridge PutTargets on one rule.
func TestTerraformCompatFixesE2E(t *testing.T) {
	const account = "555555555556"
	c := e2eClient{t: t, account: account}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// ── Lambda GetFunctionCodeSigningConfig ─────────────────────────────────
	fn := "e2e-csc-" + suffix
	status, _, body := c.do(http.MethodPost, "/2015-03-31/functions", "", "application/json", map[string]any{
		"FunctionName": fn, "Runtime": "nodejs20.x", "Handler": "index.handler",
		"Role": "arn:aws:iam::" + account + ":role/e2e", "Code": map[string]any{"ZipFile": ""},
	})
	if status != http.StatusCreated {
		t.Fatalf("CreateFunction returned %d: %s", status, body)
	}
	t.Cleanup(func() { c.do(http.MethodDelete, "/2015-03-31/functions/"+fn, "", "", nil) })

	status, _, body = c.do(http.MethodGet, "/2020-06-30/functions/"+fn+"/code-signing-config", "", "", nil)
	var csc map[string]string
	if status != http.StatusOK || json.Unmarshal(body, &csc) != nil || csc["FunctionName"] != fn {
		t.Fatalf("GetFunctionCodeSigningConfig = %d %s, want 200 JSON naming the function", status, body)
	}
	status, _, body = c.do(http.MethodGet, "/2024-08-31/functions/"+fn+"/recursion-config", "", "", nil)
	if status != http.StatusNotFound || !json.Valid(body) {
		t.Fatalf("unemulated dated Lambda path = %d %s, want JSON 404", status, body)
	}

	// ── SQS JSON missing queue carries the query-compatible code ────────────
	status, header, body := c.do(http.MethodPost, "/", "AmazonSQS.GetQueueAttributes", "application/x-amz-json-1.0",
		map[string]any{"QueueUrl": endpoint + "/" + account + "/e2e-missing-" + suffix})
	if status != http.StatusBadRequest || header.Get("X-Amzn-Query-Error") != "AWS.SimpleQueueService.NonExistentQueue;Sender" {
		t.Fatalf("missing queue = %d query-error %q body %s, want 400 with AWS.SimpleQueueService.NonExistentQueue;Sender",
			status, header.Get("X-Amzn-Query-Error"), body)
	}

	// ── Concurrent PutTargets on one rule keeps every target ────────────────
	rule := "e2e-targets-" + suffix
	status, _, body = c.do(http.MethodPost, "/", "AWSEvents.PutRule", "application/x-amz-json-1.1", map[string]any{
		"Name": rule, "EventPattern": `{"source":["e2e.targets"]}`,
	})
	if status != http.StatusOK {
		t.Fatalf("PutRule returned %d: %s", status, body)
	}
	const n = 10
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _, body := c.do(http.MethodPost, "/", "AWSEvents.PutTargets", "application/x-amz-json-1.1", map[string]any{
				"Rule":    rule,
				"Targets": []map[string]any{{"Id": fmt.Sprintf("t%02d", i), "Arn": "arn:aws:lambda:us-east-1:" + account + ":function:" + fn}},
			})
			if status != http.StatusOK {
				t.Errorf("PutTargets %d returned %d: %s", i, status, body)
			}
		}()
	}
	wg.Wait()
	t.Cleanup(func() {
		ids := make([]string, n)
		for i := range ids {
			ids[i] = fmt.Sprintf("t%02d", i)
		}
		c.do(http.MethodPost, "/", "AWSEvents.RemoveTargets", "application/x-amz-json-1.1", map[string]any{"Rule": rule, "Ids": ids})
		c.do(http.MethodPost, "/", "AWSEvents.DeleteRule", "application/x-amz-json-1.1", map[string]any{"Name": rule})
	})

	status, _, body = c.do(http.MethodPost, "/", "AWSEvents.ListTargetsByRule", "application/x-amz-json-1.1", map[string]any{"Rule": rule})
	var listed struct {
		Targets []struct {
			Id string `json:"Id"`
		} `json:"Targets"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &listed) != nil {
		t.Fatalf("ListTargetsByRule = %d %s", status, body)
	}
	if len(listed.Targets) != n {
		t.Fatalf("ListTargetsByRule returned %d targets after %d concurrent PutTargets: %s", len(listed.Targets), n, body)
	}
}
