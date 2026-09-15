package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apigatewaysvc "github.com/aircwo-systems/tarn/internal/apigateway"
	apigatewayv1svc "github.com/aircwo-systems/tarn/internal/apigatewayv1"
	"github.com/aircwo-systems/tarn/internal/config"
	dynamodbsvc "github.com/aircwo-systems/tarn/internal/dynamodb"
	ecssvc "github.com/aircwo-systems/tarn/internal/ecs"
	eventbridgesvc "github.com/aircwo-systems/tarn/internal/eventbridge"
	eventsourcesvc "github.com/aircwo-systems/tarn/internal/eventsource"
	infrasvc "github.com/aircwo-systems/tarn/internal/infrastructure"
	lambdasvc "github.com/aircwo-systems/tarn/internal/lambda"
	logssvc "github.com/aircwo-systems/tarn/internal/logs"
	s3svc "github.com/aircwo-systems/tarn/internal/s3"
	secretssvc "github.com/aircwo-systems/tarn/internal/secrets"
	snssvc "github.com/aircwo-systems/tarn/internal/sns"
	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestQueueMessagesReturnsMessages(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.sqs.CreateQueue("jobs", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if _, err := h.sqs.SendMessage("jobs", "hello queue", 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/queues/jobs/messages?limit=10", nil)
	req.SetPathValue("name", "jobs")
	rec := httptest.NewRecorder()

	h.QueueMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Queue    string         `json:"queue"`
		Messages []queueMessage `json:"messages"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Queue != "jobs" {
		t.Fatalf("queue = %q, want %q", payload.Queue, "jobs")
	}
	if len(payload.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(payload.Messages))
	}
	if payload.Messages[0].Body != "hello queue" {
		t.Fatalf("message body = %q, want %q", payload.Messages[0].Body, "hello queue")
	}
}

func TestQueueMessagesReturnsDLQRetryCount(t *testing.T) {
	h := newTestHandler(t)

	dlq, err := h.sqs.CreateQueue("jobs-dlq", nil, nil)
	if err != nil {
		t.Fatalf("create dlq: %v", err)
	}
	redrivePolicy, err := json.Marshal(map[string]any{
		"deadLetterTargetArn": dlq.QueueArn,
		"maxReceiveCount":     2,
	})
	if err != nil {
		t.Fatalf("marshal redrive policy: %v", err)
	}
	if _, err := h.sqs.CreateQueue("jobs", map[string]string{"RedrivePolicy": string(redrivePolicy)}, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if _, err := h.sqs.SendMessage("jobs", "poison", 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	var msg *types.SQSMessage
	for i := 0; i < 2; i++ {
		msgs, err := h.sqs.ReceiveMessage("jobs", 1, 0, 0)
		if err != nil {
			t.Fatalf("receive #%d: %v", i+1, err)
		}
		if len(msgs) != 1 {
			t.Fatalf("receive #%d len = %d, want 1", i+1, len(msgs))
		}
		msg = msgs[0]
	}

	moved, _, err := h.sqs.MoveToDLQIfExceeded("jobs", msg)
	if err != nil {
		t.Fatalf("move to dlq: %v", err)
	}
	if !moved {
		t.Fatalf("expected message to move to dlq")
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/queues/jobs-dlq/messages?limit=10", nil)
	req.SetPathValue("name", "jobs-dlq")
	rec := httptest.NewRecorder()

	h.QueueMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Messages []queueMessage `json:"messages"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(payload.Messages))
	}
	if payload.Messages[0].RetryCount != 2 {
		t.Fatalf("retry count = %d, want 2", payload.Messages[0].RetryCount)
	}
}

func TestQueueMessagesInvalidLimit(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/queues/jobs/messages?limit=abc", nil)
	req.SetPathValue("name", "jobs")
	rec := httptest.NewRecorder()

	h.QueueMessages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestQueueMessagesNotFound(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/queues/missing/messages", nil)
	req.SetPathValue("name", "missing")
	rec := httptest.NewRecorder()

	h.QueueMessages(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSecretValueReturnsSecretString(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.secrets.CreateSecret("api-key", "", "super-secret", nil, nil); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/secrets/api-key/value", nil)
	req.SetPathValue("name", "api-key")
	rec := httptest.NewRecorder()

	h.SecretValue(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Name      string `json:"name"`
		Value     string `json:"value"`
		ValueType string `json:"valueType"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Name != "api-key" {
		t.Fatalf("name = %q, want %q", payload.Name, "api-key")
	}
	if payload.Value != "super-secret" {
		t.Fatalf("value = %q, want %q", payload.Value, "super-secret")
	}
	if payload.ValueType != "string" {
		t.Fatalf("valueType = %q, want %q", payload.ValueType, "string")
	}
}

func TestSecretValueNotFound(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/secrets/missing/value", nil)
	req.SetPathValue("name", "missing")
	rec := httptest.NewRecorder()

	h.SecretValue(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestOverviewIncludesGateways(t *testing.T) {
	h := newTestHandler(t)

	api, err := h.apigw.CreateAPI("orders-http-api", "test api", "HTTP", "", nil)
	if err != nil {
		t.Fatalf("create api: %v", err)
	}
	if _, err := h.apigw.ListStages(api.APIID); err != nil {
		t.Fatalf("list stages: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()

	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Services []string `json:"services"`
		Counts   struct {
			Gateways int `json:"gateways"`
		} `json:"counts"`
		Gateways []gatewaySummary `json:"gateways"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Counts.Gateways != 1 {
		t.Fatalf("gateways count = %d, want 1", payload.Counts.Gateways)
	}
	if len(payload.Gateways) != 1 {
		t.Fatalf("gateways len = %d, want 1", len(payload.Gateways))
	}
	if payload.Gateways[0].Name != "orders-http-api" {
		t.Fatalf("gateway name = %q, want %q", payload.Gateways[0].Name, "orders-http-api")
	}
	found := false
	for _, svc := range payload.Services {
		if svc == "apigatewayv2" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("services missing apigatewayv2: %v", payload.Services)
	}
}

func TestOverviewIncludesEventBridgeTargetInput(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.eventbridge.PutRule("log-burst-rule", "rate(1 minute)", "", "ENABLED", "", "default"); err != nil {
		t.Fatalf("put eventbridge rule: %v", err)
	}
	failed, err := h.eventbridge.PutTargets("log-burst-rule", "default", []types.EventBridgeTarget{{
		ID:    "target-1",
		Arn:   "log-burst",
		Input: `{"shouldFail":true}`,
	}})
	if err != nil {
		t.Fatalf("put eventbridge target: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("put eventbridge target failures: %+v", failed)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()
	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		EventBridgeRules []eventBridgeRuleSummary `json:"eventBridgeRules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.EventBridgeRules) != 1 || len(payload.EventBridgeRules[0].Targets) != 1 {
		t.Fatalf("unexpected eventbridge overview: %+v", payload.EventBridgeRules)
	}
	if got := payload.EventBridgeRules[0].Targets[0].Input; got != `{"shouldFail":true}` {
		t.Fatalf("target input = %q, want %q", got, `{"shouldFail":true}`)
	}
}

func TestOverviewIncludesResourceTags(t *testing.T) {
	h := newTestHandler(t)

	_, err := h.lambda.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "orders-r10",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/lambda-role",
		Tags: map[string]string{
			"feature": "r10",
			"team":    "payments",
		},
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	if _, err := h.sqs.CreateQueue("orders-r10-queue", nil, map[string]string{"feature": "r10"}); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if _, err := h.secrets.CreateSecret("orders-r10-secret", "", "value", nil, []types.SecretTag{{Key: "feature", Value: "r10"}}); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()

	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Functions []functionSummary `json:"functions"`
		Queues    []queueSummary    `json:"queues"`
		Secrets   []secretSummary   `json:"secrets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Functions) != 1 {
		t.Fatalf("functions len = %d, want 1", len(payload.Functions))
	}
	if payload.Functions[0].Tags["feature"] != "r10" || payload.Functions[0].TagCount != 2 {
		t.Fatalf("unexpected function tags: %+v", payload.Functions[0])
	}

	if len(payload.Queues) != 1 {
		t.Fatalf("queues len = %d, want 1", len(payload.Queues))
	}
	if payload.Queues[0].Tags["feature"] != "r10" || payload.Queues[0].TagCount != 1 {
		t.Fatalf("unexpected queue tags: %+v", payload.Queues[0])
	}

	if len(payload.Secrets) != 1 {
		t.Fatalf("secrets len = %d, want 1", len(payload.Secrets))
	}
	if payload.Secrets[0].Tags["feature"] != "r10" || payload.Secrets[0].TagCount != 1 {
		t.Fatalf("unexpected secret tags: %+v", payload.Secrets[0])
	}
}

func TestOverviewIncludesQueueProcessedCount(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.sqs.CreateQueue("jobs", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if err := h.sqs.IncrementProcessedCount("jobs", 1); err != nil {
		t.Fatalf("increment processed count: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()
	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Queues []queueSummary `json:"queues"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Queues) != 1 {
		t.Fatalf("queues len = %d, want 1", len(payload.Queues))
	}
	if payload.Queues[0].ProcessedCount != 1 {
		t.Fatalf("processed count = %d, want 1", payload.Queues[0].ProcessedCount)
	}
}

func TestOverviewIncludesDynamoDBTablesAndStreams(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.dynamodb.CreateTable(&types.DynamoDBTable{
		TableName: "orders",
		AttributeDefinitions: []types.DynamoDBAttributeDefinition{
			{AttributeName: "pk", AttributeType: "S"},
		},
		KeySchema: []types.DynamoDBKeySchemaElement{
			{AttributeName: "pk", KeyType: "HASH"},
		},
		StreamSpecification: &types.DynamoDBStreamSpecification{
			StreamEnabled:  true,
			StreamViewType: "NEW_IMAGE",
		},
	}); err != nil {
		t.Fatalf("create dynamodb table: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()

	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Counts struct {
			DynamoDBTables  int `json:"dynamodbTables"`
			DynamoDBStreams int `json:"dynamodbStreams"`
		} `json:"counts"`
		DynamoDBTables  []dynamodbTableSummary  `json:"dynamodbTables"`
		DynamoDBStreams []dynamodbStreamSummary `json:"dynamodbStreams"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Counts.DynamoDBTables != 1 {
		t.Fatalf("dynamodb tables count = %d, want 1", payload.Counts.DynamoDBTables)
	}
	if payload.Counts.DynamoDBStreams != 1 {
		t.Fatalf("dynamodb streams count = %d, want 1", payload.Counts.DynamoDBStreams)
	}
	if len(payload.DynamoDBTables) != 1 || payload.DynamoDBTables[0].Name != "orders" {
		t.Fatalf("unexpected dynamodb tables: %+v", payload.DynamoDBTables)
	}
	if len(payload.DynamoDBStreams) != 1 || payload.DynamoDBStreams[0].TableName != "orders" {
		t.Fatalf("unexpected dynamodb streams: %+v", payload.DynamoDBStreams)
	}
}

func TestOverviewIncludesECSState(t *testing.T) {
	h := newTestHandler(t)

	ecsStore := ecssvc.NewStore(h.cfg)
	ecsService := ecssvc.NewService(h.cfg, ecsStore)
	if err := ecsService.Init(); err != nil {
		t.Fatalf("init ecs service: %v", err)
	}
	h.SetECSService(ecsService)

	clusterOut, err := ecsService.CreateCluster(&types.CreateClusterInput{ClusterName: "workers"})
	if err != nil {
		t.Fatalf("create ecs cluster: %v", err)
	}
	taskDefinitionOut, err := ecsService.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "worker",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "worker", Image: "worker:latest"},
		},
	})
	if err != nil {
		t.Fatalf("register ecs task definition: %v", err)
	}
	serviceOut, err := ecsService.CreateService(&types.CreateServiceInput{
		Cluster:        clusterOut.Cluster.ClusterArn,
		ServiceName:    "worker-service",
		TaskDefinition: taskDefinitionOut.TaskDefinition.TaskDefinitionArn,
		DesiredCount:   2,
		LaunchType:     types.LaunchTypeFargate,
	})
	if err != nil {
		t.Fatalf("create ecs service: %v", err)
	}
	if _, err := ecsService.SetServiceCounts(clusterOut.Cluster, serviceOut.Service.ServiceName, 1, 1); err != nil {
		t.Fatalf("set ecs service counts: %v", err)
	}
	task, err := ecsService.NewTaskRecord(
		clusterOut.Cluster,
		taskDefinitionOut.TaskDefinition,
		nil,
		types.LaunchTypeFargate,
		"service:"+serviceOut.Service.ServiceName,
	)
	if err != nil {
		t.Fatalf("create ecs task record: %v", err)
	}
	if _, err := ecsService.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
		t.Fatalf("set ecs task status: %v", err)
	}
	if _, err := ecsService.SetContainerStatus(task.TaskArn, "worker", types.TaskStatusRunning); err != nil {
		t.Fatalf("set ecs container status: %v", err)
	}
	if _, err := ecsService.SetContainerNetworkBindings(task.TaskArn, "worker", []types.NetworkBinding{
		{ContainerPort: 8080, HostPort: 32768, Protocol: "tcp", BindIP: "127.0.0.1"},
	}); err != nil {
		t.Fatalf("set ecs container network bindings: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()
	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		Services []string     `json:"services"`
		ECS      *ecsOverview `json:"ecs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ECS == nil {
		t.Fatal("ecs overview is nil")
	}
	if len(payload.ECS.Clusters) != 2 {
		t.Fatalf("ecs clusters len = %d, want 2 including default: %+v", len(payload.ECS.Clusters), payload.ECS.Clusters)
	}
	var cluster ecsClusterSummary
	for _, candidate := range payload.ECS.Clusters {
		if candidate.Name == "workers" {
			cluster = candidate
			break
		}
	}
	if cluster.Arn != clusterOut.Cluster.ClusterArn {
		t.Fatalf("ecs cluster arn = %q, want %q", cluster.Arn, clusterOut.Cluster.ClusterArn)
	}
	if cluster.Status != types.ClusterStatusActive || cluster.RunningTasks != 1 || cluster.PendingTasks != 0 || cluster.ActiveServices != 1 {
		t.Fatalf("unexpected ecs cluster summary: %+v", cluster)
	}

	if len(payload.ECS.Services) != 1 {
		t.Fatalf("ecs services len = %d, want 1", len(payload.ECS.Services))
	}
	service := payload.ECS.Services[0]
	if service.Name != serviceOut.Service.ServiceName || service.Arn != serviceOut.Service.ServiceArn {
		t.Fatalf("unexpected ecs service identity: %+v", service)
	}
	if service.TaskDefinitionArn != taskDefinitionOut.TaskDefinition.TaskDefinitionArn || service.DesiredCount != 2 || service.RunningCount != 1 || service.PendingCount != 1 || service.Status != types.ServiceStatusActive {
		t.Fatalf("unexpected ecs service summary: %+v", service)
	}

	if len(payload.ECS.Tasks) != 1 {
		t.Fatalf("ecs tasks len = %d, want 1", len(payload.ECS.Tasks))
	}
	if len(payload.ECS.TaskDefinitions) != 1 {
		t.Fatalf("ecs task definitions len = %d, want 1", len(payload.ECS.TaskDefinitions))
	}
	if definition := payload.ECS.TaskDefinitions[0]; definition.Arn != taskDefinitionOut.TaskDefinition.TaskDefinitionArn || definition.Family != "worker" || definition.Revision != 1 || definition.Status != types.TaskDefinitionStatusActive {
		t.Fatalf("unexpected ecs task definition summary: %+v", definition)
	}
	taskSummary := payload.ECS.Tasks[0]
	if taskSummary.Arn != task.TaskArn || taskSummary.ClusterArn != task.ClusterArn || taskSummary.TaskDefinitionArn != task.TaskDefinitionArn {
		t.Fatalf("unexpected ecs task identity: %+v", taskSummary)
	}
	if taskSummary.LastStatus != types.TaskStatusRunning || taskSummary.DesiredStatus != types.TaskDesiredStatusRunning || taskSummary.Group != "service:worker-service" || taskSummary.LaunchType != types.LaunchTypeFargate {
		t.Fatalf("unexpected ecs task summary: %+v", taskSummary)
	}
	if len(taskSummary.Containers) != 1 {
		t.Fatalf("ecs task containers len = %d, want 1: %+v", len(taskSummary.Containers), taskSummary)
	}
	container := taskSummary.Containers[0]
	if container.Name != "worker" || container.LastStatus != types.TaskStatusRunning {
		t.Fatalf("unexpected ecs task container summary: %+v", container)
	}
	if len(container.NetworkBindings) != 1 {
		t.Fatalf("ecs task container network bindings len = %d, want 1: %+v", len(container.NetworkBindings), container)
	}
	binding := container.NetworkBindings[0]
	if binding.ContainerPort != 8080 || binding.HostPort != 32768 || binding.Protocol != "tcp" || binding.BindIP != "127.0.0.1" {
		t.Fatalf("unexpected ecs task network binding: %+v", binding)
	}

	foundECS := false
	for _, name := range payload.Services {
		if name == "ecs" {
			foundECS = true
			break
		}
	}
	if !foundECS {
		t.Fatalf("services missing ecs: %v", payload.Services)
	}
}

func TestOverviewOmitsECSWhenUnconfigured(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()
	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["ecs"]; ok {
		t.Fatalf("ecs field present without configured ECS service: %s", payload["ecs"])
	}
}

func TestOverviewInfersInfraConnectionsFromEnvironment(t *testing.T) {
	h := newTestHandler(t)

	_, err := h.lambda.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "orders-db-handler",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/lambda-role",
		Environment: map[string]string{
			"DATABASE_URL": "postgres://postgres:postgres@localhost:5432/orders",
		},
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	h.infra.SetResult(infrasvc.ProbeResult{
		Name:     "PostgreSQL",
		Kind:     "postgresql",
		Host:     "localhost",
		Port:     5432,
		Status:   "connected",
		ProbedAt: time.Now().UTC().Format(time.RFC3339),
	})

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()

	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Connections []infraConnection `json:"connections"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Connections) != 1 {
		t.Fatalf("connections len = %d, want 1", len(payload.Connections))
	}
	if payload.Connections[0].SourceFunction != "orders-db-handler" {
		t.Fatalf("sourceFunction = %q, want %q", payload.Connections[0].SourceFunction, "orders-db-handler")
	}
	if payload.Connections[0].TargetKind != "postgresql" || payload.Connections[0].TargetPort != 5432 {
		t.Fatalf("unexpected connection target: %+v", payload.Connections[0])
	}
	if payload.Connections[0].Evidence != "env" || payload.Connections[0].Source != "DATABASE_URL" {
		t.Fatalf("unexpected connection evidence: %+v", payload.Connections[0])
	}
}

func TestOverviewInfersRedisInfraConnectionsFromEnvironment(t *testing.T) {
	h := newTestHandler(t)

	_, err := h.lambda.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "media-cache-handler",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/lambda-role",
		Environment: map[string]string{
			"REDIS_URL": "redis://localhost:6379/0",
		},
	}, nil)
	if err != nil {
		t.Fatalf("create function: %v", err)
	}

	h.infra.SetResult(infrasvc.ProbeResult{
		Name:     "Redis",
		Kind:     "redis",
		Host:     "localhost",
		Port:     6379,
		Status:   "connected",
		ProbedAt: time.Now().UTC().Format(time.RFC3339),
	})

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()

	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Connections []infraConnection `json:"connections"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Connections) != 1 {
		t.Fatalf("connections len = %d, want 1", len(payload.Connections))
	}
	if payload.Connections[0].SourceFunction != "media-cache-handler" {
		t.Fatalf("sourceFunction = %q, want %q", payload.Connections[0].SourceFunction, "media-cache-handler")
	}
	if payload.Connections[0].TargetKind != "redis" || payload.Connections[0].TargetPort != 6379 {
		t.Fatalf("unexpected connection target: %+v", payload.Connections[0])
	}
	if payload.Connections[0].Evidence != "env" || payload.Connections[0].Source != "REDIS_URL" {
		t.Fatalf("unexpected connection evidence: %+v", payload.Connections[0])
	}
}

func TestScanChaosSourceRejectsInvalidBaseDir(t *testing.T) {
	h := newTestHandler(t)

	body := bytes.NewBufferString("{\"baseDir\":\"bad\\u0000path\",\"functionNames\":[\"orders\"]}")
	req := httptest.NewRequest(http.MethodPost, "/_tarn/admin/chaos/source", body)
	rec := httptest.NewRecorder()

	h.ScanChaosSource(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "invalid control characters") {
		t.Fatalf("body = %q, want invalid control characters error", rec.Body.String())
	}
}

func TestScanChaosSourceSanitizesQuotedBaseDir(t *testing.T) {
	h := newTestHandler(t)

	baseDir := t.TempDir()
	lambdaDir := filepath.Join(baseDir, "orders-handler")
	if err := os.MkdirAll(lambdaDir, 0o755); err != nil {
		t.Fatalf("mkdir lambda dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lambdaDir, "package.json"), []byte(`{"name":"orders-handler"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	payload := map[string]any{
		"baseDir":       `  "` + baseDir + `"  `,
		"functionNames": []string{"orders-handler"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/_tarn/admin/chaos/source", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.ScanChaosSource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Matches []struct {
			FunctionName string `json:"functionName"`
			Dir          string `json:"dir"`
		} `json:"matches"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Matches) != 1 {
		t.Fatalf("matches len = %d, want 1", len(resp.Matches))
	}
	if resp.Matches[0].FunctionName != "orders-handler" {
		t.Fatalf("functionName = %q, want %q", resp.Matches[0].FunctionName, "orders-handler")
	}
	if resp.Matches[0].Dir != lambdaDir {
		t.Fatalf("dir = %q, want %q", resp.Matches[0].Dir, lambdaDir)
	}
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	cfg := config.Default()
	cfg.Host = "127.0.0.1"
	cfg.DataDir = t.TempDir()
	cfg.Port = 4566

	store := lambdasvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	logs := logssvc.NewService(cfg)
	lambda := lambdasvc.NewService(cfg, store, nil, nil, logs)
	apigw := apigatewaysvc.NewService(cfg, lambda, nil)
	apigwv1 := apigatewayv1svc.NewService(cfg, lambda, nil)
	s3 := s3svc.NewService(cfg)
	sqs := sqssvc.NewService(cfg)
	sns := snssvc.NewService(cfg, sqs, lambda)
	dynamodb := dynamodbsvc.NewService(cfg)
	if err := dynamodb.Init(); err != nil {
		t.Fatalf("init dynamodb: %v", err)
	}
	secrets := secretssvc.NewService(cfg)
	infra := infrasvc.NewService("", false)
	esmStore := eventsourcesvc.NewStore(cfg)
	esm := eventsourcesvc.NewService(cfg, esmStore, nil, nil, nil)
	eventbridgeStore := eventbridgesvc.NewStore(cfg)
	eventbridge := eventbridgesvc.NewService(cfg, eventbridgeStore, lambda)
	return NewHandler(cfg, apigw, apigwv1, lambda, logs, sqs, sns, dynamodb, secrets, infra, s3, esm, eventbridge, nil, nil)
}

// newTestHandlerWithTraces is newTestHandler plus a real (in-memory) trace
// store, for tests that exercise the traces endpoint. newTestHandler passes
// nil for the store so unrelated tests don't pay for a sqlite connection.
func newTestHandlerWithTraces(t *testing.T) (*Handler, *tracesvc.Store) {
	t.Helper()

	cfg := config.Default()
	cfg.Host = "127.0.0.1"
	cfg.DataDir = t.TempDir()
	cfg.Port = 4566

	store := lambdasvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	logs := logssvc.NewService(cfg)
	lambda := lambdasvc.NewService(cfg, store, nil, nil, logs)
	apigw := apigatewaysvc.NewService(cfg, lambda, nil)
	apigwv1 := apigatewayv1svc.NewService(cfg, lambda, nil)
	s3 := s3svc.NewService(cfg)
	sqs := sqssvc.NewService(cfg)
	sns := snssvc.NewService(cfg, sqs, lambda)
	dynamodb := dynamodbsvc.NewService(cfg)
	if err := dynamodb.Init(); err != nil {
		t.Fatalf("init dynamodb: %v", err)
	}
	secrets := secretssvc.NewService(cfg)
	infra := infrasvc.NewService("", false)
	esmStore := eventsourcesvc.NewStore(cfg)
	esm := eventsourcesvc.NewService(cfg, esmStore, nil, nil, nil)
	eventbridgeStore := eventbridgesvc.NewStore(cfg)
	eventbridge := eventbridgesvc.NewService(cfg, eventbridgeStore, lambda)
	traceStore := tracesvc.NewStore()
	h := NewHandler(cfg, apigw, apigwv1, lambda, logs, sqs, sns, dynamodb, secrets, infra, s3, esm, eventbridge, nil, traceStore)
	return h, traceStore
}

func TestTracesFiltersByCorrelationIdResourceAndKind(t *testing.T) {
	h, traceStore := newTestHandlerWithTraces(t)

	base := time.Now().UTC()
	traceStore.Add(&tracesvc.Trace{
		ID:            "trace-1",
		CorrelationID: "corr-1",
		StartedAt:     base,
		DurationMs:    10,
		Status:        200,
		Method:        "POST",
		Path:          "/",
		Spans: []tracesvc.Span{
			{Kind: "queue", Name: "jobs", DurationMs: 5, Status: "ok"},
			{Kind: "lambda", Name: "worker-fn", DurationMs: 3, Status: "ok"},
		},
	})
	traceStore.Add(&tracesvc.Trace{
		ID:            "trace-2",
		CorrelationID: "corr-2",
		StartedAt:     base.Add(time.Second),
		DurationMs:    20,
		Status:        500,
		Method:        "POST",
		Path:          "/",
		Spans: []tracesvc.Span{
			{Kind: "gateway", Name: "api", DurationMs: 2, Status: "ok"},
			{Kind: "lambda", Name: "other-fn", DurationMs: 18, Status: "error"},
		},
	})

	decode := func(t *testing.T, query string) []*tracesvc.Trace {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/traces"+query, nil)
		rec := httptest.NewRecorder()
		h.Traces(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var payload struct {
			Traces []*tracesvc.Trace `json:"traces"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return payload.Traces
	}

	t.Run("no filter returns newest first", func(t *testing.T) {
		got := decode(t, "")
		if len(got) != 2 {
			t.Fatalf("traces len = %d, want 2", len(got))
		}
		if got[0].ID != "trace-2" || got[1].ID != "trace-1" {
			t.Fatalf("unexpected order: %q, %q", got[0].ID, got[1].ID)
		}
	})

	t.Run("correlationId exact match", func(t *testing.T) {
		got := decode(t, "?correlationId=corr-1")
		if len(got) != 1 || got[0].ID != "trace-1" {
			t.Fatalf("unexpected traces for correlationId filter: %+v", got)
		}
	})

	t.Run("resource substring match against any span name", func(t *testing.T) {
		got := decode(t, "?resource=worker")
		if len(got) != 1 || got[0].ID != "trace-1" {
			t.Fatalf("unexpected traces for resource filter: %+v", got)
		}
	})

	t.Run("kind exact match against any span kind", func(t *testing.T) {
		got := decode(t, "?kind=gateway")
		if len(got) != 1 || got[0].ID != "trace-2" {
			t.Fatalf("unexpected traces for kind filter: %+v", got)
		}
	})

	t.Run("limit caps results", func(t *testing.T) {
		got := decode(t, "?limit=1")
		if len(got) != 1 || got[0].ID != "trace-2" {
			t.Fatalf("unexpected traces for limit filter: %+v", got)
		}
	})

	t.Run("no matching store returns empty traces", func(t *testing.T) {
		bare := &Handler{}
		req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/traces", nil)
		rec := httptest.NewRecorder()
		bare.Traces(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var payload struct {
			Traces []*tracesvc.Trace `json:"traces"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(payload.Traces) != 0 {
			t.Fatalf("traces len = %d, want 0", len(payload.Traces))
		}
	})
}

func TestAllLogEventsFilterGroups(t *testing.T) {
	h := newTestHandler(t)

	h.logs.CreateLogGroup("group-a")
	h.logs.CreateLogGroup("group-b")
	h.logs.CreateLogGroup("group-c")

	now := time.Now().UTC()
	h.logs.PutLogEvents("group-a", "stream-1", []logssvc.LogEvent{
		{Message: "msg-a", Timestamp: now},
	})
	h.logs.PutLogEvents("group-b", "stream-1", []logssvc.LogEvent{
		{Message: "msg-b", Timestamp: now.Add(time.Second)},
	})
	h.logs.PutLogEvents("group-c", "stream-1", []logssvc.LogEvent{
		{Message: "msg-c", Timestamp: now.Add(2 * time.Second)},
	})

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/logs/events-all?groups=group-a,group-c", nil)
	rec := httptest.NewRecorder()
	h.AllLogEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Events []logssvc.LogEvent `json:"events"`
		Total  int                `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Total != 2 || len(payload.Events) != 2 {
		t.Fatalf("got total %d, events len %d, want 2", payload.Total, len(payload.Events))
	}
}

func TestScanLogsEndpoint(t *testing.T) {
	h := newTestHandler(t)

	h.logs.CreateLogGroup("/aws/lambda/checkout")
	h.logs.CreateLogGroup("/aws/lambda/auth")
	h.logs.CreateLogGroup("/tarn/api")

	now := time.Now().UTC()
	h.logs.PutLogEvents("/aws/lambda/checkout", "stream-1", []logssvc.LogEvent{
		{Message: "User checkout started correlation-id=corr-abc-123", Timestamp: now, Level: logssvc.LevelINFO},
		{Message: "Payment processed correlation-id=corr-abc-123", Timestamp: now.Add(time.Second), Level: logssvc.LevelINFO},
	})
	h.logs.PutLogEvents("/aws/lambda/auth", "stream-1", []logssvc.LogEvent{
		{Message: "Login ok correlation-id=corr-xyz-999", Timestamp: now.Add(2 * time.Second), Level: logssvc.LevelINFO},
		{Message: "Login Alice ok", Timestamp: now.Add(3 * time.Second), Level: logssvc.LevelINFO},
	})
	h.logs.PutLogEvents("/tarn/api", "stream-1", []logssvc.LogEvent{
		{Message: "GET /health 200", Timestamp: now.Add(4 * time.Second), Level: logssvc.LevelINFO},
	})

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/logs/scan?pattern=correlation-id", nil)
	rec := httptest.NewRecorder()
	h.ScanLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var res logssvc.LogScanResult
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if res.Pattern != "correlation-id" {
		t.Fatalf("pattern = %q, want correlation-id", res.Pattern)
	}
	if res.TotalMatches != 3 {
		t.Fatalf("totalMatches = %d, want 3", res.TotalMatches)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups len = %d, want 2", len(res.Groups))
	}
	if res.Groups[0].GroupName != "/aws/lambda/checkout" || res.Groups[0].MatchCount != 2 {
		t.Fatalf("expected checkout first with 2 matches, got %+v", res.Groups[0])
	}
	if res.Groups[1].GroupName != "/aws/lambda/auth" || res.Groups[1].MatchCount != 1 {
		t.Fatalf("expected auth second with 1 match, got %+v", res.Groups[1])
	}
}
