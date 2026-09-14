package ecs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	ecssvc "github.com/aircwo-systems/tarn/internal/ecs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := ecssvc.NewStore(cfg)
	svc := ecssvc.NewService(cfg, store)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}
	return NewHandler(svc)
}

func invoke(t *testing.T, h *Handler, action string, reqBody any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", servicePrefix+action)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("decode response body=%s: %v", rec.Body.String(), err)
	}
}

// fakeRunner is a Docker-free stand-in for T7's runner, letting this
// package's tests exercise RunTask/StopTask routing and the interface's
// deliberately positional StopTask signature without Docker.
type fakeRunner struct {
	runTaskFn func(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error)

	stopCalled bool
	stopArgs   struct {
		cluster string
		taskArn string
		reason  string
	}
	stopErr error
}

func (f *fakeRunner) RunTask(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error) {
	if f.runTaskFn != nil {
		return f.runTaskFn(ctx, in)
	}
	return &types.RunTaskOutput{}, nil
}

func (f *fakeRunner) StopTask(ctx context.Context, cluster, taskArn, reason string) error {
	f.stopCalled = true
	f.stopArgs.cluster = cluster
	f.stopArgs.taskArn = taskArn
	f.stopArgs.reason = reason
	return f.stopErr
}

// --- Dispatch / routing ------------------------------------------------------

func TestIsECSRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Amz-Target", "AmazonEC2ContainerServiceV20141113.ListClusters")
	if !IsECSRequest(req) {
		t.Fatalf("expected IsECSRequest to match ECS target prefix")
	}

	other := httptest.NewRequest(http.MethodPost, "/", nil)
	other.Header.Set("X-Amz-Target", "AWSEvents.PutRule")
	if IsECSRequest(other) {
		t.Fatalf("expected IsECSRequest to reject a non-ECS target")
	}
}

func TestDispatchInvalidTarget(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Amz-Target", "AWSEvents.PutRule")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDispatchUnknownActionReturnsClientError(t *testing.T) {
	h := newTestHandler(t)
	rec := invoke(t, h, "SomeFutureAction", map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	decodeBody(t, rec, &errBody)
	if errBody["__type"] != "InvalidAction" {
		t.Fatalf("expected InvalidAction, got %+v", errBody)
	}
}

func TestResponsesUseECSWireCasingAndEpochSeconds(t *testing.T) {
	h := newTestHandler(t)

	cluster := invoke(t, h, "CreateCluster", map[string]any{"ClusterName": "wire-cluster"})
	if cluster.Code != http.StatusOK {
		t.Fatalf("CreateCluster status=%d body=%s", cluster.Code, cluster.Body.String())
	}
	var clusterBody map[string]any
	decodeBody(t, cluster, &clusterBody)
	clusterShape, ok := clusterBody["cluster"].(map[string]any)
	if !ok {
		t.Fatalf("expected cluster response object, got %T: %v", clusterBody["cluster"], clusterBody["cluster"])
	}
	if clusterShape["clusterName"] != "wire-cluster" || clusterShape["clusterArn"] == "" {
		t.Fatalf("unexpected camelCase cluster shape: %v", clusterShape)
	}
	if _, ok := clusterShape["ClusterName"]; ok {
		t.Fatalf("response leaked PascalCase ClusterName: %v", clusterShape)
	}

	taskDefinition := invoke(t, h, "RegisterTaskDefinition", types.RegisterTaskDefinitionInput{
		Family:               "wire-family",
		ContainerDefinitions: []types.ContainerDefinition{{Name: "app", Image: "example/app:latest"}},
	})
	if taskDefinition.Code != http.StatusOK {
		t.Fatalf("RegisterTaskDefinition status=%d body=%s", taskDefinition.Code, taskDefinition.Body.String())
	}
	var taskDefinitionBody map[string]any
	decodeBody(t, taskDefinition, &taskDefinitionBody)
	tdShape, ok := taskDefinitionBody["taskDefinition"].(map[string]any)
	if !ok {
		t.Fatalf("expected taskDefinition response object, got %T: %v", taskDefinitionBody["taskDefinition"], taskDefinitionBody["taskDefinition"])
	}
	if tdShape["taskDefinitionArn"] == "" || tdShape["family"] != "wire-family" {
		t.Fatalf("unexpected camelCase task-definition shape: %v", tdShape)
	}
	if _, ok := tdShape["registeredAt"].(float64); !ok {
		t.Fatalf("registeredAt must be epoch seconds, got %T: %v", tdShape["registeredAt"], tdShape["registeredAt"])
	}
	if _, ok := tdShape["TaskDefinitionArn"]; ok {
		t.Fatalf("response leaked PascalCase TaskDefinitionArn: %v", tdShape)
	}
}

// --- Clusters -----------------------------------------------------------------

func TestClusterLifecycle(t *testing.T) {
	h := newTestHandler(t)

	create := invoke(t, h, "CreateCluster", map[string]any{"ClusterName": "test-cluster"})
	if create.Code != http.StatusOK {
		t.Fatalf("CreateCluster status=%d body=%s", create.Code, create.Body.String())
	}
	var createOut types.CreateClusterOutput
	decodeBody(t, create, &createOut)
	if createOut.Cluster == nil || createOut.Cluster.ClusterName != "test-cluster" {
		t.Fatalf("unexpected cluster: %+v", createOut.Cluster)
	}

	list := invoke(t, h, "ListClusters", map[string]any{})
	if list.Code != http.StatusOK {
		t.Fatalf("ListClusters status=%d body=%s", list.Code, list.Body.String())
	}
	var listOut types.ListClustersOutput
	decodeBody(t, list, &listOut)
	found := false
	for _, arn := range listOut.ClusterArns {
		if arn == createOut.Cluster.ClusterArn {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected created cluster in list, got %v", listOut.ClusterArns)
	}

	describe := invoke(t, h, "DescribeClusters", map[string]any{"Clusters": []string{"test-cluster"}})
	if describe.Code != http.StatusOK {
		t.Fatalf("DescribeClusters status=%d body=%s", describe.Code, describe.Body.String())
	}
	var describeOut types.DescribeClustersOutput
	decodeBody(t, describe, &describeOut)
	if len(describeOut.Clusters) != 1 || describeOut.Clusters[0].ClusterName != "test-cluster" {
		t.Fatalf("unexpected describe result: %+v", describeOut)
	}

	del := invoke(t, h, "DeleteCluster", map[string]any{"Cluster": "test-cluster"})
	if del.Code != http.StatusOK {
		t.Fatalf("DeleteCluster status=%d body=%s", del.Code, del.Body.String())
	}
	var delOut deleteClusterOutput
	decodeBody(t, del, &delOut)
	if delOut.Cluster == nil || delOut.Cluster.ClusterName != "test-cluster" {
		t.Fatalf("unexpected delete result: %+v", delOut.Cluster)
	}
}

func TestDeleteClusterNotFoundMapsServiceError(t *testing.T) {
	h := newTestHandler(t)
	rec := invoke(t, h, "DeleteCluster", map[string]any{"Cluster": "does-not-exist"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	decodeBody(t, rec, &errBody)
	if errBody["__type"] != "ClusterNotFoundException" {
		t.Fatalf("expected ClusterNotFoundException, got %+v", errBody)
	}
}

// --- Task definitions ----------------------------------------------------------

func registerWebTaskDef(t *testing.T, h *Handler) types.TaskDefinition {
	t.Helper()
	rec := invoke(t, h, "RegisterTaskDefinition", types.RegisterTaskDefinitionInput{
		Family: "web",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "web", Image: "nginx:latest"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("RegisterTaskDefinition status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out types.RegisterTaskDefinitionOutput
	decodeBody(t, rec, &out)
	if out.TaskDefinition == nil {
		t.Fatalf("expected task definition in response")
	}
	return *out.TaskDefinition
}

func TestTaskDefinitionLifecycle(t *testing.T) {
	h := newTestHandler(t)
	td := registerWebTaskDef(t, h)
	if td.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", td.Revision)
	}

	describe := invoke(t, h, "DescribeTaskDefinition", map[string]any{"TaskDefinition": "web"})
	if describe.Code != http.StatusOK {
		t.Fatalf("DescribeTaskDefinition status=%d body=%s", describe.Code, describe.Body.String())
	}
	var describeOut types.DescribeTaskDefinitionOutput
	decodeBody(t, describe, &describeOut)
	if describeOut.TaskDefinition == nil || describeOut.TaskDefinition.Family != "web" {
		t.Fatalf("unexpected describe result: %+v", describeOut.TaskDefinition)
	}

	deregister := invoke(t, h, "DeregisterTaskDefinition", map[string]any{"TaskDefinition": "web:1"})
	if deregister.Code != http.StatusOK {
		t.Fatalf("DeregisterTaskDefinition status=%d body=%s", deregister.Code, deregister.Body.String())
	}
	var deregisterOut types.DescribeTaskDefinitionOutput
	decodeBody(t, deregister, &deregisterOut)
	if deregisterOut.TaskDefinition == nil || deregisterOut.TaskDefinition.Status != types.TaskDefinitionStatusInactive {
		t.Fatalf("expected INACTIVE task definition, got %+v", deregisterOut.TaskDefinition)
	}
}

func TestDeregisterTaskDefinitionRequiresRevision(t *testing.T) {
	h := newTestHandler(t)
	registerWebTaskDef(t, h)

	rec := invoke(t, h, "DeregisterTaskDefinition", map[string]any{"TaskDefinition": "web"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	decodeBody(t, rec, &errBody)
	if errBody["__type"] != "InvalidParameterException" {
		t.Fatalf("expected InvalidParameterException, got %+v", errBody)
	}
}

func TestListTaskDefinitionsRoutesAndPaginates(t *testing.T) {
	h := newTestHandler(t)
	first := registerWebTaskDef(t, h)
	second := registerWebTaskDef(t, h)

	page := invoke(t, h, "ListTaskDefinitions", types.ListTaskDefinitionsInput{
		FamilyPrefix: "web",
		MaxResults:   1,
	})
	if page.Code != http.StatusOK {
		t.Fatalf("ListTaskDefinitions status=%d body=%s", page.Code, page.Body.String())
	}
	var pageOut types.ListTaskDefinitionsOutput
	decodeBody(t, page, &pageOut)
	if len(pageOut.TaskDefinitionArns) != 1 || pageOut.TaskDefinitionArns[0] != first.TaskDefinitionArn || pageOut.NextToken != "1" {
		t.Fatalf("unexpected first page: %+v", pageOut)
	}

	page2 := invoke(t, h, "ListTaskDefinitions", types.ListTaskDefinitionsInput{
		FamilyPrefix: "web",
		MaxResults:   1,
		NextToken:    pageOut.NextToken,
	})
	if page2.Code != http.StatusOK {
		t.Fatalf("ListTaskDefinitions page 2 status=%d body=%s", page2.Code, page2.Body.String())
	}
	var page2Out types.ListTaskDefinitionsOutput
	decodeBody(t, page2, &page2Out)
	if len(page2Out.TaskDefinitionArns) != 1 || page2Out.TaskDefinitionArns[0] != second.TaskDefinitionArn || page2Out.NextToken != "" {
		t.Fatalf("unexpected second page: %+v", page2Out)
	}
}

// --- RunTask / StopTask: nil-runner path ---------------------------------------

func TestRunTaskWithoutRunnerReturnsServerException(t *testing.T) {
	h := newTestHandler(t)
	rec := invoke(t, h, "RunTask", types.RunTaskInput{TaskDefinition: "web"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	decodeBody(t, rec, &errBody)
	if errBody["__type"] != "ServerException" {
		t.Fatalf("expected ServerException, got %+v", errBody)
	}
}

func TestStopTaskWithoutRunnerReturnsServerException(t *testing.T) {
	h := newTestHandler(t)
	rec := invoke(t, h, "StopTask", types.StopTaskInput{Task: "some-task"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	decodeBody(t, rec, &errBody)
	if errBody["__type"] != "ServerException" {
		t.Fatalf("expected ServerException, got %+v", errBody)
	}
}

// --- RunTask / StopTask: fake runner happy paths --------------------------------

func TestRunTaskDelegatesToRunner(t *testing.T) {
	h := newTestHandler(t)
	registerWebTaskDef(t, h)

	var gotInput *types.RunTaskInput
	runner := &fakeRunner{
		runTaskFn: func(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error) {
			gotInput = in
			return &types.RunTaskOutput{Tasks: []types.Task{{TaskArn: "arn:aws:ecs:local:000:task/default/abc"}}}, nil
		},
	}
	h.SetTaskRunner(runner)

	rec := invoke(t, h, "RunTask", types.RunTaskInput{TaskDefinition: "web", Count: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("RunTask status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotInput == nil || gotInput.TaskDefinition != "web" {
		t.Fatalf("expected runner to receive the decoded RunTaskInput, got %+v", gotInput)
	}
	var out types.RunTaskOutput
	decodeBody(t, rec, &out)
	if len(out.Tasks) != 1 || out.Tasks[0].TaskArn == "" {
		t.Fatalf("unexpected RunTask output: %+v", out)
	}
}

// TestStopTaskCallsPositionalInterface confirms the handler unmarshals the
// wire StopTaskInput struct and calls the TaskRunner's positional
// StopTask(cluster, taskArn, reason) signature, per the design doc's rule
// that these two shapes stay distinct.
func TestStopTaskCallsPositionalInterface(t *testing.T) {
	h := newTestHandler(t)
	td := registerWebTaskDef(t, h)

	// Seed a task record directly through the service, the way T7's runner
	// would, so StopTask's post-call GetTask lookup has something to find.
	cluster, err := h.svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("resolve cluster: %v", err)
	}
	task, err := h.svc.NewTaskRecord(cluster, &td, nil, types.LaunchTypeFargate, "")
	if err != nil {
		t.Fatalf("new task record: %v", err)
	}

	runner := &fakeRunner{}
	h.SetTaskRunner(runner)

	rec := invoke(t, h, "StopTask", types.StopTaskInput{
		Cluster: "test-cluster-ref",
		Task:    task.TaskArn,
		Reason:  "user requested",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("StopTask status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !runner.stopCalled {
		t.Fatalf("expected runner.StopTask to be called")
	}
	if runner.stopArgs.cluster != "test-cluster-ref" || runner.stopArgs.taskArn != task.TaskArn || runner.stopArgs.reason != "user requested" {
		t.Fatalf("unexpected positional args: %+v", runner.stopArgs)
	}
	var out types.StopTaskOutput
	decodeBody(t, rec, &out)
	if out.Task == nil || out.Task.TaskArn != task.TaskArn {
		t.Fatalf("unexpected StopTask output: %+v", out.Task)
	}
}

func TestStopTaskRunnerErrorMapsServiceError(t *testing.T) {
	h := newTestHandler(t)
	runner := &fakeRunner{stopErr: &ecssvc.ServiceError{Code: "ClusterNotFoundException", Message: "no such cluster", HTTPStatus: http.StatusBadRequest}}
	h.SetTaskRunner(runner)

	rec := invoke(t, h, "StopTask", types.StopTaskInput{Task: "arn:aws:ecs:local:000:task/default/missing"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	decodeBody(t, rec, &errBody)
	if errBody["__type"] != "ClusterNotFoundException" {
		t.Fatalf("expected ClusterNotFoundException, got %+v", errBody)
	}
}

// --- Tasks: list/describe -------------------------------------------------------

func TestListAndDescribeTasks(t *testing.T) {
	h := newTestHandler(t)
	td := registerWebTaskDef(t, h)
	cluster, err := h.svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("resolve cluster: %v", err)
	}
	task, err := h.svc.NewTaskRecord(cluster, &td, nil, types.LaunchTypeFargate, "")
	if err != nil {
		t.Fatalf("new task record: %v", err)
	}

	list := invoke(t, h, "ListTasks", map[string]any{})
	if list.Code != http.StatusOK {
		t.Fatalf("ListTasks status=%d body=%s", list.Code, list.Body.String())
	}
	var listOut types.ListTasksOutput
	decodeBody(t, list, &listOut)
	found := false
	for _, arn := range listOut.TaskArns {
		if arn == task.TaskArn {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected task in list, got %v", listOut.TaskArns)
	}

	describe := invoke(t, h, "DescribeTasks", map[string]any{"Tasks": []string{task.TaskArn}})
	if describe.Code != http.StatusOK {
		t.Fatalf("DescribeTasks status=%d body=%s", describe.Code, describe.Body.String())
	}
	var describeOut types.DescribeTasksOutput
	decodeBody(t, describe, &describeOut)
	if len(describeOut.Tasks) != 1 || describeOut.Tasks[0].TaskArn != task.TaskArn {
		t.Fatalf("unexpected describe result: %+v", describeOut)
	}
}

// --- Wire shape: raw JSON decoding -----------------------------------------------

// assertNoPascalCaseKeys walks a decoded JSON object (recursively through
// nested objects and arrays) and fails if any key looks like it leaked the
// domain type's PascalCase persistence tags instead of the camelCase wire
// shape ECS clients expect.
func assertNoPascalCaseKeys(t *testing.T, value any, path string) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if key != "" && key[0] >= 'A' && key[0] <= 'Z' {
				t.Fatalf("response leaked PascalCase key %q at %s: %v", key, path, v)
			}
			assertNoPascalCaseKeys(t, nested, path+"."+key)
		}
	case []any:
		for i, nested := range v {
			assertNoPascalCaseKeys(t, nested, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

func TestRunTaskAndDescribeTasksWireShapeIsCamelCaseWithNumericCreatedAt(t *testing.T) {
	h := newTestHandler(t)
	registerWebTaskDef(t, h)

	runner := &fakeRunner{
		runTaskFn: func(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error) {
			return &types.RunTaskOutput{Tasks: []types.Task{{
				TaskArn:           "arn:aws:ecs:local:000:task/default/abc",
				ClusterArn:        "arn:aws:ecs:local:000:cluster/default",
				TaskDefinitionArn: "arn:aws:ecs:local:000:task-definition/web:1",
				LastStatus:        types.TaskStatusRunning,
				DesiredStatus:     types.TaskDesiredStatusRunning,
				CreatedAt:         time.Now().UTC(),
				Containers: []types.TaskContainer{
					{Name: "web", ContainerArn: "arn:aws:ecs:local:000:container/default/abc/web", LastStatus: types.TaskStatusRunning},
				},
			}}}, nil
		},
	}
	h.SetTaskRunner(runner)

	run := invoke(t, h, "RunTask", types.RunTaskInput{TaskDefinition: "web", Count: 1})
	if run.Code != http.StatusOK {
		t.Fatalf("RunTask status=%d body=%s", run.Code, run.Body.String())
	}
	var runBody map[string]any
	decodeBody(t, run, &runBody)
	assertNoPascalCaseKeys(t, runBody, "RunTaskOutput")

	tasks, ok := runBody["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("expected one task in response, got %v", runBody["tasks"])
	}
	task, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatalf("expected task object, got %T", tasks[0])
	}
	for _, field := range []string{"taskArn", "clusterArn", "lastStatus", "desiredStatus"} {
		if s, ok := task[field].(string); !ok || s == "" {
			t.Fatalf("expected non-empty string %q, got %v: %v", field, task[field], task)
		}
	}
	if _, ok := task["createdAt"].(float64); !ok {
		t.Fatalf("createdAt must be a JSON number, got %T: %v", task["createdAt"], task["createdAt"])
	}
	containers, ok := task["containers"].([]any)
	if !ok || len(containers) != 1 {
		t.Fatalf("expected one container, got %v", task["containers"])
	}
	container, ok := containers[0].(map[string]any)
	if !ok || container["name"] != "web" {
		t.Fatalf("unexpected container shape: %v", containers[0])
	}

	describe := invoke(t, h, "DescribeTasks", map[string]any{"Tasks": []string{"arn:aws:ecs:local:000:task/default/abc"}})
	if describe.Code != http.StatusOK {
		t.Fatalf("DescribeTasks status=%d body=%s", describe.Code, describe.Body.String())
	}
	var describeBody map[string]any
	decodeBody(t, describe, &describeBody)
	assertNoPascalCaseKeys(t, describeBody, "DescribeTasksOutput")
}

func TestCreateAndDescribeServicesWireShapeHasPrimaryDeployment(t *testing.T) {
	h := newTestHandler(t)
	registerWebTaskDef(t, h)

	create := invoke(t, h, "CreateService", types.CreateServiceInput{
		ServiceName:    "wire-svc",
		TaskDefinition: "web",
		DesiredCount:   2,
	})
	if create.Code != http.StatusOK {
		t.Fatalf("CreateService status=%d body=%s", create.Code, create.Body.String())
	}
	var createBody map[string]any
	decodeBody(t, create, &createBody)
	assertNoPascalCaseKeys(t, createBody, "CreateServiceOutput")

	service, ok := createBody["service"].(map[string]any)
	if !ok {
		t.Fatalf("expected service object, got %v", createBody["service"])
	}
	if service["serviceArn"] == "" || service["serviceName"] != "wire-svc" {
		t.Fatalf("unexpected camelCase service shape: %v", service)
	}
	if _, ok := service["createdAt"].(float64); !ok {
		t.Fatalf("createdAt must be a JSON number, got %T: %v", service["createdAt"], service["createdAt"])
	}
	if desired, ok := service["desiredCount"].(float64); !ok || desired != 2 {
		t.Fatalf("unexpected desiredCount: %v", service["desiredCount"])
	}
	deployments, ok := service["deployments"].([]any)
	if !ok || len(deployments) != 1 {
		t.Fatalf("expected exactly one deployment, got %v", service["deployments"])
	}
	deployment, ok := deployments[0].(map[string]any)
	if !ok || deployment["status"] != "PRIMARY" {
		t.Fatalf("expected PRIMARY deployment, got %v", deployments[0])
	}

	describe := invoke(t, h, "DescribeServices", map[string]any{"Services": []string{"wire-svc"}})
	if describe.Code != http.StatusOK {
		t.Fatalf("DescribeServices status=%d body=%s", describe.Code, describe.Body.String())
	}
	var describeBody map[string]any
	decodeBody(t, describe, &describeBody)
	assertNoPascalCaseKeys(t, describeBody, "DescribeServicesOutput")

	services, ok := describeBody["services"].([]any)
	if !ok || len(services) != 1 {
		t.Fatalf("expected one described service, got %v", describeBody["services"])
	}
	describedService, ok := services[0].(map[string]any)
	if !ok {
		t.Fatalf("expected service object, got %T", services[0])
	}
	describedDeployments, ok := describedService["deployments"].([]any)
	if !ok || len(describedDeployments) != 1 {
		t.Fatalf("expected exactly one deployment on describe, got %v", describedService["deployments"])
	}
	describedDeployment, ok := describedDeployments[0].(map[string]any)
	if !ok || describedDeployment["status"] != "PRIMARY" {
		t.Fatalf("expected PRIMARY deployment on describe, got %v", describedDeployments[0])
	}
}

// --- Services -------------------------------------------------------------------

func TestServiceLifecycle(t *testing.T) {
	h := newTestHandler(t)
	registerWebTaskDef(t, h)

	create := invoke(t, h, "CreateService", types.CreateServiceInput{
		ServiceName:    "web-svc",
		TaskDefinition: "web",
		DesiredCount:   2,
	})
	if create.Code != http.StatusOK {
		t.Fatalf("CreateService status=%d body=%s", create.Code, create.Body.String())
	}
	var createOut types.CreateServiceOutput
	decodeBody(t, create, &createOut)
	if createOut.Service == nil || createOut.Service.DesiredCount != 2 {
		t.Fatalf("unexpected create service output: %+v", createOut.Service)
	}

	newCount := 3
	update := invoke(t, h, "UpdateService", types.UpdateServiceInput{
		Service:      "web-svc",
		DesiredCount: &newCount,
	})
	if update.Code != http.StatusOK {
		t.Fatalf("UpdateService status=%d body=%s", update.Code, update.Body.String())
	}
	var updateOut types.UpdateServiceOutput
	decodeBody(t, update, &updateOut)
	if updateOut.Service == nil || updateOut.Service.DesiredCount != 3 {
		t.Fatalf("unexpected update service output: %+v", updateOut.Service)
	}

	list := invoke(t, h, "ListServices", map[string]any{})
	if list.Code != http.StatusOK {
		t.Fatalf("ListServices status=%d body=%s", list.Code, list.Body.String())
	}
	var listOut types.ListServicesOutput
	decodeBody(t, list, &listOut)
	if len(listOut.ServiceArns) != 1 {
		t.Fatalf("expected 1 service, got %v", listOut.ServiceArns)
	}

	describe := invoke(t, h, "DescribeServices", map[string]any{"Services": []string{"web-svc"}})
	if describe.Code != http.StatusOK {
		t.Fatalf("DescribeServices status=%d body=%s", describe.Code, describe.Body.String())
	}
	var describeOut types.DescribeServicesOutput
	decodeBody(t, describe, &describeOut)
	if len(describeOut.Services) != 1 || describeOut.Services[0].ServiceName != "web-svc" {
		t.Fatalf("unexpected describe services output: %+v", describeOut)
	}

	del := invoke(t, h, "DeleteService", types.DeleteServiceInput{Service: "web-svc", Force: true})
	if del.Code != http.StatusOK {
		t.Fatalf("DeleteService status=%d body=%s", del.Code, del.Body.String())
	}
}
