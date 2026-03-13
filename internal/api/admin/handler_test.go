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

	apigatewaysvc "github.com/openstack-project/openstack/internal/apigateway"
	apigatewayv1svc "github.com/openstack-project/openstack/internal/apigatewayv1"
	"github.com/openstack-project/openstack/internal/config"
	eventsourcesvc "github.com/openstack-project/openstack/internal/eventsource"
	infrasvc "github.com/openstack-project/openstack/internal/infrastructure"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	logssvc "github.com/openstack-project/openstack/internal/logs"
	s3svc "github.com/openstack-project/openstack/internal/s3"
	secretssvc "github.com/openstack-project/openstack/internal/secrets"
	sqssvc "github.com/openstack-project/openstack/internal/sqs"
	"github.com/openstack-project/openstack/pkg/types"
)

func TestQueueMessagesReturnsMessages(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.sqs.CreateQueue("jobs", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if _, err := h.sqs.SendMessage("jobs", "hello queue", 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/queues/jobs/messages?limit=10", nil)
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

func TestQueueMessagesInvalidLimit(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/queues/jobs/messages?limit=abc", nil)
	req.SetPathValue("name", "jobs")
	rec := httptest.NewRecorder()

	h.QueueMessages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestQueueMessagesNotFound(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/queues/missing/messages", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/secrets/api-key/value", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/secrets/missing/value", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/overview", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/overview", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/overview", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/_openstack/admin/overview", nil)
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
	req := httptest.NewRequest(http.MethodPost, "/_openstack/admin/chaos/source", body)
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

	req := httptest.NewRequest(http.MethodPost, "/_openstack/admin/chaos/source", bytes.NewReader(body))
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
	secrets := secretssvc.NewService(cfg)
	infra := infrasvc.NewService("", false)
	esmStore := eventsourcesvc.NewStore(cfg)
	esm := eventsourcesvc.NewService(cfg, esmStore, nil, nil)
	return NewHandler(cfg, apigw, apigwv1, lambda, logs, sqs, secrets, infra, s3, esm, nil)
}
