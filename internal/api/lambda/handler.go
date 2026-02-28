package lambda

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/pkg/types"
)

// Handler implements HTTP handlers for the Lambda API.
type Handler struct {
	svc *lambdasvc.Service
}

// NewHandler creates a new Lambda API handler.
func NewHandler(svc *lambdasvc.Service) *Handler {
	return &Handler{svc: svc}
}

// CreateFunction handles POST /2015-03-31/functions
func (h *Handler) CreateFunction(w http.ResponseWriter, r *http.Request) {
	var req createFunctionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body: "+err.Error())
		return
	}

	if req.FunctionName == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "FunctionName is required")
		return
	}
	if req.Runtime == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Runtime is required")
		return
	}
	if !types.ValidRuntime(req.Runtime) {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", fmt.Sprintf("Unsupported runtime: %s", req.Runtime))
		return
	}
	if req.Handler == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Handler is required")
		return
	}

	fn := &types.FunctionConfig{
		FunctionName: req.FunctionName,
		Runtime:      types.Runtime(req.Runtime),
		Handler:      req.Handler,
		Role:         req.Role,
		Description:  req.Description,
		Timeout:      req.Timeout,
		MemorySize:   req.MemorySize,
		Layers:       req.Layers,
		Tags:         req.Tags,
	}
	if req.Environment != nil {
		fn.Environment = req.Environment.Variables
	}

	var code []byte
	if req.Code != nil && req.Code.ZipFile != "" {
		var err error
		code, err = base64.StdEncoding.DecodeString(req.Code.ZipFile)
		if err != nil {
			writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid base64 in Code.ZipFile")
			return
		}
	}

	result, err := h.svc.CreateFunction(r.Context(), fn, code)
	if err != nil {
		writeError(w, http.StatusConflict, "ResourceConflictException", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// GetFunction handles GET /2015-03-31/functions/{name}
func (h *Handler) GetFunction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"Configuration": fn,
	})
}

// ListFunctions handles GET /2015-03-31/functions
func (h *Handler) ListFunctions(w http.ResponseWriter, r *http.Request) {
	functions, err := h.svc.ListFunctions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ServiceException", err.Error())
		return
	}
	if functions == nil {
		functions = []*types.FunctionConfig{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"Functions": functions,
	})
}

// DeleteFunction handles DELETE /2015-03-31/functions/{name}
func (h *Handler) DeleteFunction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.svc.DeleteFunction(r.Context(), name); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateFunctionCode handles PUT /2015-03-31/functions/{name}/code
func (h *Handler) UpdateFunctionCode(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req updateCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body")
		return
	}

	var code []byte
	if req.ZipFile != "" {
		var err error
		code, err = base64.StdEncoding.DecodeString(req.ZipFile)
		if err != nil {
			writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid base64 in ZipFile")
			return
		}
	}

	result, err := h.svc.UpdateFunctionCode(r.Context(), name, code)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// UpdateFunctionConfiguration handles PUT /2015-03-31/functions/{name}/configuration
func (h *Handler) UpdateFunctionConfiguration(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var update types.FunctionConfigUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body: "+err.Error())
		return
	}

	if update.Runtime != "" && !types.ValidRuntime(update.Runtime) {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", fmt.Sprintf("Unsupported runtime: %s", update.Runtime))
		return
	}

	result, err := h.svc.UpdateFunctionConfiguration(r.Context(), name, &update)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// Invoke handles POST /2015-03-31/functions/{name}/invocations
func (h *Handler) Invoke(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Failed to read request body")
		return
	}

	input := &types.InvokeInput{
		FunctionName:   name,
		Payload:        payload,
		InvocationType: r.Header.Get("X-Amz-Invocation-Type"),
		LogType:        r.Header.Get("X-Amz-Log-Type"),
	}

	if input.InvocationType == "" {
		input.InvocationType = "RequestResponse"
	}

	output, err := h.svc.Invoke(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ServiceException", err.Error())
		return
	}

	if output.FunctionError != "" {
		w.Header().Set("X-Amz-Function-Error", output.FunctionError)
	}
	if output.LogResult != "" {
		w.Header().Set("X-Amz-Log-Result", output.LogResult)
	}
	if output.ExecutedVersion != "" {
		w.Header().Set("X-Amz-Executed-Version", output.ExecutedVersion)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(output.StatusCode)
	w.Write(output.Payload)
}

// GetAccountSettings handles GET /2015-03-31/account-settings
func (h *Handler) GetAccountSettings(w http.ResponseWriter, r *http.Request) {
	functions, _ := h.svc.ListFunctions()
	var totalCodeSize int64
	for _, fn := range functions {
		totalCodeSize += fn.CodeSize
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"AccountLimit": map[string]interface{}{
			"TotalCodeSize":                    80530636800,
			"CodeSizeUnzipped":                 262144000,
			"CodeSizeZipped":                   52428800,
			"ConcurrentExecutions":             1000,
			"UnreservedConcurrentExecutions":   1000,
		},
		"AccountUsage": map[string]interface{}{
			"TotalCodeSize": totalCodeSize,
			"FunctionCount": len(functions),
		},
	})
}

// ListTags handles GET /2015-03-31/functions/{name}/tags
func (h *Handler) ListTags(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}
	tags := fn.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"Tags": tags})
}

// TagResource handles POST /2015-03-31/functions/{name}/tags
func (h *Handler) TagResource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req struct {
		Tags map[string]string `json:"Tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body")
		return
	}
	if err := h.svc.TagResource(name, req.Tags); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UntagResource handles DELETE /2015-03-31/functions/{name}/tags
func (h *Handler) UntagResource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	keys := r.URL.Query().Get("tagKeys")
	if keys == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "tagKeys parameter is required")
		return
	}
	tagKeys := strings.Split(keys, ",")
	if err := h.svc.UntagResource(name, tagKeys); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Layer handlers ---

// PublishLayerVersion handles POST /2015-03-31/layers/{layerName}/versions
func (h *Handler) PublishLayerVersion(w http.ResponseWriter, r *http.Request) {
	layerName := r.PathValue("layerName")

	var req publishLayerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body: "+err.Error())
		return
	}

	var code []byte
	if req.Content != nil && req.Content.ZipFile != "" {
		var err error
		code, err = base64.StdEncoding.DecodeString(req.Content.ZipFile)
		if err != nil {
			writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid base64 in Content.ZipFile")
			return
		}
	}
	if len(code) == 0 {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Layer code is required")
		return
	}

	result, err := h.svc.PublishLayerVersion(layerName, req.Description, req.CompatibleRuntimes, code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ServiceException", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// GetLayerVersion handles GET /2015-03-31/layers/{layerName}/versions/{version}
func (h *Handler) GetLayerVersion(w http.ResponseWriter, r *http.Request) {
	layerName := r.PathValue("layerName")
	versionStr := r.PathValue("version")
	version, err := strconv.ParseInt(versionStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid version number")
		return
	}

	result, err := h.svc.GetLayerVersion(layerName, version)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ListLayers handles GET /2015-03-31/layers
func (h *Handler) ListLayers(w http.ResponseWriter, r *http.Request) {
	layers, err := h.svc.ListLayers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ServiceException", err.Error())
		return
	}
	if layers == nil {
		layers = []*types.LayerConfig{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"Layers": layers,
	})
}

// ListLayerVersions handles GET /2015-03-31/layers/{layerName}/versions
func (h *Handler) ListLayerVersions(w http.ResponseWriter, r *http.Request) {
	layerName := r.PathValue("layerName")

	versions, err := h.svc.ListLayerVersions(layerName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ServiceException", err.Error())
		return
	}
	if versions == nil {
		versions = []*types.LayerConfig{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"LayerVersions": versions,
	})
}

// DeleteLayerVersion handles DELETE /2015-03-31/layers/{layerName}/versions/{version}
func (h *Handler) DeleteLayerVersion(w http.ResponseWriter, r *http.Request) {
	layerName := r.PathValue("layerName")
	versionStr := r.PathValue("version")
	version, err := strconv.ParseInt(versionStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid version number")
		return
	}

	if err := h.svc.DeleteLayerVersion(layerName, version); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Request/Response types ---

type createFunctionRequest struct {
	FunctionName string            `json:"FunctionName"`
	Runtime      string            `json:"Runtime"`
	Handler      string            `json:"Handler"`
	Role         string            `json:"Role"`
	Description  string            `json:"Description,omitempty"`
	Timeout      int               `json:"Timeout,omitempty"`
	MemorySize   int               `json:"MemorySize,omitempty"`
	Code         *codeInput        `json:"Code,omitempty"`
	Environment  *envInput         `json:"Environment,omitempty"`
	Layers       []string          `json:"Layers,omitempty"`
	Tags         map[string]string `json:"Tags,omitempty"`
}

type codeInput struct {
	ZipFile string `json:"ZipFile,omitempty"` // base64-encoded zip
}

type envInput struct {
	Variables map[string]string `json:"Variables,omitempty"`
}

type updateCodeRequest struct {
	ZipFile string `json:"ZipFile,omitempty"` // base64-encoded zip
}

type publishLayerRequest struct {
	Description        string      `json:"Description,omitempty"`
	CompatibleRuntimes []string    `json:"CompatibleRuntimes,omitempty"`
	Content            *codeInput  `json:"Content,omitempty"`
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"__type":  code,
		"Message": message,
	})
}
