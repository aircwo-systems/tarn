package apigateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	apisvc "github.com/openstack-project/openstack/internal/apigateway"
)

// Handler implements API Gateway v2 management and invoke endpoints.
type Handler struct {
	svc *apisvc.Service
}

// NewHandler creates a new API Gateway handler.
func NewHandler(svc *apisvc.Service) *Handler {
	return &Handler{svc: svc}
}

// CreateAPI handles POST /v2/apis.
func (h *Handler) CreateAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name                     string            `json:"name"`
		Description              string            `json:"description"`
		ProtocolType             string            `json:"protocolType"`
		RouteSelectionExpression string            `json:"routeSelectionExpression"`
		Tags                     map[string]string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}

	api, err := h.svc.CreateAPI(req.Name, req.Description, req.ProtocolType, req.RouteSelectionExpression, req.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, api)
}

// ListAPIs handles GET /v2/apis.
func (h *Handler) ListAPIs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.svc.ListAPIs()})
}

// GetAPI handles GET /v2/apis/{apiId}.
func (h *Handler) GetAPI(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	api, err := h.svc.GetAPI(apiID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api)
}

// UpdateAPI handles PATCH /v2/apis/{apiId}.
func (h *Handler) UpdateAPI(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}

	api, err := h.svc.UpdateAPI(apiID, apisvc.APIUpdateInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		status := http.StatusBadRequest
		code := "BadRequestException"
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
			code = "NotFoundException"
		}
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api)
}

// DeleteAPI handles DELETE /v2/apis/{apiId}.
func (h *Handler) DeleteAPI(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	if err := h.svc.DeleteAPI(apiID); err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateIntegration handles POST /v2/apis/{apiId}/integrations.
func (h *Handler) CreateIntegration(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	var req struct {
		IntegrationType      string            `json:"integrationType"`
		IntegrationURI       string            `json:"integrationUri"`
		PayloadFormatVersion string            `json:"payloadFormatVersion"`
		TimeoutInMillis      int               `json:"timeoutInMillis"`
		RequestParameters    map[string]string `json:"requestParameters"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}

	integration, err := h.svc.CreateIntegration(apiID, apisvc.IntegrationCreateInput{
		IntegrationType:      req.IntegrationType,
		IntegrationURI:       req.IntegrationURI,
		PayloadFormatVersion: req.PayloadFormatVersion,
		TimeoutInMillis:      req.TimeoutInMillis,
		RequestParameters:    req.RequestParameters,
	})
	if err != nil {
		status := http.StatusBadRequest
		code := "BadRequestException"
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
			code = "NotFoundException"
		}
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, integration)
}

// ListIntegrations handles GET /v2/apis/{apiId}/integrations.
func (h *Handler) ListIntegrations(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	items, err := h.svc.ListIntegrations(apiID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetIntegration handles GET /v2/apis/{apiId}/integrations/{integrationId}.
func (h *Handler) GetIntegration(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	integrationID := r.PathValue("integrationId")
	integration, err := h.svc.GetIntegration(apiID, integrationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, integration)
}

// UpdateIntegration handles PATCH /v2/apis/{apiId}/integrations/{integrationId}.
func (h *Handler) UpdateIntegration(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	integrationID := r.PathValue("integrationId")
	var req struct {
		IntegrationURI       *string           `json:"integrationUri"`
		PayloadFormatVersion *string           `json:"payloadFormatVersion"`
		TimeoutInMillis      *int              `json:"timeoutInMillis"`
		RequestParameters    map[string]string `json:"requestParameters"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}

	integration, err := h.svc.UpdateIntegration(apiID, integrationID, apisvc.IntegrationUpdateInput{
		IntegrationURI:       req.IntegrationURI,
		PayloadFormatVersion: req.PayloadFormatVersion,
		TimeoutInMillis:      req.TimeoutInMillis,
		RequestParameters:    req.RequestParameters,
	})
	if err != nil {
		status := http.StatusBadRequest
		code := "BadRequestException"
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
			code = "NotFoundException"
		}
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, integration)
}

// DeleteIntegration handles DELETE /v2/apis/{apiId}/integrations/{integrationId}.
func (h *Handler) DeleteIntegration(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	integrationID := r.PathValue("integrationId")
	if err := h.svc.DeleteIntegration(apiID, integrationID); err != nil {
		status := http.StatusBadRequest
		code := "BadRequestException"
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
			code = "NotFoundException"
		}
		writeError(w, status, code, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateRoute handles POST /v2/apis/{apiId}/routes.
func (h *Handler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	var req struct {
		RouteKey string `json:"routeKey"`
		Target   string `json:"target"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	route, err := h.svc.CreateRoute(apiID, apisvc.RouteCreateInput{RouteKey: req.RouteKey, Target: req.Target})
	if err != nil {
		status := http.StatusBadRequest
		code := "BadRequestException"
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
			code = "NotFoundException"
		}
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, route)
}

// ListRoutes handles GET /v2/apis/{apiId}/routes.
func (h *Handler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	items, err := h.svc.ListRoutes(apiID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetRoute handles GET /v2/apis/{apiId}/routes/{routeId}.
func (h *Handler) GetRoute(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	routeID := r.PathValue("routeId")
	route, err := h.svc.GetRoute(apiID, routeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, route)
}

// UpdateRoute handles PATCH /v2/apis/{apiId}/routes/{routeId}.
func (h *Handler) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	routeID := r.PathValue("routeId")
	var req struct {
		RouteKey *string `json:"routeKey"`
		Target   *string `json:"target"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}

	route, err := h.svc.UpdateRoute(apiID, routeID, apisvc.RouteUpdateInput{RouteKey: req.RouteKey, Target: req.Target})
	if err != nil {
		status := http.StatusBadRequest
		code := "BadRequestException"
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
			code = "NotFoundException"
		}
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, route)
}

// DeleteRoute handles DELETE /v2/apis/{apiId}/routes/{routeId}.
func (h *Handler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	routeID := r.PathValue("routeId")
	if err := h.svc.DeleteRoute(apiID, routeID); err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListStages handles GET /v2/apis/{apiId}/stages.
func (h *Handler) ListStages(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	items, err := h.svc.ListStages(apiID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetStage handles GET /v2/apis/{apiId}/stages/{stageName}.
func (h *Handler) GetStage(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	stageName := r.PathValue("stageName")
	stage, err := h.svc.GetStage(apiID, stageName)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stage)
}

// UpdateStage handles PATCH /v2/apis/{apiId}/stages/{stageName}.
func (h *Handler) UpdateStage(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	stageName := r.PathValue("stageName")
	var req struct {
		Description          *string        `json:"description"`
		DefaultRouteSettings map[string]any `json:"defaultRouteSettings"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	// For now DefaultRouteSettings is accepted as no-op for compatibility.

	stage, err := h.svc.UpdateStage(apiID, stageName, apisvc.StageUpdateInput{Description: req.Description})
	if err != nil {
		status := http.StatusBadRequest
		code := "BadRequestException"
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
			code = "NotFoundException"
		}
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stage)
}

// Invoke handles /_apigateway/{apiId}/{stage}/{proxy...}.
func (h *Handler) Invoke(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	stage := r.PathValue("stage")
	proxy := r.PathValue("proxy")
	path := "/"
	if proxy != "" {
		path = "/" + proxy
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", "failed to read request body")
		return
	}

	out, err := h.svc.Invoke(r.Context(), &apisvc.InvokeInput{
		APIID:    apiID,
		Stage:    stage,
		Method:   r.Method,
		Path:     path,
		RawQuery: r.URL.RawQuery,
		Query:    r.URL.Query(),
		Headers:  r.Header,
		Body:     body,
	})
	if err != nil {
		status := http.StatusInternalServerError
		code := "InternalFailure"
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
			code = "NotFoundException"
		}
		writeError(w, status, code, err.Error())
		return
	}

	for key, value := range out.Headers {
		w.Header().Set(key, value)
	}
	w.WriteHeader(out.StatusCode)
	if len(out.Body) > 0 {
		_, _ = w.Write(out.Body)
	}
}

func readJSON(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"__type":  code,
		"Message": message,
	})
}
