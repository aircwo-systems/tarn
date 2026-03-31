package dynamodb

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	dynamodbsvc "github.com/aircwo-systems/tarn/internal/dynamodb"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	svc := dynamodbsvc.NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init dynamodb: %v", err)
	}
	return NewHandler(svc)
}

func TestDispatchCreatePutGetAndStreams(t *testing.T) {
	h := newTestHandler(t)

	createBody, _ := json.Marshal(types.DynamoDBTable{
		TableName: "orders",
		AttributeDefinitions: []types.DynamoDBAttributeDefinition{
			{AttributeName: "pk", AttributeType: "S"},
			{AttributeName: "sk", AttributeType: "S"},
		},
		KeySchema: []types.DynamoDBKeySchemaElement{
			{AttributeName: "pk", KeyType: "HASH"},
			{AttributeName: "sk", KeyType: "RANGE"},
		},
		StreamSpecification: &types.DynamoDBStreamSpecification{
			StreamEnabled:  true,
			StreamViewType: "NEW_IMAGE",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(createBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.CreateTable")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		TableDescription types.DynamoDBTable `json:"TableDescription"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResp.TableDescription.LatestStreamArn == "" {
		t.Fatal("expected stream arn")
	}

	putBody := []byte(`{"TableName":"orders","Item":{"pk":{"S":"acct#1"},"sk":{"S":"order#1"},"status":{"S":"OPEN"}}}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(putBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.PutItem")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put item status=%d body=%s", rec.Code, rec.Body.String())
	}

	getBody := []byte(`{"TableName":"orders","Key":{"pk":{"S":"acct#1"},"sk":{"S":"order#1"}}}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(getBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.GetItem")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get item status=%d body=%s", rec.Code, rec.Body.String())
	}
	var getResp struct {
		Item map[string]any `json:"Item"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResp.Item["status"] == nil {
		t.Fatalf("item = %#v", getResp.Item)
	}

	streamsBody := []byte(`{"TableName":"orders"}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(streamsBody))
	req.Header.Set("X-Amz-Target", "DynamoDBStreams_20120810.ListStreams")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list streams status=%d body=%s", rec.Code, rec.Body.String())
	}
	var streamsResp struct {
		Streams []map[string]any `json:"Streams"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&streamsResp); err != nil {
		t.Fatalf("decode streams response: %v", err)
	}
	if len(streamsResp.Streams) != 1 {
		t.Fatalf("streams len=%d", len(streamsResp.Streams))
	}
}

func TestDispatchSDKStylePutGetUpdateSequence(t *testing.T) {
	h := newTestHandler(t)

	createBody := []byte(`{
		"TableName":"sdk-sequence",
		"AttributeDefinitions":[
			{"AttributeName":"pk","AttributeType":"S"},
			{"AttributeName":"sk","AttributeType":"S"}
		],
		"KeySchema":[
			{"AttributeName":"pk","KeyType":"HASH"},
			{"AttributeName":"sk","KeyType":"RANGE"}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(createBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.CreateTable")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table status=%d body=%s", rec.Code, rec.Body.String())
	}

	putBody := []byte(`{
		"TableName":"sdk-sequence",
		"Item":{
			"pk":{"S":"user#1"},
			"sk":{"S":"profile"},
			"name":{"S":"Alice"},
			"age":{"N":"30"}
		}
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(putBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.PutItem")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put item status=%d body=%s", rec.Code, rec.Body.String())
	}

	getBody := []byte(`{
		"TableName":"sdk-sequence",
		"Key":{"pk":{"S":"user#1"},"sk":{"S":"profile"}}
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(getBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.GetItem")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get item status=%d body=%s", rec.Code, rec.Body.String())
	}
	var getResp struct {
		Item map[string]map[string]any `json:"Item"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got := getResp.Item["name"]["S"]; got != "Alice" {
		t.Fatalf("name after put = %#v, want Alice", got)
	}

	updateBody := []byte(`{
		"TableName":"sdk-sequence",
		"Key":{"pk":{"S":"user#1"},"sk":{"S":"profile"}},
		"UpdateExpression":"SET #n = :v",
		"ExpressionAttributeNames":{"#n":"name"},
		"ExpressionAttributeValues":{":v":{"S":"Bob"}}
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(updateBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.UpdateItem")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update item status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(getBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.GetItem")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get item after update status=%d body=%s", rec.Code, rec.Body.String())
	}
	getResp = struct {
		Item map[string]map[string]any `json:"Item"`
	}{}
	if err := json.NewDecoder(rec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get after update response: %v", err)
	}
	if got := getResp.Item["name"]["S"]; got != "Bob" {
		t.Fatalf("name after update = %#v, want Bob", got)
	}

	deleteBody := []byte(`{"TableName":"sdk-sequence"}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(deleteBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.DeleteTable")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete table status=%d body=%s", rec.Code, rec.Body.String())
	}
	var deleteResp struct {
		TableDescription types.DynamoDBTable `json:"TableDescription"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&deleteResp); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if deleteResp.TableDescription.TableStatus != types.DynamoDBTableStatusDeleting {
		t.Fatalf("delete table status = %q, want %q", deleteResp.TableDescription.TableStatus, types.DynamoDBTableStatusDeleting)
	}

	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(getBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.DescribeTable")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("describe after delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDispatchUpdateTableEnablesStreams(t *testing.T) {
	h := newTestHandler(t)

	createBody := []byte(`{
		"TableName":"update-table-streams",
		"AttributeDefinitions":[
			{"AttributeName":"pk","AttributeType":"S"},
			{"AttributeName":"sk","AttributeType":"S"}
		],
		"KeySchema":[
			{"AttributeName":"pk","KeyType":"HASH"},
			{"AttributeName":"sk","KeyType":"RANGE"}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(createBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.CreateTable")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table status=%d body=%s", rec.Code, rec.Body.String())
	}

	updateBody := []byte(`{
		"TableName":"update-table-streams",
		"StreamSpecification":{"StreamEnabled":true,"StreamViewType":"NEW_AND_OLD_IMAGES"}
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(updateBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.UpdateTable")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update table status=%d body=%s", rec.Code, rec.Body.String())
	}

	var updateResp struct {
		TableDescription types.DynamoDBTable `json:"TableDescription"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.TableDescription.LatestStreamArn == "" {
		t.Fatalf("expected stream arn after update: %+v", updateResp.TableDescription)
	}
	if updateResp.TableDescription.StreamSpecification == nil || !updateResp.TableDescription.StreamSpecification.StreamEnabled {
		t.Fatalf("expected enabled stream spec after update: %+v", updateResp.TableDescription.StreamSpecification)
	}
}

func TestDescribeTableOmitsTagsAndIncludesCompatibilitySummaries(t *testing.T) {
	h := newTestHandler(t)

	createBody := []byte(`{
		"TableName":"describe-shape",
		"AttributeDefinitions":[
			{"AttributeName":"pk","AttributeType":"S"},
			{"AttributeName":"sk","AttributeType":"S"}
		],
		"KeySchema":[
			{"AttributeName":"pk","KeyType":"HASH"},
			{"AttributeName":"sk","KeyType":"RANGE"}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(createBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.CreateTable")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table status=%d body=%s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		TableDescription types.DynamoDBTable `json:"TableDescription"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	tagBody := []byte(`{
		"ResourceArn":"` + createResp.TableDescription.TableArn + `",
		"Tags":[{"Key":"component","Value":"source-table"}]
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(tagBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.TagResource")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tag resource status=%d body=%s", rec.Code, rec.Body.String())
	}

	describeBody := []byte(`{"TableName":"describe-shape"}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(describeBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.DescribeTable")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("describe table status=%d body=%s", rec.Code, rec.Body.String())
	}

	var describeResp struct {
		Table types.DynamoDBTable `json:"Table"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&describeResp); err != nil {
		t.Fatalf("decode describe response: %v", err)
	}
	if len(describeResp.Table.Tags) != 0 {
		t.Fatalf("describe table tags = %#v, want omitted", describeResp.Table.Tags)
	}
	if describeResp.Table.TableClassSummary == nil || describeResp.Table.TableClassSummary.TableClass != "STANDARD" {
		t.Fatalf("table class summary = %#v, want STANDARD", describeResp.Table.TableClassSummary)
	}
	if describeResp.Table.WarmThroughput == nil || describeResp.Table.WarmThroughput.Status != "ACTIVE" {
		t.Fatalf("warm throughput = %#v, want ACTIVE", describeResp.Table.WarmThroughput)
	}
}

func TestDispatchTagResourceLifecycle(t *testing.T) {
	h := newTestHandler(t)

	createBody := []byte(`{
		"TableName":"tagged-table",
		"AttributeDefinitions":[
			{"AttributeName":"pk","AttributeType":"S"},
			{"AttributeName":"sk","AttributeType":"S"}
		],
		"KeySchema":[
			{"AttributeName":"pk","KeyType":"HASH"},
			{"AttributeName":"sk","KeyType":"RANGE"}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(createBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.CreateTable")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table status=%d body=%s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		TableDescription types.DynamoDBTable `json:"TableDescription"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	tagBody := []byte(`{
		"ResourceArn":"` + createResp.TableDescription.TableArn + `",
		"Tags":[
			{"Key":"component","Value":"source-table"},
			{"Key":"flow","Value":"ddb-stream"}
		]
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(tagBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.TagResource")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tag resource status=%d body=%s", rec.Code, rec.Body.String())
	}

	listBody := []byte(`{"ResourceArn":"` + createResp.TableDescription.TableArn + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(listBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.ListTagsOfResource")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tags status=%d body=%s", rec.Code, rec.Body.String())
	}

	var listResp struct {
		Tags []types.DynamoDBTag `json:"Tags"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list tags response: %v", err)
	}
	if len(listResp.Tags) != 2 {
		t.Fatalf("tag count = %d, want 2", len(listResp.Tags))
	}

	untagBody := []byte(`{"ResourceArn":"` + createResp.TableDescription.TableArn + `","TagKeys":["component"]}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(untagBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.UntagResource")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("untag resource status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(listBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.ListTagsOfResource")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tags after untag status=%d body=%s", rec.Code, rec.Body.String())
	}

	listResp = struct {
		Tags []types.DynamoDBTag `json:"Tags"`
	}{}
	if err := json.NewDecoder(rec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list tags after untag response: %v", err)
	}
	if len(listResp.Tags) != 1 || listResp.Tags[0].Key != "flow" {
		t.Fatalf("tags after untag = %#v", listResp.Tags)
	}
}

func TestDispatchTableFeatureCompatibilityAPIs(t *testing.T) {
	h := newTestHandler(t)

	createBody := []byte(`{
		"TableName":"feature-apis",
		"AttributeDefinitions":[
			{"AttributeName":"pk","AttributeType":"S"},
			{"AttributeName":"sk","AttributeType":"S"}
		],
		"KeySchema":[
			{"AttributeName":"pk","KeyType":"HASH"},
			{"AttributeName":"sk","KeyType":"RANGE"}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(createBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.CreateTable")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table status=%d body=%s", rec.Code, rec.Body.String())
	}

	updateTTLBody := []byte(`{
		"TableName":"feature-apis",
		"TimeToLiveSpecification":{"AttributeName":"expires_at","Enabled":true}
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(updateTTLBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.UpdateTimeToLive")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update ttl status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ttlResp struct {
		TimeToLiveSpecification struct {
			AttributeName string `json:"AttributeName"`
			Enabled       bool   `json:"Enabled"`
		} `json:"TimeToLiveSpecification"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&ttlResp); err != nil {
		t.Fatalf("decode update ttl response: %v", err)
	}
	if !ttlResp.TimeToLiveSpecification.Enabled || ttlResp.TimeToLiveSpecification.AttributeName != "expires_at" {
		t.Fatalf("ttl response = %#v", ttlResp.TimeToLiveSpecification)
	}

	updateBackupsBody := []byte(`{
		"TableName":"feature-apis",
		"PointInTimeRecoverySpecification":{"PointInTimeRecoveryEnabled":true}
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(updateBackupsBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.UpdateContinuousBackups")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update continuous backups status=%d body=%s", rec.Code, rec.Body.String())
	}

	updateInsightsBody := []byte(`{
		"TableName":"feature-apis",
		"ContributorInsightsAction":"ENABLE"
	}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(updateInsightsBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.UpdateContributorInsights")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update contributor insights status=%d body=%s", rec.Code, rec.Body.String())
	}
	var insightsResp struct {
		ContributorInsightsStatus string `json:"ContributorInsightsStatus"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&insightsResp); err != nil {
		t.Fatalf("decode update contributor insights response: %v", err)
	}
	if insightsResp.ContributorInsightsStatus != "ENABLED" {
		t.Fatalf("contributor insights status = %q, want ENABLED", insightsResp.ContributorInsightsStatus)
	}

	describeKinesisBody := []byte(`{"TableName":"feature-apis"}`)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(describeKinesisBody))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.DescribeKinesisStreamingDestination")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("describe kinesis status=%d body=%s", rec.Code, rec.Body.String())
	}
	var kinesisResp struct {
		KinesisDataStreamDestinations []map[string]any `json:"KinesisDataStreamDestinations"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&kinesisResp); err != nil {
		t.Fatalf("decode describe kinesis response: %v", err)
	}
	if len(kinesisResp.KinesisDataStreamDestinations) != 0 {
		t.Fatalf("expected no kinesis destinations, got %#v", kinesisResp.KinesisDataStreamDestinations)
	}
}
