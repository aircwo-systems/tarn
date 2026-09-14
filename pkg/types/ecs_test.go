package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTaskJSONRoundTrip(t *testing.T) {
	started := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

	task := Task{
		TaskArn:           "arn:aws:ecs:us-east-1:000000000000:task/my-cluster/abc123",
		ClusterArn:        "arn:aws:ecs:us-east-1:000000000000:cluster/my-cluster",
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/my-family:1",
		LastStatus:        TaskStatusRunning,
		DesiredStatus:     TaskDesiredStatusRunning,
		Containers: []TaskContainer{
			{
				Name:        "app",
				ContainerID: "docker-id-1",
				LastStatus:  TaskStatusRunning,
				ExitCode:    nil, // not yet exited
			},
		},
		StartedAt: &started,
		CreatedAt: started,
	}

	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	wireFields := []string{
		`"TaskArn"`, `"ClusterArn"`, `"TaskDefinitionArn"`, `"LastStatus"`,
		`"DesiredStatus"`, `"Containers"`, `"StartedAt"`, `"CreatedAt"`,
	}
	for _, f := range wireFields {
		if !strings.Contains(string(raw), f) {
			t.Errorf("expected marshaled Task to contain field %s, got: %s", f, raw)
		}
	}

	// A nil ExitCode must marshal as absent, not as 0.
	if strings.Contains(string(raw), `"ExitCode"`) {
		t.Errorf("expected nil ExitCode to be omitted from JSON, got: %s", raw)
	}

	var round Task
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.TaskArn != task.TaskArn || round.LastStatus != task.LastStatus {
		t.Errorf("round trip mismatch: got %+v, want %+v", round, task)
	}
	if len(round.Containers) != 1 || round.Containers[0].ExitCode != nil {
		t.Errorf("expected round-tripped container ExitCode to remain nil, got %+v", round.Containers)
	}

	// Now confirm a non-nil exit code (including the zero value 0, which must
	// still be distinguishable from "absent") round-trips correctly.
	var zero int64 = 0
	task.Containers[0].ExitCode = &zero
	task.Containers[0].LastStatus = TaskStatusStopped

	raw, err = json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal with exit code: %v", err)
	}
	if !strings.Contains(string(raw), `"ExitCode":0`) {
		t.Errorf("expected exit code 0 to be present in JSON, got: %s", raw)
	}

	var round2 Task
	if err := json.Unmarshal(raw, &round2); err != nil {
		t.Fatalf("unmarshal with exit code: %v", err)
	}
	if round2.Containers[0].ExitCode == nil {
		t.Fatalf("expected non-nil ExitCode after round trip")
	}
	if *round2.Containers[0].ExitCode != 0 {
		t.Errorf("expected ExitCode 0, got %d", *round2.Containers[0].ExitCode)
	}
}

func TestRunTaskInputJSONRoundTrip(t *testing.T) {
	input := RunTaskInput{
		Cluster:        "my-cluster",
		TaskDefinition: "my-family:1",
		Count:          1,
		LaunchType:     LaunchTypeFargate,
		Overrides: &TaskOverride{
			ContainerOverrides: []ContainerOverride{
				{
					Name: "app",
					Environment: []KeyValuePair{
						{Name: "EVENT_PAYLOAD", Value: `{"detail-type":"test"}`},
					},
				},
			},
		},
	}

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	wireFields := []string{
		`"Cluster"`, `"TaskDefinition"`, `"Count"`, `"LaunchType"`,
		`"Overrides"`, `"ContainerOverrides"`, `"Name"`, `"Environment"`,
	}
	for _, f := range wireFields {
		if !strings.Contains(string(raw), f) {
			t.Errorf("expected marshaled RunTaskInput to contain field %s, got: %s", f, raw)
		}
	}

	var round RunTaskInput
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Cluster != input.Cluster || round.TaskDefinition != input.TaskDefinition {
		t.Errorf("round trip mismatch: got %+v, want %+v", round, input)
	}
	if round.Overrides == nil || len(round.Overrides.ContainerOverrides) != 1 {
		t.Fatalf("expected one container override, got %+v", round.Overrides)
	}
	co := round.Overrides.ContainerOverrides[0]
	if co.Name != "app" || len(co.Environment) != 1 || co.Environment[0].Name != "EVENT_PAYLOAD" {
		t.Errorf("unexpected container override: %+v", co)
	}
}

func TestListTaskDefinitionsJSONRoundTrip(t *testing.T) {
	input := ListTaskDefinitionsInput{
		FamilyPrefix: "web",
		MaxResults:   2,
		NextToken:    "2",
		Sort:         TaskDefinitionSortDescending,
		Status:       TaskDefinitionStatusInactive,
	}

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{`"FamilyPrefix"`, `"MaxResults"`, `"NextToken"`, `"Sort"`, `"Status"`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("expected marshaled input to contain %s, got %s", field, raw)
		}
	}

	var round ListTaskDefinitionsInput
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round != input {
		t.Fatalf("round trip mismatch: got %+v, want %+v", round, input)
	}

	output := ListTaskDefinitionsOutput{
		TaskDefinitionArns: []string{"arn:aws:ecs:local:000:task-definition/web:2"},
		NextToken:          "3",
	}
	raw, err = json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	var roundOutput ListTaskDefinitionsOutput
	if err := json.Unmarshal(raw, &roundOutput); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(roundOutput.TaskDefinitionArns) != 1 || roundOutput.TaskDefinitionArns[0] != output.TaskDefinitionArns[0] || roundOutput.NextToken != output.NextToken {
		t.Fatalf("output round trip mismatch: got %+v, want %+v", roundOutput, output)
	}
}

// TestTaskEventPayloadEnvMaxBytesLeavesRoomForTerminatingNUL pins the
// boundary at exactly one byte short of Linux's MAX_ARG_STRLEN, which counts
// the "NAME=value" string's terminating NUL against the 128 KiB ceiling even
// though Go's len() (and this constant) does not count it.
func TestTaskEventPayloadEnvMaxBytesLeavesRoomForTerminatingNUL(t *testing.T) {
	const linuxMaxArgStrlen = 128 * 1024
	entryLen := len("EVENT_PAYLOAD=") + TaskEventPayloadEnvMaxBytes + 1 // +1 for the NUL byte glibc/exec counts
	if entryLen != linuxMaxArgStrlen {
		t.Fatalf("EVENT_PAYLOAD=<value>\\0 must fit exactly in MAX_ARG_STRLEN, got %d want %d", entryLen, linuxMaxArgStrlen)
	}
}
