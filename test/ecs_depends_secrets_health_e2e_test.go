package test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestECSDependsOnSecretsHealthE2E exercises the three container definition
// runtime features together against real Docker: an "init" container that
// must exit 0 before "app" starts (dependsOn SUCCESS), a Secrets Manager
// secret resolved into "app"'s environment (echoed into its logs only as a
// SHA-256 hash + length, never the raw value), and a Docker HEALTHCHECK on
// "app" whose result surfaces as DescribeTasks healthStatus.
func TestECSDependsOnSecretsHealthE2E(t *testing.T) {
	const (
		ecsTarget   = "AmazonEC2ContainerServiceV20141113."
		clusterName = "e2e-depends-secrets-health"
		family      = "e2e-depends-secrets-health"
		image       = "alpine:3.19"
		secretValue = "s3cr3t-e2e-value"
	)
	logGroup := "/ecs/" + family
	secretName := fmt.Sprintf("e2e-depends-secret-%d", time.Now().UnixNano())

	sum := sha256.Sum256([]byte(secretValue))
	wantHash := hex.EncodeToString(sum[:])

	doECS := func(action string, payload any) (int, []byte, error) {
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
		respBody, err := io.ReadAll(resp.Body)
		return resp.StatusCode, respBody, err
	}
	callECS := func(action string, payload any) []byte {
		t.Helper()
		status, body, err := doECS(action, payload)
		if err != nil {
			t.Fatalf("ECS %s request failed: %v", action, err)
		}
		if status != http.StatusOK {
			t.Fatalf("ECS %s failed (%d): %s", action, status, string(body))
		}
		return body
	}
	cleanupECS := func(action string, payload any) {
		t.Helper()
		status, body, err := doECS(action, payload)
		if err != nil {
			t.Errorf("ECS cleanup %s request failed: %v", action, err)
			return
		}
		if status != http.StatusOK {
			t.Errorf("ECS cleanup %s failed (%d): %s", action, status, string(body))
		}
	}

	// A secret whose value must never appear verbatim in Tarn's own admin
	// logs endpoint once the task references it.
	secretCreate := secretsRequest(t, "CreateSecret", map[string]any{
		"Name":         secretName,
		"SecretString": secretValue,
	})
	secretARN, _ := secretCreate["ARN"].(string)
	if secretARN == "" {
		t.Fatalf("expected non-empty secret ARN, got %+v", secretCreate)
	}
	t.Cleanup(func() {
		secretsRequest(t, "DeleteSecret", map[string]any{
			"SecretId":                   secretName,
			"ForceDeleteWithoutRecovery": true,
		})
	})

	var clusterARN, taskDefinitionARN, taskARN string
	t.Cleanup(func() {
		if taskARN != "" {
			cleanupECS("StopTask", map[string]any{
				"Cluster": clusterName,
				"Task":    taskARN,
				"Reason":  "E2E test cleanup",
			})
		}
		if taskDefinitionARN != "" {
			cleanupECS("DeregisterTaskDefinition", map[string]any{"TaskDefinition": taskDefinitionARN})
		}
		if clusterARN != "" {
			cleanupECS("DeleteCluster", map[string]any{"Cluster": clusterARN})
		}
	})

	var clusterOutput struct {
		Cluster struct {
			ClusterArn string `json:"ClusterArn"`
		} `json:"Cluster"`
	}
	if err := json.Unmarshal(callECS("CreateCluster", map[string]any{"ClusterName": clusterName}), &clusterOutput); err != nil {
		t.Fatalf("decode CreateCluster response: %v", err)
	}
	clusterARN = clusterOutput.Cluster.ClusterArn

	// "init" must exit 0 before "app" (dependsOn SUCCESS) is allowed to
	// start. "app" resolves TOKEN from Secrets Manager and only ever logs its
	// SHA-256 hash and length, plus a Docker HEALTHCHECK that starts healthy
	// immediately.
	var taskDefinitionOutput struct {
		TaskDefinition struct {
			TaskDefinitionArn string `json:"TaskDefinitionArn"`
		} `json:"TaskDefinition"`
	}
	if err := json.Unmarshal(callECS("RegisterTaskDefinition", map[string]any{
		"Family": family,
		"ContainerDefinitions": []map[string]any{
			{
				"Name":      "init",
				"Image":     image,
				"Essential": false,
				"Command":   []string{"sh", "-c", "echo init-done; exit 0"},
				"LogConfiguration": map[string]any{
					"LogDriver": "awslogs",
					"Options":   map[string]string{"awslogs-group": logGroup},
				},
			},
			{
				"Name":  "app",
				"Image": image,
				"Command": []string{"sh", "-c",
					`echo "TOKEN sha256=$(echo -n "$TOKEN" | sha256sum | cut -d' ' -f1) len=${#TOKEN}"; sleep 30`,
				},
				"Essential": true,
				"Secrets": []map[string]any{
					{"Name": "TOKEN", "ValueFrom": secretARN},
				},
				"HealthCheck": map[string]any{
					"Command":  []string{"CMD-SHELL", "true"},
					"Interval": 2,
					"Timeout":  2,
					"Retries":  1,
				},
				"DependsOn": []map[string]any{
					{"ContainerName": "init", "Condition": "SUCCESS"},
				},
				"LogConfiguration": map[string]any{
					"LogDriver": "awslogs",
					"Options":   map[string]string{"awslogs-group": logGroup},
				},
			},
		},
	}), &taskDefinitionOutput); err != nil {
		t.Fatalf("decode RegisterTaskDefinition response: %v", err)
	}
	taskDefinitionARN = taskDefinitionOutput.TaskDefinition.TaskDefinitionArn
	if taskDefinitionARN == "" {
		t.Fatalf("expected non-empty task definition ARN")
	}

	type ecsContainer struct {
		Name         string `json:"Name"`
		LastStatus   string `json:"LastStatus"`
		ExitCode     *int64 `json:"ExitCode"`
		HealthStatus string `json:"HealthStatus"`
	}
	type ecsTask struct {
		TaskArn      string         `json:"TaskArn"`
		LastStatus   string         `json:"LastStatus"`
		HealthStatus string         `json:"HealthStatus"`
		Containers   []ecsContainer `json:"Containers"`
	}
	var runOutput struct {
		Tasks    []ecsTask `json:"Tasks"`
		Failures []struct {
			Reason string `json:"Reason"`
			Detail string `json:"Detail"`
		} `json:"Failures"`
	}
	if err := json.Unmarshal(callECS("RunTask", map[string]any{
		"Cluster":        clusterName,
		"TaskDefinition": taskDefinitionARN,
		"Count":          1,
	}), &runOutput); err != nil {
		t.Fatalf("decode RunTask response: %v", err)
	}
	if len(runOutput.Tasks) != 1 {
		t.Fatalf("RunTask returned %d tasks, failures=%+v", len(runOutput.Tasks), runOutput.Failures)
	}
	taskARN = runOutput.Tasks[0].TaskArn

	// Poll DescribeTasks until "init" has exited 0, "app" has started, and
	// the task's aggregate healthStatus reaches HEALTHY.
	var describedTask ecsTask
	deadline := time.Now().Add(45 * time.Second)
	for {
		body := callECS("DescribeTasks", map[string]any{"Cluster": clusterName, "Tasks": []string{taskARN}})
		var output struct {
			Tasks []ecsTask `json:"Tasks"`
		}
		if err := json.Unmarshal(body, &output); err != nil {
			t.Fatalf("decode DescribeTasks response: %v", err)
		}
		if len(output.Tasks) != 1 {
			t.Fatalf("DescribeTasks returned %d tasks", len(output.Tasks))
		}
		describedTask = output.Tasks[0]
		if describedTask.HealthStatus == "HEALTHY" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not reach healthStatus HEALTHY in time, last=%+v", describedTask)
		}
		time.Sleep(500 * time.Millisecond)
	}

	var initContainer, appContainer *ecsContainer
	for i := range describedTask.Containers {
		switch describedTask.Containers[i].Name {
		case "init":
			initContainer = &describedTask.Containers[i]
		case "app":
			appContainer = &describedTask.Containers[i]
		}
	}
	if initContainer == nil || initContainer.LastStatus != "STOPPED" || initContainer.ExitCode == nil || *initContainer.ExitCode != 0 {
		t.Fatalf("expected init to have exited 0 (dependsOn SUCCESS satisfied), got %+v", initContainer)
	}
	if appContainer == nil || appContainer.LastStatus != "RUNNING" {
		t.Fatalf("expected app to be RUNNING after init's SUCCESS condition was met, got %+v", appContainer)
	}
	if appContainer.HealthStatus != "HEALTHY" {
		t.Fatalf("expected app container healthStatus HEALTHY, got %q", appContainer.HealthStatus)
	}

	// The secret's value must show up in logs only as a hash + length, never
	// the raw value.
	logURL := endpoint + "/_tarn/admin/logs/events/" + url.PathEscape(logGroup) + "?limit=200"
	found := false
	for attempt := 0; attempt < 40; attempt++ {
		resp, err := http.Get(logURL)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Events []struct {
				Message string `json:"message"`
			} `json:"events"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
		_ = resp.Body.Close()
		if decodeErr != nil {
			t.Fatalf("decode ECS log events response: %v", decodeErr)
		}
		for _, event := range payload.Events {
			if strings.Contains(event.Message, secretValue) {
				t.Fatalf("secret value leaked verbatim into logs: %q", event.Message)
			}
			if strings.Contains(event.Message, "TOKEN sha256="+wantHash) {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected a log line with TOKEN sha256=%s", wantHash)
	}

	// Stop the task and wait for it to fully reach STOPPED before the
	// t.Cleanup functions run DeregisterTaskDefinition/DeleteCluster —
	// DeleteCluster errors while a task is still active.
	callECS("StopTask", map[string]any{"Cluster": clusterName, "Task": taskARN, "Reason": "test complete"})
	stopDeadline := time.Now().Add(30 * time.Second)
	for {
		body := callECS("DescribeTasks", map[string]any{"Cluster": clusterName, "Tasks": []string{taskARN}})
		var output struct {
			Tasks []ecsTask `json:"Tasks"`
		}
		if err := json.Unmarshal(body, &output); err != nil {
			t.Fatalf("decode DescribeTasks response: %v", err)
		}
		if len(output.Tasks) == 1 && output.Tasks[0].LastStatus == "STOPPED" {
			break
		}
		if time.Now().After(stopDeadline) {
			t.Fatalf("task did not reach STOPPED after StopTask")
		}
		time.Sleep(250 * time.Millisecond)
	}
	taskARN = "" // already stopped; skip the cleanup StopTask call
}
