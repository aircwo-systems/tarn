package ecs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }

// waitForCallSuffix polls the fake engine's call log until a call ending
// with suffix appears. Container names carry a tarn-ecs-<taskID> prefix
// (see containerDockerName), so assertions on created containers can only
// match the stable containerName suffix.
func waitForCallSuffix(t *testing.T, eng *fakeEngine, suffix string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		for _, c := range eng.callLog() {
			if strings.HasSuffix(c, suffix) {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for call with suffix %q (calls: %v)", suffix, eng.callLog())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// --- dependsOn validation ----------------------------------------------------

func TestValidateDependsOnRejectsUnknownContainer(t *testing.T) {
	_, svc, _, _ := newTestRunner(t)
	_, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "bad-dep",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "app", Image: "example/app:latest", DependsOn: []types.ContainerDependency{
				{ContainerName: "ghost", Condition: types.ContainerConditionStart},
			}},
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown dependsOn container")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) || svcErr.Code != "ClientException" {
		t.Fatalf("expected ClientException, got %v", err)
	}
}

func TestValidateDependsOnRejectsUnknownCondition(t *testing.T) {
	_, svc, _, _ := newTestRunner(t)
	_, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "bad-condition",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "init", Image: "example/init:latest"},
			{Name: "app", Image: "example/app:latest", DependsOn: []types.ContainerDependency{
				{ContainerName: "init", Condition: "WHENEVER"},
			}},
		},
	})
	var svcErr *ServiceError
	if err == nil || !errors.As(err, &svcErr) || svcErr.Code != "ClientException" {
		t.Fatalf("expected ClientException for unknown condition, got %v", err)
	}
}

func TestValidateDependsOnRejectsCycle(t *testing.T) {
	_, svc, _, _ := newTestRunner(t)
	_, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "cyclic",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "a", Image: "example/a:latest", DependsOn: []types.ContainerDependency{
				{ContainerName: "b", Condition: types.ContainerConditionStart},
			}},
			{Name: "b", Image: "example/b:latest", DependsOn: []types.ContainerDependency{
				{ContainerName: "a", Condition: types.ContainerConditionStart},
			}},
		},
	})
	var svcErr *ServiceError
	if err == nil || !errors.As(err, &svcErr) || svcErr.Code != "ClientException" {
		t.Fatalf("expected ClientException for cycle, got %v", err)
	}
}

func TestValidateDependsOnAcceptsValidChain(t *testing.T) {
	_, svc, _, _ := newTestRunner(t)
	_, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "valid-chain",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "init", Image: "example/init:latest", Essential: boolPtr(false)},
			{Name: "app", Image: "example/app:latest", DependsOn: []types.ContainerDependency{
				{ContainerName: "init", Condition: types.ContainerConditionSuccess},
			}},
		},
	})
	if err != nil {
		t.Fatalf("expected valid dependsOn chain to register, got %v", err)
	}
}

// --- dependsOn launch ordering -----------------------------------------------

func TestDependsOnSuccessGatesDependentContainerStart(t *testing.T) {
	orig := dependsOnPollInterval
	dependsOnPollInterval = 2 * time.Millisecond
	defer func() { dependsOnPollInterval = orig }()

	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "depends-success",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "init", Image: "example/init:latest", Essential: boolPtr(false)},
			{Name: "app", Image: "example/app:latest", DependsOn: []types.ContainerDependency{
				{ContainerName: "init", Condition: types.ContainerConditionSuccess},
			}},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	type result struct {
		out *types.RunTaskOutput
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
		done <- result{out, err}
	}()

	waitForCallSuffix(t, eng, dockerNameForFamily(t, svc, tdOut.TaskDefinition.Family, "init"))

	// "app" must not be created before "init" exits.
	time.Sleep(20 * time.Millisecond)
	if n := len(eng.createdSpecs()); n != 1 {
		t.Fatalf("expected only init container created before its SUCCESS condition is met, got %d specs", n)
	}

	eng.finish("container-1", 0, nil)

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("RunTask: %v", res.err)
		}
		specs := eng.createdSpecs()
		if len(specs) != 2 {
			t.Fatalf("expected both containers created, got %d", len(specs))
		}
		r.Stop()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RunTask to launch the dependent container")
	}
}

func TestDependsOnSuccessFailureStopsTaskWithoutStartingDependent(t *testing.T) {
	orig := dependsOnPollInterval
	dependsOnPollInterval = 2 * time.Millisecond
	defer func() { dependsOnPollInterval = orig }()

	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "depends-success-fail",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "init", Image: "example/init:latest", Essential: boolPtr(false)},
			{Name: "app", Image: "example/app:latest", DependsOn: []types.ContainerDependency{
				{ContainerName: "init", Condition: types.ContainerConditionSuccess},
			}},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	type result struct {
		out *types.RunTaskOutput
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
		done <- result{out, err}
	}()

	waitForCallSuffix(t, eng, dockerNameForFamily(t, svc, tdOut.TaskDefinition.Family, "init"))
	eng.finish("container-1", 1, nil) // init exits non-zero: SUCCESS condition fails

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("RunTask: %v", res.err)
		}
		// A container-launch failure (here, a dependsOn SUCCESS condition
		// that was never met) is reported through Failures, not Tasks — the
		// same convention TestPartialTaskLaunchStopsContainersAlreadyStarted
		// establishes for an image-pull/create failure. RunTask does not
		// return a PROVISIONING task that asynchronously goes STOPPED.
		if len(res.out.Tasks) != 0 || len(res.out.Failures) != 1 {
			t.Fatalf("unexpected result: %+v", res.out)
		}
		taskArn := findTaskByFamily(t, svc, tdOut.TaskDefinition.Family)
		waitForTaskDesiredStopped(t, svc, taskArn)
		specs := eng.createdSpecs()
		if len(specs) != 1 {
			t.Fatalf("app should never have been created after init's SUCCESS condition failed, got %d specs", len(specs))
		}
		r.Stop()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RunTask")
	}
}

// findTaskByFamily locates the (single, in these tests) task record launched
// from a task definition family. RunTask's output does not carry a task ARN
// for a launch that failed before completion (see wireFailure / the
// TaskFailedToStart Failure entry), so tests that need to inspect the
// record's final StoppedReason/DesiredStatus look it up directly.
func findTaskByFamily(t *testing.T, svc *Service, family string) string {
	t.Helper()
	for _, task := range svc.store.ListTasks("") {
		if strings.Contains(task.TaskDefinitionArn, "/"+family+":") {
			return task.TaskArn
		}
	}
	t.Fatalf("no task found for family %q", family)
	return ""
}

// dockerNameForFamily mirrors containerDockerName's derivation for a task
// launched from family/containerName, computed from the last task the
// service created for that family — used only to build the fakeEngine call
// key waitForCallSuffix expects. Since the task ARN isn't known before
// RunTask returns, this instead just matches on the containerName suffix.
// containerDockerName is "tarn-ecs-<taskID>-<containerName>", so the stable
// matchable suffix is "-<containerName>".
func dockerNameForFamily(t *testing.T, svc *Service, family, containerName string) string {
	t.Helper()
	// containerDockerName is "tarn-ecs-<taskID>-<containerName>"; the task ID
	// prefix is unknown ahead of time, so callers can't build the full name.
	// waitForCallSuffix only needs a suffix match, so return just what's stable.
	return "-" + containerName
}

// --- secrets resolution -------------------------------------------------------

// fakeSecretsResolver is a minimal SecretsResolver for tests, avoiding any
// dependency on the real internal/secrets package.
type fakeSecretsResolver struct {
	secrets map[string]*types.Secret // keyed by name or ARN
	err     error
}

func (f *fakeSecretsResolver) GetSecretValue(nameOrArn string) (*types.Secret, error) {
	if f.err != nil {
		return nil, f.err
	}
	if s, ok := f.secrets[nameOrArn]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("secret %s not found", nameOrArn)
}

func TestSecretsResolvedAndInjectedAsEnvNotOnEnvironmentField(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	r.SetSecretsResolver(&fakeSecretsResolver{
		secrets: map[string]*types.Secret{
			"db-password": {Name: "db-password", SecretString: "s3cr3t-value"},
			"arn:aws:secretsmanager:us-east-1:000000000000:secret:api-config-ab12cd": {
				Name:         "api-config",
				SecretString: `{"apiKey":"jsonkey-value","other":"ignored"}`,
			},
		},
	})

	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "with-secrets",
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:  "app",
				Image: "example/app:latest",
				Secrets: []types.ContainerSecret{
					{Name: "DB_PASSWORD", ValueFrom: "db-password"},
					{Name: "API_KEY", ValueFrom: "arn:aws:secretsmanager:us-east-1:000000000000:secret:api-config-ab12cd:apiKey::"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	specs := eng.createdSpecs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 container spec, got %d", len(specs))
	}
	if got := specs[0].Env["DB_PASSWORD"]; got != "s3cr3t-value" {
		t.Fatalf("DB_PASSWORD = %q, want s3cr3t-value", got)
	}
	if got := specs[0].Env["API_KEY"]; got != "jsonkey-value" {
		t.Fatalf("API_KEY = %q, want jsonkey-value", got)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
	_ = out
}

func TestSecretsWinOverSameNamedEnvironmentEntry(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	r.SetSecretsResolver(&fakeSecretsResolver{
		secrets: map[string]*types.Secret{
			"shared-name": {Name: "shared-name", SecretString: "from-secret"},
		},
	})

	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "secret-wins",
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:        "app",
				Image:       "example/app:latest",
				Environment: []types.KeyValuePair{{Name: "SHARED", Value: "from-environment"}},
				Secrets:     []types.ContainerSecret{{Name: "SHARED", ValueFrom: "shared-name"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	if _, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family}); err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	specs := eng.createdSpecs()
	if got := specs[0].Env["SHARED"]; got != "from-secret" {
		t.Fatalf("SHARED = %q, want from-secret (secrets must win)", got)
	}
	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func TestMissingSecretStopsTaskWithoutLeakingValueOrCrashingRunner(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	r.SetSecretsResolver(&fakeSecretsResolver{secrets: map[string]*types.Secret{}})

	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "missing-secret",
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:    "app",
				Image:   "example/app:latest",
				Secrets: []types.ContainerSecret{{Name: "TOKEN", ValueFrom: "does-not-exist"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
	if err != nil {
		t.Fatalf("RunTask should not itself error (failure is per-task): %v", err)
	}
	// A secret-resolution failure fails the container the same way an
	// image-pull failure does: RunTask reports it through Failures, not as a
	// task that later transitions to STOPPED (see
	// TestPartialTaskLaunchStopsContainersAlreadyStarted). The task record
	// itself is still created and persisted STOPPED, just not returned here.
	if len(out.Tasks) != 0 || len(out.Failures) != 1 {
		t.Fatalf("unexpected result: %+v", out)
	}
	taskArn := findTaskByFamily(t, svc, tdOut.TaskDefinition.Family)

	waitForTaskDesiredStopped(t, svc, taskArn)
	task, err := svc.GetTask(taskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.LastStatus != types.TaskStatusStopped {
		t.Fatalf("LastStatus = %q, want STOPPED", task.LastStatus)
	}
	if !strings.Contains(task.StoppedReason, "ResourceInitializationError") {
		t.Fatalf("StoppedReason = %q, want it to mention ResourceInitializationError", task.StoppedReason)
	}
	if !strings.Contains(task.StoppedReason, "does-not-exist") {
		t.Fatalf("StoppedReason = %q, want it to reference the valueFrom", task.StoppedReason)
	}

	// No container was ever created — the secret failed before EnsureImageRef.
	if specs := eng.createdSpecs(); len(specs) != 0 {
		t.Fatalf("expected no container created after secret resolution failure, got %d", len(specs))
	}

	r.Stop()
}

// --- health checks -------------------------------------------------------------

func TestHealthCheckMapsToDockerHealthConfig(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "with-healthcheck",
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:  "app",
				Image: "example/app:latest",
				HealthCheck: &types.ContainerHealthCheck{
					Command:  []string{"CMD-SHELL", "curl -f http://localhost/ || exit 1"},
					Interval: intPtr(10),
					Retries:  intPtr(2),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	if _, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family}); err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	specs := eng.createdSpecs()
	if specs[0].HealthCheck == nil {
		t.Fatal("expected HealthCheck to be passed through to the engine spec")
	}
	if specs[0].HealthCheck.Interval == nil || *specs[0].HealthCheck.Interval != 10 {
		t.Fatalf("Interval = %v, want 10", specs[0].HealthCheck.Interval)
	}
	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func TestUnhealthyEssentialContainerStopsServiceTaskForReplacement(t *testing.T) {
	orig := healthPollInterval
	healthPollInterval = 5 * time.Millisecond
	defer func() { healthPollInterval = orig }()

	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "unhealthy-svc",
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:  "app",
				Image: "example/app:latest",
				HealthCheck: &types.ContainerHealthCheck{
					Command: []string{"CMD-SHELL", "true"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}

	// Service-owned launches require an active service record.
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svc1",
		TaskDefinition: tdOut.TaskDefinition.Family,
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	task, err := r.launchTask(context.Background(), cluster, tdOut.TaskDefinition, nil, "", serviceGroup("svc1"), "svc1", nil)
	if err != nil {
		t.Fatalf("launchTask: %v", err)
	}

	eng.setHealth("container-1", "unhealthy")

	waitForTaskDesiredStopped(t, svc, task.TaskArn)
	got, err := svc.GetTask(task.TaskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.StoppedReason != "Task failed container health checks" {
		t.Fatalf("StoppedReason = %q, want %q", got.StoppedReason, "Task failed container health checks")
	}

	finishAllContainers(eng, 137, nil)
	r.Stop()
}
