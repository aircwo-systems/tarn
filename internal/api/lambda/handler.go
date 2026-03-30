package lambda

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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

// aliasConfig holds an in-memory Lambda alias.
type aliasConfig struct {
	Name            string `json:"Name"`
	FunctionName    string `json:"FunctionName"`
	FunctionVersion string `json:"FunctionVersion"`
	Description     string `json:"Description,omitempty"`
	AliasArn        string `json:"AliasArn"`
	RevisionId      string `json:"RevisionId"`
}

// Handler implements HTTP handlers for the Lambda API.
type Handler struct {
	svc        *lambdasvc.Service
	s3         s3Getter
	traceStore *tracesvc.Store
	collector  *tracesvc.Collector
	mu         sync.Mutex
	policies   map[string]map[string]policyStatement
	// Version publishing: function name → next version counter
	versionSeq map[string]int
	// Published versions: function name → version string → config snapshot
	versions map[string]map[string]map[string]any
	// Aliases: function name → alias name → alias config
	aliases map[string]map[string]*aliasConfig
}

type policyStatement struct {
	StatementID   string
	Action        string
	Principal     string
	SourceARN     string
	SourceAccount string
}

// NewHandler creates a new Lambda API handler.
func NewHandler(svc *lambdasvc.Service, s3 s3Getter) *Handler {
	return &Handler{
		svc:        svc,
		s3:         s3,
		policies:   make(map[string]map[string]policyStatement),
		versionSeq: make(map[string]int),
		versions:   make(map[string]map[string]map[string]any),
		aliases:    make(map[string]map[string]*aliasConfig),
	}
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

	writeJSON(w, http.StatusCreated, toFunctionConfigResponse(result))
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
		"Configuration": toFunctionConfigResponse(fn),
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
	writeJSON(w, http.StatusOK, toFunctionConfigResponse(fn))
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
		"Versions": []map[string]any{toFunctionConfigResponse(fn)},
	})
}

// PublishVersion handles POST /2015-03-31/functions/{name}/versions
func (h *Handler) PublishVersion(w http.ResponseWriter, r *http.Request) {
	name := normalizeFunctionName(r.PathValue("name"))
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
		return
	}

	var req struct {
		Description string `json:"Description,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.mu.Lock()
	h.versionSeq[name]++
	ver := strconv.Itoa(h.versionSeq[name])

	resp := toFunctionConfigResponse(fn)
	resp["Version"] = ver
	if req.Description != "" {
		resp["Description"] = req.Description
	}

	if h.versions[name] == nil {
		h.versions[name] = make(map[string]map[string]any)
	}
	h.versions[name][ver] = resp
	h.mu.Unlock()

	writeJSON(w, http.StatusCreated, resp)
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

	writeJSON(w, http.StatusOK, toFunctionConfigResponse(result))
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

	writeJSON(w, http.StatusOK, toFunctionConfigResponse(result))
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

// CreateAlias handles POST /2015-03-31/functions/{name}/aliases
func (h *Handler) CreateAlias(w http.ResponseWriter, r *http.Request) {
	name := normalizeFunctionName(r.PathValue("name"))
	fn, err := h.svc.GetFunction(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
		return
	}

	var req struct {
		Name            string `json:"Name"`
		FunctionVersion string `json:"FunctionVersion"`
		Description     string `json:"Description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "Alias name is required")
		return
	}

	h.mu.Lock()
	if h.aliases[name] == nil {
		h.aliases[name] = make(map[string]*aliasConfig)
	}
	if _, exists := h.aliases[name][req.Name]; exists {
		h.mu.Unlock()
		writeError(w, http.StatusConflict, "ResourceConflictException", "Alias already exists: "+req.Name)
		return
	}
	alias := &aliasConfig{
		Name:            req.Name,
		FunctionName:    name,
		FunctionVersion: req.FunctionVersion,
		Description:     req.Description,
		AliasArn:        fn.FunctionArn + ":" + req.Name,
		RevisionId:      uuid.NewString(),
	}
	h.aliases[name][req.Name] = alias
	h.mu.Unlock()

	writeJSON(w, http.StatusCreated, alias)
}

// GetAlias handles GET /2015-03-31/functions/{name}/aliases/{aliasName}
func (h *Handler) GetAlias(w http.ResponseWriter, r *http.Request) {
	name := normalizeFunctionName(r.PathValue("name"))
	aliasName := r.PathValue("aliasName")

	if _, err := h.svc.GetFunction(name); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
		return
	}

	h.mu.Lock()
	alias := h.aliases[name][aliasName]
	h.mu.Unlock()

	if alias == nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Alias not found: "+aliasName)
		return
	}
	writeJSON(w, http.StatusOK, alias)
}

// ListAliases handles GET /2015-03-31/functions/{name}/aliases
func (h *Handler) ListAliases(w http.ResponseWriter, r *http.Request) {
	name := normalizeFunctionName(r.PathValue("name"))
	if _, err := h.svc.GetFunction(name); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
		return
	}

	h.mu.Lock()
	fnAliases := h.aliases[name]
	result := make([]*aliasConfig, 0, len(fnAliases))
	for _, a := range fnAliases {
		result = append(result, a)
	}
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"Aliases": result})
}

// UpdateAlias handles PUT /2015-03-31/functions/{name}/aliases/{aliasName}
func (h *Handler) UpdateAlias(w http.ResponseWriter, r *http.Request) {
	name := normalizeFunctionName(r.PathValue("name"))
	aliasName := r.PathValue("aliasName")

	if _, err := h.svc.GetFunction(name); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
		return
	}

	var req struct {
		FunctionVersion string `json:"FunctionVersion,omitempty"`
		Description     string `json:"Description,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.mu.Lock()
	alias := h.aliases[name][aliasName]
	if alias == nil {
		h.mu.Unlock()
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Alias not found: "+aliasName)
		return
	}
	if req.FunctionVersion != "" {
		alias.FunctionVersion = req.FunctionVersion
	}
	if req.Description != "" {
		alias.Description = req.Description
	}
	alias.RevisionId = uuid.NewString()
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, alias)
}

// DeleteAlias handles DELETE /2015-03-31/functions/{name}/aliases/{aliasName}
func (h *Handler) DeleteAlias(w http.ResponseWriter, r *http.Request) {
	name := normalizeFunctionName(r.PathValue("name"))
	aliasName := r.PathValue("aliasName")

	if _, err := h.svc.GetFunction(name); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+name)
		return
	}

	h.mu.Lock()
	if fnAliases, ok := h.aliases[name]; ok {
		delete(fnAliases, aliasName)
	}
	h.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// AddPermission handles POST /2015-03-31/functions/{name}/policy
func (h *Handler) AddPermission(w http.ResponseWriter, r *http.Request) {
	functionName := normalizeFunctionName(r.PathValue("name"))
	fn, err := h.svc.GetFunction(functionName)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+functionName)
		return
	}

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
	if strings.TrimSpace(req.StatementID) == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "StatementId is required")
		return
	}

	h.mu.Lock()
	if _, ok := h.policies[functionName]; !ok {
		h.policies[functionName] = make(map[string]policyStatement)
	}
	h.policies[functionName][req.StatementID] = policyStatement{
		StatementID:   req.StatementID,
		Action:        req.Action,
		Principal:     req.Principal,
		SourceARN:     req.SourceARN,
		SourceAccount: req.SourceAccount,
	}
	h.mu.Unlock()

	statement := map[string]any{
		"Sid":      req.StatementID,
		"Effect":   "Allow",
		"Action":   req.Action,
		"Resource": fn.FunctionArn,
	}
	if strings.TrimSpace(req.Principal) != "" {
		statement["Principal"] = map[string]string{"Service": req.Principal}
	}
	if strings.TrimSpace(req.SourceARN) != "" {
		statement["Condition"] = map[string]any{
			"ArnLike": map[string]string{"AWS:SourceArn": req.SourceARN},
		}
	}

	raw, _ := json.Marshal(statement)
	writeJSON(w, http.StatusOK, map[string]string{"Statement": string(raw)})
}

// GetPolicy handles GET /2015-03-31/functions/{name}/policy
func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	functionName := normalizeFunctionName(r.PathValue("name"))
	fn, err := h.svc.GetFunction(functionName)
	if err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+functionName)
		return
	}

	h.mu.Lock()
	functionPolicies := h.policies[functionName]
	h.mu.Unlock()

	if len(functionPolicies) == 0 {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "The resource policy for the function does not exist.")
		return
	}

	statements := make([]map[string]any, 0, len(functionPolicies))
	for _, stmt := range functionPolicies {
		entry := map[string]any{
			"Sid":      stmt.StatementID,
			"Effect":   "Allow",
			"Action":   stmt.Action,
			"Resource": fn.FunctionArn,
		}
		if strings.TrimSpace(stmt.Principal) != "" {
			entry["Principal"] = map[string]string{"Service": stmt.Principal}
		}
		if strings.TrimSpace(stmt.SourceARN) != "" {
			entry["Condition"] = map[string]any{
				"ArnLike": map[string]string{"AWS:SourceArn": stmt.SourceARN},
			}
		}
		statements = append(statements, entry)
	}

	policy := map[string]any{
		"Version":   "2012-10-17",
		"Id":        "default",
		"Statement": statements,
	}
	raw, _ := json.Marshal(policy)
	writeJSON(w, http.StatusOK, map[string]string{"Policy": string(raw), "RevisionId": "1"})
}

// RemovePermission handles DELETE /2015-03-31/functions/{name}/policy/{statementId}
func (h *Handler) RemovePermission(w http.ResponseWriter, r *http.Request) {
	functionName := normalizeFunctionName(r.PathValue("name"))
	statementID := strings.TrimSpace(r.PathValue("statementId"))
	if statementID == "" {
		writeError(w, http.StatusBadRequest, "InvalidParameterValueException", "StatementId is required")
		return
	}

	if _, err := h.svc.GetFunction(functionName); err != nil {
		writeError(w, http.StatusNotFound, "ResourceNotFoundException", "Function not found: "+functionName)
		return
	}

	h.mu.Lock()
	if functionPolicies, ok := h.policies[functionName]; ok {
		delete(functionPolicies, statementID)
	}
	h.mu.Unlock()

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

func toFunctionConfigResponse(fn *types.FunctionConfig) map[string]any {
	if fn == nil {
		return map[string]any{}
	}

	result := map[string]any{
		"FunctionName": fn.FunctionName,
		"FunctionArn":  fn.FunctionArn,
		"Runtime":      fn.Runtime,
		"Handler":      fn.Handler,
		"Role":         fn.Role,
		"Timeout":      fn.Timeout,
		"MemorySize":   fn.MemorySize,
		"State":        fn.State,
		"CodeSha256":   fn.CodeSHA256,
		"CodeSize":     fn.CodeSize,
		"Version":      fn.Version,
		"LastModified": fn.LastModified,
	}

	if fn.Description != "" {
		result["Description"] = fn.Description
	}
	if len(fn.Environment) > 0 {
		result["Environment"] = fn.Environment
	}
	if len(fn.Tags) > 0 {
		result["Tags"] = fn.Tags
	}
	if fn.DeadLetterConfig != nil {
		result["DeadLetterConfig"] = fn.DeadLetterConfig
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

func normalizeFunctionName(name string) string {
	trimmed := strings.TrimSpace(name)
	const marker = ":function:"
	idx := strings.Index(trimmed, marker)
	if idx < 0 {
		return trimmed
	}
	tail := trimmed[idx+len(marker):]
	if colon := strings.IndexByte(tail, ':'); colon >= 0 {
		tail = tail[:colon]
	}
	return strings.TrimSpace(tail)
}
