package sqs

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	sqssvc "github.com/openstack-project/openstack/internal/sqs"
	"github.com/openstack-project/openstack/pkg/types"
)

const xmlNS = "http://queue.amazonaws.com/doc/2012-11-05/"

// Handler implements HTTP handlers for the SQS query API.
type Handler struct {
	svc *sqssvc.Service
}

// NewHandler creates a new SQS API handler.
func NewHandler(svc *sqssvc.Service) *Handler {
	return &Handler{svc: svc}
}

// Dispatch routes SQS requests by protocol. Terraform AWS provider v5+ and
// AWS SDK v2 use the JSON wire protocol (X-Amz-Target header); older clients
// use the query/XML protocol (Action form parameter).
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	if isJSONProtocol(r) {
		h.dispatchJSON(w, r)
		return
	}

	r.ParseForm()
	action := r.FormValue("Action")
	if action == "" {
		writeXMLError(w, 400, "MissingAction", "No Action parameter provided")
		return
	}

	switch action {
	case "CreateQueue":
		h.createQueue(w, r)
	case "DeleteQueue":
		h.deleteQueue(w, r)
	case "ListQueues":
		h.listQueues(w, r)
	case "GetQueueUrl":
		h.getQueueUrl(w, r)
	case "GetQueueAttributes":
		h.getQueueAttributes(w, r)
	case "SetQueueAttributes":
		h.setQueueAttributes(w, r)
	case "SendMessage":
		h.sendMessage(w, r)
	case "SendMessageBatch":
		h.sendMessageBatch(w, r)
	case "ReceiveMessage":
		h.receiveMessage(w, r)
	case "DeleteMessage":
		h.deleteMessage(w, r)
	case "DeleteMessageBatch":
		h.deleteMessageBatch(w, r)
	case "ChangeMessageVisibility":
		h.changeMessageVisibility(w, r)
	case "PurgeQueue":
		h.purgeQueue(w, r)
	case "TagQueue":
		h.tagQueue(w, r)
	case "UntagQueue":
		h.untagQueue(w, r)
	case "ListQueueTags":
		h.listQueueTags(w, r)
	default:
		writeXMLError(w, 400, "InvalidAction", "The action "+action+" is not valid for this endpoint")
	}
}

func (h *Handler) createQueue(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("QueueName")
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueName is required")
		return
	}

	attrs := parseAttributes(r)
	tags := parseTags(r)

	q, err := h.svc.CreateQueue(name, attrs, tags)
	if err != nil {
		writeXMLError(w, 400, "InvalidParameterValue", err.Error())
		return
	}

	body := fmt.Sprintf(`<CreateQueueResponse xmlns="%s">
  <CreateQueueResult>
    <QueueUrl>%s</QueueUrl>
  </CreateQueueResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</CreateQueueResponse>`, xmlNS, q.QueueUrl, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) deleteQueue(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	if err := h.svc.DeleteQueue(name); err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	body := fmt.Sprintf(`<DeleteQueueResponse xmlns="%s">
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</DeleteQueueResponse>`, xmlNS, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) listQueues(w http.ResponseWriter, r *http.Request) {
	prefix := r.FormValue("QueueNamePrefix")
	queues := h.svc.ListQueues(prefix)

	var urls string
	for _, q := range queues {
		urls += fmt.Sprintf("    <QueueUrl>%s</QueueUrl>\n", q.QueueUrl)
	}

	body := fmt.Sprintf(`<ListQueuesResponse xmlns="%s">
  <ListQueuesResult>
%s  </ListQueuesResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</ListQueuesResponse>`, xmlNS, urls, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) getQueueUrl(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("QueueName")
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueName is required")
		return
	}

	url, err := h.svc.GetQueueUrl(name)
	if err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	body := fmt.Sprintf(`<GetQueueUrlResponse xmlns="%s">
  <GetQueueUrlResult>
    <QueueUrl>%s</QueueUrl>
  </GetQueueUrlResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</GetQueueUrlResponse>`, xmlNS, url, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) getQueueAttributes(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	attrNames := parseAttributeNames(r)
	attrs, err := h.svc.GetQueueAttributes(name, attrNames)
	if err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	var attrXML string
	for k, v := range attrs {
		attrXML += fmt.Sprintf("    <Attribute>\n      <Name>%s</Name>\n      <Value>%s</Value>\n    </Attribute>\n", k, v)
	}

	body := fmt.Sprintf(`<GetQueueAttributesResponse xmlns="%s">
  <GetQueueAttributesResult>
%s  </GetQueueAttributesResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</GetQueueAttributesResponse>`, xmlNS, attrXML, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) setQueueAttributes(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	attrs := parseAttributes(r)
	if err := h.svc.SetQueueAttributes(name, attrs); err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	body := fmt.Sprintf(`<SetQueueAttributesResponse xmlns="%s">
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</SetQueueAttributesResponse>`, xmlNS, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	msgBody := r.FormValue("MessageBody")
	if msgBody == "" {
		writeXMLError(w, 400, "MissingParameter", "MessageBody is required")
		return
	}

	delaySec := 0
	if v := r.FormValue("DelaySeconds"); v != "" {
		delaySec, _ = strconv.Atoi(v)
	}

	groupId := r.FormValue("MessageGroupId")
	dedupId := r.FormValue("MessageDeduplicationId")
	msgAttrs := parseMessageAttributes(r)

	msg, err := h.svc.SendMessage(name, msgBody, delaySec, msgAttrs, groupId, dedupId)
	if err != nil {
		writeXMLError(w, 400, "InvalidParameterValue", err.Error())
		return
	}

	body := fmt.Sprintf(`<SendMessageResponse xmlns="%s">
  <SendMessageResult>
    <MD5OfMessageBody>%s</MD5OfMessageBody>
    <MessageId>%s</MessageId>
  </SendMessageResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</SendMessageResponse>`, xmlNS, msg.MD5OfBody, msg.MessageId, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) sendMessageBatch(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	var successXML, failXML string

	for i := 1; i <= 10; i++ {
		prefix := fmt.Sprintf("SendMessageBatchRequestEntry.%d.", i)
		id := r.FormValue(prefix + "Id")
		if id == "" {
			break
		}
		msgBody := r.FormValue(prefix + "MessageBody")
		delaySec := 0
		if v := r.FormValue(prefix + "DelaySeconds"); v != "" {
			delaySec, _ = strconv.Atoi(v)
		}
		groupId := r.FormValue(prefix + "MessageGroupId")
		dedupId := r.FormValue(prefix + "MessageDeduplicationId")

		msg, err := h.svc.SendMessage(name, msgBody, delaySec, nil, groupId, dedupId)
		if err != nil {
			failXML += fmt.Sprintf("    <BatchResultErrorEntry>\n      <Id>%s</Id>\n      <SenderFault>true</SenderFault>\n      <Code>InvalidParameterValue</Code>\n      <Message>%s</Message>\n    </BatchResultErrorEntry>\n", id, err.Error())
			continue
		}
		successXML += fmt.Sprintf("    <SendMessageBatchResultEntry>\n      <Id>%s</Id>\n      <MessageId>%s</MessageId>\n      <MD5OfMessageBody>%s</MD5OfMessageBody>\n    </SendMessageBatchResultEntry>\n", id, msg.MessageId, msg.MD5OfBody)
	}

	body := fmt.Sprintf(`<SendMessageBatchResponse xmlns="%s">
  <SendMessageBatchResult>
%s%s  </SendMessageBatchResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</SendMessageBatchResponse>`, xmlNS, successXML, failXML, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) receiveMessage(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	maxCount := 1
	if v := r.FormValue("MaxNumberOfMessages"); v != "" {
		maxCount, _ = strconv.Atoi(v)
	}
	visTimeout := -1 // -1 signals "use queue default"
	if v := r.FormValue("VisibilityTimeout"); v != "" {
		visTimeout, _ = strconv.Atoi(v)
	}
	waitTime := 0
	if v := r.FormValue("WaitTimeSeconds"); v != "" {
		waitTime, _ = strconv.Atoi(v)
	}

	msgs, err := h.svc.ReceiveMessage(name, maxCount, visTimeout, waitTime)
	if err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	var msgsXML string
	for _, m := range msgs {
		attrsXML := ""
		attrsXML += fmt.Sprintf("      <Attribute><Name>SenderId</Name><Value>000000000000</Value></Attribute>\n")
		attrsXML += fmt.Sprintf("      <Attribute><Name>SentTimestamp</Name><Value>%d</Value></Attribute>\n", m.SentTimestamp)
		attrsXML += fmt.Sprintf("      <Attribute><Name>ApproximateReceiveCount</Name><Value>%d</Value></Attribute>\n", m.ApproximateReceiveCount)
		attrsXML += fmt.Sprintf("      <Attribute><Name>ApproximateFirstReceiveTimestamp</Name><Value>%d</Value></Attribute>\n", m.ApproximateFirstReceiveTimestamp)

		msgsXML += fmt.Sprintf(`    <Message>
      <MessageId>%s</MessageId>
      <ReceiptHandle>%s</ReceiptHandle>
      <MD5OfBody>%s</MD5OfBody>
      <Body>%s</Body>
%s    </Message>
`, m.MessageId, m.ReceiptHandle, m.MD5OfBody, xmlEscape(m.Body), attrsXML)
	}

	body := fmt.Sprintf(`<ReceiveMessageResponse xmlns="%s">
  <ReceiveMessageResult>
%s  </ReceiveMessageResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</ReceiveMessageResponse>`, xmlNS, msgsXML, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	receiptHandle := r.FormValue("ReceiptHandle")
	if receiptHandle == "" {
		writeXMLError(w, 400, "MissingParameter", "ReceiptHandle is required")
		return
	}

	if err := h.svc.DeleteMessage(name, receiptHandle); err != nil {
		writeXMLError(w, 400, "ReceiptHandleIsInvalid", err.Error())
		return
	}

	body := fmt.Sprintf(`<DeleteMessageResponse xmlns="%s">
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</DeleteMessageResponse>`, xmlNS, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) deleteMessageBatch(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	var successXML, failXML string

	for i := 1; i <= 10; i++ {
		prefix := fmt.Sprintf("DeleteMessageBatchRequestEntry.%d.", i)
		id := r.FormValue(prefix + "Id")
		if id == "" {
			break
		}
		handle := r.FormValue(prefix + "ReceiptHandle")

		if err := h.svc.DeleteMessage(name, handle); err != nil {
			failXML += fmt.Sprintf("    <BatchResultErrorEntry>\n      <Id>%s</Id>\n      <SenderFault>true</SenderFault>\n      <Code>ReceiptHandleIsInvalid</Code>\n      <Message>%s</Message>\n    </BatchResultErrorEntry>\n", id, err.Error())
			continue
		}
		successXML += fmt.Sprintf("    <DeleteMessageBatchResultEntry>\n      <Id>%s</Id>\n    </DeleteMessageBatchResultEntry>\n", id)
	}

	body := fmt.Sprintf(`<DeleteMessageBatchResponse xmlns="%s">
  <DeleteMessageBatchResult>
%s%s  </DeleteMessageBatchResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</DeleteMessageBatchResponse>`, xmlNS, successXML, failXML, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) changeMessageVisibility(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	handle := r.FormValue("ReceiptHandle")
	timeout, _ := strconv.Atoi(r.FormValue("VisibilityTimeout"))

	if err := h.svc.ChangeMessageVisibility(name, handle, timeout); err != nil {
		writeXMLError(w, 400, "ReceiptHandleIsInvalid", err.Error())
		return
	}

	body := fmt.Sprintf(`<ChangeMessageVisibilityResponse xmlns="%s">
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</ChangeMessageVisibilityResponse>`, xmlNS, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) purgeQueue(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	if err := h.svc.PurgeQueue(name); err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	body := fmt.Sprintf(`<PurgeQueueResponse xmlns="%s">
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</PurgeQueueResponse>`, xmlNS, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) tagQueue(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	tags := parseTags(r)
	if err := h.svc.TagQueue(name, tags); err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	body := fmt.Sprintf(`<TagQueueResponse xmlns="%s">
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</TagQueueResponse>`, xmlNS, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) untagQueue(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	var tagKeys []string
	for i := 1; i <= 50; i++ {
		key := r.FormValue(fmt.Sprintf("TagKey.%d", i))
		if key == "" {
			break
		}
		tagKeys = append(tagKeys, key)
	}

	if err := h.svc.UntagQueue(name, tagKeys); err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	body := fmt.Sprintf(`<UntagQueueResponse xmlns="%s">
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</UntagQueueResponse>`, xmlNS, uuid.New().String())

	writeXML(w, 200, body)
}

func (h *Handler) listQueueTags(w http.ResponseWriter, r *http.Request) {
	name := resolveQueueName(r)
	if name == "" {
		writeXMLError(w, 400, "MissingParameter", "QueueUrl is required")
		return
	}

	tags, err := h.svc.ListQueueTags(name)
	if err != nil {
		writeXMLError(w, 400, "AWS.SimpleQueueService.NonExistentQueue", err.Error())
		return
	}

	var tagsXML string
	for k, v := range tags {
		tagsXML += fmt.Sprintf("    <entry>\n      <key>%s</key>\n      <value>%s</value>\n    </entry>\n", k, v)
	}

	body := fmt.Sprintf(`<ListQueueTagsResponse xmlns="%s">
  <ListQueueTagsResult>
%s  </ListQueueTagsResult>
  <ResponseMetadata>
    <RequestId>%s</RequestId>
  </ResponseMetadata>
</ListQueueTagsResponse>`, xmlNS, tagsXML, uuid.New().String())

	writeXML(w, 200, body)
}

// --- Helpers ---

// resolveQueueName extracts the queue name from QueueUrl param or URL path.
func resolveQueueName(r *http.Request) string {
	// Try QueueUrl form parameter first
	queueUrl := r.FormValue("QueueUrl")
	if queueUrl != "" {
		return sqssvc.QueueNameFromUrl(queueUrl)
	}

	// Try URL path: /{accountId}/{queueName}
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 2 && parts[1] != "" {
		return parts[1]
	}

	return ""
}

// parseAttributes extracts Attribute.N.Name/Value pairs from form data.
func parseAttributes(r *http.Request) map[string]string {
	attrs := make(map[string]string)
	for i := 1; i <= 30; i++ {
		name := r.FormValue(fmt.Sprintf("Attribute.%d.Name", i))
		if name == "" {
			break
		}
		value := r.FormValue(fmt.Sprintf("Attribute.%d.Value", i))
		attrs[name] = value
	}
	return attrs
}

// parseAttributeNames extracts AttributeName.N values from form data.
func parseAttributeNames(r *http.Request) []string {
	var names []string
	for i := 1; i <= 30; i++ {
		name := r.FormValue(fmt.Sprintf("AttributeName.%d", i))
		if name == "" {
			break
		}
		names = append(names, name)
	}
	return names
}

// parseTags extracts Tag.N.Key/Value pairs from form data.
func parseTags(r *http.Request) map[string]string {
	tags := make(map[string]string)
	for i := 1; i <= 50; i++ {
		key := r.FormValue(fmt.Sprintf("Tag.%d.Key", i))
		if key == "" {
			break
		}
		value := r.FormValue(fmt.Sprintf("Tag.%d.Value", i))
		tags[key] = value
	}
	return tags
}

// parseMessageAttributes extracts MessageAttribute.N.Name/Value pairs from form data.
func parseMessageAttributes(r *http.Request) map[string]*types.MessageAttribute {
	attrs := make(map[string]*types.MessageAttribute)
	for i := 1; i <= 10; i++ {
		name := r.FormValue(fmt.Sprintf("MessageAttribute.%d.Name", i))
		if name == "" {
			break
		}
		dataType := r.FormValue(fmt.Sprintf("MessageAttribute.%d.Value.DataType", i))
		stringValue := r.FormValue(fmt.Sprintf("MessageAttribute.%d.Value.StringValue", i))
		attrs[name] = &types.MessageAttribute{
			DataType:    dataType,
			StringValue: stringValue,
		}
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

func writeXML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	fmt.Fprint(w, `<?xml version="1.0"?>`)
	fmt.Fprint(w, body)
}

func writeXMLError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0"?><ErrorResponse xmlns="%s"><Error><Type>Sender</Type><Code>%s</Code><Message>%s</Message></Error><RequestId>%s</RequestId></ErrorResponse>`,
		xmlNS, code, xmlEscape(message), uuid.New().String())
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
