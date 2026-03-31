package types

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DynamoDB types for table, stream, and item operations.

const (
	DynamoDBTableStatusActive   = "ACTIVE"
	DynamoDBTableStatusDeleting = "DELETING"
	DynamoDBSourceTypeTable     = "table"
	DynamoDBSourceTypeStream    = "stream"
)

type DynamoDBAttributeDefinition struct {
	AttributeName string `json:"AttributeName"`
	AttributeType string `json:"AttributeType"`
}

type DynamoDBKeySchemaElement struct {
	AttributeName string `json:"AttributeName"`
	KeyType       string `json:"KeyType"`
}

type DynamoDBProvisionedThroughput struct {
	ReadCapacityUnits      int64 `json:"ReadCapacityUnits"`
	WriteCapacityUnits     int64 `json:"WriteCapacityUnits"`
	NumberOfDecreasesToday int64 `json:"NumberOfDecreasesToday"`
}

type DynamoDBProjection struct {
	ProjectionType   string   `json:"ProjectionType,omitempty"`
	NonKeyAttributes []string `json:"NonKeyAttributes,omitempty"`
}

type DynamoDBLocalSecondaryIndex struct {
	IndexName  string                     `json:"IndexName"`
	KeySchema  []DynamoDBKeySchemaElement `json:"KeySchema"`
	Projection DynamoDBProjection         `json:"Projection"`
}

type DynamoDBGlobalSecondaryIndex struct {
	IndexName             string                         `json:"IndexName"`
	KeySchema             []DynamoDBKeySchemaElement     `json:"KeySchema"`
	Projection            DynamoDBProjection             `json:"Projection"`
	ProvisionedThroughput *DynamoDBProvisionedThroughput `json:"ProvisionedThroughput,omitempty"`
}

type DynamoDBStreamSpecification struct {
	StreamEnabled  bool   `json:"StreamEnabled"`
	StreamViewType string `json:"StreamViewType,omitempty"`
}

type DynamoDBTag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

type DynamoDBTable struct {
	TableName              string                         `json:"TableName"`
	TableArn               string                         `json:"TableArn"`
	TableId                string                         `json:"TableId"`
	TableStatus            string                         `json:"TableStatus"`
	CreationDateTime       float64                        `json:"CreationDateTime"`
	AttributeDefinitions   []DynamoDBAttributeDefinition  `json:"AttributeDefinitions"`
	KeySchema              []DynamoDBKeySchemaElement     `json:"KeySchema"`
	BillingModeSummary     *DynamoDBBillingModeSummary    `json:"BillingModeSummary,omitempty"`
	TableClassSummary      *DynamoDBTableClassSummary     `json:"TableClassSummary,omitempty"`
	ProvisionedThroughput  *DynamoDBProvisionedThroughput `json:"ProvisionedThroughput"`
	LocalSecondaryIndexes  []DynamoDBLocalSecondaryIndex  `json:"LocalSecondaryIndexes,omitempty"`
	GlobalSecondaryIndexes []DynamoDBGlobalSecondaryIndex `json:"GlobalSecondaryIndexes,omitempty"`
	StreamSpecification    *DynamoDBStreamSpecification   `json:"StreamSpecification,omitempty"`
	LatestStreamArn        string                         `json:"LatestStreamArn,omitempty"`
	LatestStreamLabel      string                         `json:"LatestStreamLabel,omitempty"`
	Tags                   []DynamoDBTag                  `json:"Tags,omitempty"`
	ItemCount              int64                          `json:"ItemCount"`
	TableSizeBytes         int64                          `json:"TableSizeBytes"`
	WarmThroughput         *DynamoDBWarmThroughput        `json:"WarmThroughput,omitempty"`
}

func (t *DynamoDBTable) UnmarshalJSON(data []byte) error {
	type rawTable DynamoDBTable
	type decodedTable struct {
		rawTable
		CreationDateTime any `json:"CreationDateTime"`
	}

	var decoded decodedTable
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	*t = DynamoDBTable(decoded.rawTable)

	value, err := parseFlexibleEpoch(decoded.CreationDateTime)
	if err != nil {
		return fmt.Errorf("parse CreationDateTime: %w", err)
	}
	t.CreationDateTime = value
	return nil
}

type DynamoDBBillingModeSummary struct {
	BillingMode string `json:"BillingMode"`
}

type DynamoDBTableClassSummary struct {
	TableClass string `json:"TableClass"`
}

type DynamoDBWarmThroughput struct {
	ReadUnitsPerSecond  int64  `json:"ReadUnitsPerSecond,omitempty"`
	Status              string `json:"Status,omitempty"`
	WriteUnitsPerSecond int64  `json:"WriteUnitsPerSecond,omitempty"`
}

type StreamRecord struct {
	EventID        string           `json:"eventID"`
	EventName      string           `json:"eventName"`
	EventVersion   string           `json:"eventVersion"`
	EventSource    string           `json:"eventSource"`
	AwsRegion      string           `json:"awsRegion"`
	EventSourceARN string           `json:"eventSourceARN"`
	Dynamodb       StreamRecordData `json:"dynamodb"`
}

type StreamRecordData struct {
	ApproximateCreationDateTime float64        `json:"ApproximateCreationDateTime"`
	Keys                        map[string]any `json:"Keys,omitempty"`
	NewImage                    map[string]any `json:"NewImage,omitempty"`
	OldImage                    map[string]any `json:"OldImage,omitempty"`
	SequenceNumber              string         `json:"SequenceNumber"`
	SizeBytes                   int64          `json:"SizeBytes"`
	StreamViewType              string         `json:"StreamViewType"`
}

func (d *StreamRecordData) UnmarshalJSON(data []byte) error {
	type rawRecordData StreamRecordData
	type decodedRecordData struct {
		rawRecordData
		ApproximateCreationDateTime any `json:"ApproximateCreationDateTime"`
	}

	var decoded decodedRecordData
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	*d = StreamRecordData(decoded.rawRecordData)

	value, err := parseFlexibleEpoch(decoded.ApproximateCreationDateTime)
	if err != nil {
		return fmt.Errorf("parse ApproximateCreationDateTime: %w", err)
	}
	d.ApproximateCreationDateTime = value
	return nil
}

func parseFlexibleEpoch(value any) (float64, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case float64:
		return typed, nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, nil
		}
		if numeric, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return numeric, nil
		}
		t, err := time.Parse(time.RFC3339, trimmed)
		if err != nil {
			return 0, fmt.Errorf("unsupported timestamp %q", typed)
		}
		return float64(t.UnixMilli()) / 1000.0, nil
	default:
		return 0, fmt.Errorf("unsupported timestamp type %T", value)
	}
}
