package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	ecssvc "github.com/aircwo-systems/tarn/internal/ecs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// newTestECSService mirrors production wiring (store + service) for tests
// that exercise ECS-backed admin endpoints.
func newTestECSService(t *testing.T) *ecssvc.Service {
	t.Helper()
	cfg := config.Default()
	cfg.Host = "127.0.0.1"
	cfg.DataDir = t.TempDir()
	cfg.Port = 4566
	store := ecssvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init ecs store: %v", err)
	}
	return ecssvc.NewService(cfg, store)
}

// stubTaskRunner is a minimal types.TaskRunner for exercising the admin
// run-task trigger without Docker.
type stubTaskRunner struct {
	runTaskFn func(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error)
	lastInput *types.RunTaskInput
}

func (f *stubTaskRunner) RunTask(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error) {
	f.lastInput = in
	return f.runTaskFn(ctx, in)
}

func (f *stubTaskRunner) StopTask(ctx context.Context, cluster, taskArn, reason string) error {
	return nil
}

func TestRunECSTaskWithoutRunnerReturns503(t *testing.T) {
	h := newTestHandler(t)
	// newTestHandler wires no runner, mirroring a fresh account bundle.

	req := httptest.NewRequest(http.MethodPost, "/_tarn/admin/ecs/run-task",
		strings.NewReader(`{"cluster":"c","taskDefinition":"web","count":1}`))
	rec := httptest.NewRecorder()
	h.RunECSTask(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestRunECSTaskValidation(t *testing.T) {
	h := newTestHandler(t)
	h.SetECSTaskRunner(&stubTaskRunner{
		runTaskFn: func(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error) {
			return &types.RunTaskOutput{}, nil
		},
	})

	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"missing task definition", `{"cluster":"c","count":1}`, http.StatusBadRequest},
		{"bad count", `{"cluster":"c","taskDefinition":"web","count":99}`, http.StatusBadRequest},
		{"override without name", `{"taskDefinition":"web","overrides":{"containerOverrides":[{"environment":[{"name":"A","value":"1"}]}]}}`, http.StatusBadRequest},
		{"env without name", `{"taskDefinition":"web","overrides":{"containerOverrides":[{"name":"app","environment":[{"value":"1"}]}]}}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/_tarn/admin/ecs/run-task", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.RunECSTask(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestRunECSTaskPassesInputToRunner(t *testing.T) {
	h := newTestHandler(t)
	stub := &stubTaskRunner{
		runTaskFn: func(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error) {
			return &types.RunTaskOutput{
				Tasks: []types.Task{{
					TaskArn:           "arn:aws:ecs:us-east-1:000000000000:task/c/t1",
					ClusterArn:        "arn:aws:ecs:us-east-1:000000000000:cluster/c",
					TaskDefinitionArn: "arn:aws:ecs:us-east-1:000000000000:task-definition/web:3",
					LastStatus:        "RUNNING",
					DesiredStatus:     "RUNNING",
				}},
			}, nil
		},
	}
	h.SetECSTaskRunner(stub)

	req := httptest.NewRequest(http.MethodPost, "/_tarn/admin/ecs/run-task", strings.NewReader(`{
		"cluster": "c",
		"taskDefinition": "web:3",
		"count": 2,
		"launchType": "FARGATE",
		"overrides": {"containerOverrides": [{"name": "app", "environment": [{"name": "RUN_TAG", "value": "demo"}]}]}
	}`))
	rec := httptest.NewRecorder()
	h.RunECSTask(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if stub.lastInput == nil {
		t.Fatal("runner never called")
	}
	got := stub.lastInput
	if got.Cluster != "c" || got.TaskDefinition != "web:3" || got.Count != 2 || got.LaunchType != "FARGATE" {
		t.Fatalf("unexpected runner input: %+v", got)
	}
	if len(got.Overrides.ContainerOverrides) != 1 || got.Overrides.ContainerOverrides[0].Name != "app" {
		t.Fatalf("unexpected overrides: %+v", got.Overrides)
	}
	env := got.Overrides.ContainerOverrides[0].Environment
	if len(env) != 1 || env[0].Name != "RUN_TAG" || env[0].Value != "demo" {
		t.Fatalf("unexpected env: %+v", env)
	}

	var payload struct {
		Tasks []struct {
			TaskArn       string `json:"taskArn"`
			LastStatus    string `json:"lastStatus"`
			DesiredStatus string `json:"desiredStatus"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Tasks) != 1 || payload.Tasks[0].TaskArn == "" || payload.Tasks[0].LastStatus != "RUNNING" {
		t.Fatalf("unexpected tasks in response: %+v", payload.Tasks)
	}
}

func TestECSTaskDefinitionEndpoint(t *testing.T) {
	h := newTestHandler(t)
	h.SetECSService(newTestECSService(t))

	// Unknown family → 404.
	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/ecs/task-definitions/ghost", nil)
	req.SetPathValue("family", "ghost")
	rec := httptest.NewRecorder()
	h.ECSTaskDefinition(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown family status = %d, want 404", rec.Code)
	}

	// Register one, then describe it back with container names.
	reg, err := h.ecs.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "web",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "app", Image: "example/app:latest"},
			{Name: "sidecar", Image: "example/sidecar:latest"},
		},
	})
	if err != nil {
		t.Fatalf("register task definition: %v", err)
	}
	_ = reg

	req = httptest.NewRequest(http.MethodGet, "/_tarn/admin/ecs/task-definitions/web", nil)
	req.SetPathValue("family", "web")
	rec = httptest.NewRecorder()
	h.ECSTaskDefinition(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("describe status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Family     string `json:"family"`
		Revision   int    `json:"revision"`
		Containers []struct {
			Name      string `json:"name"`
			Image     string `json:"image"`
			Essential bool   `json:"essential"`
		} `json:"containers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Family != "web" || payload.Revision < 1 {
		t.Fatalf("unexpected definition: %+v", payload)
	}
	if len(payload.Containers) != 2 || payload.Containers[0].Name != "app" || !payload.Containers[0].Essential {
		t.Fatalf("unexpected containers: %+v", payload.Containers)
	}
}
