// Package ecs implements the AWS JSON 1.1 protocol surface for ECS, on top
// of the control-plane service in internal/ecs. It is a thin translation
// layer, following the pattern in internal/api/eventbridge: decode the wire
// request, call the service, marshal the wire response.
//
// RunTask and StopTask are the one exception. Per docs/design/ecs-support.md
// ("Resolved during T1"), they go through the types.TaskRunner interface
// (T7's runner), not this package's *ecssvc.Service, so the handler can be
// built and tested before the runner exists.
package ecs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	ecssvc "github.com/aircwo-systems/tarn/internal/ecs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const servicePrefix = "AmazonEC2ContainerServiceV20141113."

// Handler dispatches ECS JSON protocol requests.
type Handler struct {
	svc    *ecssvc.Service
	runner types.TaskRunner
}

func NewHandler(svc *ecssvc.Service) *Handler {
	return &Handler{svc: svc}
}

// SetTaskRunner wires the task runner used for RunTask/StopTask, mirroring
// how sibling handlers take their optional collaborators (e.g.
// EventBridge's SetTraceStore/SetCollector). Until this is called, RunTask
// and StopTask return a clean ServerException rather than reaching a nil
// runner.
func (h *Handler) SetTaskRunner(runner types.TaskRunner) {
	h.runner = runner
}

// Service exposes the underlying control-plane service so callers outside
// the AWS JSON protocol surface (e.g. the /_ecs/ reverse proxy in
// internal/api/ecsproxy.go) can resolve clusters/services/tasks directly.
func (h *Handler) Service() *ecssvc.Service {
	return h.svc
}

// IsECSRequest reports whether r targets the ECS JSON protocol, following
// the IsDynamoDBRequest / IsSNSRequest convention used by sibling handlers.
func IsECSRequest(r *http.Request) bool {
	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	return strings.HasPrefix(target, servicePrefix)
}

// Dispatch routes one ECS JSON protocol request.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	if !strings.HasPrefix(target, servicePrefix) {
		writeError(w, http.StatusBadRequest, "ValidationException", "Invalid X-Amz-Target for ECS")
		return
	}
	action := strings.TrimPrefix(target, servicePrefix)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", "Failed to read request body")
		return
	}

	switch action {
	case "CreateCluster":
		h.createCluster(w, body)
	case "ListClusters":
		h.listClusters(w, body)
	case "DescribeClusters":
		h.describeClusters(w, body)
	case "DeleteCluster":
		h.deleteCluster(w, body)
	case "RegisterTaskDefinition":
		h.registerTaskDefinition(w, body)
	case "DescribeTaskDefinition":
		h.describeTaskDefinition(w, body)
	case "ListTaskDefinitions":
		h.listTaskDefinitions(w, body)
	case "DeregisterTaskDefinition":
		h.deregisterTaskDefinition(w, body)
	case "RunTask":
		h.runTask(w, r.Context(), body)
	case "StopTask":
		h.stopTask(w, r.Context(), body)
	case "ListTasks":
		h.listTasks(w, body)
	case "DescribeTasks":
		h.describeTasks(w, body)
	case "CreateService":
		h.createService(w, body)
	case "UpdateService":
		h.updateService(w, body)
	case "DeleteService":
		h.deleteService(w, body)
	case "ListServices":
		h.listServices(w, body)
	case "DescribeServices":
		h.describeServices(w, body)
	case "TagResource":
		h.tagResource(w, body)
	case "UntagResource":
		h.untagResource(w, body)
	case "ListTagsForResource":
		h.listTagsForResource(w, body)
	default:
		log.Printf("[ecs] unhandled action: %s", action)
		writeError(w, http.StatusBadRequest, "InvalidAction", fmt.Sprintf("Unsupported ECS action: %s", action))
	}
}

// --- Clusters ----------------------------------------------------------------

func (h *Handler) createCluster(w http.ResponseWriter, body []byte) {
	var in types.CreateClusterInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.CreateCluster(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) listClusters(w http.ResponseWriter, body []byte) {
	var in types.ListClustersInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.ListClusters(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) describeClusters(w http.ResponseWriter, body []byte) {
	var in types.DescribeClustersInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeClusters(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// deleteClusterOutput mirrors CreateClusterOutput's shape. There is no
// DeleteClusterInput/Output pair in pkg/types/ecs.go (see T1's notes), so
// the handler builds the wire shape locally rather than inventing types
// there.
type deleteClusterOutput struct {
	Cluster *types.Cluster `json:"Cluster"`
}

func (h *Handler) deleteCluster(w http.ResponseWriter, body []byte) {
	var req struct {
		Cluster string `json:"Cluster"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	cluster, err := h.svc.DeleteCluster(req.Cluster)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deleteClusterOutput{Cluster: cluster})
}

// --- Task definitions ---------------------------------------------------------

func (h *Handler) registerTaskDefinition(w http.ResponseWriter, body []byte) {
	var in types.RegisterTaskDefinitionInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.RegisterTaskDefinition(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) describeTaskDefinition(w http.ResponseWriter, body []byte) {
	var in types.DescribeTaskDefinitionInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeTaskDefinition(in.TaskDefinition, in.Include)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) listTaskDefinitions(w http.ResponseWriter, body []byte) {
	var in types.ListTaskDefinitionsInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.ListTaskDefinitions(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) deregisterTaskDefinition(w http.ResponseWriter, body []byte) {
	var req struct {
		TaskDefinition string `json:"TaskDefinition"`
	}
	if err := decodeJSON(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DeregisterTaskDefinition(req.TaskDefinition)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Tasks ---------------------------------------------------------------------

func (h *Handler) runTask(w http.ResponseWriter, ctx context.Context, body []byte) {
	var in types.RunTaskInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	if h.runner == nil {
		writeNoRunnerError(w)
		return
	}
	out, err := h.runner.RunTask(ctx, &in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// stopTask unmarshals the StopTaskInput wire struct and calls the
// TaskRunner's positional StopTask(cluster, taskArn, reason) signature. Per
// docs/design/ecs-support.md ("Resolved during T1"), these signatures
// diverge on purpose and must not be unified. After the runner reports
// success, the handler reads the resulting record back from the service to
// populate the StopTaskOutput's Task field, since the interface itself
// returns only an error.
func (h *Handler) stopTask(w http.ResponseWriter, ctx context.Context, body []byte) {
	var in types.StopTaskInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	if h.runner == nil {
		writeNoRunnerError(w)
		return
	}
	if err := h.runner.StopTask(ctx, in.Cluster, in.Task, in.Reason); err != nil {
		writeSvcError(w, err)
		return
	}
	task, err := h.svc.GetTask(in.Task)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, types.StopTaskOutput{Task: task})
}

func (h *Handler) listTasks(w http.ResponseWriter, body []byte) {
	var in types.ListTasksInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.ListTasks(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) describeTasks(w http.ResponseWriter, body []byte) {
	var in types.DescribeTasksInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeTasks(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Services ------------------------------------------------------------------

func (h *Handler) createService(w http.ResponseWriter, body []byte) {
	var in types.CreateServiceInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.CreateService(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) updateService(w http.ResponseWriter, body []byte) {
	var in types.UpdateServiceInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.UpdateService(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteService(w http.ResponseWriter, body []byte) {
	var in types.DeleteServiceInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DeleteService(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) listServices(w http.ResponseWriter, body []byte) {
	var in types.ListServicesInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.ListServices(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) describeServices(w http.ResponseWriter, body []byte) {
	var in types.DescribeServicesInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.DescribeServices(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Tagging ---------------------------------------------------------------

func (h *Handler) tagResource(w http.ResponseWriter, body []byte) {
	var in types.TagResourceInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.TagResource(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) untagResource(w http.ResponseWriter, body []byte) {
	var in types.UntagResourceInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.UntagResource(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) listTagsForResource(w http.ResponseWriter, body []byte) {
	var in types.ListTagsForResourceInput
	if err := decodeJSON(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "ValidationException", err.Error())
		return
	}
	out, err := h.svc.ListTagsForResource(&in)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- plumbing --------------------------------------------------------------

func decodeJSON(body []byte, dst any) error {
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
}

// writeSvcError maps err to the AWS JSON error shape, preserving the
// service's code and HTTP status the way EventBridge's handler does. Errors
// from the TaskRunner (T7) that are not *ecssvc.ServiceError fall through to
// a generic InternalException, since the runner is free to return plain
// errors.
func writeSvcError(w http.ResponseWriter, err error) {
	if se, ok := err.(*ecssvc.ServiceError); ok {
		writeError(w, se.StatusCode(), se.Code, se.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "InternalException", err.Error())
}

// writeNoRunnerError reports a clean, AWS-shaped failure when RunTask or
// StopTask is called before T8 wires a task runner via SetTaskRunner. Never
// let a nil h.runner reach an interface call.
func writeNoRunnerError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "ServerException", "ECS task runner is not configured")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ecsWireBody(body))
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"__type":  code,
		"message": message,
	})
}
