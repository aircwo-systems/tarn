package eventsource

import (
	"encoding/json"
	"net/http"

	eventsourcesvc "github.com/openstack-project/openstack/internal/eventsource"
	"github.com/openstack-project/openstack/pkg/types"
)

// Handler serves event source mapping REST endpoints.
type Handler struct {
	svc *eventsourcesvc.Service
}

// NewHandler creates a new event source mapping handler.
func NewHandler(svc *eventsourcesvc.Service) *Handler {
	return &Handler{svc: svc}
}

type createMappingRequest struct {
	EventSourceArn                 string                  `json:"EventSourceArn"`
	FunctionName                   string                  `json:"FunctionName"`
	BatchSize                      int                     `json:"BatchSize"`
	MaximumBatchingWindowInSeconds int                     `json:"MaximumBatchingWindowInSeconds"`
	Enabled                        *bool                   `json:"Enabled"`
	FilterCriteria                 *types.FilterCriteria   `json:"FilterCriteria,omitempty"`
}

type updateMappingRequest struct {
	FunctionName                   *string                 `json:"FunctionName,omitempty"`
	BatchSize                      *int                    `json:"BatchSize,omitempty"`
	MaximumBatchingWindowInSeconds *int                    `json:"MaximumBatchingWindowInSeconds,omitempty"`
	Enabled                        *bool                   `json:"Enabled,omitempty"`
	FilterCriteria                 *types.FilterCriteria   `json:"FilterCriteria,omitempty"`
}

// eventSourceMappingResponse mirrors AWS response field names while keeping
// LastModified encoded as a numeric Unix timestamp (seconds), which the
// Terraform AWS provider expects for Lambda event source mapping APIs.
type eventSourceMappingResponse struct {
	UUID                           string                `json:"UUID"`
	EventSourceArn                 string                `json:"EventSourceArn"`
	FunctionArn                    string                `json:"FunctionArn"`
	FunctionName                   string                `json:"FunctionName"`
	BatchSize                      int                   `json:"BatchSize"`
	MaximumBatchingWindowInSeconds int                   `json:"MaximumBatchingWindowInSeconds"`
	Enabled                        bool                  `json:"Enabled"`
	State                          string                `json:"State"`
	LastProcessingResult           string                `json:"LastProcessingResult,omitempty"`
	LastModified                   float64               `json:"LastModified"`
	FilterCriteria                 *types.FilterCriteria `json:"FilterCriteria,omitempty"`
}

// Create handles POST /2015-03-31/event-source-mappings
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "invalid JSON body")
		return
	}

	if req.EventSourceArn == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "EventSourceArn is required")
		return
	}
	if req.FunctionName == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "FunctionName is required")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Build the function ARN from name (simplified)
	functionArn := req.FunctionName

	mapping, err := h.svc.CreateMapping(req.EventSourceArn, functionArn, req.FunctionName, req.BatchSize, req.MaximumBatchingWindowInSeconds, enabled, req.FilterCriteria)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toEventSourceMappingResponse(mapping))
}

// List handles GET /2015-03-31/event-source-mappings
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	eventSourceArn := r.URL.Query().Get("EventSourceArn")
	functionName := r.URL.Query().Get("FunctionName")

	mappings := h.svc.ListMappings(eventSourceArn, functionName)
	if mappings == nil {
		mappings = make([]*types.EventSourceMapping, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"EventSourceMappings": toEventSourceMappingResponses(mappings),
	})
}

// Get handles GET /2015-03-31/event-source-mappings/{uuid}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "UUID is required")
		return
	}

	mapping, err := h.svc.GetMapping(uuid)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toEventSourceMappingResponse(mapping))
}

// Update handles PUT /2015-03-31/event-source-mappings/{uuid}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "UUID is required")
		return
	}

	var req updateMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "invalid JSON body")
		return
	}

	mapping, err := h.svc.UpdateMapping(uuid, req.BatchSize, req.MaximumBatchingWindowInSeconds, req.Enabled, req.FunctionName, req.FilterCriteria)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toEventSourceMappingResponse(mapping))
}

// Delete handles DELETE /2015-03-31/event-source-mappings/{uuid}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "UUID is required")
		return
	}

	if err := h.svc.DeleteMapping(uuid); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"__type":  code,
		"Type":    code,
		"Message": message,
	})
}

func toEventSourceMappingResponse(mapping *types.EventSourceMapping) *eventSourceMappingResponse {
	if mapping == nil {
		return nil
	}
	return &eventSourceMappingResponse{
		UUID:                           mapping.UUID,
		EventSourceArn:                 mapping.EventSourceArn,
		FunctionArn:                    mapping.FunctionArn,
		FunctionName:                   mapping.FunctionName,
		BatchSize:                      mapping.BatchSize,
		MaximumBatchingWindowInSeconds: mapping.MaximumBatchingWindowInSeconds,
		Enabled:                        mapping.Enabled,
		State:                          mapping.State,
		LastProcessingResult:           mapping.LastProcessingResult,
		LastModified:                   float64(mapping.LastModified.UnixNano()) / 1e9,
		FilterCriteria:                 mapping.FilterCriteria,
	}
}

func toEventSourceMappingResponses(mappings []*types.EventSourceMapping) []*eventSourceMappingResponse {
	out := make([]*eventSourceMappingResponse, 0, len(mappings))
	for _, mapping := range mappings {
		out = append(out, toEventSourceMappingResponse(mapping))
	}
	return out
}
