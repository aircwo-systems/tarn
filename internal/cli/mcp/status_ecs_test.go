package mcp

import (
	"testing"
)

// TestStatusReportsECSState covers that tarn_status decodes the overview's
// ecs key into clusters/services/tasks, resolves clusterArn to the cluster
// name for services and tasks, and turns each container's network bindings
// into a dialable host URL — this is what lets a caller verify an ECS
// service's task actually came up and where to reach it.
func TestStatusReportsECSState(t *testing.T) {
	exitCode := int64(0)
	inst := newInstance(t).json("GET /_tarn/admin/overview", map[string]any{
		"services": []string{"ecs"},
		"config":   map[string]any{"region": "us-east-1", "accountId": "000000000000"},
		"counts":   map[string]int{},
		"ecs": map[string]any{
			"clusters": []map[string]any{
				{"name": "workers", "arn": "arn:aws:ecs:us-east-1:000000000000:cluster/workers",
					"status": "ACTIVE", "runningTasks": 1, "pendingTasks": 0},
			},
			"services": []map[string]any{
				{"name": "worker-service", "clusterArn": "arn:aws:ecs:us-east-1:000000000000:cluster/workers",
					"status": "ACTIVE", "desiredCount": 2, "runningCount": 1, "pendingCount": 1},
			},
			"tasks": []map[string]any{
				{
					"arn":           "arn:aws:ecs:us-east-1:000000000000:task/workers/abc123",
					"clusterArn":    "arn:aws:ecs:us-east-1:000000000000:cluster/workers",
					"group":         "service:worker-service",
					"lastStatus":    "RUNNING",
					"desiredStatus": "RUNNING",
					"healthStatus":  "HEALTHY",
					"containers": []map[string]any{
						{
							"name": "worker", "lastStatus": "RUNNING", "exitCode": exitCode, "healthStatus": "HEALTHY",
							"networkBindings": []map[string]any{
								{"containerPort": 8080, "hostPort": 32768, "protocol": "tcp", "bindIP": "127.0.0.1"},
							},
						},
					},
				},
				{
					"arn":           "arn:aws:ecs:us-east-1:000000000000:task/workers/def456",
					"clusterArn":    "arn:aws:ecs:us-east-1:000000000000:cluster/workers",
					"lastStatus":    "STOPPED",
					"desiredStatus": "STOPPED",
					"stoppedReason": "Essential container in task exited",
					"stopCode":      "EssentialContainerExited",
					"containers": []map[string]any{
						{"name": "app", "lastStatus": "STOPPED", "exitCode": 1, "reason": "OutOfMemoryError: container killed"},
					},
				},
			},
		},
	})

	out := callStatus(t, inst.session(), nil)

	if out.ECS == nil {
		t.Fatal("ecs is nil")
	}
	if len(out.ECS.Clusters) != 1 || out.ECS.Clusters[0].Name != "workers" || out.ECS.Clusters[0].RunningTasks != 1 {
		t.Fatalf("unexpected clusters: %+v", out.ECS.Clusters)
	}
	if len(out.ECS.Services) != 1 {
		t.Fatalf("services len = %d, want 1", len(out.ECS.Services))
	}
	svc := out.ECS.Services[0]
	if svc.Name != "worker-service" || svc.Cluster != "workers" || svc.DesiredCount != 2 || svc.RunningCount != 1 || svc.PendingCount != 1 {
		t.Errorf("unexpected service: %+v, want cluster resolved to its name", svc)
	}
	if len(out.ECS.Tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2", len(out.ECS.Tasks))
	}
	task := out.ECS.Tasks[0]
	if task.Cluster != "workers" || task.Group != "service:worker-service" || task.LastStatus != "RUNNING" || task.HealthStatus != "HEALTHY" {
		t.Errorf("unexpected task: %+v", task)
	}
	stopped := out.ECS.Tasks[1]
	if stopped.StopCode != "EssentialContainerExited" || stopped.StoppedReason != "Essential container in task exited" {
		t.Errorf("stopped task = %+v, want stopCode and stoppedReason carried through", stopped)
	}
	if len(stopped.Containers) != 1 || stopped.Containers[0].Reason != "OutOfMemoryError: container killed" {
		t.Errorf("stopped task containers = %+v, want container reason carried through", stopped.Containers)
	}
	if len(task.Containers) != 1 {
		t.Fatalf("containers len = %d, want 1", len(task.Containers))
	}
	container := task.Containers[0]
	if container.Name != "worker" || container.ExitCode == nil || *container.ExitCode != 0 || container.HealthStatus != "HEALTHY" {
		t.Errorf("unexpected container: %+v", container)
	}
	if len(container.HostURLs) != 1 || container.HostURLs[0] != "http://127.0.0.1:32768" {
		t.Errorf("hostUrls = %v, want a dialable URL for the published port", container.HostURLs)
	}
}

// TestStatusOmitsECSWhenNotProvisioned covers that a plain instance (no ECS)
// does not report a spurious ecs section.
func TestStatusOmitsECSWhenNotProvisioned(t *testing.T) {
	tarn := fakeTarn(t)
	out := callStatus(t, connect(t, tarn.URL), nil)

	if out.ECS != nil {
		t.Errorf("ecs = %+v, want nil when the instance has no ECS state", out.ECS)
	}
}
