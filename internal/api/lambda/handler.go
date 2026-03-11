package lambda

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	tracesvc "github.com/openstack-project/openstack/internal/trace"
	"github.com/openstack-project/openstack/pkg/types"
)

// s3Getter is the subset of the S3 service used to fetch Lambda deployment packages.
type s3Getter interface {
	GetObject(bucket, key string) (*types.Object, io.ReadCloser, error)
}

// Handler implements HTTP handlers for the Lambda API.
type Handler struct {
	svc        *lambdasvc.Service
	s3         s3Getter
	traceStore *tracesvc.Store
	collector  *tracesvc.Collector
}

// NewHandler creates a new Lambda API handler.
func NewHandler(svc *lambdasvc.Service, s3 s3Getter) *Handler {
	return &Handler{svc: svc, s3: s3}
}

// SetTraceStore attaches a trace store so direct invocations are recorded.
func (h *Handler) SetTraceStore(ts *tracesvc.Store) { h.traceStore = ts }

// SetCollector attaches a trace collector so sub-spans during invocation are captured.
func (h *Handler) SetCollector(c *tracesvc.Collector) { h.collector = c }

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
		FunctionName:     req.FunctionName,
		Runtime:          types.Runtime(req.Runtime),
		Handler:          req.Handler,
		Role:             req.Role,
		Description:      req.Description,
		Timeout:          req.Timeout,
		MemorySize:       req.MemorySize,
		Layers:           req.Layers,
		Tags:             req.Tags,
		DeadLetterConfig: req.DeadLetterConfig,
	}
	if req.Environment != nil {
		fn.Environment = req.Environment.Variables
	}

	var code []byte
	if req.Code != nil {
		if req.Code.ZipFile != "" {
			var err error
			code, err = base64.StdEncoding.DecodeString(req.Code.ZipFile)
			if err != nil {
				writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid base64 in Code.ZipFile")
				return
			}
		} else if req.Code.S3Bucket != "" && req.Code.S3Key != "" {
			var err error
			code, err = h.fetchS3Code(req.Code.S3Bucket, req.Code.S3Key)
			if err != nil {
				writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Failed to fetch code from S3: "+err.Error())
				return
			}
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
	// ensure field is populated for clients that don't know about it
	if fn.LastUpdateStatus == "" {
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"Configuration": fn,
		// Terraform AWS provider (v5/v6) treats GetFunction output as empty when
		// either Configuration or Code is missing.
		"Code": map[string]interface{}{
			"RepositoryType": "S3",
		},
	})
}

// GetFunctionConfiguration handles GET /2015-03-31/functions/{name}/configuration.
// Returns the configuration object at the root level (no "Configuration" wrapper).
// Used by the TF AWS provider v5 waiter (WaitUntilFunctionActiveV2) to poll State.
func (h *Handler) GetFunctionConfiguration(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}
	if fn.LastUpdateStatus == "" {
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	}
	writeJSON(w, http.StatusOK, fn)
}

// ListVersionsByFunction handles GET /2015-03-31/functions/{name}/versions.
// Terraform uses this to resolve the latest published (or $LATEST) version
// during Lambda function reads.
func (h *Handler) ListVersionsByFunction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", err.Error())
		return
	}
	if fn.Version == "" {
		fn.Version = "$LATEST"
	}
	if fn.LastUpdateStatus == "" {
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"Versions": []*types.FunctionConfig{fn},
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
	} else if req.S3Bucket != "" && req.S3Key != "" {
		var err error
		code, err = h.fetchS3Code(req.S3Bucket, req.S3Key)
		if err != nil {
			writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Failed to fetch code from S3: "+err.Error())
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

	if h.collector != nil {
		h.collector.Begin(name)
	}
	start := time.Now()
	output, err := h.svc.Invoke(r.Context(), input)
	durationMs := time.Since(start).Milliseconds()

	var subSpans []tracesvc.Span
	if h.collector != nil {
		subSpans = tracesvc.SubSpansToSpans(h.collector.CollectWithFlush(name))
	}

	if h.traceStore != nil {
		status := 200
		spanStatus := "ok"
		if err != nil {
			status = 500
			spanStatus = "error"
		} else if output != nil && output.FunctionError != "" {
			spanStatus = "error"
		}
		h.traceStore.Add(&tracesvc.Trace{
			ID:         uuid.NewString()[:8],
			StartedAt:  start,
			DurationMs: durationMs,
			Status:     status,
			Spans:      append([]tracesvc.Span{{Kind: "lambda", Name: name, DurationMs: durationMs, Status: spanStatus}}, subSpans...),
		})
	}

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

// PutFunctionConcurrency handles PUT /2017-10-31/functions/{name}/concurrency.
// TF provider v6 calls this endpoint using the 2017-10-31 API date prefix, which
// was not registered and fell through to the S3 handler returning XML.
func (h *Handler) PutFunctionConcurrency(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReservedConcurrentExecutions int `json:"ReservedConcurrentExecutions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	writeJSON(w, http.StatusOK, map[string]int{
		"ReservedConcurrentExecutions": req.ReservedConcurrentExecutions,
	})
}

// DeleteFunctionConcurrency handles DELETE /2017-10-31/functions/{name}/concurrency.
func (h *Handler) DeleteFunctionConcurrency(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// NotFound returns a well-formed AWS ResourceNotFoundException for any Lambda
// sub-resource endpoint we don't emulate (e.g. code-signing-config, concurrency,
// policy).  The Terraform AWS provider v5 calls several optional-feature endpoints
// during every function read and tolerates ResourceNotFoundException but treats any
// other error as fatal.  Without this, the Go mux returns a plain-text 404 that
// the AWS SDK cannot parse, causing terraform apply to fail after creation.
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
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
			"TotalCodeSize":                  80530636800,
			"CodeSizeUnzipped":               262144000,
			"CodeSizeZipped":                 52428800,
			"ConcurrentExecutions":           1000,
			"UnreservedConcurrentExecutions": 1000,
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
	FunctionName     string                  `json:"FunctionName"`
	Runtime          string                  `json:"Runtime"`
	Handler          string                  `json:"Handler"`
	Role             string                  `json:"Role"`
	Description      string                  `json:"Description,omitempty"`
	Timeout          int                     `json:"Timeout,omitempty"`
	MemorySize       int                     `json:"MemorySize,omitempty"`
	Code             *codeInput              `json:"Code,omitempty"`
	Environment      *envInput               `json:"Environment,omitempty"`
	Layers           []string                `json:"Layers,omitempty"`
	Tags             map[string]string       `json:"Tags,omitempty"`
	DeadLetterConfig *types.DeadLetterConfig `json:"DeadLetterConfig,omitempty"`
}

type codeInput struct {
	ZipFile  string `json:"ZipFile,omitempty"`  // base64-encoded zip
	S3Bucket string `json:"S3Bucket,omitempty"` // S3 bucket containing deployment package
	S3Key    string `json:"S3Key,omitempty"`    // S3 key of deployment package zip
}

type envInput struct {
	Variables map[string]string `json:"Variables,omitempty"`
}

type updateCodeRequest struct {
	ZipFile  string `json:"ZipFile,omitempty"`  // base64-encoded zip
	S3Bucket string `json:"S3Bucket,omitempty"` // S3 bucket containing deployment package
	S3Key    string `json:"S3Key,omitempty"`    // S3 key of deployment package zip
}

type publishLayerRequest struct {
	Description        string     `json:"Description,omitempty"`
	CompatibleRuntimes []string   `json:"CompatibleRuntimes,omitempty"`
	Content            *codeInput `json:"Content,omitempty"`
}

// fetchS3Code downloads a Lambda deployment package from an S3 bucket.
func (h *Handler) fetchS3Code(bucket, key string) ([]byte, error) {
	if h.s3 == nil {
		return nil, fmt.Errorf("S3 service not available")
	}
	_, rc, err := h.s3.GetObject(bucket, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
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
