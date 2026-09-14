package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEventBridgeTargetECSParametersJSONRoundTrip(t *testing.T) {
	target := EventBridgeTarget{
		ID:  "ecs-target",
		Arn: "arn:aws:ecs:us-east-1:000000000000:cluster/default",
		EcsParameters: &EcsParameters{
			TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:3",
			TaskCount:         2,
			LaunchType:        LaunchTypeFargate,
			ContainerOverrides: []ContainerOverride{{
				Name: "app",
				Environment: []KeyValuePair{{
					Name:  "EVENT_PAYLOAD",
					Value: `{"source":"orders"}`,
				}},
			}},
		},
	}

	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{`"EcsParameters"`, `"TaskDefinitionArn"`, `"TaskCount"`, `"LaunchType"`, `"ContainerOverrides"`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("expected marshaled target to contain %s, got %s", field, raw)
		}
	}

	var roundTrip EventBridgeTarget
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTrip.EcsParameters == nil {
		t.Fatal("expected EcsParameters after round trip")
	}
	params := roundTrip.EcsParameters
	if params.TaskDefinitionArn != target.EcsParameters.TaskDefinitionArn || params.TaskCount != 2 || params.LaunchType != LaunchTypeFargate {
		t.Fatalf("unexpected ECS parameters after round trip: %+v", params)
	}
	if len(params.ContainerOverrides) != 1 || len(params.ContainerOverrides[0].Environment) != 1 {
		t.Fatalf("unexpected container overrides after round trip: %+v", params.ContainerOverrides)
	}
}

// TestEcsParametersNetworkConfigurationJSONShape guards the exact EventBridge
// wire casing for an ECS target's NetworkConfiguration: outer PascalCase
// "NetworkConfiguration", a lowercase-leading "awsvpcConfiguration" object,
// with PascalCase Subnets/SecurityGroups/AssignPublicIp inside it — matching
// AWS's EventBridge API, not ECS's own all-camelCase DescribeServices shape.
func TestEcsParametersNetworkConfigurationJSONShape(t *testing.T) {
	params := EcsParameters{
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/worker:3",
		LaunchType:        LaunchTypeFargate,
		NetworkConfiguration: &EcsNetworkConfiguration{
			AwsvpcConfiguration: &EcsAwsVpcConfiguration{
				Subnets:        []string{"subnet-1", "subnet-2"},
				SecurityGroups: []string{"sg-1"},
				AssignPublicIp: "ENABLED",
			},
		},
	}

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, field := range []string{
		`"NetworkConfiguration"`,
		`"awsvpcConfiguration"`,
		`"Subnets"`,
		`"SecurityGroups"`,
		`"AssignPublicIp"`,
	} {
		if !strings.Contains(body, field) {
			t.Errorf("expected marshaled EcsParameters to contain %s, got %s", field, body)
		}
	}
	// The all-camelCase ECS DescribeServices casing must not appear.
	for _, notExpected := range []string{`"networkConfiguration"`, `"subnets"`, `"securityGroups"`, `"assignPublicIp"`} {
		if strings.Contains(body, notExpected) {
			t.Errorf("expected EventBridge casing, but found ECS-style key %s in %s", notExpected, body)
		}
	}

	var roundTrip EcsParameters
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTrip.NetworkConfiguration == nil || roundTrip.NetworkConfiguration.AwsvpcConfiguration == nil {
		t.Fatal("expected NetworkConfiguration to survive round trip")
	}
	avc := roundTrip.NetworkConfiguration.AwsvpcConfiguration
	if len(avc.Subnets) != 2 || avc.Subnets[0] != "subnet-1" || len(avc.SecurityGroups) != 1 || avc.AssignPublicIp != "ENABLED" {
		t.Fatalf("unexpected network configuration after round trip: %+v", avc)
	}
}
