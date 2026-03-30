package sns

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	snssvc "github.com/aircwo-systems/tarn/internal/sns"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

const xmlNS = "http://sns.amazonaws.com/doc/2010-03-31/"

var supportedActions = map[string]struct{}{
	"CreateTopic":               {},
	"DeleteTopic":               {},
	"ListTopics":                {},
	"GetTopicAttributes":        {},
	"SetTopicAttributes":        {},
	"Subscribe":                 {},
	"Unsubscribe":               {},
	"ListSubscriptions":         {},
	"ListSubscriptionsByTopic":  {},
	"GetSubscriptionAttributes": {},
	"SetSubscriptionAttributes": {},
	"Publish":                   {},
	"PublishBatch":              {},
	"TagResource":               {},
	"UntagResource":             {},
	"ListTagsForResource":       {},
}

// IsSNSAction reports whether an action should be routed to SNS query API.
func IsSNSAction(action string) bool {
	_, ok := supportedActions[strings.TrimSpace(action)]
	return ok
}

// IsSNSRequest reports whether the request is an SNS query-protocol call.
func IsSNSRequest(r *http.Request) bool {
	return r.FormValue("Version") == "2010-03-31"
}

// Handler implements HTTP handlers for SNS query API.
type Handler struct {
	svc *snssvc.Service
}

// NewHandler creates a new SNS API handler.
func NewHandler(svc *snssvc.Service) *Handler {
	return &Handler{svc: svc}
}

// Dispatch routes SNS requests by Action parameter.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}
	action := strings.TrimSpace(r.FormValue("Action"))
	if action == "" {
		writeError(w, http.StatusBadRequest, "InvalidAction", "No Action parameter provided")
		return
	}

	switch action {
	case "CreateTopic":
		h.createTopic(w, r)
	case "DeleteTopic":
		h.deleteTopic(w, r)
	case "ListTopics":
		h.listTopics(w)
	case "GetTopicAttributes":
		h.getTopicAttributes(w, r)
	case "SetTopicAttributes":
		h.setTopicAttributes(w, r)
	case "Subscribe":
		h.subscribe(w, r)
	case "Unsubscribe":
		h.unsubscribe(w, r)
	case "ListSubscriptions":
		h.listSubscriptions(w)
	case "ListSubscriptionsByTopic":
		h.listSubscriptionsByTopic(w, r)
	case "GetSubscriptionAttributes":
		h.getSubscriptionAttributes(w, r)
	case "SetSubscriptionAttributes":
		h.setSubscriptionAttributes(w, r)
	case "Publish":
		h.publish(w, r)
	case "PublishBatch":
		h.publishBatch(w, r)
	case "TagResource":
		h.tagResource(w, r)
	case "UntagResource":
		h.untagResource(w, r)
	case "ListTagsForResource":
		h.listTagsForResource(w, r)
	default:
		log.Printf("[sns] unhandled action (returning empty OK): %s", action)
		emptyOK(w, action)
	}
}

func emptyOK(w http.ResponseWriter, action string) {
	body := fmt.Sprintf(`<%sResponse xmlns="%s"><%sResult/><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></%sResponse>`,
		action, xmlNS, action, requestID(), action)
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) createTopic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("Name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "Name is required")
		return
	}

	topic, err := h.svc.CreateTopic(name, parseAttributes(r), parseTags(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<CreateTopicResponse xmlns="%s"><CreateTopicResult><TopicArn>%s</TopicArn></CreateTopicResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></CreateTopicResponse>`, xmlNS, xmlEscape(topic.TopicArn), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) deleteTopic(w http.ResponseWriter, r *http.Request) {
	topicArn := strings.TrimSpace(r.FormValue("TopicArn"))
	if topicArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "TopicArn is required")
		return
	}
	if err := h.svc.DeleteTopic(topicArn); err != nil {
		if isNotFoundErr(err) {
			writeError(w, http.StatusNotFound, "NotFound", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<DeleteTopicResponse xmlns="%s"><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></DeleteTopicResponse>`, xmlNS, requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) listTopics(w http.ResponseWriter) {
	topics := h.svc.ListTopics()
	var members strings.Builder
	for i, topic := range topics {
		_, _ = fmt.Fprintf(&members, `<member><TopicArn>%s</TopicArn></member>`, xmlEscape(topic.TopicArn))
		if i+1 < len(topics) {
			members.WriteString("\n")
		}
	}

	body := fmt.Sprintf(`<ListTopicsResponse xmlns="%s"><ListTopicsResult><Topics>%s</Topics></ListTopicsResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></ListTopicsResponse>`, xmlNS, members.String(), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) getTopicAttributes(w http.ResponseWriter, r *http.Request) {
	topicArn := strings.TrimSpace(r.FormValue("TopicArn"))
	if topicArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "TopicArn is required")
		return
	}
	attrs, err := h.svc.GetTopicAttributes(topicArn)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFound", err.Error())
		return
	}

	body := fmt.Sprintf(`<GetTopicAttributesResponse xmlns="%s"><GetTopicAttributesResult><Attributes>%s</Attributes></GetTopicAttributesResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></GetTopicAttributesResponse>`, xmlNS, xmlAttributes(attrs), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) setTopicAttributes(w http.ResponseWriter, r *http.Request) {
	topicArn := strings.TrimSpace(r.FormValue("TopicArn"))
	name := strings.TrimSpace(r.FormValue("AttributeName"))
	value := r.FormValue("AttributeValue")
	if topicArn == "" || name == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "TopicArn and AttributeName are required")
		return
	}
	if err := h.svc.SetTopicAttribute(topicArn, name, value); err != nil {
		if isNotFoundErr(err) {
			writeError(w, http.StatusNotFound, "NotFound", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<SetTopicAttributesResponse xmlns="%s"><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></SetTopicAttributesResponse>`, xmlNS, requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	topicArn := strings.TrimSpace(r.FormValue("TopicArn"))
	protocol := strings.TrimSpace(r.FormValue("Protocol"))
	endpoint := strings.TrimSpace(r.FormValue("Endpoint"))
	if topicArn == "" || protocol == "" || endpoint == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "TopicArn, Protocol and Endpoint are required")
		return
	}

	sub, err := h.svc.Subscribe(topicArn, protocol, endpoint, parseAttributes(r))
	if err != nil {
		if isNotFoundErr(err) {
			writeError(w, http.StatusNotFound, "NotFound", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<SubscribeResponse xmlns="%s"><SubscribeResult><SubscriptionArn>%s</SubscriptionArn></SubscribeResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></SubscribeResponse>`, xmlNS, xmlEscape(sub.SubscriptionArn), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	subArn := strings.TrimSpace(r.FormValue("SubscriptionArn"))
	if subArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "SubscriptionArn is required")
		return
	}
	if err := h.svc.Unsubscribe(subArn); err != nil {
		if isNotFoundErr(err) {
			writeError(w, http.StatusNotFound, "NotFound", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<UnsubscribeResponse xmlns="%s"><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></UnsubscribeResponse>`, xmlNS, requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) listSubscriptions(w http.ResponseWriter) {
	subs := h.svc.ListSubscriptions()
	body := fmt.Sprintf(`<ListSubscriptionsResponse xmlns="%s"><ListSubscriptionsResult><Subscriptions>%s</Subscriptions></ListSubscriptionsResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></ListSubscriptionsResponse>`, xmlNS, xmlSubscriptions(subs), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) listSubscriptionsByTopic(w http.ResponseWriter, r *http.Request) {
	topicArn := strings.TrimSpace(r.FormValue("TopicArn"))
	if topicArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "TopicArn is required")
		return
	}
	subs, err := h.svc.ListSubscriptionsByTopic(topicArn)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFound", err.Error())
		return
	}

	body := fmt.Sprintf(`<ListSubscriptionsByTopicResponse xmlns="%s"><ListSubscriptionsByTopicResult><Subscriptions>%s</Subscriptions></ListSubscriptionsByTopicResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></ListSubscriptionsByTopicResponse>`, xmlNS, xmlSubscriptions(subs), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) getSubscriptionAttributes(w http.ResponseWriter, r *http.Request) {
	subArn := strings.TrimSpace(r.FormValue("SubscriptionArn"))
	if subArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "SubscriptionArn is required")
		return
	}
	attrs, err := h.svc.GetSubscriptionAttributes(subArn)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFound", err.Error())
		return
	}

	body := fmt.Sprintf(`<GetSubscriptionAttributesResponse xmlns="%s"><GetSubscriptionAttributesResult><Attributes>%s</Attributes></GetSubscriptionAttributesResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></GetSubscriptionAttributesResponse>`, xmlNS, xmlAttributes(attrs), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) setSubscriptionAttributes(w http.ResponseWriter, r *http.Request) {
	subArn := strings.TrimSpace(r.FormValue("SubscriptionArn"))
	name := strings.TrimSpace(r.FormValue("AttributeName"))
	value := r.FormValue("AttributeValue")
	if subArn == "" || name == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "SubscriptionArn and AttributeName are required")
		return
	}
	if err := h.svc.SetSubscriptionAttribute(subArn, name, value); err != nil {
		if isNotFoundErr(err) {
			writeError(w, http.StatusNotFound, "NotFound", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<SetSubscriptionAttributesResponse xmlns="%s"><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></SetSubscriptionAttributesResponse>`, xmlNS, requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	msg := r.FormValue("Message")
	if strings.TrimSpace(msg) == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "Message is required")
		return
	}

	messageStructure := strings.TrimSpace(r.FormValue("MessageStructure"))
	if strings.EqualFold(messageStructure, "json") {
		var envelope map[string]string
		if err := json.Unmarshal([]byte(msg), &envelope); err != nil {
			writeError(w, http.StatusBadRequest, "InvalidParameter", "Message must be valid JSON when MessageStructure=json")
			return
		}
		if def, ok := envelope["default"]; ok {
			msg = def
		}
	}

	out, err := h.svc.Publish(r.Context(), snssvc.PublishInput{
		TopicArn:          strings.TrimSpace(r.FormValue("TopicArn")),
		TargetArn:         strings.TrimSpace(r.FormValue("TargetArn")),
		Message:           msg,
		Subject:           r.FormValue("Subject"),
		MessageStructure:  messageStructure,
		MessageAttributes: parseMessageAttributes(r),
	})
	if err != nil {
		if isNotFoundErr(err) {
			writeError(w, http.StatusNotFound, "NotFound", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<PublishResponse xmlns="%s"><PublishResult><MessageId>%s</MessageId></PublishResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></PublishResponse>`, xmlNS, xmlEscape(out.MessageID), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) publishBatch(w http.ResponseWriter, r *http.Request) {
	topicArn := strings.TrimSpace(r.FormValue("TopicArn"))
	if topicArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "TopicArn is required")
		return
	}

	type batchEntry struct {
		id      string
		message string
	}
	var entries []batchEntry
	for i := 1; i <= 100; i++ {
		id := strings.TrimSpace(r.FormValue(fmt.Sprintf("PublishBatchRequestEntries.member.%d.Id", i)))
		if id == "" {
			break
		}
		msg := r.FormValue(fmt.Sprintf("PublishBatchRequestEntries.member.%d.Message", i))
		entries = append(entries, batchEntry{id: id, message: msg})
	}

	if len(entries) == 0 {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "At least one entry is required")
		return
	}

	var successful strings.Builder
	var failed strings.Builder
	for _, entry := range entries {
		out, err := h.svc.Publish(r.Context(), snssvc.PublishInput{
			TopicArn: topicArn,
			Message:  entry.message,
		})
		if err != nil {
			fmt.Fprintf(&failed, `<member><Id>%s</Id><Code>InternalError</Code><Message>%s</Message><SenderFault>false</SenderFault></member>`,
				xmlEscape(entry.id), xmlEscape(err.Error()))
			continue
		}
		fmt.Fprintf(&successful, `<member><Id>%s</Id><MessageId>%s</MessageId></member>`,
			xmlEscape(entry.id), xmlEscape(out.MessageID))
	}

	body := fmt.Sprintf(`<PublishBatchResponse xmlns="%s"><PublishBatchResult><Successful>%s</Successful><Failed>%s</Failed></PublishBatchResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></PublishBatchResponse>`,
		xmlNS, successful.String(), failed.String(), requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) tagResource(w http.ResponseWriter, r *http.Request) {
	resourceArn := strings.TrimSpace(r.FormValue("ResourceArn"))
	if resourceArn == "" {
		resourceArn = strings.TrimSpace(r.FormValue("TopicArn"))
	}
	if resourceArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "ResourceArn is required")
		return
	}

	if err := h.svc.TagTopic(resourceArn, parseTags(r)); err != nil {
		if isNotFoundErr(err) {
			writeError(w, http.StatusNotFound, "NotFound", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<TagResourceResponse xmlns="%s"><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></TagResourceResponse>`, xmlNS, requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) untagResource(w http.ResponseWriter, r *http.Request) {
	resourceArn := strings.TrimSpace(r.FormValue("ResourceArn"))
	if resourceArn == "" {
		resourceArn = strings.TrimSpace(r.FormValue("TopicArn"))
	}
	if resourceArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "ResourceArn is required")
		return
	}

	if err := h.svc.UntagTopic(resourceArn, parseTagKeys(r)); err != nil {
		if isNotFoundErr(err) {
			writeError(w, http.StatusNotFound, "NotFound", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidParameter", err.Error())
		return
	}

	body := fmt.Sprintf(`<UntagResourceResponse xmlns="%s"><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></UntagResourceResponse>`, xmlNS, requestID())
	writeXML(w, http.StatusOK, body)
}

func (h *Handler) listTagsForResource(w http.ResponseWriter, r *http.Request) {
	resourceArn := strings.TrimSpace(r.FormValue("ResourceArn"))
	if resourceArn == "" {
		resourceArn = strings.TrimSpace(r.FormValue("TopicArn"))
	}
	if resourceArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameter", "ResourceArn is required")
		return
	}

	tags, err := h.svc.ListTopicTags(resourceArn)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFound", err.Error())
		return
	}

	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var members strings.Builder
	for i, key := range keys {
		_, _ = fmt.Fprintf(&members, `<member><Key>%s</Key><Value>%s</Value></member>`, xmlEscape(key), xmlEscape(tags[key]))
		if i+1 < len(keys) {
			members.WriteString("\n")
		}
	}

	body := fmt.Sprintf(`<ListTagsForResourceResponse xmlns="%s"><ListTagsForResourceResult><Tags>%s</Tags></ListTagsForResourceResult><ResponseMetadata><RequestId>%s</RequestId></ResponseMetadata></ListTagsForResourceResponse>`, xmlNS, members.String(), requestID())
	writeXML(w, http.StatusOK, body)
}

func parseAttributes(r *http.Request) map[string]string {
	out := make(map[string]string)
	for i := 1; i <= 100; i++ {
		key := strings.TrimSpace(r.FormValue(fmt.Sprintf("Attributes.entry.%d.key", i)))
		if key == "" {
			key = strings.TrimSpace(r.FormValue(fmt.Sprintf("Attributes.entry.%d.Key", i)))
		}
		if key == "" {
			continue
		}
		value := r.FormValue(fmt.Sprintf("Attributes.entry.%d.value", i))
		if value == "" {
			value = r.FormValue(fmt.Sprintf("Attributes.entry.%d.Value", i))
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseTags(r *http.Request) map[string]string {
	out := make(map[string]string)
	for i := 1; i <= 100; i++ {
		key := strings.TrimSpace(r.FormValue(fmt.Sprintf("Tags.member.%d.Key", i)))
		if key == "" {
			key = strings.TrimSpace(r.FormValue(fmt.Sprintf("Tag.%d.Key", i)))
		}
		if key == "" {
			continue
		}
		value := r.FormValue(fmt.Sprintf("Tags.member.%d.Value", i))
		if value == "" {
			value = r.FormValue(fmt.Sprintf("Tag.%d.Value", i))
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseTagKeys(r *http.Request) []string {
	out := make([]string, 0)
	for i := 1; i <= 100; i++ {
		key := strings.TrimSpace(r.FormValue(fmt.Sprintf("TagKeys.member.%d", i)))
		if key == "" {
			key = strings.TrimSpace(r.FormValue(fmt.Sprintf("TagKey.%d", i)))
		}
		if key == "" {
			continue
		}
		out = append(out, key)
	}
	return out
}

func parseMessageAttributes(r *http.Request) map[string]types.SNSMessageAttribute {
	out := make(map[string]types.SNSMessageAttribute)
	for i := 1; i <= 100; i++ {
		name := strings.TrimSpace(r.FormValue(fmt.Sprintf("MessageAttributes.entry.%d.Name", i)))
		if name == "" {
			continue
		}
		attr := types.SNSMessageAttribute{
			DataType:    strings.TrimSpace(r.FormValue(fmt.Sprintf("MessageAttributes.entry.%d.Value.DataType", i))),
			StringValue: r.FormValue(fmt.Sprintf("MessageAttributes.entry.%d.Value.StringValue", i)),
			BinaryValue: r.FormValue(fmt.Sprintf("MessageAttributes.entry.%d.Value.BinaryValue", i)),
		}
		if attr.DataType == "" {
			attr.DataType = "String"
		}
		out[name] = attr
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func xmlSubscriptions(subs []*types.SNSSubscription) string {
	var b strings.Builder
	for i, sub := range subs {
		b.WriteString(`<member>`)
		b.WriteString(`<SubscriptionArn>` + xmlEscape(sub.SubscriptionArn) + `</SubscriptionArn>`)
		b.WriteString(`<TopicArn>` + xmlEscape(sub.TopicArn) + `</TopicArn>`)
		b.WriteString(`<Protocol>` + xmlEscape(sub.Protocol) + `</Protocol>`)
		b.WriteString(`<Endpoint>` + xmlEscape(sub.Endpoint) + `</Endpoint>`)
		b.WriteString(`<Owner>` + xmlEscape(sub.Owner) + `</Owner>`)
		b.WriteString(`</member>`)
		if i+1 < len(subs) {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func xmlAttributes(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, key := range keys {
		b.WriteString(`<entry><key>` + xmlEscape(key) + `</key><value>` + xmlEscape(attrs[key]) + `</value></entry>`)
		if i+1 < len(keys) {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func requestID() string { return uuid.NewString() }

func writeXML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, `<?xml version="1.0"?>`)
	_, _ = fmt.Fprint(w, body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	if status < 400 {
		status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><ErrorResponse xmlns="%s"><Error><Type>Sender</Type><Code>%s</Code><Message>%s</Message></Error><RequestId>%s</RequestId></ErrorResponse>`, xmlNS, xmlEscape(code), xmlEscape(message), requestID())
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
