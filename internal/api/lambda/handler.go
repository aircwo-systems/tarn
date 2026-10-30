package lambda

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	lambdasvc "github.com/aircwo-systems/tarn/internal/lambda"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
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
	image := req.PackageType == "Image" || (req.Code != nil && req.Code.ImageUri != "")
	if !image {
		if req.Runtime == "" || !types.ValidRuntime(req.Runtime) {
			writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "A supported Runtime is required")
			return
		}
		if req.Handler == "" {
			writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Handler is required")
			return
		}
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
		KMSKeyArn:        req.KMSKeyArn,
		PackageType:      req.PackageType,
		ImageConfig:      req.ImageConfig,
		Architectures:    req.Architectures,
	}
	if req.Environment != nil {
		fn.Environment = req.Environment.Variables
	}
	if req.Code != nil {
		fn.ImageURI = req.Code.ImageUri
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
		writeServiceError(w, err)
		return
	}

	if req.Publish {
		result, err = h.svc.PublishVersion(r.Context(), result.FunctionName, req.Description)
		if err != nil {
			writeServiceError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, toFunctionConfigResponse(result))
}

// GetFunction handles GET /2015-03-31/functions/{name}
func (h *Handler) GetFunction(w http.ResponseWriter, r *http.Request) {
	name, err := requestFunctionReference(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	// ensure field is populated for clients that don't know about it
	if fn.LastUpdateStatus == "" {
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	}
	code := map[string]any{"RepositoryType": "S3"}
	if fn.PackageType == "Image" {
		code = map[string]any{"RepositoryType": "ECR", "ImageUri": fn.ImageURI, "ResolvedImageUri": fn.ResolvedImageURI}
	}
	writeJSON(w, http.StatusOK, map[string]any{"Configuration": toFunctionConfigResponse(fn), "Code": code, "Tags": fn.Tags})
}

// GetFunctionConfiguration handles GET /2015-03-31/functions/{name}/configuration.
// Returns the configuration object at the root level (no "Configuration" wrapper).
// Used by the TF AWS provider v5 waiter (WaitUntilFunctionActiveV2) to poll State.
func (h *Handler) GetFunctionConfiguration(w http.ResponseWriter, r *http.Request) {
	name, err := requestFunctionReference(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if fn.LastUpdateStatus == "" {
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	}
	writeJSON(w, http.StatusOK, toFunctionConfigResponse(fn))
}

// ListVersionsByFunction handles GET /2015-03-31/functions/{name}/versions.
// Terraform uses this to resolve the latest published (or $LATEST) version
// during Lambda function reads.
func (h *Handler) ListVersionsByFunction(w http.ResponseWriter, r *http.Request) {
	versions, err := h.svc.ListVersions(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	results := make([]map[string]any, 0, len(versions))
	for _, fn := range versions {
		results = append(results, toFunctionConfigResponse(fn))
	}
	writeJSON(w, http.StatusOK, map[string]any{"Versions": results})
}

// PublishVersion handles POST /2015-03-31/functions/{name}/versions
func (h *Handler) PublishVersion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description string `json:"Description,omitempty"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body")
			return
		}
	}
	result, err := h.svc.PublishVersion(r.Context(), r.PathValue("name"), req.Description)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toFunctionConfigResponse(result))
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
	functionResults := make([]map[string]any, 0, len(functions))
	for _, fn := range functions {
		functionResults = append(functionResults, toFunctionConfigResponse(fn))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"Functions": functionResults,
	})
}

// DeleteFunction handles DELETE /2015-03-31/functions/{name}
func (h *Handler) DeleteFunction(w http.ResponseWriter, r *http.Request) {
	name, err := requestFunctionReference(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if err := h.svc.DeleteFunction(r.Context(), name); err != nil {
		writeServiceError(w, err)
		return
	}
	setRequestID(w)
	w.WriteHeader(http.StatusNoContent)
}

// UpdateFunctionCode handles PUT /2015-03-31/functions/{name}/code
func (h *Handler) UpdateFunctionCode(w http.ResponseWriter, r *http.Request) {
	name, err := requestFunctionReference(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}

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

	var result *types.FunctionConfig
	if req.ImageUri != "" {
		result, err = h.svc.UpdateFunctionImage(r.Context(), name, req.ImageUri, req.Architectures)
	} else {
		result, err = h.svc.UpdateFunctionCode(r.Context(), name, code, req.Architectures)
	}
	if err != nil {
		writeServiceError(w, err)
		return
	}

	if req.Publish {
		result, err = h.svc.PublishVersion(r.Context(), name, "")
		if err != nil {
			writeServiceError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toFunctionConfigResponse(result))
}

// UpdateFunctionConfiguration handles PUT /2015-03-31/functions/{name}/configuration
func (h *Handler) UpdateFunctionConfiguration(w http.ResponseWriter, r *http.Request) {
	name, err := requestFunctionReference(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}

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
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toFunctionConfigResponse(result))
}

// Invoke handles POST /2015-03-31/functions/{name}/invocations
func (h *Handler) Invoke(w http.ResponseWriter, r *http.Request) {
	name, err := requestFunctionReference(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}

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
	if input.InvocationType != "RequestResponse" && input.InvocationType != "Event" && input.InvocationType != "DryRun" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid invocation type")
		return
	}
	if input.LogType != "" && input.LogType != "None" && input.LogType != "Tail" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid log type")
		return
	}

	correlationID := tracesvc.CorrelationIDFromHeaders(r.Header)
	if correlationID == "" {
		correlationID = tracesvc.CorrelationIDFromPayload(payload)
	}
	if correlationID == "" {
		correlationID = tracesvc.NewCorrelationID()
	}

	if input.InvocationType == "Event" {
		// Queue the invoke and answer 202 now, as AWS does. The queued invoke
		// waits for its result so the trace records how it really went.
		input.InvocationType = "RequestResponse"
		err := h.svc.Enqueue(name, func(ctx context.Context) {
			h.invokeAndTrace(ctx, name, input, correlationID)
		})
		if err != nil {
			status, code := invokeErrorCode(err)
			writeError(w, status, code, err.Error())
			return
		}
		setRequestID(w)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	output, err := h.invokeAndTrace(r.Context(), name, input, correlationID)
	if err != nil {
		status, code := invokeErrorCode(err)
		writeError(w, status, code, err.Error())
		return
	}

	if output.RequestID != "" {
		w.Header().Set("X-Amzn-RequestId", output.RequestID)
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
	setRequestID(w)
	w.WriteHeader(output.StatusCode)
	w.Write(output.Payload)
}

// invokeAndTrace invokes the function and records the invocation's trace
// once its late telemetry has arrived.
func (h *Handler) invokeAndTrace(ctx context.Context, name string, input *types.InvokeInput, correlationID string) (*types.InvokeOutput, error) {
	inv := h.collector.Begin()
	start := time.Now()
	output, err := h.svc.Invoke(ctx, input)
	durationMs := time.Since(start).Milliseconds()

	status := 200
	spanStatus := "ok"
	if err != nil {
		status = 500
		spanStatus = "error"
	} else if output != nil && output.FunctionError != "" {
		spanStatus = "error"
	}
	inv.Finish(func(subSpans []tracesvc.Span) {
		if h.traceStore == nil {
			return
		}
		h.traceStore.Add(&tracesvc.Trace{
			ID:            uuid.NewString()[:8],
			CorrelationID: correlationID,
			StartedAt:     start,
			DurationMs:    durationMs,
			Status:        status,
			Spans:         append([]tracesvc.Span{{Kind: "lambda", Name: name, DurationMs: durationMs, Status: spanStatus}}, subSpans...),
		})
	})
	return output, err
}

// PutFunctionConcurrency handles PUT /2017-10-31/functions/{name}/concurrency.
// TF provider v6 calls this endpoint using the 2017-10-31 API date prefix, which
// was not registered and fell through to the S3 handler returning XML.
func (h *Handler) PutFunctionConcurrency(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReservedConcurrentExecutions *int `json:"ReservedConcurrentExecutions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ReservedConcurrentExecutions == nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "ReservedConcurrentExecutions is required")
		return
	}
	if err := h.svc.PutFunctionConcurrency(r.PathValue("name"), *req.ReservedConcurrentExecutions); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"ReservedConcurrentExecutions": *req.ReservedConcurrentExecutions})
}

// DeleteFunctionConcurrency handles DELETE /2017-10-31/functions/{name}/concurrency.
func (h *Handler) DeleteFunctionConcurrency(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteFunctionConcurrency(r.PathValue("name")); err != nil {
		writeServiceError(w, err)
		return
	}
	setRequestID(w)
	w.WriteHeader(http.StatusNoContent)
}

// GetFunctionCodeSigningConfig handles
// GET /2020-06-30/functions/{name}/code-signing-config. Tarn has no code
// signing configs, so an existing function reports an empty ARN, as AWS does
// for a function with none attached.
func (h *Handler) GetFunctionCodeSigningConfig(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"CodeSigningConfigArn": "",
		"FunctionName":         fn.FunctionName,
	})
}

// DeleteFunctionCodeSigningConfig handles
// DELETE /2020-06-30/functions/{name}/code-signing-config.
func (h *Handler) DeleteFunctionCodeSigningConfig(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := h.svc.GetFunction(name); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
		return
	}
	setRequestID(w)
	w.WriteHeader(http.StatusNoContent)
}

// CreateAlias handles POST /2015-03-31/functions/{name}/aliases
func (h *Handler) CreateAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"Name"`
		FunctionVersion string `json:"FunctionVersion"`
		Description     string `json:"Description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body")
		return
	}
	result, err := h.svc.CreateAlias(r.PathValue("name"), lambdasvc.Alias{Name: req.Name, FunctionVersion: req.FunctionVersion, Description: req.Description})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// GetAlias handles GET /2015-03-31/functions/{name}/aliases/{aliasName}
func (h *Handler) GetAlias(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetAlias(r.PathValue("name"), r.PathValue("aliasName"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ListAliases handles GET /2015-03-31/functions/{name}/aliases
func (h *Handler) ListAliases(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListAliases(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"Aliases": result})
}

// UpdateAlias handles PUT /2015-03-31/functions/{name}/aliases/{aliasName}
func (h *Handler) UpdateAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FunctionVersion *string `json:"FunctionVersion,omitempty"`
		Description     *string `json:"Description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body")
		return
	}
	result, err := h.svc.UpdateAlias(r.PathValue("name"), r.PathValue("aliasName"), req.FunctionVersion, req.Description)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// DeleteAlias handles DELETE /2015-03-31/functions/{name}/aliases/{aliasName}
func (h *Handler) DeleteAlias(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteAlias(r.PathValue("name"), r.PathValue("aliasName")); err != nil {
		writeServiceError(w, err)
		return
	}
	setRequestID(w)
	w.WriteHeader(http.StatusNoContent)
}

// AddPermission handles POST /2015-03-31/functions/{name}/policy
func (h *Handler) AddPermission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StatementID   string `json:"StatementId"`
		Action        string `json:"Action"`
		Principal     string `json:"Principal"`
		SourceARN     string `json:"SourceArn"`
		SourceAccount string `json:"SourceAccount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body")
		return
	}
	permission := lambdasvc.Permission{StatementID: req.StatementID, Action: req.Action, Principal: req.Principal, SourceARN: req.SourceARN, SourceAccount: req.SourceAccount}
	fn, err := h.svc.GetFunction(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if err := h.svc.AddPermission(r.PathValue("name"), permission); err != nil {
		writeServiceError(w, err)
		return
	}
	raw, _ := json.Marshal(permissionResponse(permission, fn.FunctionArn))
	writeJSON(w, http.StatusOK, map[string]string{"Statement": string(raw)})
}

// GetPolicy handles GET /2015-03-31/functions/{name}/policy
func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	fn, err := h.svc.GetFunction(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	permissions, err := h.svc.GetPolicy(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if len(permissions) == 0 {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "The resource policy for the function does not exist.")
		return
	}
	statements := make([]map[string]any, 0, len(permissions))
	for _, permission := range permissions {
		statements = append(statements, permissionResponse(permission, fn.FunctionArn))
	}
	raw, _ := json.Marshal(map[string]any{"Version": "2012-10-17", "Id": "default", "Statement": statements})
	writeJSON(w, http.StatusOK, map[string]string{"Policy": string(raw), "RevisionId": "1"})
}

// RemovePermission handles DELETE /2015-03-31/functions/{name}/policy/{statementId}
func (h *Handler) RemovePermission(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RemovePermission(r.PathValue("name"), r.PathValue("statementId")); err != nil {
		writeServiceError(w, err)
		return
	}
	setRequestID(w)
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

// tagTargetName resolves the function name for tagging endpoints. It supports
// both the legacy Tarn route (/2015-03-31/functions/{name}/tags) and the
// AWS-standard routes (/2015-03-31/tags/{resource} and
// /2017-03-31/tags/{resource}) where {resource} is the function ARN
// (e.g. arn:aws:lambda:us-east-1:123456789012:function:my-fn).
func (h *Handler) tagTargetName(r *http.Request) string {
	name := r.PathValue("name")
	if name == "" {
		name = r.PathValue("resource")
	}
	// The AWS-standard routes carry the URL-escaped ARN in {resource}.
	// PathValue is already decoded, but unescape defensively in case the
	// SDK double-encodes colons.
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	if base, _, err := lambdasvc.ParseFunctionReference(name, ""); err == nil {
		return base
	}
	return name
}

// parseTagKeys collects tag keys from the query string, supporting both
// comma-separated (tagKeys=a,b) and repeated (tagKeys=a&tagKeys=b) forms,
// as emitted by different AWS SDK versions.
func parseTagKeys(r *http.Request) []string {
	rawValues, ok := r.URL.Query()["tagKeys"]
	if !ok || len(rawValues) == 0 {
		return nil
	}
	var keys []string
	for _, raw := range rawValues {
		for _, k := range strings.Split(raw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// ListTags handles GET /2015-03-31/functions/{name}/tags and the AWS-standard
// GET /2015-03-31/tags/{resource} and GET /2017-03-31/tags/{resource}.
func (h *Handler) ListTags(w http.ResponseWriter, r *http.Request) {
	name := h.tagTargetName(r)
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	tags := fn.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"Tags": tags})
}

// TagResource handles POST /2015-03-31/functions/{name}/tags and the
// AWS-standard POST /2015-03-31/tags/{resource} and
// POST /2017-03-31/tags/{resource} (body: {"Tags": {...}}).
func (h *Handler) TagResource(w http.ResponseWriter, r *http.Request) {
	name := h.tagTargetName(r)
	var req struct {
		Tags map[string]string `json:"Tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body")
		return
	}
	if err := h.svc.TagResource(name, req.Tags); err != nil {
		writeServiceError(w, err)
		return
	}
	setRequestID(w)
	w.WriteHeader(http.StatusNoContent)
}

// UntagResource handles DELETE /2015-03-31/functions/{name}/tags and the
// AWS-standard DELETE /2015-03-31/tags/{resource} and
// DELETE /2017-03-31/tags/{resource} (?tagKeys=...).
func (h *Handler) UntagResource(w http.ResponseWriter, r *http.Request) {
	name := h.tagTargetName(r)
	tagKeys := parseTagKeys(r)
	if len(tagKeys) == 0 {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "tagKeys parameter is required")
		return
	}
	if err := h.svc.UntagResource(name, tagKeys); err != nil {
		writeServiceError(w, err)
		return
	}
	setRequestID(w)
	w.WriteHeader(http.StatusNoContent)
}

// --- Layer handlers ---

// PublishLayerVersion handles POST /2018-10-31/layers/{layerName}/versions
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

// GetLayerVersion handles GET /2018-10-31/layers/{layerName}/versions/{version}
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
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ListLayers handles GET /2018-10-31/layers
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

// ListLayerVersions handles GET /2018-10-31/layers/{layerName}/versions
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

// DeleteLayerVersion handles DELETE /2018-10-31/layers/{layerName}/versions/{version}
func (h *Handler) DeleteLayerVersion(w http.ResponseWriter, r *http.Request) {
	layerName := r.PathValue("layerName")
	versionStr := r.PathValue("version")
	version, err := strconv.ParseInt(versionStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid version number")
		return
	}

	if err := h.svc.DeleteLayerVersion(layerName, version); err != nil {
		writeServiceError(w, err)
		return
	}

	setRequestID(w)
	w.WriteHeader(http.StatusNoContent)
}

// --- Request/Response types ---

type createFunctionRequest struct {
	Architectures    []string                   `json:"Architectures,omitempty"`
	FunctionName     string                     `json:"FunctionName"`
	Runtime          string                     `json:"Runtime"`
	Handler          string                     `json:"Handler"`
	Role             string                     `json:"Role"`
	Description      string                     `json:"Description,omitempty"`
	Timeout          int                        `json:"Timeout,omitempty"`
	MemorySize       int                        `json:"MemorySize,omitempty"`
	Code             *codeInput                 `json:"Code,omitempty"`
	Environment      *envInput                  `json:"Environment,omitempty"`
	Layers           []string                   `json:"Layers,omitempty"`
	Tags             map[string]string          `json:"Tags,omitempty"`
	DeadLetterConfig *types.DeadLetterConfig    `json:"DeadLetterConfig,omitempty"`
	KMSKeyArn        string                     `json:"KMSKeyArn,omitempty"`
	PackageType      string                     `json:"PackageType,omitempty"`
	ImageConfig      *types.FunctionImageConfig `json:"ImageConfig,omitempty"`
	Publish          bool                       `json:"Publish,omitempty"`
}

type codeInput struct {
	ZipFile  string `json:"ZipFile,omitempty"`  // base64-encoded zip
	S3Bucket string `json:"S3Bucket,omitempty"` // S3 bucket containing deployment package
	S3Key    string `json:"S3Key,omitempty"`    // S3 key of deployment package zip
	ImageUri string `json:"ImageUri,omitempty"` // container image URI (PackageType Image)
}

type envInput struct {
	Variables map[string]string `json:"Variables,omitempty"`
}

type updateCodeRequest struct {
	Architectures []string `json:"Architectures,omitempty"`
	ImageUri      string   `json:"ImageUri,omitempty"`
	Publish       bool     `json:"Publish,omitempty"`
	ZipFile       string   `json:"ZipFile,omitempty"`  // base64-encoded zip
	S3Bucket      string   `json:"S3Bucket,omitempty"` // S3 bucket containing deployment package
	S3Key         string   `json:"S3Key,omitempty"`    // S3 key of deployment package zip
}

type publishLayerRequest struct {
	Description        string     `json:"Description,omitempty"`
	CompatibleRuntimes []string   `json:"CompatibleRuntimes,omitempty"`
	Content            *codeInput `json:"Content,omitempty"`
}

func toFunctionConfigResponse(fn *types.FunctionConfig) map[string]any {
	if fn == nil {
		return map[string]any{}
	}

	packageType := fn.PackageType
	if packageType == "" {
		packageType = "Zip"
	}
	result := map[string]any{
		"Architectures": fn.Architectures,
		"FunctionName":  fn.FunctionName,
		"FunctionArn":   fn.FunctionArn,
		"Runtime":       fn.Runtime,
		"Handler":       fn.Handler,
		"Role":          fn.Role,
		"Timeout":       fn.Timeout,
		"MemorySize":    fn.MemorySize,
		"State":         fn.State,
		"CodeSha256":    fn.CodeSHA256,
		"CodeSize":      fn.CodeSize,
		"Version":       fn.Version,
		"LastModified":  fn.LastModified,
		"PackageType":   packageType,
	}

	if fn.Description != "" {
		result["Description"] = fn.Description
	}
	if len(fn.Environment) > 0 {
		result["Environment"] = types.EnvVars{Variables: fn.Environment}
	}
	if len(fn.Tags) > 0 {
		result["Tags"] = fn.Tags
	}
	if fn.DeadLetterConfig != nil {
		result["DeadLetterConfig"] = fn.DeadLetterConfig
	}
	if fn.KMSKeyArn != "" {
		result["KMSKeyArn"] = fn.KMSKeyArn
	}
	if fn.LastUpdateStatus != "" {
		result["LastUpdateStatus"] = fn.LastUpdateStatus
	}
	if len(fn.Layers) > 0 {
		layers := make([]map[string]any, 0, len(fn.Layers))
		for _, arn := range fn.Layers {
			layerArn := strings.TrimSpace(arn)
			if layerArn == "" {
				continue
			}
			layers = append(layers, map[string]any{
				"Arn":      layerArn,
				"CodeSize": int64(0),
			})
		}
		if len(layers) > 0 {
			result["Layers"] = layers
		}
	}

	if fn.ImageConfig != nil {
		result["ImageConfigResponse"] = map[string]any{"ImageConfig": fn.ImageConfig}
	}
	return result
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
	setRequestID(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError emits the Lambda REST-JSON error shape. smithy-go resolves the
// typed exception from the X-Amzn-ErrorType header first, then the body Type
// or __type fields; we set all of them so every SDK finds the code.
func writeError(w http.ResponseWriter, status int, code string, message string) {
	setRequestID(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Amzn-ErrorType", code)
	w.WriteHeader(status)
	errType := "User"
	if status >= 500 {
		errType = "Service"
	}
	json.NewEncoder(w).Encode(map[string]string{
		"__type":  code,
		"Type":    errType,
		"Message": message,
		"message": message,
	})
}

// setRequestID sets X-Amzn-RequestId unless the handler already supplied one.
func setRequestID(w http.ResponseWriter) {
	if w.Header().Get("X-Amzn-RequestId") == "" {
		w.Header().Set("X-Amzn-RequestId", uuid.NewString())
	}
}

// invokeErrorCode maps an Invoke service error to its AWS status and code.
func invokeErrorCode(err error) (int, string) {
	switch {
	case errors.Is(err, lambdasvc.ErrInvalidParameter):
		return http.StatusBadRequest, "InvalidParameterValueException"
	case errors.Is(err, lambdasvc.ErrResourceConflict):
		return http.StatusConflict, "ResourceConflictException"
	case errors.Is(err, lambdasvc.ErrFunctionNotFound):
		return http.StatusNotFound, "ResourceNotFoundException"
	case errors.Is(err, lambdasvc.ErrFunctionNotReady):
		return http.StatusBadGateway, "ResourceNotReadyException"
	case errors.Is(err, lambdasvc.ErrTooManyRequests):
		return http.StatusTooManyRequests, "TooManyRequestsException"
	default:
		return http.StatusInternalServerError, "ServiceException"
	}
}

func writeServiceError(w http.ResponseWriter, err error) {
	status, code := invokeErrorCode(err)
	writeError(w, status, code, err.Error())
}

func requestFunctionReference(r *http.Request) (string, error) {
	name, qualifier, err := lambdasvc.ParseFunctionReference(r.PathValue("name"), r.URL.Query().Get("Qualifier"))
	if err != nil {
		return "", err
	}
	if qualifier != "" {
		name += ":" + qualifier
	}
	return name, nil
}

func permissionResponse(permission lambdasvc.Permission, arn string) map[string]any {
	result := map[string]any{"Sid": permission.StatementID, "Effect": "Allow", "Action": permission.Action, "Resource": arn}
	if permission.Principal != "" {
		result["Principal"] = map[string]string{"Service": permission.Principal}
	}
	conditions := map[string]any{}
	if permission.SourceARN != "" {
		conditions["ArnLike"] = map[string]string{"AWS:SourceArn": permission.SourceARN}
	}
	if permission.SourceAccount != "" {
		conditions["StringEquals"] = map[string]string{"AWS:SourceAccount": permission.SourceAccount}
	}
	if len(conditions) > 0 {
		result["Condition"] = conditions
	}
	return result
}

func (h *Handler) GetFunctionConcurrency(w http.ResponseWriter, r *http.Request) {
	value, err := h.svc.GetFunctionConcurrency(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	response := map[string]int{}
	if value != nil {
		response["ReservedConcurrentExecutions"] = *value
	}
	writeJSON(w, http.StatusOK, response)
}
