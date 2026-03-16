package apigatewayv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openstack-project/openstack/internal/config"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	tracesvc "github.com/openstack-project/openstack/internal/trace"
	"github.com/openstack-project/openstack/pkg/types"
)

const (
	integrationTypeAWS      = "AWS"
	integrationTypeAWSProxy = "AWS_PROXY"
	integrationTypeHTTP     = "HTTP"
	integrationTypeMock     = "MOCK"
)

// SQSSendFunc sends a message to an SQS queue.
// groupId and dedupId are required for FIFO queues; pass empty strings for standard queues.
type SQSSendFunc func(queueName, body, groupId, dedupId string) (messageID, md5 string, err error)

// PatchOp is a single JSON Patch operation (RFC 6902) as used by UpdateIntegration.
type PatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

// Service implements API Gateway REST API (v1) behaviour.
type Service struct {
	cfg        *config.Config
	lambda     *lambdasvc.Service
	sqsSend    SQSSendFunc
	store      *Store
	traceStore *tracesvc.Store
	collector  *tracesvc.Collector
}

// InvokeInput captures request data for v1 invocations.
type InvokeInput struct {
	APIID    string
	Stage    string
	Method   string
	Path     string
	RawQuery string
	Query    url.Values
	Headers  http.Header
	Body     []byte
}

// InvokeOutput captures v1 invoke response data.
type InvokeOutput struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// NewService creates a new v1 REST API service.
func NewService(cfg *config.Config, lambdaSvc *lambdasvc.Service, sqsSend SQSSendFunc) *Service {
	return &Service{
		cfg:     cfg,
		lambda:  lambdaSvc,
		sqsSend: sqsSend,
		store:   NewStore(cfg),
	}
}

// SetTraceStore attaches a trace store.
func (s *Service) SetTraceStore(ts *tracesvc.Store) { s.traceStore = ts }

// SetCollector attaches a trace collector.
func (s *Service) SetCollector(c *tracesvc.Collector) { s.collector = c }

// Init loads persisted state.
func (s *Service) Init() error {
	return s.store.Init()
}

// CreateAPI creates a new REST API with an auto-created root resource.
func (s *Service) CreateAPI(name, description string, tags map[string]string) (*types.RestAPI, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}

	// Support LocalStack-compatible _custom_id_ tag for stable IDs in Terraform
	apiID := randomID(10)
	hasCustomID := false
	if customID, ok := tags["_custom_id_"]; ok && customID != "" {
		apiID = customID
		hasCustomID = true
	}

	// Idempotency: if a stable custom ID was requested and the API already
	// exists (e.g. a previous apply whose SDK deserialization failed), return
	// the existing record rather than an error.
	if hasCustomID {
		if existing, err := s.store.GetAPI(apiID); err == nil {
			return existing, nil
		}
	}

	rootID := randomID(10)
	now := time.Now().UTC()

	api := &types.RestAPI{
		ID:             apiID,
		Name:           name,
		Description:    description,
		RootResourceID: rootID,
		APIArn:         fmt.Sprintf("arn:aws:apigateway:%s::/restapis/%s", s.cfg.Region, apiID),
		Tags:           cloneTags(tags),
		CreatedDate:    types.UnixTime{Time: now},
	}
	rootRes := &types.RestResource{
		ID:        rootID,
		RestAPIID: apiID,
		PathPart:  "",
		Path:      "/",
	}
	if err := s.store.CreateAPI(api, rootRes); err != nil {
		return nil, err
	}
	return s.store.GetAPI(apiID)
}

// ListAPIs returns all REST APIs.
func (s *Service) ListAPIs() []*types.RestAPI {
	return s.store.ListAPIs()
}

// GetAPI returns a REST API by ID, falling back to name lookup.
func (s *Service) GetAPI(apiID string) (*types.RestAPI, error) {
	api, err := s.store.GetAPI(apiID)
	if err == nil {
		return api, nil
	}
	// Fall back to name lookup for compatibility
	return s.store.GetAPIByName(apiID)
}

// DeleteAPI deletes a REST API and all nested resources.
func (s *Service) DeleteAPI(apiID string) error {
	return s.store.DeleteAPI(apiID)
}

// CreateResource creates a child resource under parentID.
func (s *Service) CreateResource(apiID, parentID, pathPart string) (*types.RestResource, error) {
	if pathPart == "" {
		return nil, errors.New("pathPart is required")
	}
	parent, err := s.store.GetResource(apiID, parentID)
	if err != nil {
		return nil, fmt.Errorf("parent resource not found: %w", err)
	}
	parentPath := parent.Path
	if parentPath == "/" {
		parentPath = ""
	}
	res := &types.RestResource{
		ID:        randomID(10),
		RestAPIID: apiID,
		ParentID:  parentID,
		PathPart:  pathPart,
		Path:      parentPath + "/" + pathPart,
	}
	if err := s.store.CreateResource(apiID, res); err != nil {
		return nil, err
	}
	return s.store.GetResource(apiID, res.ID)
}

// ListResources returns all resources for an API.
func (s *Service) ListResources(apiID string) ([]*types.RestResource, error) {
	return s.store.ListResources(apiID)
}

// GetResource returns a resource by ID.
func (s *Service) GetResource(apiID, resourceID string) (*types.RestResource, error) {
	return s.store.GetResource(apiID, resourceID)
}

// DeleteResource removes a resource.
func (s *Service) DeleteResource(apiID, resourceID string) error {
	api, err := s.store.GetAPI(apiID)
	if err != nil {
		return err
	}
	if api.RootResourceID == resourceID {
		return errors.New("cannot delete root resource")
	}
	return s.store.DeleteResource(apiID, resourceID)
}

// PutMethod creates or replaces a method on a resource.
func (s *Service) PutMethod(apiID, resourceID, httpMethod, authorizationType string, requestParameters map[string]bool) (*types.RestMethod, error) {
	if httpMethod == "" {
		return nil, errors.New("httpMethod is required")
	}
	if _, err := s.store.GetResource(apiID, resourceID); err != nil {
		return nil, fmt.Errorf("resource not found: %w", err)
	}
	m := &types.RestMethod{
		HTTPMethod:        strings.ToUpper(httpMethod),
		ResourceID:        resourceID,
		RestAPIID:         apiID,
		AuthorizationType: authorizationType,
		RequestParameters: requestParameters,
	}
	if err := s.store.PutMethod(apiID, m); err != nil {
		return nil, err
	}
	return s.store.GetMethod(apiID, resourceID, m.HTTPMethod)
}

// GetMethod returns a method.
func (s *Service) GetMethod(apiID, resourceID, httpMethod string) (*types.RestMethod, error) {
	return s.store.GetMethod(apiID, resourceID, strings.ToUpper(httpMethod))
}

// DeleteMethod removes a method.
func (s *Service) DeleteMethod(apiID, resourceID, httpMethod string) error {
	return s.store.DeleteMethod(apiID, resourceID, strings.ToUpper(httpMethod))
}

// PatchMethod applies JSON Patch operations (RFC 6902) to an existing method.
// Terraform calls this as UpdateMethod when request_parameters change.
func (s *Service) PatchMethod(apiID, resourceID, httpMethod string, ops []PatchOp) (*types.RestMethod, error) {
	httpMethod = strings.ToUpper(httpMethod)
	method, err := s.store.GetMethod(apiID, resourceID, httpMethod)
	if err != nil {
		return nil, fmt.Errorf("method not found: %w", err)
	}
	for _, op := range ops {
		path := op.Path
		switch {
		case strings.HasPrefix(path, "/requestParameters/"):
			key := jsonPointerDecode(strings.TrimPrefix(path, "/requestParameters/"))
			if op.Op == "remove" {
				delete(method.RequestParameters, key)
			} else {
				if method.RequestParameters == nil {
					method.RequestParameters = map[string]bool{}
				}
				method.RequestParameters[key] = op.Value != "false"
			}
		case path == "/authorizationType" && (op.Op == "replace" || op.Op == "add"):
			method.AuthorizationType = op.Value
		}
	}
	if err := s.store.PutMethod(apiID, method); err != nil {
		return nil, err
	}
	return s.store.GetMethod(apiID, resourceID, httpMethod)
}

// PutIntegration creates or replaces an integration on a method.
func (s *Service) PutIntegration(apiID, resourceID, httpMethod string, intType, intHTTPMethod, uri string, requestParameters map[string]string, requestTemplates map[string]string) (*types.RestIntegration, error) {
	if intType == "" {
		return nil, errors.New("type is required")
	}
	httpMethod = strings.ToUpper(httpMethod)
	if _, err := s.store.GetMethod(apiID, resourceID, httpMethod); err != nil {
		return nil, fmt.Errorf("method not found: %w", err)
	}

	integ := &types.RestIntegration{
		Type:              intType,
		HTTPMethod:        intHTTPMethod,
		URI:               uri,
		ResourceID:        resourceID,
		RestAPIID:         apiID,
		MethodHTTPMethod:  httpMethod,
		RequestParameters: requestParameters,
		RequestTemplates:  requestTemplates,
	}

	switch intType {
	case integrationTypeAWSProxy:
		if uri != "" {
			_, fnName, err := parseLambdaIntegrationURI(uri)
			if err != nil {
				return nil, err
			}
			// Do not validate function existence here — AWS does not check at
			// integration creation time, only at invoke time.
			integ.LambdaFunctionName = fnName
		}
	case integrationTypeAWS:
		if uri != "" {
			queueName, err := parseSQSIntegrationPath(uri)
			if err != nil {
				// Not necessarily SQS — could be another AWS service; store as-is
				_ = err
			} else {
				integ.SQSQueueName = queueName
			}
		}
	}

	if err := s.store.PutIntegration(apiID, integ); err != nil {
		return nil, err
	}
	return s.store.GetIntegration(apiID, resourceID, httpMethod)
}

// PatchIntegration applies JSON Patch operations to an existing integration (UpdateIntegration).
func (s *Service) PatchIntegration(apiID, resourceID, httpMethod string, ops []PatchOp) (*types.RestIntegration, error) {
	httpMethod = strings.ToUpper(httpMethod)
	integ, err := s.store.GetIntegration(apiID, resourceID, httpMethod)
	if err != nil {
		return nil, fmt.Errorf("integration not found: %w", err)
	}

	for _, op := range ops {
		if op.Op != "replace" && op.Op != "add" {
			continue
		}
		path := op.Path
		switch {
		case path == "/uri":
			integ.URI = op.Value
			// Re-resolve backend name from updated URI
			switch integ.Type {
			case integrationTypeAWSProxy:
				if _, fnName, err := parseLambdaIntegrationURI(op.Value); err == nil {
					integ.LambdaFunctionName = fnName
				}
			case integrationTypeAWS:
				if q, err := parseSQSIntegrationPath(op.Value); err == nil {
					integ.SQSQueueName = q
				}
			}
		case path == "/type":
			integ.Type = op.Value
		case path == "/httpMethod":
			integ.HTTPMethod = op.Value
		case strings.HasPrefix(path, "/requestParameters/"):
			key := jsonPointerDecode(strings.TrimPrefix(path, "/requestParameters/"))
			if integ.RequestParameters == nil {
				integ.RequestParameters = map[string]string{}
			}
			integ.RequestParameters[key] = op.Value
		case strings.HasPrefix(path, "/requestTemplates/"):
			key := jsonPointerDecode(strings.TrimPrefix(path, "/requestTemplates/"))
			if integ.RequestTemplates == nil {
				integ.RequestTemplates = map[string]string{}
			}
			integ.RequestTemplates[key] = op.Value
		}
	}

	if err := s.store.PutIntegration(apiID, integ); err != nil {
		return nil, err
	}
	return s.store.GetIntegration(apiID, resourceID, httpMethod)
}

// jsonPointerDecode decodes a JSON Pointer token (RFC 6901): ~1 → /, ~0 → ~.
func jsonPointerDecode(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	s = strings.ReplaceAll(s, "~0", "~")
	return s
}

// GetIntegration returns an integration.
func (s *Service) GetIntegration(apiID, resourceID, httpMethod string) (*types.RestIntegration, error) {
	return s.store.GetIntegration(apiID, resourceID, strings.ToUpper(httpMethod))
}

// DeleteIntegration removes an integration.
func (s *Service) DeleteIntegration(apiID, resourceID, httpMethod string) error {
	return s.store.DeleteIntegration(apiID, resourceID, strings.ToUpper(httpMethod))
}

// PutMethodResponse creates or replaces a method response.
func (s *Service) PutMethodResponse(apiID, resourceID, httpMethod, statusCode string, responseModels map[string]string) (*types.RestMethodResponse, error) {
	if _, err := s.store.GetMethod(apiID, resourceID, strings.ToUpper(httpMethod)); err != nil {
		return nil, fmt.Errorf("method not found: %w", err)
	}
	mr := &types.RestMethodResponse{
		StatusCode:     statusCode,
		ResourceID:     resourceID,
		RestAPIID:      apiID,
		HTTPMethod:     strings.ToUpper(httpMethod),
		ResponseModels: responseModels,
	}
	if err := s.store.PutMethodResponse(apiID, mr); err != nil {
		return nil, err
	}
	return s.store.GetMethodResponse(apiID, resourceID, strings.ToUpper(httpMethod), statusCode)
}

// GetMethodResponse returns a method response.
func (s *Service) GetMethodResponse(apiID, resourceID, httpMethod, statusCode string) (*types.RestMethodResponse, error) {
	return s.store.GetMethodResponse(apiID, resourceID, strings.ToUpper(httpMethod), statusCode)
}

// PutIntegrationResponse creates or replaces an integration response.
func (s *Service) PutIntegrationResponse(apiID, resourceID, httpMethod, statusCode, selectionPattern string, responseTemplates map[string]string) (*types.RestIntegrationResponse, error) {
	if _, err := s.store.GetIntegration(apiID, resourceID, strings.ToUpper(httpMethod)); err != nil {
		return nil, fmt.Errorf("integration not found: %w", err)
	}
	ir := &types.RestIntegrationResponse{
		StatusCode:        statusCode,
		SelectionPattern:  selectionPattern,
		ResourceID:        resourceID,
		RestAPIID:         apiID,
		HTTPMethod:        strings.ToUpper(httpMethod),
		ResponseTemplates: responseTemplates,
	}
	if err := s.store.PutIntegrationResponse(apiID, ir); err != nil {
		return nil, err
	}
	return s.store.GetIntegrationResponse(apiID, resourceID, strings.ToUpper(httpMethod), statusCode)
}

// GetIntegrationResponse returns an integration response.
func (s *Service) GetIntegrationResponse(apiID, resourceID, httpMethod, statusCode string) (*types.RestIntegrationResponse, error) {
	return s.store.GetIntegrationResponse(apiID, resourceID, strings.ToUpper(httpMethod), statusCode)
}

// CreateDeployment creates a new deployment and optionally a stage.
func (s *Service) CreateDeployment(apiID, description, stageName string) (*types.RestDeployment, error) {
	if _, err := s.store.GetAPI(apiID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	dep := &types.RestDeployment{
		ID:          randomID(10),
		RestAPIID:   apiID,
		Description: description,
		CreatedDate: types.UnixTime{Time: now},
	}
	if err := s.store.CreateDeployment(apiID, dep); err != nil {
		return nil, err
	}
	// If stageName provided, create a stage automatically
	if stageName != "" {
		stage := &types.RestStage{
			StageName:    stageName,
			RestAPIID:    apiID,
			DeploymentID: dep.ID,
			InvokeURL:    fmt.Sprintf("%s/_aws/execute-api/%s/%s", s.cfg.Endpoint(), apiID, stageName),
			StageArn:     fmt.Sprintf("arn:aws:apigateway:%s::/restapis/%s/stages/%s", s.cfg.Region, apiID, stageName),
			CreatedDate:  types.UnixTime{Time: now},
		}
		// Ignore error if stage already exists; update it
		_ = s.store.CreateStage(apiID, stage)
	}
	return s.store.GetDeployment(apiID, dep.ID)
}

// ListDeployments returns all deployments.
func (s *Service) ListDeployments(apiID string) ([]*types.RestDeployment, error) {
	return s.store.ListDeployments(apiID)
}

// GetDeployment returns a deployment by ID.
func (s *Service) GetDeployment(apiID, deploymentID string) (*types.RestDeployment, error) {
	return s.store.GetDeployment(apiID, deploymentID)
}

// CreateStage creates a named stage linked to a deployment.
func (s *Service) CreateStage(apiID, stageName, deploymentID, description string) (*types.RestStage, error) {
	if stageName == "" {
		return nil, errors.New("stageName is required")
	}
	if deploymentID == "" {
		return nil, errors.New("deploymentId is required")
	}
	if _, err := s.store.GetDeployment(apiID, deploymentID); err != nil {
		return nil, fmt.Errorf("deployment not found: %w", err)
	}
	now := time.Now().UTC()
	stage := &types.RestStage{
		StageName:    stageName,
		RestAPIID:    apiID,
		DeploymentID: deploymentID,
		Description:  description,
		InvokeURL:    fmt.Sprintf("%s/_aws/execute-api/%s/%s", s.cfg.Endpoint(), apiID, stageName),
		StageArn:     fmt.Sprintf("arn:aws:apigateway:%s::/restapis/%s/stages/%s", s.cfg.Region, apiID, stageName),
		CreatedDate:  types.UnixTime{Time: now},
	}
	if err := s.store.CreateStage(apiID, stage); err != nil {
		return nil, err
	}
	return s.store.GetStage(apiID, stageName)
}

// ListStages returns all stages.
func (s *Service) ListStages(apiID string) ([]*types.RestStage, error) {
	return s.store.ListStages(apiID)
}

// GetStage returns a stage by name.
func (s *Service) GetStage(apiID, stageName string) (*types.RestStage, error) {
	return s.store.GetStage(apiID, stageName)
}

// DeleteStage removes a stage.
func (s *Service) DeleteStage(apiID, stageName string) error {
	return s.store.DeleteStage(apiID, stageName)
}

// ListIntegrations returns all integrations for admin overview.
func (s *Service) ListIntegrations(apiID string) ([]*types.RestIntegration, error) {
	return s.store.ListIntegrations(apiID)
}

// Invoke dispatches an incoming request to the correct resource+method+integration.
func (s *Service) Invoke(ctx context.Context, input *InvokeInput) (*InvokeOutput, error) {
	traceStart := time.Now()

	// Resolve API by ID or name
	api, err := s.resolveAPI(input.APIID)
	if err != nil {
		return nil, err
	}

	// Verify stage exists
	if _, err := s.store.GetStage(api.ID, input.Stage); err != nil {
		return nil, fmt.Errorf("stage %q not found", input.Stage)
	}

	// Resolve path to resource
	resources, err := s.store.ListResources(api.ID)
	if err != nil {
		return nil, err
	}
	resource, pathParams := matchResource(resources, normalizePath(input.Path))
	if resource == nil {
		return &InvokeOutput{StatusCode: http.StatusNotFound, Body: []byte(`{"message":"Resource not found"}`)}, nil
	}

	// Get method
	method, err := s.store.GetMethod(api.ID, resource.ID, strings.ToUpper(input.Method))
	if err != nil {
		return &InvokeOutput{StatusCode: http.StatusMethodNotAllowed, Body: []byte(`{"message":"Method not allowed"}`)}, nil
	}
	_ = method

	// Get integration
	integration, err := s.store.GetIntegration(api.ID, resource.ID, strings.ToUpper(input.Method))
	if err != nil {
		return &InvokeOutput{StatusCode: http.StatusInternalServerError, Body: []byte(`{"message":"Integration not configured"}`)}, nil
	}

	switch integration.Type {
	case integrationTypeAWS:
		out, svcErr := s.invokeAWSIntegration(ctx, api, input, integration, pathParams, traceStart)
		return out, svcErr
	case integrationTypeAWSProxy:
		out, svcErr := s.invokeLambdaProxyIntegration(ctx, api, input, integration, resource, pathParams, traceStart)
		return out, svcErr
	default:
		return &InvokeOutput{StatusCode: http.StatusNotImplemented, Body: []byte(`{"message":"Integration type not supported"}`)}, nil
	}
}

func (s *Service) resolveAPI(idOrName string) (*types.RestAPI, error) {
	api, err := s.store.GetAPI(idOrName)
	if err == nil {
		return api, nil
	}
	return s.store.GetAPIByName(idOrName)
}

func (s *Service) invokeAWSIntegration(ctx context.Context, api *types.RestAPI, input *InvokeInput, integ *types.RestIntegration, pathParams map[string]string, traceStart time.Time) (*InvokeOutput, error) {
	if s.sqsSend == nil {
		return nil, errors.New("SQS service not configured")
	}

	// Determine queue from integration URI
	queueName := integ.SQSQueueName
	if queueName == "" {
		var err error
		queueName, err = parseSQSIntegrationPath(integ.URI)
		if err != nil {
			return nil, fmt.Errorf("cannot determine SQS queue from URI: %w", err)
		}
	}

	// Apply request template (VTL) to produce the message body
	contentType := input.Headers.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	// Strip parameters from content type
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	body := string(input.Body)
	if tmpl, ok := integ.RequestTemplates[contentType]; ok && tmpl != "" {
		body = evaluateVTL(tmpl, input, pathParams)
	} else if tmpl, ok := integ.RequestTemplates["application/json"]; ok && tmpl != "" {
		body = evaluateVTL(tmpl, input, pathParams)
	}

	// Parse the URL-encoded body to extract Action and MessageBody
	params, err := url.ParseQuery(body)
	if err != nil {
		// Fall back: send raw body
		params = url.Values{}
		params.Set("MessageBody", body)
	}
	params = normalizeQueryParams(params)

	messageBody := getQueryParam(params, "MessageBody")
	if messageBody == "" {
		messageBody = string(input.Body)
	}
	messageGroupId := getQueryParam(params, "MessageGroupId")
	messageDedupId := getQueryParam(params, "MessageDeduplicationId")

	sqsStart := time.Now()
	messageID, md5, sendErr := s.sqsSend(queueName, messageBody, messageGroupId, messageDedupId)
	if s.traceStore != nil {
		status, spanStatus := 200, "ok"
		if sendErr != nil {
			status, spanStatus = 500, "error"
		}
		s.recordTrace(input, api, traceStart, status, []tracesvc.Span{
			{Kind: "queue", Name: queueName, DurationMs: time.Since(sqsStart).Milliseconds(), Status: spanStatus},
		})
	}
	if sendErr != nil {
		return nil, fmt.Errorf("SQS send failed: %w", sendErr)
	}

	respBody, _ := json.Marshal(map[string]string{
		"MessageId":        messageID,
		"MD5OfMessageBody": md5,
	})
	return &InvokeOutput{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       respBody,
	}, nil
}

func (s *Service) invokeLambdaProxyIntegration(ctx context.Context, api *types.RestAPI, input *InvokeInput, integ *types.RestIntegration, resource *types.RestResource, pathParams map[string]string, traceStart time.Time) (*InvokeOutput, error) {
	fnName := integ.LambdaFunctionName
	if fnName == "" {
		var err error
		_, fnName, err = parseLambdaIntegrationURI(integ.URI)
		if err != nil {
			return nil, fmt.Errorf("invalid lambda uri: %w", err)
		}
	}

	event, err := buildV1LambdaProxyEvent(input, resource, pathParams, api.ID, input.Stage)
	if err != nil {
		return nil, err
	}

	if s.collector != nil {
		s.collector.Begin(fnName)
	}
	lambdaStart := time.Now()
	gatewayDurationMs := lambdaStart.Sub(traceStart).Milliseconds()
	gwName := input.APIID
	if api != nil {
		gwName = api.Name
	}

	invokeOut, invokeErr := s.lambda.Invoke(ctx, &types.InvokeInput{
		FunctionName:   fnName,
		Payload:        event,
		InvocationType: "RequestResponse",
	})
	if invokeErr != nil {
		subSpans := s.collectSubSpans(fnName)
		if s.traceStore != nil {
			s.recordTrace(input, api, traceStart, 500, append([]tracesvc.Span{
				{Kind: "gateway", Name: gwName, DurationMs: gatewayDurationMs, Status: "error"},
				{Kind: "lambda", Name: fnName, DurationMs: time.Since(lambdaStart).Milliseconds(), Status: "error"},
			}, subSpans...))
		}
		return nil, fmt.Errorf("invoke failed: %w", invokeErr)
	}

	subSpans := s.collectSubSpans(fnName)
	lambdaDurationMs := time.Since(lambdaStart).Milliseconds()

	out, mapErr := mapLambdaProxyResponse(invokeOut.Payload)
	if mapErr != nil {
		if s.traceStore != nil {
			s.recordTrace(input, api, traceStart, 500, append([]tracesvc.Span{
				{Kind: "gateway", Name: gwName, DurationMs: gatewayDurationMs, Status: "error"},
				{Kind: "lambda", Name: fnName, DurationMs: lambdaDurationMs, Status: "error"},
			}, subSpans...))
		}
		return nil, mapErr
	}

	if s.traceStore != nil {
		spanStatus := "ok"
		if out.StatusCode >= 500 {
			spanStatus = "error"
		} else if out.StatusCode >= 400 {
			spanStatus = "client_error"
		}
		s.recordTrace(input, api, traceStart, out.StatusCode, append([]tracesvc.Span{
			{Kind: "gateway", Name: gwName, DurationMs: gatewayDurationMs, Status: spanStatus},
			{Kind: "lambda", Name: fnName, DurationMs: lambdaDurationMs, Status: spanStatus},
		}, subSpans...))
	}
	return out, nil
}

func (s *Service) collectSubSpans(fnName string) []tracesvc.Span {
	if s.collector == nil {
		return nil
	}
	return tracesvc.SubSpansToSpans(s.collector.CollectWithFlush(fnName))
}

func (s *Service) recordTrace(input *InvokeInput, api *types.RestAPI, start time.Time, status int, spans []tracesvc.Span) {
	gwName := input.APIID
	if api != nil {
		gwName = api.Name
	}
	s.traceStore.Add(&tracesvc.Trace{
		ID:          uuid.NewString()[:8],
		StartedAt:   start,
		DurationMs:  time.Since(start).Milliseconds(),
		Status:      status,
		Method:      strings.ToUpper(input.Method),
		Path:        input.Path,
		GatewayID:   input.APIID,
		GatewayName: gwName,
		Spans:       spans,
	})
}

// matchResource finds the resource in the tree that matches path, returning path parameters.
func matchResource(resources []*types.RestResource, path string) (*types.RestResource, map[string]string) {
	path = normalizePath(path)
	pathSegs := pathSegments(path)

	var best *types.RestResource
	var bestParams map[string]string
	bestSpecificity := -1

	for _, res := range resources {
		resPath := normalizePath(res.Path)
		resSegs := pathSegments(resPath)
		if len(resSegs) != len(pathSegs) {
			continue
		}
		params := map[string]string{}
		matched := true
		specificity := 0
		for i, seg := range resSegs {
			if isPathParam(seg) {
				name := seg[1 : len(seg)-1]
				params[name] = pathSegs[i]
			} else if seg == pathSegs[i] {
				specificity++
			} else {
				matched = false
				break
			}
		}
		if matched && specificity >= bestSpecificity {
			best = res
			bestParams = params
			bestSpecificity = specificity
		}
	}
	if best != nil && len(bestParams) == 0 {
		bestParams = nil
	}
	return best, bestParams
}

func isPathParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func normalizePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func pathSegments(path string) []string {
	if path == "/" {
		return nil
	}
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// evaluateVTL evaluates a minimal subset of VTL templates used in API Gateway SQS integrations.
// Handles:
//   - $input.json('$')                    — URL-encoded body as JSON
//   - $input.body                         — raw body
//   - $input.path('$.field')              — JSON path
//   - $input.params().path.get('name')    — path parameter
//   - $input.params().header.get('name')  — header value
//   - $util.urlEncode(...)               — URL encode expression
//   - $util.toJson(...)                  — JSON encode value
//   - Variable assignments (#set)
func evaluateVTL(template string, input *InvokeInput, pathParams map[string]string) string {
	ev := &vtlEvaluator{
		input:      input,
		pathParams: pathParams,
		vars:       make(map[string]string),
	}
	return ev.evaluate(template)
}

type vtlEvaluator struct {
	input      *InvokeInput
	pathParams map[string]string
	vars       map[string]string
}

func (v *vtlEvaluator) evaluate(tmpl string) string {
	// Process line-by-line, handling #set directives (including multiline values).
	lines := strings.Split(tmpl, "\n")
	output := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#set(") {
			directive := line
			balance := strings.Count(line, "(") - strings.Count(line, ")")
			for balance > 0 && i+1 < len(lines) {
				i++
				next := strings.TrimRight(lines[i], " \t")
				directive += "\n" + next
				balance += strings.Count(next, "(") - strings.Count(next, ")")
			}
			v.processSetDirective(directive)
			continue
		}

		output = append(output, v.expandExpressions(line))
	}
	// Join and expand remaining expressions
	result := strings.Join(output, "\n")
	return strings.TrimSpace(result)
}

func (v *vtlEvaluator) processSetDirective(directive string) {
	trimmed := strings.TrimSpace(directive)
	if !strings.HasPrefix(trimmed, "#set(") || !strings.HasSuffix(trimmed, ")") {
		return
	}

	inner := strings.TrimSpace(trimmed[len("#set(") : len(trimmed)-1])
	eqIdx := strings.Index(inner, "=")
	if eqIdx < 0 {
		return
	}

	left := strings.TrimSpace(inner[:eqIdx])
	right := strings.TrimSpace(inner[eqIdx+1:])
	if !strings.HasPrefix(left, "$") {
		return
	}

	varName := strings.TrimPrefix(left, "$")
	v.vars[varName] = v.expandExpr(right)
}

func (v *vtlEvaluator) expandExpressions(s string) string {
	// Replace $util.urlEncode(...), $util.toJson(...), $input.*, etc.
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '$' {
			expr, length := extractVTLExpr(s[i:])
			if length > 0 {
				result.WriteString(v.expandExpr(expr))
				i += length
				continue
			}
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

func (v *vtlEvaluator) expandExpr(expr string) string {
	expr = strings.TrimSpace(expr)

	// $util.urlEncode(inner)
	if strings.HasPrefix(expr, "$util.urlEncode(") && strings.HasSuffix(expr, ")") {
		inner := expr[len("$util.urlEncode(") : len(expr)-1]
		return url.QueryEscape(v.expandExpr(inner))
	}

	// $util.toJson(inner)
	if strings.HasPrefix(expr, "$util.toJson(") && strings.HasSuffix(expr, ")") {
		inner := expr[len("$util.toJson(") : len(expr)-1]
		val := strings.TrimSpace(v.expandExpr(inner))
		if json.Valid([]byte(val)) {
			// API Gateway's $util.toJson emits compact JSON.
			var decoded any
			if err := json.Unmarshal([]byte(val), &decoded); err == nil {
				if b, err := json.Marshal(decoded); err == nil {
					return string(b)
				}
			}
			return val
		}
		b, _ := json.Marshal(val)
		return string(b)
	}

	// JSON object/array literal
	if (strings.HasPrefix(expr, "{") && strings.HasSuffix(expr, "}")) ||
		(strings.HasPrefix(expr, "[") && strings.HasSuffix(expr, "]")) {
		return strings.TrimSpace(v.expandExpressions(expr))
	}

	// $input.json('$') — entire body as JSON string
	if expr == "$input.json('$')" || expr == `$input.json("$")` {
		return string(v.input.Body)
	}

	// $input.body
	if expr == "$input.body" {
		return string(v.input.Body)
	}

	// $input.path('$.field')
	if strings.HasPrefix(expr, "$input.path('") || strings.HasPrefix(expr, `$input.path("`) {
		path := extractStringArg(expr)
		return extractJSONPath(v.input.Body, path)
	}

	// $input.params().path.get('name')
	if strings.Contains(expr, ".path.get(") {
		name := extractStringArg(expr)
		if v.pathParams != nil {
			return v.pathParams[name]
		}
		return ""
	}

	// $input.params().header.get('name')
	if strings.Contains(expr, ".header.get(") {
		name := extractStringArg(expr)
		return v.input.Headers.Get(name)
	}

	// $input.params().querystring.get('name')
	if strings.Contains(expr, ".querystring.get(") {
		name := extractStringArg(expr)
		return v.input.Query.Get(name)
	}

	// Named variable: $varName or $varName.field
	varName := strings.TrimPrefix(expr, "$")
	dotIdx := strings.Index(varName, ".")
	if dotIdx >= 0 {
		// Nested access like $payload.aggregate.aggregateId — look up $payload as JSON
		rootVar := varName[:dotIdx]
		fieldPath := varName[dotIdx+1:]
		if val, ok := v.vars[rootVar]; ok {
			return extractJSONPath([]byte(val), "$."+fieldPath)
		}
	}
	if val, ok := v.vars[varName]; ok {
		return val
	}

	return expr
}

// extractVTLExpr extracts a VTL expression starting with '$' and returns (expr, length).
func extractVTLExpr(s string) (string, int) {
	if len(s) == 0 || s[0] != '$' {
		return "", 0
	}
	// Handle chained calls/paths, e.g.:
	//   $input.params().path.get('id')
	// and nested wrappers, e.g.:
	//   $util.urlEncode($util.toJson($payload))
	depth := 0
	inQuote := byte(0)
	i := 1
	for i < len(s) {
		c := s[i]

		if inQuote != 0 {
			if c == inQuote && s[i-1] != '\\' {
				inQuote = 0
			}
			i++
			continue
		}

		switch c {
		case '\'', '"':
			if depth == 0 {
				return s[:i], i
			}
			inQuote = c
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			} else {
				return s[:i], i
			}
		default:
			if depth == 0 && isVTLExprTerminator(c) {
				return s[:i], i
			}
		}
		i++
	}

	if depth == 0 && inQuote == 0 {
		return s[:i], i
	}
	return "", 0
}

func isVTLExprTerminator(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '&', ',', ';', '{', '}', '[', ']', '+', '-', '*', '/', '%', '?', ':', '=', '!', '<', '>', '|', '\\', '\'', '"':
		return true
	default:
		return false
	}
}

func extractStringArg(expr string) string {
	start := strings.LastIndex(expr, "(")
	end := strings.LastIndex(expr, ")")
	if start < 0 || end <= start {
		return ""
	}
	inner := strings.TrimSpace(expr[start+1 : end])
	if len(inner) >= 2 && ((inner[0] == '\'' && inner[len(inner)-1] == '\'') || (inner[0] == '"' && inner[len(inner)-1] == '"')) {
		return inner[1 : len(inner)-1]
	}
	return strings.Trim(inner, `'"`)
}

func extractJSONPath(body []byte, path string) string {
	if len(body) == 0 {
		return ""
	}
	// Strip leading "$."
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	if path == "" {
		return string(body)
	}
	parts := strings.SplitN(path, ".", 2)
	field := parts[0]

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return ""
	}
	raw, ok := obj[field]
	if !ok {
		return ""
	}
	if len(parts) > 1 {
		return extractJSONPath(raw, parts[1])
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func normalizeQueryParams(params url.Values) url.Values {
	if len(params) == 0 {
		return params
	}

	normalized := url.Values{}
	for k, vals := range params {
		key := strings.TrimSpace(k)
		normalized[key] = append(normalized[key], vals...)
	}
	return normalized
}

func getQueryParam(params url.Values, key string) string {
	if val := params.Get(key); val != "" {
		return val
	}
	for k, vals := range params {
		if strings.EqualFold(strings.TrimSpace(k), key) && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

// buildV1LambdaProxyEvent builds the API Gateway v1 proxy event payload.
func buildV1LambdaProxyEvent(input *InvokeInput, resource *types.RestResource, pathParams map[string]string, apiID, stage string) ([]byte, error) {
	headers := make(map[string]string)
	for k, vals := range input.Headers {
		if len(vals) > 0 {
			headers[strings.ToLower(k)] = vals[0]
		}
	}
	qs := make(map[string]string)
	for k, vals := range input.Query {
		if len(vals) > 0 {
			qs[k] = vals[0]
		}
	}
	body := ""
	if len(input.Body) > 0 {
		body = string(input.Body)
	}
	event := map[string]any{
		"version":               "1.0",
		"resource":              resource.Path,
		"path":                  normalizePath(input.Path),
		"httpMethod":            strings.ToUpper(input.Method),
		"headers":               headers,
		"queryStringParameters": qs,
		"pathParameters":        pathParams,
		"body":                  body,
		"isBase64Encoded":       false,
		"requestContext": map[string]any{
			"resourceId":   resource.ID,
			"resourcePath": resource.Path,
			"httpMethod":   strings.ToUpper(input.Method),
			"apiId":        apiID,
			"stage":        stage,
		},
	}
	if len(pathParams) == 0 {
		event["pathParameters"] = nil
	}
	return json.Marshal(event)
}

// mapLambdaProxyResponse converts the Lambda response payload to an InvokeOutput.
func mapLambdaProxyResponse(payload []byte) (*InvokeOutput, error) {
	if len(payload) == 0 {
		return &InvokeOutput{StatusCode: http.StatusOK, Headers: map[string]string{}, Body: nil}, nil
	}
	var resp struct {
		StatusCode int               `json:"statusCode"`
		Headers    map[string]string `json:"headers"`
		Body       json.RawMessage   `json:"body"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return &InvokeOutput{StatusCode: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Body: payload}, nil
	}
	if resp.StatusCode == 0 {
		resp.StatusCode = http.StatusOK
	}
	if resp.Headers == nil {
		resp.Headers = map[string]string{}
	}
	var body []byte
	if len(resp.Body) > 0 && string(resp.Body) != "null" {
		if resp.Body[0] == '"' {
			var s string
			if err := json.Unmarshal(resp.Body, &s); err == nil {
				body = []byte(s)
			}
		} else {
			body = resp.Body
			if _, ok := resp.Headers["Content-Type"]; !ok {
				resp.Headers["Content-Type"] = "application/json"
			}
		}
	}
	return &InvokeOutput{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: body}, nil
}

func parseLambdaIntegrationURI(uri string) (lambdaArn, functionName string, err error) {
	uri = strings.TrimSpace(uri)
	prefix := ":lambda:path/2015-03-31/functions/"
	if strings.HasPrefix(uri, "arn:aws:apigateway:") && strings.Contains(uri, prefix) && strings.HasSuffix(uri, "/invocations") {
		start := strings.Index(uri, prefix)
		if start == -1 {
			return "", "", fmt.Errorf("invalid IntegrationUri %q", uri)
		}
		start += len(prefix)
		end := strings.LastIndex(uri, "/invocations")
		if end <= start {
			return "", "", fmt.Errorf("invalid IntegrationUri %q", uri)
		}
		lambdaArn = uri[start:end]
		idx := strings.Index(lambdaArn, ":function:")
		if idx == -1 {
			return "", "", fmt.Errorf("invalid lambda arn %q", lambdaArn)
		}
		tail := lambdaArn[idx+len(":function:"):]
		parts := strings.Split(tail, ":")
		if parts[0] == "" {
			return "", "", fmt.Errorf("invalid lambda arn %q", lambdaArn)
		}
		return lambdaArn, parts[0], nil
	}
	return "", "", fmt.Errorf("unsupported IntegrationUri %q", uri)
}

// parseSQSIntegrationPath extracts the queue name from an SQS integration URI.
// Supports both:
//   - arn:aws:apigateway:{region}:sqs:path/{accountId}/{queueName}  (REST API v1 style)
//   - arn:aws:sqs:{region}:{accountId}:{queueName}                   (ARN style)
func parseSQSIntegrationPath(uri string) (string, error) {
	uri = strings.TrimSpace(uri)
	// REST API v1 style: arn:aws:apigateway:{region}:sqs:path/{accountId}/{queueName}
	if strings.Contains(uri, ":sqs:path/") {
		idx := strings.Index(uri, ":sqs:path/")
		rest := uri[idx+len(":sqs:path/"):]
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 2 && parts[1] != "" {
			return parts[1], nil
		}
		return "", fmt.Errorf("invalid SQS path URI: %q", uri)
	}
	// ARN style: arn:aws:sqs:{region}:{accountId}:{queueName}
	if strings.HasPrefix(uri, "arn:aws:sqs:") {
		parts := strings.Split(uri, ":")
		if len(parts) >= 6 && parts[5] != "" {
			return parts[5], nil
		}
	}
	return "", fmt.Errorf("cannot parse SQS queue from URI: %q", uri)
}

func cloneTags(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func randomID(length int) string {
	if length <= 0 {
		length = 8
	}
	raw := strings.ReplaceAll(uuid.NewString(), "-", "")
	for len(raw) < length {
		raw += strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return raw[:length]
}
