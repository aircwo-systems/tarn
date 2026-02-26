package lambda

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

// --- Request/Response types ---

type createFunctionRequest struct {
	FunctionName string       `json:"FunctionName"`
	Runtime      string       `json:"Runtime"`
	Handler      string       `json:"Handler"`
	Role         string       `json:"Role"`
	Description  string       `json:"Description,omitempty"`
	Timeout      int          `json:"Timeout,omitempty"`
	MemorySize   int          `json:"MemorySize,omitempty"`
	Code         *codeInput   `json:"Code,omitempty"`
	Environment  *envInput    `json:"Environment,omitempty"`
	Layers       []string     `json:"Layers,omitempty"`
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
