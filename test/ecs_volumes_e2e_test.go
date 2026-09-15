package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestECSDockerTaskVolumesAndFieldsE2E exercises the local Docker
// application of the Terraform-drift-avoidance fields added to ECS task
// definitions: a bare task-scoped volume shared between two containers of
// the same task (one writes, the other reads it back), plus
// workingDirectory and user observed through container output. This is the
// end-to-end complement to the fakeEngine-based unit tests in
// internal/ecs/runner_test.go and internal/engine/task_test.go, which check
// the same mapping without a real Docker daemon.
func TestECSDockerTaskVolumesAndFieldsE2E(t *testing.T) {
	const (
		clusterName = "e2e-ecs-volumes-cluster"
		family      = "e2e-ecs-volumes-task"
		writerName  = "writer"
		readerName  = "reader"
		image       = "alpine:3.19"
		logGroup    = "/ecs/e2e-ecs-volumes-task"
		sharedFile  = "shared.txt"
		payload     = "hello-from-writer"
	)

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
		StoppedReason string         `json:"StoppedReason"`
	}

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
		req.Header.Set("X-Amz-Target", "AmazonEC2ContainerServiceV20141113."+action)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		responseBody, err := io.ReadAll(resp.Body)
		return resp.StatusCode, responseBody, err
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

	var clusterARN, taskDefinitionARN, taskARN string
	t.Cleanup(func() {
		if taskARN != "" {
			status, body, err := doECS("DescribeTasks", map[string]any{
				"Cluster": clusterName,
				"Tasks":   []string{taskARN},
			})
			if err == nil && status == http.StatusOK {
				var output struct {
					Tasks []ecsTask `json:"Tasks"`
				}
				if json.Unmarshal(body, &output) == nil && len(output.Tasks) > 0 && output.Tasks[0].LastStatus != "STOPPED" {
					cleanupECS("StopTask", map[string]any{
						"Cluster": clusterName,
						"Task":    taskARN,
						"Reason":  "E2E test cleanup",
					})
				}
			}
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
			Status     string `json:"Status"`
		} `json:"Cluster"`
	}
	if err := json.Unmarshal(callECS("CreateCluster", map[string]any{"ClusterName": clusterName}), &clusterOutput); err != nil {
		t.Fatalf("decode CreateCluster response: %v", err)
	}
	clusterARN = clusterOutput.Cluster.ClusterArn
	if clusterOutput.Cluster.Status != "ACTIVE" || clusterARN == "" {
		t.Fatalf("unexpected created cluster: %+v", clusterOutput.Cluster)
	}

	// writer runs as a non-default working directory and a fixed numeric
	// user, then writes into the bare shared volume; reader waits for the
	// writer to complete successfully (DependsOn: COMPLETE) before reading
	// the same file back from its own mount point.
	var taskDefinitionOutput struct {
		TaskDefinition struct {
			TaskDefinitionArn string `json:"TaskDefinitionArn"`
			Revision          int    `json:"Revision"`
		} `json:"TaskDefinition"`
	}
	registerBody := map[string]any{
		"Family": family,
		"Volumes": []map[string]any{
			{"Name": "shared"},
		},
		"ContainerDefinitions": []map[string]any{
			{
				"Name":             writerName,
				"Image":            image,
				"Essential":        true,
				"WorkingDirectory": "/tmp",
				"User":             "0:0",
				"Command": []string{"sh", "-c",
					"echo WORKDIR=$(pwd); echo UID=$(id -u); echo " + payload + " > /data/" + sharedFile,
				},
				"MountPoints": []map[string]any{
					{"SourceVolume": "shared", "ContainerPath": "/data"},
				},
				"LogConfiguration": map[string]any{
					"LogDriver": "awslogs",
					"Options":   map[string]string{"awslogs-group": logGroup},
				},
			},
			{
				"Name":      readerName,
				"Image":     image,
				"Essential": true,
				"Command":   []string{"sh", "-c", "cat /mnt/" + sharedFile},
				"MountPoints": []map[string]any{
					{"SourceVolume": "shared", "ContainerPath": "/mnt", "ReadOnly": true},
				},
				"DependsOn": []map[string]any{
					{"ContainerName": writerName, "Condition": "COMPLETE"},
				},
				"LogConfiguration": map[string]any{
					"LogDriver": "awslogs",
					"Options":   map[string]string{"awslogs-group": logGroup},
				},
			},
		},
	}
	if err := json.Unmarshal(callECS("RegisterTaskDefinition", registerBody), &taskDefinitionOutput); err != nil {
		t.Fatalf("decode RegisterTaskDefinition response: %v", err)
	}
	taskDefinitionARN = taskDefinitionOutput.TaskDefinition.TaskDefinitionArn
	if taskDefinitionARN == "" {
		t.Fatalf("unexpected registered task definition: %+v", taskDefinitionOutput.TaskDefinition)
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
		"LaunchType":     "FARGATE",
	}), &runOutput); err != nil {
		t.Fatalf("decode RunTask response: %v", err)
	}
	if len(runOutput.Tasks) != 1 {
		t.Fatalf("RunTask returned %d tasks, failures=%+v", len(runOutput.Tasks), runOutput.Failures)
	}
	taskARN = runOutput.Tasks[0].TaskArn

	var stoppedTask ecsTask
	deadline := time.Now().Add(30 * time.Second)
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
		if output.Tasks[0].LastStatus == "STOPPED" {
			stoppedTask = output.Tasks[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not reach STOPPED, last status=%s", output.Tasks[0].LastStatus)
		}
		time.Sleep(250 * time.Millisecond)
	}

	if len(stoppedTask.Containers) != 2 {
		t.Fatalf("expected two stopped containers, got %+v", stoppedTask.Containers)
	}
	for _, c := range stoppedTask.Containers {
		if c.ExitCode == nil || *c.ExitCode != 0 {
			t.Fatalf("container %s exited non-zero: %+v (task StoppedReason=%q)", c.Name, c.ExitCode, stoppedTask.StoppedReason)
		}
	}

	logURL := endpoint + "/_tarn/admin/logs/events/" + url.PathEscape(logGroup) + "?limit=200"
	var sawWorkdir, sawUID, sawSharedContent bool
	for attempt := 0; attempt < 40; attempt++ {
		resp, err := http.Get(logURL)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Events []struct {
				Message string `json:"message"`
			} `json:"events"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if decodeErr != nil {
			t.Fatalf("decode ECS log events response: %v", decodeErr)
		}
		for _, event := range body.Events {
			if strings.Contains(event.Message, "WORKDIR=/tmp") {
				sawWorkdir = true
			}
			if strings.Contains(event.Message, "UID=0") {
				sawUID = true
			}
			if strings.TrimSpace(event.Message) == payload {
				sawSharedContent = true
			}
		}
		if sawWorkdir && sawUID && sawSharedContent {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("did not observe expected log output: workdir=%v uid=%v sharedContent=%v", sawWorkdir, sawUID, sawSharedContent)
}
