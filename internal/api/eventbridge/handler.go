package eventbridge

import (
	"encoding/json"
	"io"
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
	default:
		writeError(w, http.StatusBadRequest, "ValidationException", "Unsupported EventBridge action: "+action)
	}
}

func (h *Handler) putRule(w http.ResponseWriter, body []byte) {
	var req struct {
		Name               string `json:"Name"`
		ScheduleExpression string `json:"ScheduleExpression"`
		State              string `json:"State"`
		Description        string `json:"Description"`
		EventBusName       string `json:"EventBusName"`
		EventPattern       string `json:"EventPattern"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	if strings.TrimSpace(req.EventPattern) != "" {
		writeError(w, http.StatusBadRequest, "ValidationException", "Only scheduled rules are supported in this phase")
		return
	}

	rule, err := h.svc.PutRule(req.Name, req.ScheduleExpression, req.State, req.Description, req.EventBusName)
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

func toRuleShape(rule *types.EventBridgeRule) map[string]any {
	if rule == nil {
		return map[string]any{}
	}
	shape := map[string]any{
		"Name":               rule.Name,
		"Arn":                rule.Arn,
		"State":              rule.State,
		"ScheduleExpression": rule.ScheduleExpression,
		"EventBusName":       "default",
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
