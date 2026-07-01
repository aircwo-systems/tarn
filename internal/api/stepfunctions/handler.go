// Package stepfunctions provides the HTTP handler for the AWS Step Functions
// JSON-1.0 protocol (dispatched by the X-Amz-Target header).
package stepfunctions

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	stepfunctionssvc "github.com/aircwo-systems/tarn/internal/stepfunctions"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const servicePrefix = "AWSStepFunctions."

// Handler dispatches Step Functions JSON protocol requests to the service.
type Handler struct {
	svc *stepfunctionssvc.Service
}

// NewHandler creates a Step Functions HTTP handler.
func NewHandler(svc *stepfunctionssvc.Service) *Handler {
	return &Handler{svc: svc}
}

// Dispatch routes a request by its X-Amz-Target action.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	if !strings.HasPrefix(target, servicePrefix) {
		writeError(w, http.StatusBadRequest, "AccessDeniedException", "Invalid X-Amz-Target for Step Functions")
		return
	}
	action := strings.TrimPrefix(target, servicePrefix)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidRequestException", "Failed to read request body")
		return
	}

	switch action {
	case "CreateStateMachine":
		h.createStateMachine(w, body)
	case "DescribeStateMachine":
		h.describeStateMachine(w, body)
	case "UpdateStateMachine":
		h.updateStateMachine(w, body)
	case "DeleteStateMachine":
		h.deleteStateMachine(w, body)
	case "ListStateMachines":
		h.listStateMachines(w)
	case "TagResource":
		h.tagResource(w, body)
	case "UntagResource":
		h.untagResource(w, body)
	case "ListTagsForResource":
		h.listTagsForResource(w, body)
	case "ValidateStateMachineDefinition":
		h.validateStateMachineDefinition(w, body)
	case "StartExecution":
		h.startExecution(w, body)
	case "DescribeExecution":
		h.describeExecution(w, body)
	case "StopExecution":
		h.stopExecution(w, body)
	case "ListExecutions":
		h.listExecutions(w, body)
	case "GetExecutionHistory":
		h.getExecutionHistory(w, body)
	default:
		log.Printf("[stepfunctions] unhandled action (returning empty OK): %s", action)
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}

// ---- State machine actions ----

func (h *Handler) createStateMachine(w http.ResponseWriter, body []byte) {
	var req struct {
		Name       string          `json:"name"`
		Definition string          `json:"definition"`
		RoleArn    string          `json:"roleArn"`
		Type       string          `json:"type"`
		Tags       json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	sm, err := h.svc.CreateStateMachine(req.Name, req.Definition, req.RoleArn, req.Type, parseTagMap(req.Tags))
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stateMachineArn": sm.Arn,
		"creationDate":    epochSeconds(sm.CreatedAt),
	})
}

func (h *Handler) describeStateMachine(w http.ResponseWriter, body []byte) {
	var req struct {
		StateMachineArn string `json:"stateMachineArn"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	sm, err := h.svc.DescribeStateMachine(req.StateMachineArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stateMachineDetail(sm))
}

func (h *Handler) validateStateMachineDefinition(w http.ResponseWriter, body []byte) {
	var req struct {
		Definition string `json:"definition"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	// AWS carries the verdict in a 200 response body (result + diagnostics); it
	// does NOT return an error status for an invalid definition. Returning a bare
	// {} or a 4xx makes the Terraform AWS provider read result != "OK" with no
	// diagnostics and render "invalid Step Functions State Machine definition:
	// %!w(<nil>)".
	if err := h.svc.ValidateStateMachineDefinition(req.Definition); err != nil {
		message := err.Error()
		if se, ok := err.(*stepfunctionssvc.ServiceError); ok {
			message = se.Message
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"result": "FAIL",
			"diagnostics": []map[string]any{{
				"severity": "ERROR",
				"code":     "SCHEMA_VALIDATION_FAILED",
				"message":  message,
			}},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": "OK"})
}

func (h *Handler) updateStateMachine(w http.ResponseWriter, body []byte) {
	var req struct {
		StateMachineArn string `json:"stateMachineArn"`
		Definition      string `json:"definition"`
		RoleArn         string `json:"roleArn"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	if _, err := h.svc.UpdateStateMachine(req.StateMachineArn, req.Definition, req.RoleArn); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updateDate": epochSeconds(time.Now().UTC())})
}

func (h *Handler) deleteStateMachine(w http.ResponseWriter, body []byte) {
	var req struct {
		StateMachineArn string `json:"stateMachineArn"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	if err := h.svc.DeleteStateMachine(req.StateMachineArn); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) listStateMachines(w http.ResponseWriter) {
	machines := h.svc.ListStateMachines()
	out := make([]map[string]any, 0, len(machines))
	for _, sm := range machines {
		out = append(out, stateMachineSummary(sm))
	}
	writeJSON(w, http.StatusOK, map[string]any{"stateMachines": out})
}

// ---- Tag actions ----

func (h *Handler) tagResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceArn string          `json:"resourceArn"`
		Tags        json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	if err := h.svc.TagResource(req.ResourceArn, parseTagMap(req.Tags)); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) untagResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceArn string   `json:"resourceArn"`
		TagKeys     []string `json:"tagKeys"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	if err := h.svc.UntagResource(req.ResourceArn, req.TagKeys); err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) listTagsForResource(w http.ResponseWriter, body []byte) {
	var req struct {
		ResourceArn string `json:"resourceArn"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	tags, err := h.svc.ListTagsForResource(req.ResourceArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tagsToArray(tags)})
}

// ---- Execution actions ----

func (h *Handler) startExecution(w http.ResponseWriter, body []byte) {
	var req struct {
		StateMachineArn string `json:"stateMachineArn"`
		Name            string `json:"name"`
		Input           string `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	ex, err := h.svc.StartExecution(req.StateMachineArn, req.Name, req.Input)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"executionArn": ex.Arn,
		"startDate":    epochSeconds(ex.StartDate),
	})
}

func (h *Handler) describeExecution(w http.ResponseWriter, body []byte) {
	var req struct {
		ExecutionArn string `json:"executionArn"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	ex, err := h.svc.DescribeExecution(req.ExecutionArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, executionDetail(ex))
}

func (h *Handler) stopExecution(w http.ResponseWriter, body []byte) {
	var req struct {
		ExecutionArn string `json:"executionArn"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	ex, err := h.svc.StopExecution(req.ExecutionArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	stop := time.Now().UTC()
	if ex.StopDate != nil {
		stop = *ex.StopDate
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopDate": epochSeconds(stop)})
}

func (h *Handler) listExecutions(w http.ResponseWriter, body []byte) {
	var req struct {
		StateMachineArn string `json:"stateMachineArn"`
		StatusFilter    string `json:"statusFilter"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	execs, err := h.svc.ListExecutions(req.StateMachineArn, req.StatusFilter)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(execs))
	for _, ex := range execs {
		out = append(out, executionSummary(ex))
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": out})
}

func (h *Handler) getExecutionHistory(w http.ResponseWriter, body []byte) {
	var req struct {
		ExecutionArn string `json:"executionArn"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, err)
		return
	}
	events, err := h.svc.GetExecutionHistory(req.ExecutionArn)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": historyEvents(events)})
}

// ---- Response shaping ----

// awsTag is the AWS Step Functions tag wire shape ({"key":..,"value":..}).
type awsTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func stateMachineSummary(sm *types.StateMachine) map[string]any {
	return map[string]any{
		"stateMachineArn": sm.Arn,
		"name":            sm.Name,
		"type":            sm.Type,
		"creationDate":    epochSeconds(sm.CreatedAt),
	}
}

func stateMachineDetail(sm *types.StateMachine) map[string]any {
	return map[string]any{
		"stateMachineArn": sm.Arn,
		"name":            sm.Name,
		"definition":      sm.Definition,
		"roleArn":         sm.RoleArn,
		"status":          sm.Status,
		"type":            sm.Type,
		"creationDate":    epochSeconds(sm.CreatedAt),
	}
}

func executionSummary(ex *types.Execution) map[string]any {
	m := map[string]any{
		"executionArn":    ex.Arn,
		"stateMachineArn": ex.StateMachineArn,
		"name":            ex.Name,
		"status":          ex.Status,
		"startDate":       epochSeconds(ex.StartDate),
	}
	if ex.StopDate != nil {
		m["stopDate"] = epochSeconds(*ex.StopDate)
	}
	return m
}

func executionDetail(ex *types.Execution) map[string]any {
	m := executionSummary(ex)
	m["input"] = ex.Input
	if ex.Output != "" {
		m["output"] = ex.Output
	}
	if ex.Error != "" {
		m["error"] = ex.Error
	}
	if ex.Cause != "" {
		m["cause"] = ex.Cause
	}
	return m
}

func historyEvents(events []types.HistoryEvent) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		m := map[string]any{
			"id":              ev.ID,
			"previousEventId": ev.PreviousID,
			"type":            ev.Type,
			"timestamp":       epochSeconds(ev.Timestamp),
		}
		if len(ev.Details) > 0 {
			m["details"] = ev.Details
		}
		out = append(out, m)
	}
	return out
}

// parseTagMap accepts both the AWS array form ([{"key":..,"value":..}]) and a
// plain {key: value} object (as the Tarn CLI sends).
func parseTagMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var arr []awsTag
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		m := make(map[string]string, len(arr))
		for _, t := range arr {
			m[t.Key] = t.Value
		}
		return m
	}
	var obj map[string]string
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}
	return nil
}

func tagsToArray(m map[string]string) []awsTag {
	out := make([]awsTag, 0, len(m))
	for k, v := range m {
		out = append(out, awsTag{Key: k, Value: v})
	}
	return out
}

// epochSeconds renders a time as a Unix timestamp in seconds, the wire format
// AWS Step Functions uses for date fields.
func epochSeconds(t time.Time) float64 {
	return float64(t.UnixMilli()) / 1000.0
}

// ---- Low-level response helpers ----

func (h *Handler) badRequest(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadRequest, "InvalidRequestException", err.Error())
}

func writeSvcError(w http.ResponseWriter, err error) {
	if se, ok := err.(*stepfunctionssvc.ServiceError); ok {
		writeError(w, se.StatusCode(), se.Code, se.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "InternalFailure", err.Error())
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.0")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"__type":  code,
		"message": message,
	})
}
