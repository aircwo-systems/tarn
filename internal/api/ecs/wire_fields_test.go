package ecs

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// TestRegisterTaskDefinitionEchoesDriftAvoidanceFields registers a task
// definition using every field added to avoid Terraform aws_ecs_task_definition
// drift (see docs/design/ecs-support.md), then asserts DescribeTaskDefinition
// echoes back an identical JSON value for every one of those keys. Comparing
// as generic maps (rather than decoding into types.TaskDefinition again)
// catches a key silently missing from the wire shape, which decoding into
// the same Go struct used to build the request would not.
func TestRegisterTaskDefinitionEchoesDriftAvoidanceFields(t *testing.T) {
	h := newTestHandler(t)

	trueVal := true
	falseVal := false
	in := types.RegisterTaskDefinitionInput{
		Family: "drift-check",
		Cpu:    "512",
		Memory: "1024",
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:                   "app",
				Image:                  "example/app:latest",
				WorkingDirectory:       "/srv/app",
				User:                   "1000:1000",
				StopTimeout:            45,
				StartTimeout:           60,
				Ulimits:                []types.Ulimit{{Name: "nofile", SoftLimit: 1024, HardLimit: 2048}},
				DockerLabels:           map[string]string{"team": "platform"},
				MountPoints:            []types.MountPoint{{SourceVolume: "shared", ContainerPath: "/data", ReadOnly: true}},
				VolumesFrom:            []types.VolumeFrom{{SourceContainer: "sidecar", ReadOnly: false}},
				ReadonlyRootFilesystem: &trueVal,
				Privileged:             &falseVal,
				LinuxParameters: &types.LinuxParameters{
					InitProcessEnabled: &trueVal,
					Capabilities:       &types.KernelCapabilities{Add: []string{"NET_ADMIN"}, Drop: []string{"MKNOD"}},
					SharedMemorySize:   64,
					Tmpfs:              []types.Tmpfs{{ContainerPath: "/tmp", Size: 32, MountOptions: []string{"noexec"}}},
				},
				Hostname:       "app-host",
				DnsServers:     []string{"1.1.1.1"},
				ExtraHosts:     []types.HostEntry{{Hostname: "db.local", IpAddress: "10.0.0.5"}},
				Interactive:    &trueVal,
				PseudoTerminal: &falseVal,
				SystemControls: []types.SystemControl{{Namespace: "net.core.somaxconn", Value: "1024"}},
				EnvironmentFiles: []types.EnvironmentFile{
					{Value: "arn:aws:s3:::bucket/env", Type: "s3"},
				},
				RepositoryCredentials: &types.RepositoryCredentials{CredentialsParameter: "arn:aws:secretsmanager:::creds"},
				FirelensConfiguration: &types.FirelensConfiguration{Type: "fluentbit", Options: map[string]string{"enable-ecs-log-metadata": "true"}},
				Secrets: []types.ContainerSecret{
					{Name: "DB_PASSWORD", ValueFrom: "arn:aws:secretsmanager:us-east-1:000000000000:secret:db-pass-ab12cd"},
				},
				HealthCheck: &types.ContainerHealthCheck{
					Command:  []string{"CMD-SHELL", "curl -f http://localhost/ || exit 1"},
					Interval: intPtrForTest(15),
					Retries:  intPtrForTest(2),
				},
				DependsOn: []types.ContainerDependency{
					{ContainerName: "sidecar", Condition: types.ContainerConditionStart},
				},
			},
			{Name: "sidecar", Image: "example/sidecar:latest"},
		},
		TaskRoleArn:      "arn:aws:iam::000000000000:role/task-role",
		ExecutionRoleArn: "arn:aws:iam::000000000000:role/execution-role",
		PidMode:          "task",
		IpcMode:          "task",
		RuntimePlatform:  &types.RuntimePlatform{CpuArchitecture: "ARM64", OperatingSystemFamily: "LINUX"},
		EphemeralStorage: &types.EphemeralStorage{SizeInGiB: 30},
		Volumes: []types.Volume{
			{
				Name: "shared",
				DockerVolumeConfiguration: &types.DockerVolumeConfiguration{
					Scope:         "shared",
					Autoprovision: true,
					Driver:        "local",
					DriverOpts:    map[string]string{"type": "nfs"},
					Labels:        map[string]string{"env": "test"},
				},
			},
			{
				Name: "host-vol",
				Host: &types.HostVolumeProperties{SourcePath: "/host/data"},
			},
			{
				Name: "efs-vol",
				EfsVolumeConfiguration: &types.EFSVolumeConfiguration{
					FileSystemId:          "fs-12345",
					RootDirectory:         "/",
					TransitEncryption:     "ENABLED",
					TransitEncryptionPort: 2999,
					AuthorizationConfig:   &types.EFSAuthorizationConfig{AccessPointId: "fsap-1", IAM: "ENABLED"},
				},
			},
		},
		PlacementConstraints: []types.PlacementConstraint{{Type: "memberOf", Expression: "attribute:ecs.instance-type =~ t2.*"}},
	}

	rec := invoke(t, h, "RegisterTaskDefinition", in)
	if rec.Code != http.StatusOK {
		t.Fatalf("RegisterTaskDefinition status=%d body=%s", rec.Code, rec.Body.String())
	}
	var registerOut map[string]any
	decodeBody(t, rec, &registerOut)
	registered, ok := registerOut["taskDefinition"].(map[string]any)
	if !ok {
		t.Fatalf("expected taskDefinition in response, got %+v", registerOut)
	}

	describe := invoke(t, h, "DescribeTaskDefinition", map[string]any{"TaskDefinition": "drift-check"})
	if describe.Code != http.StatusOK {
		t.Fatalf("DescribeTaskDefinition status=%d body=%s", describe.Code, describe.Body.String())
	}
	var describeOut map[string]any
	decodeBody(t, describe, &describeOut)
	described, ok := describeOut["taskDefinition"].(map[string]any)
	if !ok {
		t.Fatalf("expected taskDefinition in describe response, got %+v", describeOut)
	}

	taskDefKeys := []string{
		"taskRoleArn", "executionRoleArn", "pidMode", "ipcMode",
		"runtimePlatform", "ephemeralStorage", "volumes", "placementConstraints",
	}
	for _, key := range taskDefKeys {
		assertJSONEqual(t, key, registered[key], described[key])
	}

	registeredContainers, _ := registered["containerDefinitions"].([]any)
	describedContainers, _ := described["containerDefinitions"].([]any)
	if len(registeredContainers) != 2 || len(describedContainers) != 2 {
		t.Fatalf("expected 2 container definitions on both sides, got %d/%d", len(registeredContainers), len(describedContainers))
	}
	regApp, _ := registeredContainers[0].(map[string]any)
	descApp, _ := describedContainers[0].(map[string]any)

	containerKeys := []string{
		"workingDirectory", "user", "stopTimeout", "startTimeout",
		"ulimits", "dockerLabels", "mountPoints", "volumesFrom",
		"readonlyRootFilesystem", "privileged", "linuxParameters",
		"hostname", "dnsServers", "extraHosts", "interactive", "pseudoTerminal",
		"systemControls", "environmentFiles", "repositoryCredentials", "firelensConfiguration",
		"secrets", "healthCheck", "dependsOn",
	}
	for _, key := range containerKeys {
		assertJSONEqual(t, key, regApp[key], descApp[key])
	}

	// secrets must echo the valueFrom reference verbatim and never contain a
	// resolved value.
	secrets, _ := descApp["secrets"].([]any)
	if len(secrets) != 1 {
		t.Fatalf("expected 1 echoed secret, got %+v", descApp["secrets"])
	}
	secret, _ := secrets[0].(map[string]any)
	if secret["name"] != "DB_PASSWORD" {
		t.Fatalf("secret name = %v, want DB_PASSWORD", secret["name"])
	}
	if secret["valueFrom"] != "arn:aws:secretsmanager:us-east-1:000000000000:secret:db-pass-ab12cd" {
		t.Fatalf("secret valueFrom = %v, want the Secrets Manager ARN unchanged", secret["valueFrom"])
	}
}

func intPtrForTest(v int) *int { return &v }

func assertJSONEqual(t *testing.T, key string, registered, described any) {
	t.Helper()
	if registered == nil {
		t.Fatalf("key %q: RegisterTaskDefinition response had no value to compare against", key)
	}
	if !reflect.DeepEqual(registered, described) {
		registeredJSON, _ := json.Marshal(registered)
		describedJSON, _ := json.Marshal(described)
		t.Errorf("key %q: DescribeTaskDefinition echo mismatch\n  registered: %s\n  described:  %s", key, registeredJSON, describedJSON)
	}
}
