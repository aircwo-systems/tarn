package dynamodb

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	dynamodbsvc "github.com/aircwo-systems/tarn/internal/dynamodb"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const (
	dynamoPrefix       = "DynamoDB_20120810."
	dynamoStreamPrefix = "DynamoDBStreams_20120810."
)

type Handler struct {
	svc *dynamodbsvc.Service
}

func NewHandler(svc *dynamodbsvc.Service) *Handler {
	return &Handler{svc: svc}
}

func IsDynamoDBRequest(r *http.Request) bool {
	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	return strings.HasPrefix(target, dynamoPrefix) || strings.HasPrefix(target, dynamoStreamPrefix)
}

// operation decodes a request body and returns the response payload.
type operation func(svc *dynamodbsvc.Service, body []byte) (any, error)

// op adapts a typed handler to an operation, so each action is one line in
// the tables below.
func op[T any](fn func(svc *dynamodbsvc.Service, req *T) (any, error)) operation {
	return func(svc *dynamodbsvc.Service, body []byte) (any, error) {
		req := new(T)
		if len(body) > 0 {
			if err := json.Unmarshal(body, req); err != nil {
				return nil, &dynamodbsvc.ServiceError{Code: "SerializationException", Message: err.Error()}
			}
		}
		return fn(svc, req)
	}
}

type tableName struct {
	TableName string `json:"TableName"`
}

type resourceArn struct {
	ResourceArn string              `json:"ResourceArn"`
	Tags        []types.DynamoDBTag `json:"Tags"`
	TagKeys     []string            `json:"TagKeys"`
}

type kinesisDestination struct {
	TableName string `json:"TableName"`
	StreamArn string `json:"StreamArn"`
}

type contributorInsights struct {
	TableName                 string `json:"TableName"`
	IndexName                 string `json:"IndexName"`
	ContributorInsightsAction string `json:"ContributorInsightsAction"`
}

var tableOps = map[string]operation{
	"CreateTable": func(svc *dynamodbsvc.Service, body []byte) (any, error) {
		table, err := decodeCreateTable(body)
		if err != nil {
			return nil, err
		}
		out, err := svc.CreateTable(table)
		return tableDescription(out), err
	},
	"UpdateTable": op(func(svc *dynamodbsvc.Service, req *dynamodbsvc.UpdateTableInput) (any, error) {
		out, err := svc.UpdateTable(req)
		return tableDescription(out), err
	}),
	"DeleteTable": op(func(svc *dynamodbsvc.Service, req *tableName) (any, error) {
		out, err := svc.DeleteTable(req.TableName)
		return tableDescription(out), err
	}),
	"DescribeTable": op(func(svc *dynamodbsvc.Service, req *tableName) (any, error) {
		out, err := svc.DescribeTable(req.TableName)
		if err != nil {
			return nil, err
		}
		return map[string]any{"Table": tableOutput(out)}, nil
	}),
	"ListTables": op(func(svc *dynamodbsvc.Service, req *struct {
		ExclusiveStartTableName string `json:"ExclusiveStartTableName"`
		Limit                   int    `json:"Limit"`
	}) (any, error) {
		tables, last, err := svc.ListTables(req.Limit, req.ExclusiveStartTableName)
		resp := map[string]any{"TableNames": tables}
		if last != "" {
			resp["LastEvaluatedTableName"] = last
		}
		return resp, err
	}),
	"TagResource": op(func(svc *dynamodbsvc.Service, req *resourceArn) (any, error) {
		return struct{}{}, svc.TagResource(req.ResourceArn, req.Tags)
	}),
	"UntagResource": op(func(svc *dynamodbsvc.Service, req *resourceArn) (any, error) {
		return struct{}{}, svc.UntagResource(req.ResourceArn, req.TagKeys)
	}),
	"ListTagsOfResource": op(func(svc *dynamodbsvc.Service, req *resourceArn) (any, error) {
		tags, err := svc.ListTagsOfResource(req.ResourceArn)
		return map[string]any{"Tags": tags}, err
	}),
	"DescribeTimeToLive": op(func(svc *dynamodbsvc.Service, req *tableName) (any, error) {
		return svc.DescribeTimeToLive(req.TableName)
	}),
	"UpdateTimeToLive": op(func(svc *dynamodbsvc.Service, req *struct {
		TableName               string `json:"TableName"`
		TimeToLiveSpecification struct {
			AttributeName string `json:"AttributeName"`
			Enabled       bool   `json:"Enabled"`
		} `json:"TimeToLiveSpecification"`
	}) (any, error) {
		return svc.UpdateTimeToLive(req.TableName, req.TimeToLiveSpecification.AttributeName, req.TimeToLiveSpecification.Enabled)
	}),
	"DescribeContinuousBackups": op(func(svc *dynamodbsvc.Service, req *tableName) (any, error) {
		return svc.DescribeContinuousBackups(req.TableName)
	}),
	"UpdateContinuousBackups": op(func(svc *dynamodbsvc.Service, req *struct {
		TableName                        string `json:"TableName"`
		PointInTimeRecoverySpecification struct {
			PointInTimeRecoveryEnabled bool `json:"PointInTimeRecoveryEnabled"`
		} `json:"PointInTimeRecoverySpecification"`
	}) (any, error) {
		return svc.UpdateContinuousBackups(req.TableName, req.PointInTimeRecoverySpecification.PointInTimeRecoveryEnabled)
	}),
	"DescribeContributorInsights": op(func(svc *dynamodbsvc.Service, req *contributorInsights) (any, error) {
		return svc.DescribeContributorInsights(req.TableName, req.IndexName)
	}),
	"UpdateContributorInsights": op(func(svc *dynamodbsvc.Service, req *contributorInsights) (any, error) {
		return svc.UpdateContributorInsights(req.TableName, req.IndexName, req.ContributorInsightsAction)
	}),
	"DescribeKinesisStreamingDestination": op(func(svc *dynamodbsvc.Service, req *tableName) (any, error) {
		return svc.DescribeKinesisStreamingDestination(req.TableName)
	}),
	"EnableKinesisStreamingDestination": op(func(svc *dynamodbsvc.Service, req *kinesisDestination) (any, error) {
		return svc.EnableKinesisStreamingDestination(req.TableName, req.StreamArn)
	}),
	"DisableKinesisStreamingDestination": op(func(svc *dynamodbsvc.Service, req *kinesisDestination) (any, error) {
		return svc.DisableKinesisStreamingDestination(req.TableName, req.StreamArn)
	}),
	"PutItem": op(func(svc *dynamodbsvc.Service, req *dynamodbsvc.WriteInput) (any, error) {
		return attributes(svc.PutItem(req))
	}),
	"UpdateItem": op(func(svc *dynamodbsvc.Service, req *dynamodbsvc.WriteInput) (any, error) {
		return attributes(svc.UpdateItem(req))
	}),
	"DeleteItem": op(func(svc *dynamodbsvc.Service, req *dynamodbsvc.WriteInput) (any, error) {
		return attributes(svc.DeleteItem(req))
	}),
	"GetItem": op(func(svc *dynamodbsvc.Service, req *struct {
		TableName                string            `json:"TableName"`
		Key                      map[string]any    `json:"Key"`
		ProjectionExpression     string            `json:"ProjectionExpression"`
		ExpressionAttributeNames map[string]string `json:"ExpressionAttributeNames"`
	}) (any, error) {
		item, err := svc.GetItem(req.TableName, req.Key, req.ProjectionExpression, req.ExpressionAttributeNames)
		if item == nil {
			return struct{}{}, err
		}
		return map[string]any{"Item": item}, err
	}),
	"Scan": op(func(svc *dynamodbsvc.Service, req *dynamodbsvc.ReadInput) (any, error) {
		return page(svc.Scan(req))
	}),
	"Query": op(func(svc *dynamodbsvc.Service, req *dynamodbsvc.ReadInput) (any, error) {
		return page(svc.Query(req))
	}),
	"BatchWriteItem": op(func(svc *dynamodbsvc.Service, req *struct {
		RequestItems map[string][]dynamodbsvc.BatchWriteRequest `json:"RequestItems"`
	}) (any, error) {
		if err := svc.BatchWriteItem(req.RequestItems); err != nil {
			return nil, err
		}
		return map[string]any{"UnprocessedItems": map[string]any{}}, nil
	}),
	"BatchGetItem": op(func(svc *dynamodbsvc.Service, req *struct {
		RequestItems map[string]dynamodbsvc.KeysAndAttributes `json:"RequestItems"`
	}) (any, error) {
		out, err := svc.BatchGetItem(req.RequestItems)
		if err != nil {
			return nil, err
		}
		return map[string]any{"Responses": out, "UnprocessedKeys": map[string]any{}}, nil
	}),
	"TransactWriteItems": op(func(svc *dynamodbsvc.Service, req *struct {
		TransactItems []dynamodbsvc.TransactWriteItem `json:"TransactItems"`
	}) (any, error) {
		return struct{}{}, svc.TransactWriteItems(req.TransactItems)
	}),
	"TransactGetItems": op(func(svc *dynamodbsvc.Service, req *struct {
		TransactItems []dynamodbsvc.TransactGetItem `json:"TransactItems"`
	}) (any, error) {
		items, err := svc.TransactGetItems(req.TransactItems)
		if err != nil {
			return nil, err
		}
		responses := make([]map[string]any, len(items))
		for i, item := range items {
			responses[i] = map[string]any{}
			if item != nil {
				responses[i]["Item"] = item
			}
		}
		return map[string]any{"Responses": responses}, nil
	}),
}

var streamOps = map[string]operation{
	"ListStreams": op(func(svc *dynamodbsvc.Service, req *struct {
		TableName               string `json:"TableName"`
		Limit                   int    `json:"Limit"`
		ExclusiveStartStreamArn string `json:"ExclusiveStartStreamArn"`
	}) (any, error) {
		streams, last, err := svc.ListStreams(req.TableName, req.Limit, req.ExclusiveStartStreamArn)
		resp := map[string]any{"Streams": streams}
		if last != "" {
			resp["LastEvaluatedStreamArn"] = last
		}
		return resp, err
	}),
	"DescribeStream": op(func(svc *dynamodbsvc.Service, req *struct {
		StreamArn             string `json:"StreamArn"`
		Limit                 int    `json:"Limit"`
		ExclusiveStartShardId string `json:"ExclusiveStartShardId"`
	}) (any, error) {
		return svc.DescribeStream(req.StreamArn, req.Limit, req.ExclusiveStartShardId)
	}),
	"GetShardIterator": op(func(svc *dynamodbsvc.Service, req *struct {
		StreamArn         string `json:"StreamArn"`
		ShardId           string `json:"ShardId"`
		ShardIteratorType string `json:"ShardIteratorType"`
		SequenceNumber    string `json:"SequenceNumber"`
	}) (any, error) {
		iterator, err := svc.GetShardIterator(req.StreamArn, req.ShardId, req.ShardIteratorType, req.SequenceNumber)
		return map[string]any{"ShardIterator": iterator}, err
	}),
	"GetRecords": op(func(svc *dynamodbsvc.Service, req *struct {
		ShardIterator string `json:"ShardIterator"`
		Limit         int    `json:"Limit"`
	}) (any, error) {
		records, next, err := svc.GetRecords(req.ShardIterator, req.Limit)
		return map[string]any{"Records": records, "NextShardIterator": next}, err
	}),
}

func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", "Failed to read request body")
		return
	}

	var fn operation
	if action, ok := strings.CutPrefix(target, dynamoPrefix); ok {
		fn = tableOps[action]
	} else if action, ok := strings.CutPrefix(target, dynamoStreamPrefix); ok {
		fn = streamOps[action]
	}
	if fn == nil {
		// Answering an unknown action with success would let a client believe
		// writes landed that never happened.
		writeError(w, http.StatusBadRequest, "UnknownOperationException", "Operation not supported: "+target)
		return
	}
	out, err := fn(h.svc, body)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// decodeCreateTable reads CreateTable's request-only fields alongside the
// table definition, which shares DescribeTable's shape.
func decodeCreateTable(body []byte) (*types.DynamoDBTable, error) {
	var table types.DynamoDBTable
	var extra struct {
		BillingMode               string `json:"BillingMode"`
		TableClass                string `json:"TableClass"`
		DeletionProtectionEnabled bool   `json:"DeletionProtectionEnabled"`
	}
	if err := json.Unmarshal(body, &table); err != nil {
		return nil, &dynamodbsvc.ServiceError{Code: "SerializationException", Message: err.Error()}
	}
	_ = json.Unmarshal(body, &extra)
	if extra.BillingMode != "" {
		table.BillingModeSummary = &types.DynamoDBBillingModeSummary{BillingMode: extra.BillingMode}
	}
	if extra.TableClass != "" {
		table.TableClassSummary = &types.DynamoDBTableClassSummary{TableClass: extra.TableClass}
	}
	table.DeletionProtectionEnabled = extra.DeletionProtectionEnabled
	return &table, nil
}

func tableDescription(table *types.DynamoDBTable) any {
	return map[string]any{"TableDescription": tableOutput(table)}
}

func attributes(item map[string]any, err error) (any, error) {
	if item == nil {
		return struct{}{}, err
	}
	return map[string]any{"Attributes": item}, err
}

func page(out *dynamodbsvc.ReadResult, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	resp := map[string]any{"Count": out.Count, "ScannedCount": out.ScannedCount}
	if out.Items != nil {
		resp["Items"] = out.Items
	}
	if out.LastEvaluatedKey != nil {
		resp["LastEvaluatedKey"] = out.LastEvaluatedKey
	}
	return resp, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.0")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"__type": code, "message": message})
}

func writeSvcError(w http.ResponseWriter, err error) {
	svcErr, ok := err.(*dynamodbsvc.ServiceError)
	if !ok {
		writeError(w, http.StatusInternalServerError, "InternalServerError", err.Error())
		return
	}
	body := map[string]any{"__type": svcErr.Code, "message": svcErr.Message}
	if svcErr.Item != nil {
		body["Item"] = svcErr.Item
	}
	if svcErr.CancellationReasons != nil {
		body["CancellationReasons"] = svcErr.CancellationReasons
	}
	writeJSON(w, svcErr.StatusCode(), body)
}

func tableOutput(table *types.DynamoDBTable) *types.DynamoDBTable {
	if table == nil {
		return nil
	}
	out := *table
	out.Tags = nil
	return &out
}
