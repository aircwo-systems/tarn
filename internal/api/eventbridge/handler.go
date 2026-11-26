package eventbridge

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	eventbridgesvc "github.com/aircwo-systems/tarn/internal/eventbridge"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const servicePrefix = "AWSEvents."

// Handler dispatches EventBridge JSON protocol requests.
type Handler struct {
	svc *eventbridgesvc.Service
}

func NewHandler(svc *eventbridgesvc.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	if !strings.HasPrefix(target, servicePrefix) {
		writeError(w, http.StatusBadRequest, "ValidationException", "Invalid X-Amz-Target for EventBridge")
		return
	}
	action := strings.TrimPrefix(target, servicePrefix)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", "Failed to read request body")
		return
	}

	switch action {
	case "PutRule":
		h.putRule(w, body)
	case "DescribeRule":
		h.describeRule(w, body)
	case "ListRules":
		h.listRules(w, body)
	case "DeleteRule":
		h.deleteRule(w, body)
	case "EnableRule":
		h.enableRule(w, body)
	case "DisableRule":
		h.disableRule(w, body)
	case "PutTargets":
		h.putTargets(w, body)
	case "ListTargetsByRule":
		h.listTargetsByRule(w, body)
	case "RemoveTargets":
		h.removeTargets(w, body)
	case "ListRuleNamesByTarget":
		h.listRuleNamesByTarget(w, body)
	case "ListTagsForResource":
		h.listTagsForResource(w, body)
	case "TagResource":
		h.tagResource(w, body)
	case "UntagResource":
		h.untagResource(w, body)
	case "PutEvents":
		h.putEvents(w, body)
	case "CreateEventBus":
		h.createEventBus(w, body)
	case "DescribeEventBus":
		h.describeEventBus(w, body)
	case "DeleteEventBus":
		h.deleteEventBus(w, body)
	case "ListEventBuses":
		h.listEventBuses(w, body)
	default:
		log.Printf("[eventbridge] unhandled action (returning empty OK): %s", action)
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}

func (h *Handler) putRule(w http.ResponseWriter, body []byte) {
	var req struct {
		Name               string `json:"Name"`
		ScheduleExpression string `json:"ScheduleExpression"`
		EventPattern       string `json:"EventPattern"`
		State              string `json:"State"`
		Description        string `json:"Description"`
		EventBusName       string `json:"EventBusName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	rule, err := h.svc.PutRule(req.Name, req.ScheduleExpression, req.EventPattern, req.State, req.Description, req.EventBusName)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"RuleArn": rule.Arn})
}

func (h *Handler) describeRule(w http.ResponseWriter, body []byte) {
	var req struct {
		Name         string `json:"Name"`
		EventBusName string `json:"EventBusName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	rule, err := h.svc.DescribeRule(req.Name, req.EventBusName)
	if err != nil {
		writeSvcError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toRuleShape(rule))
}

func (h *Handler) listRules(w http.ResponseWriter, body []byte) {
	var req struct {
		NamePrefix   string `json:"NamePrefix"`
		EventBusName string `json:"EventBusName"`
		Limit        int    `json:"Limit"`
		NextToken    string `json:"NextToken"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	rules, next, err := h.svc.ListRules(req.NamePrefix, req.EventBusName, req.Limit, req.NextToken)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, toRuleShape(rule))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"Rules":     out,
		"NextToken": next,
	})
}

func (h *Handler) deleteRule(w http.ResponseWriter, body []byte) {
	var req struct {
		Name         string `json:"Name"`
		EventBusName string `json:"EventBusName"`
		Force        bool   `json:"Force"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	if err := h.svc.DeleteRule(req.Name, req.EventBusName, req.Force); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) enableRule(w http.ResponseWriter, body []byte) {
	var req struct {
		Name         string `json:"Name"`
		EventBusName string `json:"EventBusName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	if err := h.svc.EnableRule(req.Name, req.EventBusName); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) disableRule(w http.ResponseWriter, body []byte) {
	var req struct {
		Name         string `json:"Name"`
		EventBusName string `json:"EventBusName"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	if err := h.svc.DisableRule(req.Name, req.EventBusName); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) putTargets(w http.ResponseWriter, body []byte) {
	var req struct {
		Rule         string                    `json:"Rule"`
		EventBusName string                    `json:"EventBusName"`
		Targets      []types.EventBridgeTarget `json:"Targets"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	failed, err := h.svc.PutTargets(req.Rule, req.EventBusName, req.Targets)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"FailedEntryCount": len(failed),
		"FailedEntries":    failed,
	})
}

func (h *Handler) listTargetsByRule(w http.ResponseWriter, body []byte) {
	var req struct {
		Rule         string `json:"Rule"`
		EventBusName string `json:"EventBusName"`
		Limit        int    `json:"Limit"`
		NextToken    string `json:"NextToken"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	targets, next, err := h.svc.ListTargetsByRule(req.Rule, req.EventBusName, req.Limit, req.NextToken)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"Targets":   targets,
		"NextToken": next,
	})
}

func (h *Handler) removeTargets(w http.ResponseWriter, body []byte) {
	var req struct {
		Rule         string   `json:"Rule"`
		EventBusName string   `json:"EventBusName"`
		IDs          []string `json:"Ids"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	failed, err := h.svc.RemoveTargets(req.Rule, req.EventBusName, req.IDs)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"FailedEntryCount": len(failed),
		"FailedEntries":    failed,
	})
}

func (h *Handler) listRuleNamesByTarget(w http.ResponseWriter, body []byte) {
	var req struct {
		TargetArn    string `json:"TargetArn"`
		EventBusName string `json:"EventBusName"`
		Limit        int    `json:"Limit"`
		NextToken    string `json:"NextToken"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	names, next, err := h.svc.ListRuleNamesByTarget(req.TargetArn, req.EventBusName, req.Limit, req.NextToken)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"RuleNames": names,
		"NextToken": next,
	})
}

func (h *Handler) listTagsForResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceARN string `json:"ResourceARN"`
		ResourceArn string `json:"ResourceArn"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	resourceARN := strings.TrimSpace(req.ResourceARN)
	if resourceARN == "" {
		resourceARN = strings.TrimSpace(req.ResourceArn)
	}

	tags, err := h.svc.ListTagsForResource(resourceARN)
	if err != nil {
		writeSvcError(w, err)
		return
	}

	members := make([]map[string]string, 0, len(tags))
	for key, value := range tags {
		members = append(members, map[string]string{"Key": key, "Value": value})
	}
	writeJSON(w, http.StatusOK, map[string]any{"Tags": members})
}

func (h *Handler) tagResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceARN string `json:"ResourceARN"`
		ResourceArn string `json:"ResourceArn"`
		Tags        []struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
		} `json:"Tags"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	resourceARN := strings.TrimSpace(req.ResourceARN)
	if resourceARN == "" {
		resourceARN = strings.TrimSpace(req.ResourceArn)
	}

	tags := make(map[string]string, len(req.Tags))
	for _, member := range req.Tags {
		key := strings.TrimSpace(member.Key)
		if key == "" {
			continue
		}
		tags[key] = member.Value
	}
	if err := h.svc.TagResource(resourceARN, tags); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) untagResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceARN string   `json:"ResourceARN"`
		ResourceArn string   `json:"ResourceArn"`
		TagKeys     []string `json:"TagKeys"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	resourceARN := strings.TrimSpace(req.ResourceARN)
	if resourceARN == "" {
		resourceARN = strings.TrimSpace(req.ResourceArn)
	}

	if err := h.svc.UntagResource(resourceARN, req.TagKeys); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) putEvents(w http.ResponseWriter, body []byte) {
	var req struct {
		Entries []types.PutEventsEntry `json:"Entries"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}

	results, failedCount, err := h.svc.PutEvents(req.Entries)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"FailedEntryCount": failedCount,
		"Entries":          results,
	})
}

func toRuleShape(rule *types.EventBridgeRule) map[string]any {
	if rule == nil {
		return map[string]any{}
	}
	shape := map[string]any{
		"Name":         rule.Name,
		"Arn":          rule.Arn,
		"State":        rule.State,
		"EventBusName": ruleEventBusName(rule),
	}
	if strings.TrimSpace(rule.ScheduleExpression) != "" {
		shape["ScheduleExpression"] = rule.ScheduleExpression
	}
	if strings.TrimSpace(rule.EventPattern) != "" {
		shape["EventPattern"] = rule.EventPattern
	}
	if strings.TrimSpace(rule.Description) != "" {
		shape["Description"] = rule.Description
	}
	if rule.LastRunAt != nil {
		shape["LastRunAt"] = rule.LastRunAt.Format(timeRFC3339Milli)
	}
	if rule.NextRunAt != nil {
		shape["NextRunAt"] = rule.NextRunAt.Format(timeRFC3339Milli)
	}
	if strings.TrimSpace(rule.LastResult) != "" {
		shape["LastResult"] = rule.LastResult
	}
	return shape
}

const timeRFC3339Milli = "2006-01-02T15:04:05.000Z"

func ruleEventBusName(rule *types.EventBridgeRule) string {
	if rule == nil || strings.TrimSpace(rule.EventBusName) == "" {
		return "default"
	}
	return rule.EventBusName
}

func (h *Handler) createEventBus(w http.ResponseWriter, body []byte) {
	var req struct {
		Name        string `json:"Name"`
		Description string `json:"Description"`
		Tags        []struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
		} `json:"Tags"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	tags := make(map[string]string, len(req.Tags))
	for _, tag := range req.Tags {
		if key := strings.TrimSpace(tag.Key); key != "" {
			tags[key] = tag.Value
		}
	}
	bus, err := h.svc.CreateEventBus(req.Name, req.Description, tags)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	out := map[string]any{"EventBusArn": bus.Arn}
	if bus.Description != "" {
		out["Description"] = bus.Description
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) describeEventBus(w http.ResponseWriter, body []byte) {
	var req struct {
		Name string `json:"Name"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	bus, err := h.svc.DescribeEventBus(req.Name)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEventBusShape(bus))
}

func (h *Handler) deleteEventBus(w http.ResponseWriter, body []byte) {
	var req struct {
		Name string `json:"Name"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	if err := h.svc.DeleteEventBus(req.Name); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) listEventBuses(w http.ResponseWriter, body []byte) {
	var req struct {
		NamePrefix string `json:"NamePrefix"`
		Limit      int    `json:"Limit"`
		NextToken  string `json:"NextToken"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	buses, next, err := h.svc.ListEventBuses(req.NamePrefix, req.Limit, req.NextToken)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(buses))
	for _, bus := range buses {
		out = append(out, toEventBusShape(bus))
	}
	resp := map[string]any{"EventBuses": out}
	if next != "" {
		resp["NextToken"] = next
	}
	writeJSON(w, http.StatusOK, resp)
}

// toEventBusShape renders a bus in the AWS JSON-protocol shape, where
// timestamps are epoch seconds.
func toEventBusShape(bus *types.EventBridgeEventBus) map[string]any {
	shape := map[string]any{"Name": bus.Name, "Arn": bus.Arn}
	if bus.Description != "" {
		shape["Description"] = bus.Description
	}
	if !bus.CreatedAt.IsZero() {
		shape["CreationTime"] = float64(bus.CreatedAt.UnixMilli()) / 1000
	}
	if !bus.LastModifiedAt.IsZero() {
		shape["LastModifiedTime"] = float64(bus.LastModifiedAt.UnixMilli()) / 1000
	}
	return shape
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

func writeSvcError(w http.ResponseWriter, err error) {
	if se, ok := err.(*eventbridgesvc.ServiceError); ok {
		writeError(w, se.StatusCode(), se.Code, se.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "InternalException", err.Error())
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"__type":  code,
		"message": message,
	})
}
