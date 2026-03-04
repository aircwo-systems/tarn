package apigateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/openstack-project/openstack/internal/config"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/pkg/types"
)

const (
	protocolHTTP                    = "HTTP"
	integrationTypeAWSProxy         = "AWS_PROXY"
	integrationTypeAWS              = "AWS"
	defaultPayloadFormatVersion     = "2.0"
	defaultRouteSelectionExpression = "$request.method $request.path"
)

// SQSSendFunc is a function type for sending SQS messages.
type SQSSendFunc func(queueName, body string) (messageID, md5 string, err error)

// Service implements API Gateway HTTP API (v2) behavior.
type Service struct {
	cfg      *config.Config
	lambda   *lambdasvc.Service
	sqsSend  SQSSendFunc
	store    *Store
}

// APIUpdateInput contains mutable API fields.
type APIUpdateInput struct {
	Name        *string
	Description *string
}

// IntegrationCreateInput contains create parameters for integrations.
type IntegrationCreateInput struct {
	IntegrationType      string
	IntegrationURI       string
	PayloadFormatVersion string
	TimeoutInMillis      int
	RequestParameters    map[string]string
}

// IntegrationUpdateInput contains mutable integration fields.
type IntegrationUpdateInput struct {
	IntegrationURI       *string
	PayloadFormatVersion *string
	TimeoutInMillis      *int
	RequestParameters    map[string]string
}

// RouteCreateInput contains create parameters for routes.
type RouteCreateInput struct {
	RouteKey string
	Target   string
}

// RouteUpdateInput contains mutable route fields.
type RouteUpdateInput struct {
	RouteKey *string
	Target   *string
}

// StageUpdateInput contains mutable stage fields.
type StageUpdateInput struct {
	Description *string
}

// InvokeInput captures request data for invoke dispatch.
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

// InvokeOutput captures invoke response data.
type InvokeOutput struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// NewService creates a new API Gateway service.
func NewService(cfg *config.Config, lambdaSvc *lambdasvc.Service, sqsSend SQSSendFunc) *Service {
	return &Service{
		cfg:     cfg,
		lambda:  lambdaSvc,
		sqsSend: sqsSend,
		store:   NewStore(cfg),
	}
}

// Init loads persisted API Gateway state if configured.
func (s *Service) Init() error {
	return s.store.Init()
}

// CreateAPI creates a new HTTP API and an auto-deployed $default stage.
func (s *Service) CreateAPI(name, description, protocolType, routeSelectionExpression string, tags map[string]string) (*types.APIGatewayAPI, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if protocolType == "" {
		protocolType = protocolHTTP
	}
	if protocolType != protocolHTTP {
		return nil, fmt.Errorf("unsupported ProtocolType %q (only HTTP is supported)", protocolType)
	}
	if routeSelectionExpression == "" {
		routeSelectionExpression = defaultRouteSelectionExpression
	}

	now := time.Now().UTC()
	apiID := randomID(10)
	api := &types.APIGatewayAPI{
		APIID:                    apiID,
		Name:                     name,
		Description:              description,
		ProtocolType:             protocolType,
		RouteSelectionExpression: routeSelectionExpression,
		APIEndpoint:              fmt.Sprintf("%s/_apigateway/%s/$default", s.cfg.Endpoint(), apiID),
		APIArn:                   fmt.Sprintf("arn:aws:apigateway:%s::/apis/%s", s.cfg.Region, apiID),
		Tags:                     cloneTags(tags),
		CreatedDate:              now,
	}

	stage := &types.APIGatewayStage{
		APIID:           apiID,
		StageName:       "$default",
		AutoDeploy:      true,
		InvokeURL:       fmt.Sprintf("%s/_apigateway/%s/$default", s.cfg.Endpoint(), apiID),
		StageArn:        fmt.Sprintf("arn:aws:apigateway:%s::/apis/%s/stages/$default", s.cfg.Region, apiID),
		CreatedDate:     now,
		LastUpdatedDate: now,
	}

	if err := s.store.CreateAPI(api, stage); err != nil {
		return nil, err
	}
	return s.store.GetAPI(apiID)
}

// ListAPIs returns all APIs.
func (s *Service) ListAPIs() []*types.APIGatewayAPI {
	return s.store.ListAPIs()
}

// GetAPI returns an API by id.
func (s *Service) GetAPI(apiID string) (*types.APIGatewayAPI, error) {
	return s.store.GetAPI(apiID)
}

// UpdateAPI updates mutable API fields.
func (s *Service) UpdateAPI(apiID string, input APIUpdateInput) (*types.APIGatewayAPI, error) {
	api, err := s.store.GetAPI(apiID)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		if *input.Name == "" {
			return nil, errors.New("name cannot be empty")
		}
		api.Name = *input.Name
	}
	if input.Description != nil {
		api.Description = *input.Description
	}
	if err := s.store.SaveAPI(api); err != nil {
		return nil, err
	}
	return s.store.GetAPI(apiID)
}

// DeleteAPI deletes an API and all nested resources.
func (s *Service) DeleteAPI(apiID string) error {
	return s.store.DeleteAPI(apiID)
}

// CreateIntegration creates a Lambda AWS_PROXY or SQS AWS integration.
func (s *Service) CreateIntegration(apiID string, input IntegrationCreateInput) (*types.APIGatewayIntegration, error) {
	if input.IntegrationType == "" {
		input.IntegrationType = integrationTypeAWSProxy
	}
	if input.IntegrationType != integrationTypeAWSProxy && input.IntegrationType != integrationTypeAWS {
		return nil, fmt.Errorf("unsupported IntegrationType %q (only AWS_PROXY and AWS are supported)", input.IntegrationType)
	}
	if input.IntegrationURI == "" {
		return nil, errors.New("IntegrationUri is required")
	}
	if input.PayloadFormatVersion == "" {
		input.PayloadFormatVersion = defaultPayloadFormatVersion
	}
	if input.TimeoutInMillis == 0 {
		input.TimeoutInMillis = 30000
	}
	if input.TimeoutInMillis < 50 || input.TimeoutInMillis > 30000 {
		return nil, errors.New("TimeoutInMillis must be between 50 and 30000")
	}

	if _, err := s.store.GetAPI(apiID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	integrationID := randomID(12)
	integration := &types.APIGatewayIntegration{
		IntegrationID:        integrationID,
		APIID:                apiID,
		IntegrationType:      input.IntegrationType,
		IntegrationMethod:    http.MethodPost,
		IntegrationURI:       input.IntegrationURI,
		PayloadFormatVersion: input.PayloadFormatVersion,
		TimeoutInMillis:      input.TimeoutInMillis,
		RequestParameters:    input.RequestParameters,
		IntegrationArn:       fmt.Sprintf("arn:aws:apigateway:%s::/apis/%s/integrations/%s", s.cfg.Region, apiID, integrationID),
		CreatedDate:          now,
	}

	switch input.IntegrationType {
	case integrationTypeAWSProxy:
		lambdaArn, lambdaName, err := parseLambdaIntegrationURI(input.IntegrationURI)
		if err != nil {
			return nil, err
		}
		if _, err := s.lambda.GetFunction(lambdaName); err != nil {
			return nil, fmt.Errorf("invalid IntegrationUri: %w", err)
		}
		integration.LambdaFunctionArn = lambdaArn
		integration.LambdaFunctionName = lambdaName

	case integrationTypeAWS:
		sqsArn, queueName, err := parseSQSIntegrationURI(input.IntegrationURI)
		if err != nil {
			return nil, err
		}
		integration.SQSQueueArn = sqsArn
		integration.SQSQueueName = queueName
	}

	if err := s.store.CreateIntegration(apiID, integration); err != nil {
		return nil, err
	}
	return s.store.GetIntegration(apiID, integrationID)
}

// ListIntegrations returns all integrations for an API.
func (s *Service) ListIntegrations(apiID string) ([]*types.APIGatewayIntegration, error) {
	return s.store.ListIntegrations(apiID)
}

// GetIntegration returns a single integration.
func (s *Service) GetIntegration(apiID, integrationID string) (*types.APIGatewayIntegration, error) {
	return s.store.GetIntegration(apiID, integrationID)
}

// UpdateIntegration updates mutable integration fields.
func (s *Service) UpdateIntegration(apiID, integrationID string, input IntegrationUpdateInput) (*types.APIGatewayIntegration, error) {
	integration, err := s.store.GetIntegration(apiID, integrationID)
	if err != nil {
		return nil, err
	}

	if input.IntegrationURI != nil {
		if *input.IntegrationURI == "" {
			return nil, errors.New("IntegrationUri cannot be empty")
		}
		lambdaArn, lambdaName, err := parseLambdaIntegrationURI(*input.IntegrationURI)
		if err != nil {
			return nil, err
		}
		if _, err := s.lambda.GetFunction(lambdaName); err != nil {
			return nil, fmt.Errorf("invalid IntegrationUri: %w", err)
		}
		integration.IntegrationURI = *input.IntegrationURI
		integration.LambdaFunctionArn = lambdaArn
		integration.LambdaFunctionName = lambdaName
	}

	if input.PayloadFormatVersion != nil {
		if *input.PayloadFormatVersion == "" {
			return nil, errors.New("PayloadFormatVersion cannot be empty")
		}
		integration.PayloadFormatVersion = *input.PayloadFormatVersion
	}

	if input.TimeoutInMillis != nil {
		if *input.TimeoutInMillis < 50 || *input.TimeoutInMillis > 30000 {
			return nil, errors.New("TimeoutInMillis must be between 50 and 30000")
		}
		integration.TimeoutInMillis = *input.TimeoutInMillis
	}

	if input.RequestParameters != nil {
		integration.RequestParameters = input.RequestParameters
	}

	if err := s.store.SaveIntegration(apiID, integration); err != nil {
		return nil, err
	}
	return s.store.GetIntegration(apiID, integrationID)
}

// DeleteIntegration deletes an integration if no route references it.
func (s *Service) DeleteIntegration(apiID, integrationID string) error {
	routes, err := s.store.ListRoutes(apiID)
	if err != nil {
		return err
	}
	for _, route := range routes {
		targetID, err := parseIntegrationTarget(route.Target)
		if err == nil && targetID == integrationID {
			return fmt.Errorf("integration %s is still referenced by route %s", integrationID, route.RouteID)
		}
	}
	return s.store.DeleteIntegration(apiID, integrationID)
}

// CreateRoute creates a route bound to an integration.
func (s *Service) CreateRoute(apiID string, input RouteCreateInput) (*types.APIGatewayRoute, error) {
	if input.RouteKey == "" {
		return nil, errors.New("RouteKey is required")
	}
	if input.Target == "" {
		return nil, errors.New("target is required")
	}
	if _, _, err := parseRouteKey(input.RouteKey); err != nil {
		return nil, err
	}
	integrationID, err := parseIntegrationTarget(input.Target)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetIntegration(apiID, integrationID); err != nil {
		return nil, err
	}
	normalizedRouteKey := normalizeRouteKey(input.RouteKey)
	routes, err := s.store.ListRoutes(apiID)
	if err != nil {
		return nil, err
	}
	for _, existing := range routes {
		if strings.EqualFold(existing.RouteKey, normalizedRouteKey) {
			return nil, fmt.Errorf("route with RouteKey %q already exists", normalizedRouteKey)
		}
	}

	now := time.Now().UTC()
	routeID := randomID(12)
	route := &types.APIGatewayRoute{
		RouteID:     routeID,
		APIID:       apiID,
		RouteKey:    normalizedRouteKey,
		Target:      input.Target,
		RouteArn:    fmt.Sprintf("arn:aws:apigateway:%s::/apis/%s/routes/%s", s.cfg.Region, apiID, routeID),
		CreatedDate: now,
	}

	if err := s.store.CreateRoute(apiID, route); err != nil {
		return nil, err
	}
	return s.store.GetRoute(apiID, routeID)
}

// ListRoutes returns all routes for an API.
func (s *Service) ListRoutes(apiID string) ([]*types.APIGatewayRoute, error) {
	return s.store.ListRoutes(apiID)
}

// GetRoute returns a route by id.
func (s *Service) GetRoute(apiID, routeID string) (*types.APIGatewayRoute, error) {
	return s.store.GetRoute(apiID, routeID)
}

// UpdateRoute updates mutable route fields.
func (s *Service) UpdateRoute(apiID, routeID string, input RouteUpdateInput) (*types.APIGatewayRoute, error) {
	route, err := s.store.GetRoute(apiID, routeID)
	if err != nil {
		return nil, err
	}

	if input.RouteKey != nil {
		if *input.RouteKey == "" {
			return nil, errors.New("RouteKey cannot be empty")
		}
		if _, _, err := parseRouteKey(*input.RouteKey); err != nil {
			return nil, err
		}
		normalizedRouteKey := normalizeRouteKey(*input.RouteKey)
		routes, err := s.store.ListRoutes(apiID)
		if err != nil {
			return nil, err
		}
		for _, existing := range routes {
			if existing.RouteID != routeID && strings.EqualFold(existing.RouteKey, normalizedRouteKey) {
				return nil, fmt.Errorf("route with RouteKey %q already exists", normalizedRouteKey)
			}
		}
		route.RouteKey = normalizedRouteKey
	}

	if input.Target != nil {
		if *input.Target == "" {
			return nil, errors.New("target cannot be empty")
		}
		integrationID, err := parseIntegrationTarget(*input.Target)
		if err != nil {
			return nil, err
		}
		if _, err := s.store.GetIntegration(apiID, integrationID); err != nil {
			return nil, err
		}
		route.Target = *input.Target
	}

	if err := s.store.SaveRoute(apiID, route); err != nil {
		return nil, err
	}
	return s.store.GetRoute(apiID, routeID)
}

// DeleteRoute deletes a route.
func (s *Service) DeleteRoute(apiID, routeID string) error {
	return s.store.DeleteRoute(apiID, routeID)
}

// ListStages returns all stages for an API.
func (s *Service) ListStages(apiID string) ([]*types.APIGatewayStage, error) {
	return s.store.ListStages(apiID)
}

// GetStage returns a stage by name.
func (s *Service) GetStage(apiID, stageName string) (*types.APIGatewayStage, error) {
	return s.store.GetStage(apiID, stageName)
}

// UpdateStage updates mutable stage fields while forcing AutoDeploy=true.
func (s *Service) UpdateStage(apiID, stageName string, input StageUpdateInput) (*types.APIGatewayStage, error) {
	stage, err := s.store.GetStage(apiID, stageName)
	if err != nil {
		return nil, err
	}
	if input.Description != nil {
		stage.Description = *input.Description
	}
	stage.AutoDeploy = true
	stage.LastUpdatedDate = time.Now().UTC()
	if err := s.store.SaveStage(apiID, stage); err != nil {
		return nil, err
	}
	return s.store.GetStage(apiID, stageName)
}

// Invoke resolves an incoming request to a route+integration and invokes Lambda.
func (s *Service) Invoke(ctx context.Context, input *InvokeInput) (*InvokeOutput, error) {
	if input.APIID == "" {
		return nil, errors.New("api id is required")
	}
	if input.Stage == "" {
		return nil, errors.New("stage is required")
	}

	if _, err := s.store.GetStage(input.APIID, input.Stage); err != nil {
		return nil, err
	}

	routes, err := s.store.ListRoutes(input.APIID)
	if err != nil {
		return nil, err
	}
	matched, pathParams, err := selectRoute(routes, input.Method, normalizePath(input.Path))
	if err != nil {
		return nil, err
	}
	if matched == nil {
		return &InvokeOutput{StatusCode: http.StatusNotFound, Body: []byte("route not found")}, nil
	}

	integrationID, err := parseIntegrationTarget(matched.Target)
	if err != nil {
		return nil, err
	}
	integration, err := s.store.GetIntegration(input.APIID, integrationID)
	if err != nil {
		return nil, err
	}

	// Branch on integration type
	switch integration.IntegrationType {
	case integrationTypeAWS:
		return s.invokeSQSIntegration(integration, input, pathParams)
	default:
		// AWS_PROXY — Lambda
	}

	eventPayload, err := buildLambdaProxyEvent(input, matched, pathParams)
	if err != nil {
		return nil, err
	}

	invokeOut, err := s.lambda.Invoke(ctx, &types.InvokeInput{
		FunctionName:   integration.LambdaFunctionName,
		Payload:        eventPayload,
		InvocationType: "RequestResponse",
	})
	if err != nil {
		// Cold starts can intermittently fail with EOF before the runtime is fully ready.
		// Retry once for transient transport/runtime startup errors.
		if isTransientInvokeError(err) {
			time.Sleep(200 * time.Millisecond)
			invokeOut, err = s.lambda.Invoke(ctx, &types.InvokeInput{
				FunctionName:   integration.LambdaFunctionName,
				Payload:        eventPayload,
				InvocationType: "RequestResponse",
			})
		}
		if err != nil {
			return nil, fmt.Errorf("invoke failed: %w", err)
		}
	}

	return mapLambdaProxyResponse(invokeOut.Payload)
}

type lambdaProxyResponse struct {
	StatusCode      int               `json:"statusCode"`
	Headers         map[string]string `json:"headers"`
	Body            json.RawMessage   `json:"body"`
	IsBase64Encoded bool              `json:"isBase64Encoded"`
}

func mapLambdaProxyResponse(payload []byte) (*InvokeOutput, error) {
	if len(payload) == 0 {
		return &InvokeOutput{StatusCode: http.StatusOK, Headers: map[string]string{}, Body: nil}, nil
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(payload, &probe); err != nil {
		return &InvokeOutput{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       payload,
		}, nil
	}

	_, hasStatus := probe["statusCode"]
	_, hasHeaders := probe["headers"]
	_, hasBody := probe["body"]
	_, hasB64 := probe["isBase64Encoded"]
	if !hasStatus && !hasHeaders && !hasBody && !hasB64 {
		return &InvokeOutput{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       payload,
		}, nil
	}

	var response lambdaProxyResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("invalid lambda proxy response: %w", err)
	}
	if response.StatusCode == 0 {
		response.StatusCode = http.StatusOK
	}
	if response.Headers == nil {
		response.Headers = map[string]string{}
	}

	var body []byte
	if len(response.Body) > 0 && string(response.Body) != "null" {
		if response.Body[0] == '"' {
			var s string
			if err := json.Unmarshal(response.Body, &s); err != nil {
				return nil, fmt.Errorf("invalid body string in lambda proxy response: %w", err)
			}
			if response.IsBase64Encoded {
				decoded, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return nil, fmt.Errorf("invalid base64 lambda response body: %w", err)
				}
				body = decoded
			} else {
				body = []byte(s)
			}
		} else {
			body = response.Body
			if _, ok := response.Headers["Content-Type"]; !ok {
				response.Headers["Content-Type"] = "application/json"
			}
		}
	}

	return &InvokeOutput{
		StatusCode: response.StatusCode,
		Headers:    response.Headers,
		Body:       body,
	}, nil
}

func buildLambdaProxyEvent(input *InvokeInput, route *types.APIGatewayRoute, pathParams map[string]string) ([]byte, error) {
	query := map[string]string{}
	for key, values := range input.Query {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}

	headers := map[string]string{}
	for key, values := range input.Headers {
		if len(values) == 0 {
			continue
		}
		headers[strings.ToLower(key)] = values[0]
	}

	body := ""
	isBase64 := false
	if len(input.Body) > 0 {
		if utf8.Valid(input.Body) {
			body = string(input.Body)
		} else {
			body = base64.StdEncoding.EncodeToString(input.Body)
			isBase64 = true
		}
	}

	event := map[string]any{
		"version":               "2.0",
		"routeKey":              route.RouteKey,
		"rawPath":               normalizePath(input.Path),
		"rawQueryString":        input.RawQuery,
		"headers":               headers,
		"queryStringParameters": query,
		"isBase64Encoded":       isBase64,
		"requestContext": map[string]any{
			"apiId":    input.APIID,
			"routeKey": route.RouteKey,
			"stage":    input.Stage,
			"http": map[string]any{
				"method": strings.ToUpper(input.Method),
				"path":   normalizePath(input.Path),
			},
		},
	}
	if body != "" {
		event["body"] = body
	}
	if len(pathParams) > 0 {
		event["pathParameters"] = pathParams
	}

	return json.Marshal(event)
}

type candidateRoute struct {
	route       *types.APIGatewayRoute
	pathParams  map[string]string
	priority    int
	specificity int
}

func selectRoute(routes []*types.APIGatewayRoute, method, path string) (*types.APIGatewayRoute, map[string]string, error) {
	var candidates []candidateRoute
	var defaultRoute *types.APIGatewayRoute

	normalizedMethod := strings.ToUpper(method)
	normalizedPath := normalizePath(path)

	for _, route := range routes {
		routeMethod, routePath, err := parseRouteKey(route.RouteKey)
		if err != nil {
			continue
		}
		if routeMethod == "$default" {
			defaultRoute = route
			continue
		}

		params, matched, templated, specificity := matchRoutePath(routePath, normalizedPath)
		if !matched {
			continue
		}

		priority := 0
		switch {
		case routeMethod == normalizedMethod && !templated:
			priority = 1
		case routeMethod == normalizedMethod && templated:
			priority = 2
		case routeMethod == "ANY" && !templated:
			priority = 3
		case routeMethod == "ANY" && templated:
			priority = 4
		default:
			continue
		}

		candidates = append(candidates, candidateRoute{
			route:       route,
			pathParams:  params,
			priority:    priority,
			specificity: specificity,
		})
	}

	if len(candidates) == 0 {
		if defaultRoute != nil {
			return defaultRoute, nil, nil
		}
		return nil, nil, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity > candidates[j].specificity
		}
		return candidates[i].route.RouteKey < candidates[j].route.RouteKey
	})

	best := candidates[0]
	return best.route, best.pathParams, nil
}

func parseRouteKey(routeKey string) (method string, path string, err error) {
	routeKey = strings.TrimSpace(routeKey)
	if routeKey == "" {
		return "", "", errors.New("RouteKey is required")
	}
	if routeKey == "$default" {
		return "$default", "", nil
	}
	parts := strings.SplitN(routeKey, " ", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid RouteKey %q", routeKey)
	}
	method = strings.ToUpper(strings.TrimSpace(parts[0]))
	path = normalizePath(parts[1])
	if path == "" {
		return "", "", fmt.Errorf("invalid RouteKey %q", routeKey)
	}
	return method, path, nil
}

func normalizeRouteKey(routeKey string) string {
	method, path, err := parseRouteKey(routeKey)
	if err != nil {
		return strings.TrimSpace(routeKey)
	}
	if method == "$default" {
		return "$default"
	}
	return method + " " + path
}

func parseIntegrationTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("target is required")
	}
	if !strings.HasPrefix(target, "integrations/") {
		return "", fmt.Errorf("unsupported Target %q (expected integrations/{integrationId})", target)
	}
	parts := strings.Split(target, "/")
	if len(parts) != 2 || parts[1] == "" {
		return "", fmt.Errorf("invalid Target %q", target)
	}
	return parts[1], nil
}

func parseLambdaIntegrationURI(integrationURI string) (lambdaArn string, functionName string, err error) {
	integrationURI = strings.TrimSpace(integrationURI)
	if integrationURI == "" {
		return "", "", errors.New("IntegrationUri is required")
	}

	if strings.HasPrefix(integrationURI, "arn:aws:lambda:") {
		name, err := lambdaFunctionNameFromARN(integrationURI)
		if err != nil {
			return "", "", err
		}
		return integrationURI, name, nil
	}

	prefix := ":lambda:path/2015-03-31/functions/"
	if strings.HasPrefix(integrationURI, "arn:aws:apigateway:") && strings.Contains(integrationURI, prefix) && strings.HasSuffix(integrationURI, "/invocations") {
		start := strings.Index(integrationURI, prefix)
		if start == -1 {
			return "", "", fmt.Errorf("invalid IntegrationUri %q", integrationURI)
		}
		start += len(prefix)
		end := strings.LastIndex(integrationURI, "/invocations")
		if end <= start {
			return "", "", fmt.Errorf("invalid IntegrationUri %q", integrationURI)
		}
		lambdaArn = integrationURI[start:end]
		name, err := lambdaFunctionNameFromARN(lambdaArn)
		if err != nil {
			return "", "", err
		}
		return lambdaArn, name, nil
	}

	return "", "", fmt.Errorf("unsupported IntegrationUri %q", integrationURI)
}

func lambdaFunctionNameFromARN(arn string) (string, error) {
	idx := strings.Index(arn, ":function:")
	if idx == -1 {
		return "", fmt.Errorf("invalid lambda arn %q", arn)
	}
	tail := arn[idx+len(":function:"):]
	if tail == "" {
		return "", fmt.Errorf("invalid lambda arn %q", arn)
	}
	parts := strings.Split(tail, ":")
	if parts[0] == "" {
		return "", fmt.Errorf("invalid lambda arn %q", arn)
	}
	return parts[0], nil
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
		if p == "" {
			p = "/"
		}
	}
	return p
}

func matchRoutePath(templatePath, actualPath string) (params map[string]string, matched bool, templated bool, specificity int) {
	templatePath = normalizePath(templatePath)
	actualPath = normalizePath(actualPath)

	if templatePath == actualPath {
		return nil, true, false, len(pathSegments(templatePath))
	}

	templateSeg := pathSegments(templatePath)
	actualSeg := pathSegments(actualPath)

	params = make(map[string]string)
	i := 0
	j := 0
	for i < len(templateSeg) {
		seg := templateSeg[i]
		if isTemplateSegment(seg) {
			templated = true
			name, greedy := parseTemplateSegment(seg)
			if greedy {
				if i != len(templateSeg)-1 {
					return nil, false, true, 0
				}
				params[name] = strings.Join(actualSeg[j:], "/")
				j = len(actualSeg)
				break
			}
			if j >= len(actualSeg) {
				return nil, false, true, 0
			}
			params[name] = actualSeg[j]
			i++
			j++
			continue
		}

		if j >= len(actualSeg) || seg != actualSeg[j] {
			return nil, false, templated, 0
		}
		specificity++
		i++
		j++
	}

	if j != len(actualSeg) {
		return nil, false, templated, 0
	}
	if len(params) == 0 {
		params = nil
	}
	return params, true, templated, specificity
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

func isTemplateSegment(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func parseTemplateSegment(seg string) (name string, greedy bool) {
	inner := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
	if strings.HasSuffix(inner, "+") {
		return strings.TrimSuffix(inner, "+"), true
	}
	return inner, false
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

func (s *Service) invokeSQSIntegration(integration *types.APIGatewayIntegration, input *InvokeInput, pathParams map[string]string) (*InvokeOutput, error) {
	if s.sqsSend == nil {
		return nil, errors.New("SQS service not configured for AWS integration type")
	}

	// Resolve message body: use RequestParameters["MessageBody"] expression if present,
	// otherwise fall back to the raw request body.
	body := string(input.Body)
	if expr, ok := integration.RequestParameters["MessageBody"]; ok {
		body = evaluateExpression(expr, input, pathParams)
	}

	messageID, md5, err := s.sqsSend(integration.SQSQueueName, body)
	if err != nil {
		return nil, fmt.Errorf("SQS send failed: %w", err)
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

// evaluateExpression evaluates an HTTP API v2 request parameter expression against
// the incoming request. Supported forms:
//
//	$request.body              — full request body as string
//	$request.body.field        — top-level JSON field extracted from body
//	$request.header.name       — request header value
//	$request.querystring.name  — query string parameter
//	$request.path.name         — path parameter captured by the route
//	'literal'                  — static string (single-quoted)
func evaluateExpression(expr string, input *InvokeInput, pathParams map[string]string) string {
	expr = strings.TrimSpace(expr)

	// Static literal: 'value'
	if len(expr) >= 2 && expr[0] == '\'' && expr[len(expr)-1] == '\'' {
		return expr[1 : len(expr)-1]
	}

	switch {
	case expr == "$request.body":
		return string(input.Body)

	case strings.HasPrefix(expr, "$request.body."):
		field := expr[len("$request.body."):]
		return extractJSONField(input.Body, field)

	case strings.HasPrefix(expr, "$request.header."):
		name := expr[len("$request.header."):]
		return input.Headers.Get(name)

	case strings.HasPrefix(expr, "$request.querystring."):
		name := expr[len("$request.querystring."):]
		return input.Query.Get(name)

	case strings.HasPrefix(expr, "$request.path."):
		name := expr[len("$request.path."):]
		if pathParams != nil {
			return pathParams[name]
		}
	}

	return expr
}

// extractJSONField extracts a top-level string or scalar field from a JSON object body.
func extractJSONField(body []byte, field string) string {
	if len(body) == 0 {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return ""
	}
	raw, ok := obj[field]
	if !ok {
		return ""
	}
	// Unwrap JSON strings; return other scalars as-is.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func parseSQSIntegrationURI(uri string) (sqsArn string, queueName string, err error) {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "arn:aws:sqs:") {
		return "", "", fmt.Errorf("invalid SQS IntegrationUri %q (expected arn:aws:sqs:...)", uri)
	}
	parts := strings.Split(uri, ":")
	if len(parts) < 6 || parts[5] == "" {
		return "", "", fmt.Errorf("invalid SQS ARN in IntegrationUri: %q", uri)
	}
	return uri, parts[5], nil
}

func isTransientInvokeError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "eof") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe")
}
