package apigatewayv1

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	svc "github.com/openstack-project/openstack/internal/apigatewayv1"
)

// Handler implements the AWS API Gateway REST API (v1) management and invoke endpoints.
type Handler struct {
	svc *svc.Service
}

// NewHandler creates a new v1 handler.
func NewHandler(s *svc.Service) *Handler {
	return &Handler{svc: s}
}

// --- REST API CRUD ---

// CreateRestAPI handles POST /restapis.
func (h *Handler) CreateRestAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Tags        map[string]string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	api, err := h.svc.CreateAPI(req.Name, req.Description, req.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, api)
}

// ListRestAPIs handles GET /restapis.
func (h *Handler) ListRestAPIs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"item": h.svc.ListAPIs()})
}

// GetRestAPI handles GET /restapis/{restApiId}.
func (h *Handler) GetRestAPI(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	api, err := h.svc.GetAPI(apiID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api)
}

// DeleteRestAPI handles DELETE /restapis/{restApiId}.
func (h *Handler) DeleteRestAPI(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	if err := h.svc.DeleteAPI(apiID); err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Resources ---

// CreateResource handles POST /restapis/{restApiId}/resources/{parentId}.
func (h *Handler) CreateResource(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	parentID := r.PathValue("parentId")
	var req struct {
		PathPart string `json:"pathPart"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	res, err := h.svc.CreateResource(apiID, parentID, req.PathPart)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// ListResources handles GET /restapis/{restApiId}/resources.
func (h *Handler) ListResources(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resources, err := h.svc.ListResources(apiID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": resources})
}

// GetResource handles GET /restapis/{restApiId}/resources/{resourceId}.
func (h *Handler) GetResource(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	res, err := h.svc.GetResource(apiID, resourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// DeleteResource handles DELETE /restapis/{restApiId}/resources/{resourceId}.
func (h *Handler) DeleteResource(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	if err := h.svc.DeleteResource(apiID, resourceID); err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Methods ---

// PutMethod handles PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}.
func (h *Handler) PutMethod(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	var req struct {
		AuthorizationType string          `json:"authorizationType"`
		RequestParameters map[string]bool `json:"requestParameters"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	method, err := h.svc.PutMethod(apiID, resourceID, httpMethod, req.AuthorizationType, req.RequestParameters)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, method)
}

// GetMethod handles GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}.
func (h *Handler) GetMethod(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	method, err := h.svc.GetMethod(apiID, resourceID, httpMethod)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, method)
}

// DeleteMethod handles DELETE /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}.
func (h *Handler) DeleteMethod(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	if err := h.svc.DeleteMethod(apiID, resourceID, httpMethod); err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PatchMethod handles PATCH /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}.
// Terraform calls this as UpdateMethod when request_parameters change on an existing method.
func (h *Handler) PatchMethod(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	var req struct {
		PatchOperations []svc.PatchOp `json:"patchOperations"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	method, err := h.svc.PatchMethod(apiID, resourceID, httpMethod, req.PatchOperations)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, method)
}

// --- Integrations ---

// PutIntegration handles PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration.
func (h *Handler) PutIntegration(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	var req struct {
		Type              string            `json:"type"`
		HTTPMethod        string            `json:"httpMethod"`
		URI               string            `json:"uri"`
		RequestParameters map[string]string `json:"requestParameters"`
		RequestTemplates  map[string]string `json:"requestTemplates"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	integ, err := h.svc.PutIntegration(apiID, resourceID, httpMethod, req.Type, req.HTTPMethod, req.URI, req.RequestParameters, req.RequestTemplates)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, integ)
}

// PatchIntegration handles PATCH /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration.
// Terraform calls this as UpdateIntegration when an existing integration needs updating.
func (h *Handler) PatchIntegration(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	var req struct {
		PatchOperations []svc.PatchOp `json:"patchOperations"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	integ, err := h.svc.PatchIntegration(apiID, resourceID, httpMethod, req.PatchOperations)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, integ)
}

// GetIntegration handles GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration.
func (h *Handler) GetIntegration(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	integ, err := h.svc.GetIntegration(apiID, resourceID, httpMethod)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, integ)
}

// DeleteIntegration handles DELETE /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration.
func (h *Handler) DeleteIntegration(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	if err := h.svc.DeleteIntegration(apiID, resourceID, httpMethod); err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Method Responses ---

// PutMethodResponse handles PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/responses/{statusCode}.
func (h *Handler) PutMethodResponse(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	statusCode := r.PathValue("statusCode")
	var req struct {
		ResponseModels map[string]string `json:"responseModels"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	mr, err := h.svc.PutMethodResponse(apiID, resourceID, httpMethod, statusCode, req.ResponseModels)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, mr)
}

// GetMethodResponse handles GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/responses/{statusCode}.
func (h *Handler) GetMethodResponse(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	statusCode := r.PathValue("statusCode")
	mr, err := h.svc.GetMethodResponse(apiID, resourceID, httpMethod, statusCode)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mr)
}

// --- Integration Responses ---

// PutIntegrationResponse handles PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration/responses/{statusCode}.
func (h *Handler) PutIntegrationResponse(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	statusCode := r.PathValue("statusCode")
	var req struct {
		SelectionPattern  string            `json:"selectionPattern"`
		ResponseTemplates map[string]string `json:"responseTemplates"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	ir, err := h.svc.PutIntegrationResponse(apiID, resourceID, httpMethod, statusCode, req.SelectionPattern, req.ResponseTemplates)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ir)
}

// GetIntegrationResponse handles GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration/responses/{statusCode}.
func (h *Handler) GetIntegrationResponse(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	resourceID := r.PathValue("resourceId")
	httpMethod := r.PathValue("httpMethod")
	statusCode := r.PathValue("statusCode")
	ir, err := h.svc.GetIntegrationResponse(apiID, resourceID, httpMethod, statusCode)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ir)
}

// --- Deployments ---

// CreateDeployment handles POST /restapis/{restApiId}/deployments.
func (h *Handler) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	var req struct {
		StageName   string `json:"stageName"`
		Description string `json:"description"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	dep, err := h.svc.CreateDeployment(apiID, req.Description, req.StageName)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, dep)
}

// ListDeployments handles GET /restapis/{restApiId}/deployments.
func (h *Handler) ListDeployments(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	deps, err := h.svc.ListDeployments(apiID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": deps})
}

// GetDeployment handles GET /restapis/{restApiId}/deployments/{deploymentId}.
func (h *Handler) GetDeployment(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	deploymentID := r.PathValue("deploymentId")
	dep, err := h.svc.GetDeployment(apiID, deploymentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dep)
}

// --- Stages ---

// CreateStage handles POST /restapis/{restApiId}/stages.
func (h *Handler) CreateStage(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	var req struct {
		StageName    string `json:"stageName"`
		DeploymentId string `json:"deploymentId"`
		Description  string `json:"description"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", err.Error())
		return
	}
	stage, err := h.svc.CreateStage(apiID, req.StageName, req.DeploymentId, req.Description)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, stage)
}

// ListStages handles GET /restapis/{restApiId}/stages.
func (h *Handler) ListStages(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	stages, err := h.svc.ListStages(apiID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": stages})
}

// GetStage handles GET /restapis/{restApiId}/stages/{stageName}.
func (h *Handler) GetStage(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	stageName := r.PathValue("stageName")
	stage, err := h.svc.GetStage(apiID, stageName)
	if err != nil {
		writeError(w, http.StatusNotFound, "NotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stage)
}

// DeleteStage handles DELETE /restapis/{restApiId}/stages/{stageName}.
func (h *Handler) DeleteStage(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("restApiId")
	stageName := r.PathValue("stageName")
	if err := h.svc.DeleteStage(apiID, stageName); err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Invoke ---

// Invoke handles /_aws/execute-api/{apiId}/{stage}/{proxy...}.
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

	out, err := h.svc.Invoke(r.Context(), &svc.InvokeInput{
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

	for k, v := range out.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(out.StatusCode)
	if len(out.Body) > 0 {
		_, _ = w.Write(out.Body)
	}
}

func readJSON(r *http.Request, v any) error {
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"__type":  code,
		"message": message,
	})
}

func errorStatus(err error) (int, string) {
	if err == nil {
		return http.StatusOK, "OK"
	}
	msg := err.Error()
	if strings.Contains(msg, "not found") {
		return http.StatusNotFound, "NotFoundException"
	}
	return http.StatusBadRequest, "BadRequestException"
}
