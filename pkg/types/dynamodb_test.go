package types

import (
	"encoding/json"
	"testing"
)

func TestDynamoDBTableUnmarshalLegacyCreationDateTime(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"TableName":"orders",
		"TableArn":"arn:aws:dynamodb:eu-west-2:000000000000:table/orders",
		"TableId":"orders-1",
		"TableStatus":"ACTIVE",
		"CreationDateTime":"2026-03-30T10:20:30Z",
		"AttributeDefinitions":[{"AttributeName":"pk","AttributeType":"S"}],
		"KeySchema":[{"AttributeName":"pk","KeyType":"HASH"}],
		"ProvisionedThroughput":{"ReadCapacityUnits":0,"WriteCapacityUnits":0,"NumberOfDecreasesToday":0},
		"ItemCount":0,
		"TableSizeBytes":0
	}`)

	var table DynamoDBTable
	if err := json.Unmarshal(payload, &table); err != nil {
		t.Fatalf("unmarshal table: %v", err)
	}

	if table.CreationDateTime == 0 {
		t.Fatalf("expected parsed creation date time, got 0")
	}
}

func TestStreamRecordDataUnmarshalLegacyApproximateCreationDateTime(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"ApproximateCreationDateTime":"2026-03-30T10:20:30Z",
		"SequenceNumber":"1",
		"SizeBytes":1,
		"StreamViewType":"NEW_IMAGE"
	}`)

	var record StreamRecordData
	if err := json.Unmarshal(payload, &record); err != nil {
		t.Fatalf("unmarshal stream record data: %v", err)
	}

	if record.ApproximateCreationDateTime == 0 {
		t.Fatalf("expected parsed creation date time, got 0")
	}
}
