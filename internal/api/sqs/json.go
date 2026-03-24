package sqs

// SQS JSON wire protocol support (AWS SDK v2 / Terraform AWS provider v5+).
//
// AWS switched SQS to a JSON protocol in November 2023. Clients send:
//
//	POST /
//	Content-Type: application/x-amz-json-1.0
//	X-Amz-Target: AmazonSQS.CreateQueue
//	{"QueueName": "orders", "Attributes": {...}}
//
// and expect a JSON response instead of the older query/XML protocol.

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	sqssvc "github.com/openstack-project/openstack/internal/sqs"
	"github.com/openstack-project/openstack/pkg/types"
)

// isJSONProtocol reports whether the request uses the SQS JSON wire protocol.
func isJSONProtocol(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("X-Amz-Target"), "AmazonSQS.")
}

// jsonAction extracts the action name from the X-Amz-Target header,
// e.g. "AmazonSQS.CreateQueue" → "CreateQueue".
func jsonAction(r *http.Request) string {
	t := r.Header.Get("X-Amz-Target")
	if idx := strings.LastIndex(t, "."); idx >= 0 {
		return t[idx+1:]
	}
	return t
}

// dispatchJSON handles SQS JSON protocol requests.
func (h *Handler) dispatchJSON(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, 400, "InvalidRequest", "failed to read request body")
		return
	}

	action := jsonAction(r)
	switch action {
	case "CreateQueue":
		h.jsonCreateQueue(w, body)
	case "DeleteQueue":
		h.jsonDeleteQueue(w, body)
	case "ListQueues":
		h.jsonListQueues(w, body)
	case "GetQueueUrl":
		h.jsonGetQueueUrl(w, body)
	case "GetQueueAttributes":
		h.jsonGetQueueAttributes(w, body)
	case "SetQueueAttributes":
		h.jsonSetQueueAttributes(w, body)
	case "SendMessage":
		h.jsonSendMessage(w, body)
	case "SendMessageBatch":
		h.jsonSendMessageBatch(w, body)
	case "ReceiveMessage":
		h.jsonReceiveMessage(w, body)
	case "DeleteMessage":
		h.jsonDeleteMessage(w, body)
	case "DeleteMessageBatch":
		h.jsonDeleteMessageBatch(w, body)
	case "ChangeMessageVisibility":
		h.jsonChangeMessageVisibility(w, body)
	case "PurgeQueue":
		h.jsonPurgeQueue(w, body)
	case "TagQueue":
		h.jsonTagQueue(w, body)
	case "UntagQueue":
		h.jsonUntagQueue(w, body)
	case "ListQueueTags":
		h.jsonListQueueTags(w, body)
	default:
		writeJSONError(w, 400, "InvalidAction", "The action "+action+" is not valid for this endpoint")
	}
}

func (h *Handler) jsonCreateQueue(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueName  string            `json:"QueueName"`
		Attributes map[string]string `json:"Attributes"`
		Tags       map[string]string `json:"tags"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	if req.QueueName == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueName is required")
		return
	}
	q, err := h.svc.CreateQueue(req.QueueName, req.Attributes, req.Tags)
	if err != nil {
		writeJSONError(w, 400, "InvalidParameterValue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"QueueUrl": q.QueueUrl})
}

func (h *Handler) jsonDeleteQueue(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl string `json:"QueueUrl"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	if err := h.svc.DeleteQueue(name); err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{})
}

func (h *Handler) jsonListQueues(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueNamePrefix string `json:"QueueNamePrefix"`
	}
	_ = json.Unmarshal(body, &req)
	queues := h.svc.ListQueues(req.QueueNamePrefix)
	urls := make([]string, 0, len(queues))
	for _, q := range queues {
		urls = append(urls, q.QueueUrl)
	}
	writeJSON(w, 200, map[string]any{"QueueUrls": urls})
}

func (h *Handler) jsonGetQueueUrl(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueName string `json:"QueueName"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	if req.QueueName == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueName is required")
		return
	}
	url, err := h.svc.GetQueueUrl(req.QueueName)
	if err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"QueueUrl": url})
}

func (h *Handler) jsonGetQueueAttributes(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl       string   `json:"QueueUrl"`
		AttributeNames []string `json:"AttributeNames"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	attrs, err := h.svc.GetQueueAttributes(name, req.AttributeNames)
	if err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"Attributes": attrs})
}

func (h *Handler) jsonSetQueueAttributes(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl   string            `json:"QueueUrl"`
		Attributes map[string]string `json:"Attributes"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	if err := h.svc.SetQueueAttributes(name, req.Attributes); err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{})
}

func (h *Handler) jsonSendMessage(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl               string                             `json:"QueueUrl"`
		MessageBody            string                             `json:"MessageBody"`
		DelaySeconds           int                                `json:"DelaySeconds"`
		MessageGroupId         string                             `json:"MessageGroupId"`
		MessageDeduplicationId string                             `json:"MessageDeduplicationId"`
		MessageAttributes      map[string]*types.MessageAttribute `json:"MessageAttributes"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	if req.MessageBody == "" {
		writeJSONError(w, 400, "MissingParameter", "MessageBody is required")
		return
	}
	msg, err := h.svc.SendMessage(name, req.MessageBody, req.DelaySeconds, req.MessageAttributes, req.MessageGroupId, req.MessageDeduplicationId)
	if err != nil {
		writeJSONError(w, 400, "InvalidParameterValue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{
		"MessageId":        msg.MessageId,
		"MD5OfMessageBody": msg.MD5OfBody,
	})
}

type jsonBatchEntry struct {
	Id                     string `json:"Id"`
	MessageBody            string `json:"MessageBody"`
	DelaySeconds           int    `json:"DelaySeconds"`
	MessageGroupId         string `json:"MessageGroupId"`
	MessageDeduplicationId string `json:"MessageDeduplicationId"`
}

func (h *Handler) jsonSendMessageBatch(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl string           `json:"QueueUrl"`
		Entries  []jsonBatchEntry `json:"Entries"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	type successEntry struct {
		Id               string `json:"Id"`
		MessageId        string `json:"MessageId"`
		MD5OfMessageBody string `json:"MD5OfMessageBody"`
	}
	type failEntry struct {
		Id          string `json:"Id"`
		Code        string `json:"Code"`
		Message     string `json:"Message"`
		SenderFault bool   `json:"SenderFault"`
	}

	var successes []successEntry
	var failures []failEntry

	for _, entry := range req.Entries {
		msg, err := h.svc.SendMessage(name, entry.MessageBody, entry.DelaySeconds, nil, entry.MessageGroupId, entry.MessageDeduplicationId)
		if err != nil {
			failures = append(failures, failEntry{Id: entry.Id, Code: "InvalidParameterValue", Message: err.Error(), SenderFault: true})
			continue
		}
		successes = append(successes, successEntry{Id: entry.Id, MessageId: msg.MessageId, MD5OfMessageBody: msg.MD5OfBody})
	}

	writeJSON(w, 200, map[string]any{
		"Successful": successes,
		"Failed":     failures,
	})
}

func (h *Handler) jsonReceiveMessage(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl            string `json:"QueueUrl"`
		MaxNumberOfMessages int    `json:"MaxNumberOfMessages"`
		VisibilityTimeout   int    `json:"VisibilityTimeout"`
		WaitTimeSeconds     int    `json:"WaitTimeSeconds"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	maxCount := req.MaxNumberOfMessages
	if maxCount == 0 {
		maxCount = 1
	}
	visTimeout := req.VisibilityTimeout
	if visTimeout == 0 {
		visTimeout = -1 // use queue default
	}

	msgs, err := h.svc.ReceiveMessage(name, maxCount, visTimeout, req.WaitTimeSeconds)
	if err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	type jsonMessage struct {
		MessageId     string            `json:"MessageId"`
		ReceiptHandle string            `json:"ReceiptHandle"`
		MD5OfBody     string            `json:"MD5OfBody"`
		Body          string            `json:"Body"`
		Attributes    map[string]string `json:"Attributes"`
	}

	out := make([]jsonMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, jsonMessage{
			MessageId:     m.MessageId,
			ReceiptHandle: m.ReceiptHandle,
			MD5OfBody:     m.MD5OfBody,
			Body:          m.Body,
			Attributes: map[string]string{
				"SenderId":                         "000000000000",
				"SentTimestamp":                    strconv.FormatInt(m.SentTimestamp, 10),
				"ApproximateReceiveCount":          strconv.Itoa(m.ApproximateReceiveCount),
				"ApproximateFirstReceiveTimestamp": strconv.FormatInt(m.ApproximateFirstReceiveTimestamp, 10),
			},
		})
	}
	writeJSON(w, 200, map[string]any{"Messages": out})
}

func (h *Handler) jsonDeleteMessage(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl      string `json:"QueueUrl"`
		ReceiptHandle string `json:"ReceiptHandle"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	if req.ReceiptHandle == "" {
		writeJSONError(w, 400, "MissingParameter", "ReceiptHandle is required")
		return
	}
	if err := h.svc.DeleteMessage(name, req.ReceiptHandle); err != nil {
		writeJSONError(w, 400, "ReceiptHandleIsInvalid", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{})
}

type jsonDeleteBatchEntry struct {
	Id            string `json:"Id"`
	ReceiptHandle string `json:"ReceiptHandle"`
}

func (h *Handler) jsonDeleteMessageBatch(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl string                 `json:"QueueUrl"`
		Entries  []jsonDeleteBatchEntry `json:"Entries"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	type successEntry struct {
		Id string `json:"Id"`
	}
	type failEntry struct {
		Id          string `json:"Id"`
		Code        string `json:"Code"`
		Message     string `json:"Message"`
		SenderFault bool   `json:"SenderFault"`
	}

	var successes []successEntry
	var failures []failEntry

	for _, entry := range req.Entries {
		if err := h.svc.DeleteMessage(name, entry.ReceiptHandle); err != nil {
			failures = append(failures, failEntry{Id: entry.Id, Code: "ReceiptHandleIsInvalid", Message: err.Error(), SenderFault: true})
			continue
		}
		successes = append(successes, successEntry{Id: entry.Id})
	}

	writeJSON(w, 200, map[string]any{
		"Successful": successes,
		"Failed":     failures,
	})
}

func (h *Handler) jsonChangeMessageVisibility(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl          string `json:"QueueUrl"`
		ReceiptHandle     string `json:"ReceiptHandle"`
		VisibilityTimeout int    `json:"VisibilityTimeout"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	if err := h.svc.ChangeMessageVisibility(name, req.ReceiptHandle, req.VisibilityTimeout); err != nil {
		writeJSONError(w, 400, "ReceiptHandleIsInvalid", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{})
}

func (h *Handler) jsonPurgeQueue(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl string `json:"QueueUrl"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	if err := h.svc.PurgeQueue(name); err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{})
}

func (h *Handler) jsonTagQueue(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl string            `json:"QueueUrl"`
		Tags     map[string]string `json:"Tags"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	if err := h.svc.TagQueue(name, req.Tags); err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{})
}

func (h *Handler) jsonUntagQueue(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl string   `json:"QueueUrl"`
		TagKeys  []string `json:"TagKeys"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	if err := h.svc.UntagQueue(name, req.TagKeys); err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{})
}

func (h *Handler) jsonListQueueTags(w http.ResponseWriter, body []byte) {
	var req struct {
		QueueUrl string `json:"QueueUrl"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, 400, "InvalidRequest", "invalid JSON")
		return
	}
	name := sqssvc.QueueNameFromUrl(req.QueueUrl)
	if name == "" {
		writeJSONError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}
	tags, err := h.svc.ListQueueTags(name)
	if err != nil {
		writeJSONError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"Tags": tags})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"__type":  code,
		"message": message,
	})
}
