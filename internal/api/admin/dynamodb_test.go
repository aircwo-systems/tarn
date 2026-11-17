package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ecssvc "github.com/aircwo-systems/tarn/internal/ecs"
	eventsourcesvc "github.com/aircwo-systems/tarn/internal/eventsource"
	kmssvc "github.com/aircwo-systems/tarn/internal/kms"
	stepfunctionssvc "github.com/aircwo-systems/tarn/internal/stepfunctions"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestDynamoTableDetailAndIntegrations(t *testing.T) {
	h := newTestHandler(t)
	h.esm = eventsourcesvc.NewService(h.cfg, eventsourcesvc.NewStore(h.cfg), h.lambda, h.sqs, h.dynamodb)

	kmsSvc := kmssvc.NewService(h.cfg)
	h.SetKMSService(kmsSvc)
	keyOut, err := kmsSvc.CreateKey(&kmssvc.CreateKeyInput{Description: "orders table key"})
	if err != nil {
		t.Fatalf("create kms key: %v", err)
	}

	ecsStore := ecssvc.NewStore(h.cfg)
	ecsSvc := ecssvc.NewService(h.cfg, ecsStore)
	h.SetECSService(ecsSvc)

	sfnStore := stepfunctionssvc.NewStore(h.cfg)
	sfnSvc := stepfunctionssvc.NewService(h.cfg, sfnStore, h.lambda)
	h.stepfunctions = sfnSvc

	enabled := true
	table, err := h.dynamodb.CreateTable(&types.DynamoDBTable{
		TableName: "orders",
		AttributeDefinitions: []types.DynamoDBAttributeDefinition{
			{AttributeName: "pk", AttributeType: "S"},
			{AttributeName: "sk", AttributeType: "S"},
			{AttributeName: "gsiPk", AttributeType: "S"},
		},
		KeySchema: []types.DynamoDBKeySchemaElement{
			{AttributeName: "pk", KeyType: "HASH"},
			{AttributeName: "sk", KeyType: "RANGE"},
		},
		BillingModeSummary: &types.DynamoDBBillingModeSummary{BillingMode: "PAY_PER_REQUEST"},
		GlobalSecondaryIndexes: []types.DynamoDBGlobalSecondaryIndex{
			{
				IndexName:  "by-customer",
				KeySchema:  []types.DynamoDBKeySchemaElement{{AttributeName: "gsiPk", KeyType: "HASH"}},
				Projection: types.DynamoDBProjection{ProjectionType: "ALL"},
			},
		},
		StreamSpecification: &types.DynamoDBStreamSpecification{
			StreamEnabled:  true,
			StreamViewType: "NEW_AND_OLD_IMAGES",
		},
		SSESpecification: &types.DynamoDBSSESpecification{
			Enabled:        &enabled,
			SSEType:        "KMS",
			KMSMasterKeyId: keyOut.KeyMetadata.Arn,
		},
		Tags: []types.DynamoDBTag{{Key: "env", Value: "local"}},
	})
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	if _, err := h.dynamodb.UpdateTimeToLive("orders", "expiresAt", true); err != nil {
		t.Fatalf("enable ttl: %v", err)
	}
	if _, err := h.dynamodb.UpdateContinuousBackups("orders", true); err != nil {
		t.Fatalf("enable pitr: %v", err)
	}

	code := zippedFunction(t, "index.js", "exports.handler=async()=>({statusCode:200})")

	// Lambda function referencing table via env var
	if _, err := h.lambda.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "orders-api",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/lambda",
		Environment:  map[string]string{"ORDERS_TABLE": "orders"},
	}, code); err != nil {
		t.Fatalf("create lambda orders-api: %v", err)
	}

	// Lambda trigger via DynamoDB Stream ESM
	streamFn, err := h.lambda.CreateFunction(context.Background(), &types.FunctionConfig{
		FunctionName: "orders-stream-worker",
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/lambda",
	}, code)
	if err != nil {
		t.Fatalf("create lambda orders-stream-worker: %v", err)
	}
	if _, err := h.esm.CreateMapping(table.LatestStreamArn, streamFn.FunctionArn, "orders-stream-worker", 25, 0, true, nil, nil); err != nil {
		t.Fatalf("create esm: %v", err)
	}

	// ECS cluster, task def, service referencing table
	if _, err := ecsSvc.CreateCluster(&types.CreateClusterInput{ClusterName: "app"}); err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	tdOut, err := ecsSvc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "orders-worker",
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:  "worker",
				Image: "alpine:latest",
				Environment: []types.KeyValuePair{
					{Name: "DYNAMODB_TABLE_ARN", Value: table.TableArn},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("register td: %v", err)
	}
	if _, err := ecsSvc.CreateService(&types.CreateServiceInput{
		Cluster:        "app",
		ServiceName:    "orders-svc",
		TaskDefinition: tdOut.TaskDefinition.TaskDefinitionArn,
		DesiredCount:   1,
	}); err != nil {
		t.Fatalf("create ecs service: %v", err)
	}

	// Step Functions referencing DynamoDB table
	if _, err := sfnSvc.CreateStateMachine("order-saga", `{
		"StartAt": "SaveOrder",
		"States": {
			"SaveOrder": {
				"Type": "Task",
				"Resource": "arn:aws:states:::dynamodb:putItem",
				"Parameters": {"TableName": "orders"},
				"End": true
			}
		}
	}`, "", "STANDARD", nil); err != nil {
		t.Fatalf("create state machine: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/dynamodb/tables/orders", nil)
	req.SetPathValue("name", "orders")
	rec := httptest.NewRecorder()
	h.DynamoTableDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var detail dynamoTableDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Table.Name != "orders" || !detail.Table.StreamEnabled {
		t.Fatalf("unexpected table header: %+v", detail.Table)
	}
	if detail.TTL.Status != "ENABLED" || detail.TTL.AttributeName != "expiresAt" {
		t.Fatalf("unexpected TTL: %+v", detail.TTL)
	}
	if detail.ContinuousBackups.PointInTimeRecoveryStatus != "ENABLED" {
		t.Fatalf("unexpected PITR: %+v", detail.ContinuousBackups)
	}
	if detail.Tags["env"] != "local" {
		t.Fatalf("unexpected tags: %+v", detail.Tags)
	}
	if len(detail.Integrations.Triggers) != 1 || detail.Integrations.Triggers[0].FunctionName != "orders-stream-worker" {
		t.Fatalf("unexpected triggers: %+v", detail.Integrations.Triggers)
	}
	if len(detail.Integrations.Lambdas) != 1 || detail.Integrations.Lambdas[0].FunctionName != "orders-api" {
		t.Fatalf("unexpected lambdas: %+v", detail.Integrations.Lambdas)
	}
	if len(detail.Integrations.ECS) != 1 || detail.Integrations.ECS[0].Name != "orders-svc" || detail.Integrations.ECS[0].Kind != "service" {
		t.Fatalf("unexpected ecs integrations: %+v", detail.Integrations.ECS)
	}
	if len(detail.Integrations.StepFunctions) != 1 || detail.Integrations.StepFunctions[0].Name != "order-saga" {
		t.Fatalf("unexpected step functions: %+v", detail.Integrations.StepFunctions)
	}
	if detail.Integrations.KMSKey == nil || detail.Integrations.KMSKey.KeyID != keyOut.KeyMetadata.KeyId {
		t.Fatalf("unexpected KMS integration: %+v", detail.Integrations.KMSKey)
	}

	// Unknown table returns 404
	missingReq := httptest.NewRequest(http.MethodGet, "/_tarn/admin/dynamodb/tables/nope", nil)
	missingReq.SetPathValue("name", "nope")
	missingRec := httptest.NewRecorder()
	h.DynamoTableDetail(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing table status = %d, want 404", missingRec.Code)
	}
}

func TestDynamoItemsCRUDScanQueryAndStreamRecords(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.dynamodb.CreateTable(&types.DynamoDBTable{
		TableName: "users",
		AttributeDefinitions: []types.DynamoDBAttributeDefinition{
			{AttributeName: "pk", AttributeType: "S"},
			{AttributeName: "sk", AttributeType: "S"},
		},
		KeySchema: []types.DynamoDBKeySchemaElement{
			{AttributeName: "pk", KeyType: "HASH"},
			{AttributeName: "sk", KeyType: "RANGE"},
		},
		BillingModeSummary: &types.DynamoDBBillingModeSummary{BillingMode: "PAY_PER_REQUEST"},
		StreamSpecification: &types.DynamoDBStreamSpecification{
			StreamEnabled:  true,
			StreamViewType: "NEW_AND_OLD_IMAGES",
		},
	}); err != nil {
		t.Fatalf("create table: %v", err)
	}

	doJSON := func(method, path string, body any, fn func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatalf("encode body: %v", err)
			}
		}
		req := httptest.NewRequest(method, path, &buf)
		req.SetPathValue("name", "users")
		rec := httptest.NewRecorder()
		fn(rec, req)
		return rec
	}

	// Put 2 items
	for _, id := range []string{"a", "b"} {
		rec := doJSON(http.MethodPut, "/_tarn/admin/dynamodb/tables/users/items", map[string]any{
			"item": map[string]any{
				"pk":     map[string]any{"S": "tenant#1"},
				"sk":     map[string]any{"S": "user#" + id},
				"status": map[string]any{"S": "active"},
			},
		}, h.DynamoPutItem)
		if rec.Code != http.StatusOK {
			t.Fatalf("put item %s failed (%d): %s", id, rec.Code, rec.Body.String())
		}
	}

	// Scan
	scanRec := doJSON(http.MethodPost, "/_tarn/admin/dynamodb/tables/users/scan", map[string]any{"limit": 10}, h.DynamoScan)
	if scanRec.Code != http.StatusOK {
		t.Fatalf("scan status = %d: %s", scanRec.Code, scanRec.Body.String())
	}
	var scanOut dynamoReadResponse
	if err := json.NewDecoder(scanRec.Body).Decode(&scanOut); err != nil {
		t.Fatalf("decode scan: %v", err)
	}
	if scanOut.Count != 2 || len(scanOut.Items) != 2 {
		t.Fatalf("scan count = %d items=%d, want 2", scanOut.Count, len(scanOut.Items))
	}

	// Query
	queryRec := doJSON(http.MethodPost, "/_tarn/admin/dynamodb/tables/users/query", map[string]any{
		"keyConditionExpression": "pk = :pk AND begins_with(sk, :prefix)",
		"expressionAttributeValues": map[string]any{
			":pk":     map[string]any{"S": "tenant#1"},
			":prefix": map[string]any{"S": "user#a"},
		},
	}, h.DynamoQuery)
	if queryRec.Code != http.StatusOK {
		t.Fatalf("query status = %d: %s", queryRec.Code, queryRec.Body.String())
	}
	var queryOut dynamoReadResponse
	if err := json.NewDecoder(queryRec.Body).Decode(&queryOut); err != nil {
		t.Fatalf("decode query: %v", err)
	}
	if queryOut.Count != 1 {
		t.Fatalf("query count = %d, want 1", queryOut.Count)
	}

	// Update
	updRec := doJSON(http.MethodPatch, "/_tarn/admin/dynamodb/tables/users/items", map[string]any{
		"key": map[string]any{
			"pk": map[string]any{"S": "tenant#1"},
			"sk": map[string]any{"S": "user#a"},
		},
		"updateExpression":          "SET #s = :v",
		"expressionAttributeNames":  map[string]string{"#s": "status"},
		"expressionAttributeValues": map[string]any{":v": map[string]any{"S": "paused"}},
	}, h.DynamoUpdateItem)
	if updRec.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", updRec.Code, updRec.Body.String())
	}

	// GetItem
	getRec := doJSON(http.MethodPost, "/_tarn/admin/dynamodb/tables/users/items/get", map[string]any{
		"key": map[string]any{
			"pk": map[string]any{"S": "tenant#1"},
			"sk": map[string]any{"S": "user#a"},
		},
	}, h.DynamoGetItem)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d: %s", getRec.Code, getRec.Body.String())
	}
	var getOut struct {
		Found bool           `json:"found"`
		Item  map[string]any `json:"item"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&getOut); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if !getOut.Found {
		t.Fatalf("expected item to be found")
	}

	// DeleteItem
	delRec := doJSON(http.MethodPost, "/_tarn/admin/dynamodb/tables/users/items/delete", map[string]any{
		"key": map[string]any{
			"pk": map[string]any{"S": "tenant#1"},
			"sk": map[string]any{"S": "user#b"},
		},
	}, h.DynamoDeleteItem)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", delRec.Code, delRec.Body.String())
	}

	// Stream records (2 inserts + 1 modify + 1 remove = 4 records)
	streamReq := httptest.NewRequest(http.MethodGet, "/_tarn/admin/dynamodb/tables/users/stream/records?iteratorType=TRIM_HORIZON&limit=50", nil)
	streamReq.SetPathValue("name", "users")
	streamRec := httptest.NewRecorder()
	h.DynamoStreamRecords(streamRec, streamReq)
	if streamRec.Code != http.StatusOK {
		t.Fatalf("stream status = %d: %s", streamRec.Code, streamRec.Body.String())
	}
	var streamOut struct {
		Records           []types.StreamRecord `json:"records"`
		NextShardIterator string               `json:"nextShardIterator"`
	}
	if err := json.NewDecoder(streamRec.Body).Decode(&streamOut); err != nil {
		t.Fatalf("decode stream: %v", err)
	}
	if len(streamOut.Records) != 4 {
		t.Fatalf("stream records len = %d, want 4", len(streamOut.Records))
	}
}
