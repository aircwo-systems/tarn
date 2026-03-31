package dynamodb

import (
	"encoding/json"
	"io"
	"log"
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

func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", "Failed to read request body")
		return
	}

	switch {
	case strings.HasPrefix(target, dynamoPrefix):
		h.dispatchTables(w, strings.TrimPrefix(target, dynamoPrefix), body)
	case strings.HasPrefix(target, dynamoStreamPrefix):
		h.dispatchStreams(w, strings.TrimPrefix(target, dynamoStreamPrefix), body)
	default:
		writeError(w, http.StatusBadRequest, "ValidationException", "Invalid X-Amz-Target for DynamoDB")
	}
}

func (h *Handler) dispatchTables(w http.ResponseWriter, action string, body []byte) {
	switch action {
	case "CreateTable":
		h.createTable(w, body)
	case "UpdateTable":
		h.updateTable(w, body)
	case "TagResource":
		h.tagResource(w, body)
	case "UntagResource":
		h.untagResource(w, body)
	case "ListTagsOfResource":
		h.listTagsOfResource(w, body)
	case "DescribeTimeToLive":
		h.describeTimeToLive(w, body)
	case "UpdateTimeToLive":
		h.updateTimeToLive(w, body)
	case "DescribeContinuousBackups":
		h.describeContinuousBackups(w, body)
	case "UpdateContinuousBackups":
		h.updateContinuousBackups(w, body)
	case "DescribeContributorInsights":
		h.describeContributorInsights(w, body)
	case "UpdateContributorInsights":
		h.updateContributorInsights(w, body)
	case "DescribeKinesisStreamingDestination":
		h.describeKinesisStreamingDestination(w, body)
	case "EnableKinesisStreamingDestination":
		h.enableKinesisStreamingDestination(w, body)
	case "DisableKinesisStreamingDestination":
		h.disableKinesisStreamingDestination(w, body)
	case "DeleteTable":
		h.deleteTable(w, body)
	case "DescribeTable":
		h.describeTable(w, body)
	case "ListTables":
		h.listTables(w, body)
	case "PutItem":
		h.putItem(w, body)
	case "GetItem":
		h.getItem(w, body)
	case "UpdateItem":
		h.updateItem(w, body)
	case "DeleteItem":
		h.deleteItem(w, body)
	case "Scan":
		h.scan(w, body)
	case "Query":
		h.query(w, body)
	default:
		log.Printf("[dynamodb] unhandled action (returning empty OK): %s", action)
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}

func (h *Handler) dispatchStreams(w http.ResponseWriter, action string, body []byte) {
	switch action {
	case "ListStreams":
		h.listStreams(w, body)
	case "DescribeStream":
		h.describeStream(w, body)
	case "GetShardIterator":
		h.getShardIterator(w, body)
	case "GetRecords":
		h.getRecords(w, body)
	default:
		log.Printf("[dynamodbstreams] unhandled action (returning empty OK): %s", action)
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}

func (h *Handler) createTable(w http.ResponseWriter, body []byte) {
	var req types.DynamoDBTable
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	table, err := h.svc.CreateTable(&req)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"TableDescription": tableOutput(table)})
}

func (h *Handler) describeTable(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName string `json:"TableName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	table, err := h.svc.DescribeTable(req.TableName)
	if err != nil {
		log.Printf("[dynamodb] DescribeTable table=%q error=%v", req.TableName, err)
		writeSvcError(w, err)
		return
	}
	log.Printf(
		"[dynamodb] DescribeTable table=%q resolved=%q status=%q arn=%q stream_arn=%q stream_label=%q billing=%q",
		req.TableName,
		table.TableName,
		table.TableStatus,
		table.TableArn,
		table.LatestStreamArn,
		table.LatestStreamLabel,
		billingModeOf(table),
	)
	writeJSON(w, http.StatusOK, map[string]any{"Table": tableOutput(table)})
}

func (h *Handler) deleteTable(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName string `json:"TableName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	table, err := h.svc.DeleteTable(req.TableName)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"TableDescription": tableOutput(table)})
}

func (h *Handler) updateTable(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName           string                             `json:"TableName"`
		StreamSpecification *types.DynamoDBStreamSpecification `json:"StreamSpecification"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	table, err := h.svc.UpdateTable(req.TableName, req.StreamSpecification)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"TableDescription": tableOutput(table)})
}

func (h *Handler) tagResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceArn string              `json:"ResourceArn"`
		Tags        []types.DynamoDBTag `json:"Tags"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	if err := h.svc.TagResource(req.ResourceArn, req.Tags); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) untagResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceArn string   `json:"ResourceArn"`
		TagKeys     []string `json:"TagKeys"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	if err := h.svc.UntagResource(req.ResourceArn, req.TagKeys); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) listTagsOfResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceArn string `json:"ResourceArn"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	tags, err := h.svc.ListTagsOfResource(req.ResourceArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"Tags": tags})
}

func (h *Handler) describeTimeToLive(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName string `json:"TableName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeTimeToLive(req.TableName)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) updateTimeToLive(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName               string `json:"TableName"`
		TimeToLiveSpecification struct {
			AttributeName string `json:"AttributeName"`
			Enabled       bool   `json:"Enabled"`
		} `json:"TimeToLiveSpecification"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.UpdateTimeToLive(req.TableName, req.TimeToLiveSpecification.AttributeName, req.TimeToLiveSpecification.Enabled)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) describeContinuousBackups(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName string `json:"TableName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeContinuousBackups(req.TableName)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) updateContinuousBackups(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName                        string `json:"TableName"`
		PointInTimeRecoverySpecification struct {
			PointInTimeRecoveryEnabled bool `json:"PointInTimeRecoveryEnabled"`
		} `json:"PointInTimeRecoverySpecification"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.UpdateContinuousBackups(req.TableName, req.PointInTimeRecoverySpecification.PointInTimeRecoveryEnabled)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) describeContributorInsights(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName string `json:"TableName"`
		IndexName string `json:"IndexName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeContributorInsights(req.TableName, req.IndexName)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) updateContributorInsights(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName                 string `json:"TableName"`
		IndexName                 string `json:"IndexName"`
		ContributorInsightsAction string `json:"ContributorInsightsAction"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.UpdateContributorInsights(req.TableName, req.IndexName, req.ContributorInsightsAction)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) describeKinesisStreamingDestination(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName string `json:"TableName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeKinesisStreamingDestination(req.TableName)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) enableKinesisStreamingDestination(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName string `json:"TableName"`
		StreamArn string `json:"StreamArn"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.EnableKinesisStreamingDestination(req.TableName, req.StreamArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) disableKinesisStreamingDestination(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName string `json:"TableName"`
		StreamArn string `json:"StreamArn"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DisableKinesisStreamingDestination(req.TableName, req.StreamArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) listTables(w http.ResponseWriter, body []byte) {
	var req struct {
		ExclusiveStartTableName string `json:"ExclusiveStartTableName"`
		Limit                   int    `json:"Limit"`
	}
	_ = decodeJSON(body, &req)
	tables, last, err := h.svc.ListTables(req.Limit, req.ExclusiveStartTableName)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	resp := map[string]any{"TableNames": tables}
	if last != "" {
		resp["LastEvaluatedTableName"] = last
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) putItem(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName                 string            `json:"TableName"`
		Item                      map[string]any    `json:"Item"`
		ConditionExpression       string            `json:"ConditionExpression"`
		ExpressionAttributeNames  map[string]string `json:"ExpressionAttributeNames"`
		ExpressionAttributeValues map[string]any    `json:"ExpressionAttributeValues"`
		ReturnValues              string            `json:"ReturnValues"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.PutItem(req.TableName, req.Item, req.ConditionExpression, req.ExpressionAttributeNames, req.ExpressionAttributeValues, req.ReturnValues)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	resp := map[string]any{}
	if out != nil && out.Attributes != nil {
		resp["Attributes"] = out.Attributes
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getItem(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName                string            `json:"TableName"`
		Key                      map[string]any    `json:"Key"`
		ProjectionExpression     string            `json:"ProjectionExpression"`
		ExpressionAttributeNames map[string]string `json:"ExpressionAttributeNames"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.GetItem(req.TableName, req.Key, req.ProjectionExpression, req.ExpressionAttributeNames)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	resp := map[string]any{}
	if out != nil && out.Item != nil {
		resp["Item"] = out.Item
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) updateItem(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName                 string            `json:"TableName"`
		Key                       map[string]any    `json:"Key"`
		UpdateExpression          string            `json:"UpdateExpression"`
		ConditionExpression       string            `json:"ConditionExpression"`
		ExpressionAttributeNames  map[string]string `json:"ExpressionAttributeNames"`
		ExpressionAttributeValues map[string]any    `json:"ExpressionAttributeValues"`
		ReturnValues              string            `json:"ReturnValues"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.UpdateItem(req.TableName, req.Key, req.UpdateExpression, req.ConditionExpression, req.ExpressionAttributeNames, req.ExpressionAttributeValues, req.ReturnValues)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	resp := map[string]any{}
	if out != nil && out.Attributes != nil {
		resp["Attributes"] = out.Attributes
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) deleteItem(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName                 string            `json:"TableName"`
		Key                       map[string]any    `json:"Key"`
		ConditionExpression       string            `json:"ConditionExpression"`
		ExpressionAttributeNames  map[string]string `json:"ExpressionAttributeNames"`
		ExpressionAttributeValues map[string]any    `json:"ExpressionAttributeValues"`
		ReturnValues              string            `json:"ReturnValues"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DeleteItem(req.TableName, req.Key, req.ConditionExpression, req.ExpressionAttributeNames, req.ExpressionAttributeValues, req.ReturnValues)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	resp := map[string]any{}
	if out != nil && out.Attributes != nil {
		resp["Attributes"] = out.Attributes
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) scan(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName                 string            `json:"TableName"`
		IndexName                 string            `json:"IndexName"`
		FilterExpression          string            `json:"FilterExpression"`
		ProjectionExpression      string            `json:"ProjectionExpression"`
		ExpressionAttributeNames  map[string]string `json:"ExpressionAttributeNames"`
		ExpressionAttributeValues map[string]any    `json:"ExpressionAttributeValues"`
		Limit                     int               `json:"Limit"`
		ExclusiveStartKey         map[string]any    `json:"ExclusiveStartKey"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.Scan(req.TableName, req.IndexName, req.FilterExpression, req.ProjectionExpression, req.ExpressionAttributeNames, req.ExpressionAttributeValues, req.Limit, req.ExclusiveStartKey)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	resp := map[string]any{"Items": out.Items, "Count": out.Count, "ScannedCount": out.Count}
	if out.LastEvaluatedKey != nil {
		resp["LastEvaluatedKey"] = out.LastEvaluatedKey
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) query(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName                 string            `json:"TableName"`
		IndexName                 string            `json:"IndexName"`
		KeyConditionExpression    string            `json:"KeyConditionExpression"`
		FilterExpression          string            `json:"FilterExpression"`
		ProjectionExpression      string            `json:"ProjectionExpression"`
		ExpressionAttributeNames  map[string]string `json:"ExpressionAttributeNames"`
		ExpressionAttributeValues map[string]any    `json:"ExpressionAttributeValues"`
		Limit                     int               `json:"Limit"`
		ExclusiveStartKey         map[string]any    `json:"ExclusiveStartKey"`
		ScanIndexForward          *bool             `json:"ScanIndexForward"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	scanForward := true
	if req.ScanIndexForward != nil {
		scanForward = *req.ScanIndexForward
	}
	out, err := h.svc.Query(req.TableName, req.IndexName, req.KeyConditionExpression, req.FilterExpression, req.ProjectionExpression, req.ExpressionAttributeNames, req.ExpressionAttributeValues, req.Limit, req.ExclusiveStartKey, scanForward)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	resp := map[string]any{"Items": out.Items, "Count": out.Count, "ScannedCount": out.Count}
	if out.LastEvaluatedKey != nil {
		resp["LastEvaluatedKey"] = out.LastEvaluatedKey
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listStreams(w http.ResponseWriter, body []byte) {
	var req struct {
		TableName               string `json:"TableName"`
		Limit                   int    `json:"Limit"`
		ExclusiveStartStreamArn string `json:"ExclusiveStartStreamArn"`
	}
	_ = decodeJSON(body, &req)
	streams, last, err := h.svc.ListStreams(req.TableName, req.Limit, req.ExclusiveStartStreamArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	resp := map[string]any{"Streams": streams}
	if last != "" {
		resp["LastEvaluatedStreamArn"] = last
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) describeStream(w http.ResponseWriter, body []byte) {
	var req struct {
		StreamArn             string `json:"StreamArn"`
		Limit                 int    `json:"Limit"`
		ExclusiveStartShardId string `json:"ExclusiveStartShardId"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeStream(req.StreamArn, req.Limit, req.ExclusiveStartShardId)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getShardIterator(w http.ResponseWriter, body []byte) {
	var req struct {
		StreamArn         string `json:"StreamArn"`
		ShardId           string `json:"ShardId"`
		ShardIteratorType string `json:"ShardIteratorType"`
		SequenceNumber    string `json:"SequenceNumber"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	iterator, err := h.svc.GetShardIterator(req.StreamArn, req.ShardId, req.ShardIteratorType, req.SequenceNumber)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ShardIterator": iterator})
}

func (h *Handler) getRecords(w http.ResponseWriter, body []byte) {
	var req struct {
		ShardIterator string `json:"ShardIterator"`
		Limit         int    `json:"Limit"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	records, next, err := h.svc.GetRecords(req.ShardIterator, req.Limit)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"Records": records, "NextShardIterator": next})
}

func decodeJSON(body []byte, dst any) error {
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return err
	}
	return nil
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
	if svcErr, ok := err.(*dynamodbsvc.ServiceError); ok {
		writeError(w, svcErr.StatusCode(), svcErr.Code, svcErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "InternalServerError", err.Error())
}

func billingModeOf(table *types.DynamoDBTable) string {
	if table == nil || table.BillingModeSummary == nil {
		return ""
	}
	return table.BillingModeSummary.BillingMode
}

func tableOutput(table *types.DynamoDBTable) *types.DynamoDBTable {
	if table == nil {
		return nil
	}
	out := *table
	out.Tags = nil
	return &out
}
