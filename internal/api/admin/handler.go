package admin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	apigatewaysvc "github.com/openstack-project/openstack/internal/apigateway"
	apigatewayv1svc "github.com/openstack-project/openstack/internal/apigatewayv1"
	"github.com/openstack-project/openstack/internal/collection"
	"github.com/openstack-project/openstack/internal/config"
	eventbridgesvc "github.com/openstack-project/openstack/internal/eventbridge"
	eventsourcesvc "github.com/openstack-project/openstack/internal/eventsource"
	infrasvc "github.com/openstack-project/openstack/internal/infrastructure"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	logssvc "github.com/openstack-project/openstack/internal/logs"
	s3svc "github.com/openstack-project/openstack/internal/s3"
	secretssvc "github.com/openstack-project/openstack/internal/secrets"
	snssvc "github.com/openstack-project/openstack/internal/sns"
	sqssvc "github.com/openstack-project/openstack/internal/sqs"
	tracesvc "github.com/openstack-project/openstack/internal/trace"
	"github.com/openstack-project/openstack/pkg/types"
)

// Handler serves JSON endpoints used by the dashboard UI.
type Handler struct {
	cfg        *config.Config
	apigw      *apigatewaysvc.Service
	apigwv1    *apigatewayv1svc.Service
	lambda     *lambdasvc.Service
	logs       *logssvc.Service
	s3         *s3svc.Service
	sqs        *sqssvc.Service
	sns        *snssvc.Service
	secrets    *secretssvc.Service
	infra      *infrasvc.Service
	esm        *eventsourcesvc.Service
	eventbridge *eventbridgesvc.Service
	traceStore *tracesvc.Store
}

func NewHandler(cfg *config.Config, apigw *apigatewaysvc.Service, apigwv1 *apigatewayv1svc.Service, lambda *lambdasvc.Service, logs *logssvc.Service, sqs *sqssvc.Service, sns *snssvc.Service, secrets *secretssvc.Service, infra *infrasvc.Service, s3 *s3svc.Service, esm *eventsourcesvc.Service, eventbridge *eventbridgesvc.Service, traceStore *tracesvc.Store) *Handler {
	return &Handler{
		cfg:        cfg,
		apigw:      apigw,
		apigwv1:    apigwv1,
		lambda:     lambda,
		logs:       logs,
		s3:         s3,
		sqs:        sqs,
		sns:        sns,
		secrets:    secrets,
		infra:      infra,
		esm:        esm,
		eventbridge: eventbridge,
		traceStore: traceStore,
	}
}

type overviewResponse struct {
	Status              string                 `json:"status"`
	Timestamp           time.Time              `json:"timestamp"`
	Services            []string               `json:"services"`
	Config              overviewConfig         `json:"config"`
	Counts              overviewCounts         `json:"counts"`
	Gateways            []gatewaySummary       `json:"gateways"`
	Functions           []functionSummary      `json:"functions"`
	Queues              []queueSummary         `json:"queues"`
	Topics              []topicSummary         `json:"topics"`
	Subscriptions       []subscriptionSummary  `json:"subscriptions"`
	Secrets             []secretSummary        `json:"secrets"`
	Buckets             []s3BucketSummary      `json:"buckets"`
	EventSourceMappings []esmSummary           `json:"eventSourceMappings"`
	EventBridgeRules    []eventBridgeRuleSummary `json:"eventBridgeRules,omitempty"`
	Infrastructure      []infrasvc.ProbeResult `json:"infrastructure"`
	Connections         []infraConnection      `json:"connections,omitempty"`
	RecentTraces        []*tracesvc.Trace      `json:"recentTraces,omitempty"`
	Warnings            []string               `json:"warnings,omitempty"`
}

type overviewConfig struct {
	Region    string `json:"region"`
	AccountID string `json:"accountId"`
	Endpoint  string `json:"endpoint"`
	DataDir   string `json:"dataDir"`
	UIEnabled bool   `json:"uiEnabled"`
}

type overviewCounts struct {
	Gateways            int `json:"gateways"`
	Functions           int `json:"functions"`
	Queues              int `json:"queues"`
	Topics              int `json:"topics"`
	Subscriptions       int `json:"subscriptions"`
	Secrets             int `json:"secrets"`
	Buckets             int `json:"buckets"`
	LogGroups           int `json:"logGroups"`
	EventSourceMappings int `json:"eventSourceMappings"`
	EventBridgeRules    int `json:"eventBridgeRules"`
}

type esmSummary struct {
	UUID           string                `json:"uuid"`
	QueueName      string                `json:"queueName"`
	FunctionName   string                `json:"functionName"`
	BatchSize      int                   `json:"batchSize"`
	State          string                `json:"state"`
	LastResult     string                `json:"lastResult,omitempty"`
	FilterCriteria *types.FilterCriteria `json:"filterCriteria,omitempty"`
}

type eventBridgeTargetSummary struct {
	ID           string    `json:"id"`
	Arn          string    `json:"arn"`
	LastResult   string    `json:"lastResult,omitempty"`
	LastInvokedAt *time.Time `json:"lastInvokedAt,omitempty"`
}

type eventBridgeRuleSummary struct {
	Name               string                    `json:"name"`
	Arn                string                    `json:"arn"`
	ScheduleExpression string                    `json:"scheduleExpression"`
	State              string                    `json:"state"`
	Description        string                    `json:"description,omitempty"`
	LastRunAt          *time.Time                `json:"lastRunAt,omitempty"`
	NextRunAt          *time.Time                `json:"nextRunAt,omitempty"`
	LastResult         string                    `json:"lastResult,omitempty"`
	Targets            []eventBridgeTargetSummary `json:"targets,omitempty"`
}

type s3BucketSummary struct {
	Name        string    `json:"name"`
	Objects     int       `json:"objects"`
	TotalSize   int64     `json:"totalSize"`
	CreatedDate time.Time `json:"createdDate"`
}

type routeDetailSummary struct {
	RouteKey            string            `json:"routeKey"`
	Method              string            `json:"method,omitempty"`
	Path                string            `json:"path,omitempty"`
	IntegrationType     string            `json:"integrationType,omitempty"`
	IntegrationTarget   string            `json:"integrationTarget,omitempty"`
	RequestTemplates    map[string]string `json:"requestTemplates,omitempty"`
	RequestParameters   map[string]string `json:"requestParameters,omitempty"`
	MethodRequestParams map[string]bool   `json:"methodRequestParams,omitempty"` // v1 method-level: "method.request.header.X-Foo": required
	BodyExample         json.RawMessage   `json:"bodyExample,omitempty"`         // best-match event from events/ folder
}

type gatewaySummary struct {
	APIID        string               `json:"apiId"`
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	ProtocolType string               `json:"protocolType"`
	Version      string               `json:"version"`
	Arn          string               `json:"arn"`
	ApiEndpoint  string               `json:"apiEndpoint"`
	DefaultStage string               `json:"defaultStage"`
	InvokeURL    string               `json:"invokeUrl"`
	Routes       int                  `json:"routes"`
	Integrations int                  `json:"integrations"`
	Stages       int                  `json:"stages"`
	Tags         map[string]string    `json:"tags,omitempty"`
	TagCount     int                  `json:"tagCount"`
	RouteKeys    []string             `json:"routeKeys,omitempty"`
	RouteDetails []routeDetailSummary `json:"routeDetails,omitempty"`
}

type functionSummary struct {
	Name              string            `json:"name"`
	Arn               string            `json:"arn"`
	Runtime           string            `json:"runtime"`
	State             string            `json:"state"`
	TimeoutSec        int               `json:"timeoutSec"`
	MemoryMB          int               `json:"memoryMB"`
	CodeSize          int64             `json:"codeSize"`
	MessagesProcessed int64             `json:"messagesProcessed"`
	Version           string            `json:"version"`
	LastModified      time.Time         `json:"lastModified"`
	Layers            int               `json:"layers"`
	Tags              map[string]string `json:"tags,omitempty"`
	TagCount          int               `json:"tagCount"`
}

type infraConnection struct {
	SourceFunction string                `json:"sourceFunction"`
	TargetID       string                `json:"targetId"`
	TargetName     string                `json:"targetName"`
	TargetKind     string                `json:"targetKind"`
	TargetHost     string                `json:"targetHost"`
	TargetPort     int                   `json:"targetPort"`
	Evidence       string                `json:"evidence"`
	Source         string                `json:"source"`
	FilterCriteria *types.FilterCriteria `json:"filterCriteria,omitempty"`
}

type queueSummary struct {
	Name             string            `json:"name"`
	URL              string            `json:"url"`
	Arn              string            `json:"arn"`
	FIFO             bool              `json:"fifo"`
	VisibilitySec    int               `json:"visibilitySec"`
	WaitTimeSec      int               `json:"waitTimeSec"`
	ApproxVisible    int               `json:"approxVisible"`
	ApproxInFlight   int               `json:"approxInFlight"`
	ApproxDelayed    int               `json:"approxDelayed"`
	ApproxStale      int               `json:"approxStale"`
	CreatedTimestamp int64             `json:"createdTimestamp"`
	DLQName          string            `json:"dlqName,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
	TagCount         int               `json:"tagCount"`
	RecentMessages   []queueMessage    `json:"recentMessages,omitempty"`
}

type topicSummary struct {
	Name             string            `json:"name"`
	Arn              string            `json:"arn"`
	FIFO             bool              `json:"fifo"`
	Subscriptions    int               `json:"subscriptions"`
	CreatedTimestamp int64             `json:"createdTimestamp"`
	Tags             map[string]string `json:"tags,omitempty"`
	TagCount         int               `json:"tagCount"`
}

type subscriptionSummary struct {
	SubscriptionArn    string `json:"subscriptionArn"`
	TopicArn           string `json:"topicArn"`
	TopicName          string `json:"topicName"`
	Protocol           string `json:"protocol"`
	Endpoint           string `json:"endpoint"`
	RawMessageDelivery bool   `json:"rawMessageDelivery"`
	FilterPolicy       string `json:"filterPolicy,omitempty"`
	FilterPolicyScope  string `json:"filterPolicyScope,omitempty"`
}

type queueMessage struct {
	ID           string `json:"id"`
	Body         string `json:"body"`
	State        string `json:"state"`
	SentAt       int64  `json:"sentAt"`
	ReceiveCount int    `json:"receiveCount"`
}

type secretSummary struct {
	Name            string            `json:"name"`
	Arn             string            `json:"arn"`
	Description     string            `json:"description,omitempty"`
	VersionID       string            `json:"versionId"`
	Tags            map[string]string `json:"tags,omitempty"`
	TagCount        int               `json:"tagCount"`
	CreatedDate     time.Time         `json:"createdDate"`
	LastChangedDate time.Time         `json:"lastChangedDate"`
}

// pickBodyExample selects the most relevant event file for a given HTTP method
// from the map returned by lambda.GetEventExamples (filename → raw JSON).
//
// Priority:
//  1. Filename starts with the lowercase method (e.g. "post-batch" for POST).
//  2. File is a full API Gateway proxy event whose httpMethod matches.
//  3. Any plain JSON object (not a proxy event).
//
// For proxy events the body field is extracted (and decoded if it is a JSON string).
func pickBodyExample(events map[string]json.RawMessage, method string) json.RawMessage {
	if len(events) == 0 {
		return nil
	}
	lm := strings.ToLower(method)

	extractProxyBody := func(data json.RawMessage) json.RawMessage {
		var proxy struct {
			HTTPMethod string          `json:"httpMethod"`
			Body       json.RawMessage `json:"body"`
		}
		if json.Unmarshal(data, &proxy) != nil || !strings.EqualFold(proxy.HTTPMethod, method) {
			return nil
		}
		if len(proxy.Body) == 0 {
			return nil
		}
		// body is sometimes a JSON-encoded string — decode it
		var bodyStr string
		if json.Unmarshal(proxy.Body, &bodyStr) == nil && json.Valid([]byte(bodyStr)) {
			return json.RawMessage(bodyStr)
		}
		if json.Valid(proxy.Body) {
			return proxy.Body
		}
		return nil
	}

	isProxyEvent := func(data json.RawMessage) bool {
		var check struct {
			HTTPMethod string `json:"httpMethod"`
		}
		return json.Unmarshal(data, &check) == nil && check.HTTPMethod != ""
	}

	// Pass 1: filename prefix match (post-batch.json, patch-status.json …)
	for name, data := range events {
		if !strings.HasPrefix(strings.ToLower(name), lm) {
			continue
		}
		if body := extractProxyBody(data); body != nil {
			return body
		}
		if !isProxyEvent(data) {
			return data
		}
	}

	// Pass 2: proxy event with matching httpMethod
	for _, data := range events {
		if body := extractProxyBody(data); body != nil {
			return body
		}
	}

	// Pass 3: any plain JSON object
	for _, data := range events {
		if !isProxyEvent(data) {
			return data
		}
	}
	return nil
}

// Overview returns a dashboard-friendly snapshot of current resources.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	gateways := h.apigw.ListAPIs()
	var v1APIs []*types.RestAPI
	if h.apigwv1 != nil {
		v1APIs = h.apigwv1.ListAPIs()
	}

	functions, err := h.lambda.ListFunctions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list functions")
		return
	}

	queues := h.sqs.ListQueues("")
	topics := make([]*types.SNSTopic, 0)
	subscriptions := make([]*types.SNSSubscription, 0)
	if h.sns != nil {
		topics = h.sns.ListTopics()
		subscriptions = h.sns.ListSubscriptions()
	}
	secrets := h.secrets.ListSecrets()
	buckets := h.s3.ListBuckets()
	logGroups := h.logs.ListGroups()
	infraResults := []infrasvc.ProbeResult{}
	if h.infra != nil {
		infraResults = h.infra.Results()
	}

	var esmMappings []*types.EventSourceMapping
	if h.esm != nil {
		esmMappings = h.esm.ListMappings("", "")
	}
	var eventBridgeRules []*types.EventBridgeRule
	if h.eventbridge != nil {
		eventBridgeRules, _, _ = h.eventbridge.ListRules("", "", 500, "")
	}

	resp := overviewResponse{
		Status:    "running",
		Timestamp: time.Now().UTC(),
		Services:  []string{"apigateway", "apigatewayv2", "lambda", "s3", "sqs", "sns", "secretsmanager", "eventsource", "eventbridge"},
		Config: overviewConfig{
			Region:    h.cfg.Region,
			AccountID: h.cfg.AccountID,
			Endpoint:  h.cfg.Endpoint(),
			DataDir:   h.cfg.DataDir,
			UIEnabled: h.cfg.UIEnabled,
		},
		Counts: overviewCounts{
			Gateways:            len(gateways) + len(v1APIs),
			Functions:           len(functions),
			Queues:              len(queues),
			Topics:              len(topics),
			Subscriptions:       len(subscriptions),
			Secrets:             len(secrets),
			Buckets:             len(buckets),
			LogGroups:           len(logGroups),
			EventSourceMappings: len(esmMappings),
			EventBridgeRules:    len(eventBridgeRules),
		},
		Gateways:            make([]gatewaySummary, 0, len(gateways)),
		Functions:           make([]functionSummary, 0, len(functions)),
		Queues:              make([]queueSummary, 0, len(queues)),
		Topics:              make([]topicSummary, 0, len(topics)),
		Subscriptions:       make([]subscriptionSummary, 0, len(subscriptions)),
		Secrets:             make([]secretSummary, 0, len(secrets)),
		Buckets:             make([]s3BucketSummary, 0, len(buckets)),
		EventSourceMappings: make([]esmSummary, 0, len(esmMappings)),
		EventBridgeRules:    make([]eventBridgeRuleSummary, 0, len(eventBridgeRules)),
		Infrastructure:      infraResults,
		Connections:         inferInfraConnections(functions, infraResults),
		RecentTraces:        h.recentTraces(),
	}

	// Event source mappings
	for _, m := range esmMappings {
		resp.EventSourceMappings = append(resp.EventSourceMappings, esmSummary{
			UUID:           m.UUID,
			QueueName:      m.QueueName,
			FunctionName:   m.FunctionName,
			BatchSize:      m.BatchSize,
			State:          m.State,
			LastResult:     m.LastProcessingResult,
			FilterCriteria: m.FilterCriteria,
		})

		// Add SQS→Lambda connection
		resp.Connections = append(resp.Connections, infraConnection{
			SourceFunction: m.QueueName,
			TargetID:       m.FunctionName,
			TargetName:     m.FunctionName,
			TargetKind:     "sqs-lambda",
			Evidence:       "esm",
			Source:         m.UUID,
			FilterCriteria: m.FilterCriteria,
		})
	}

	// EventBridge scheduled-rule summaries + EventBridge→Lambda connections.
	for _, rule := range eventBridgeRules {
		targets := make([]eventBridgeTargetSummary, 0, len(rule.Targets))
		for _, target := range rule.Targets {
			targets = append(targets, eventBridgeTargetSummary{
				ID:            target.ID,
				Arn:           target.Arn,
				LastResult:    target.LastResult,
				LastInvokedAt: target.LastInvokedAt,
			})
			targetName := lambdaNameFromARNOrName(target.Arn)
			resp.Connections = append(resp.Connections, infraConnection{
				SourceFunction: rule.Name,
				TargetID:       targetName,
				TargetName:     targetName,
				TargetKind:     "eventbridge-lambda",
				Evidence:       "rule-target",
				Source:         target.ID,
			})
		}
		resp.EventBridgeRules = append(resp.EventBridgeRules, eventBridgeRuleSummary{
			Name:               rule.Name,
			Arn:                rule.Arn,
			ScheduleExpression: rule.ScheduleExpression,
			State:              rule.State,
			Description:        rule.Description,
			LastRunAt:          rule.LastRunAt,
			NextRunAt:          rule.NextRunAt,
			LastResult:         rule.LastResult,
			Targets:            targets,
		})
	}
	sort.Slice(resp.EventBridgeRules, func(i, j int) bool { return resp.EventBridgeRules[i].Name < resp.EventBridgeRules[j].Name })

	// S3→Lambda notification connections
	for _, b := range buckets {
		notifCfg := h.s3.GetBucketNotificationConfiguration(b.Name)
		if notifCfg == nil {
			continue
		}
		for _, lc := range notifCfg.LambdaConfigurations {
			resp.Connections = append(resp.Connections, infraConnection{
				SourceFunction: b.Name,
				TargetID:       lc.LambdaFunctionName,
				TargetName:     lc.LambdaFunctionName,
				TargetKind:     "s3-lambda",
				Evidence:       "notification",
				Source:         lc.ID,
			})
		}
	}

	// APIGW→SQS integration connections (added inside gateway loop below)

	for _, api := range gateways {
		routes, err := h.apigw.ListRoutes(api.APIID)
		if err != nil {
			resp.Warnings = append(resp.Warnings, "gateway routes unavailable for "+api.APIID)
			routes = nil
		}
		integrations, err := h.apigw.ListIntegrations(api.APIID)
		if err != nil {
			resp.Warnings = append(resp.Warnings, "gateway integrations unavailable for "+api.APIID)
			integrations = nil
		}
		stages, err := h.apigw.ListStages(api.APIID)
		if err != nil {
			resp.Warnings = append(resp.Warnings, "gateway stages unavailable for "+api.APIID)
			stages = nil
		}

		routeKeys := make([]string, 0, len(routes))
		for _, route := range routes {
			routeKeys = append(routeKeys, route.RouteKey)
		}
		sort.Strings(routeKeys)

		defaultStage := "$default"
		invokeURL := ""
		for _, stage := range stages {
			if stage.StageName == "$default" {
				defaultStage = stage.StageName
				invokeURL = stage.InvokeURL
				break
			}
		}
		if invokeURL == "" {
			invokeURL = api.APIEndpoint
		}

		// Build per-route integration detail for the UI
		integByID := make(map[string]*types.APIGatewayIntegration, len(integrations))
		for _, ig := range integrations {
			integByID[ig.IntegrationID] = ig
		}
		routeDetails := make([]routeDetailSummary, 0, len(routes))
		for _, route := range routes {
			detail := routeDetailSummary{RouteKey: route.RouteKey}
			if parts := strings.SplitN(route.RouteKey, " ", 2); len(parts) == 2 {
				detail.Method = parts[0]
				detail.Path = parts[1]
			}
			integID := strings.TrimPrefix(route.Target, "integrations/")
			if ig, ok := integByID[integID]; ok {
				detail.IntegrationType = ig.IntegrationType
				switch {
				case ig.SQSQueueName != "":
					detail.IntegrationTarget = "sqs:" + ig.SQSQueueName
				case ig.LambdaFunctionName != "":
					detail.IntegrationTarget = "lambda:" + ig.LambdaFunctionName
				}
				if len(ig.RequestParameters) > 0 {
					detail.RequestParameters = ig.RequestParameters
				}
			}
			routeDetails = append(routeDetails, detail)
		}

		resp.Gateways = append(resp.Gateways, gatewaySummary{
			APIID:        api.APIID,
			Name:         api.Name,
			Description:  api.Description,
			ProtocolType: api.ProtocolType,
			Version:      "v2",
			Arn:          api.APIArn,
			ApiEndpoint:  api.APIEndpoint,
			DefaultStage: defaultStage,
			InvokeURL:    invokeURL,
			Routes:       len(routes),
			Integrations: len(integrations),
			Stages:       len(stages),
			Tags:         cloneStringMap(api.Tags),
			TagCount:     len(api.Tags),
			RouteKeys:    routeKeys,
			RouteDetails: routeDetails,
		})

		// APIGW→SQS and APIGW→Lambda connections
		for _, integ := range integrations {
			if integ.IntegrationType == "AWS" && integ.SQSQueueName != "" {
				resp.Connections = append(resp.Connections, infraConnection{
					SourceFunction: api.APIID,
					TargetID:       integ.SQSQueueName,
					TargetName:     integ.SQSQueueName,
					TargetKind:     "apigw-sqs",
					Evidence:       "integration",
					Source:         integ.IntegrationID,
				})
			}
			if integ.IntegrationType == "AWS_PROXY" && integ.LambdaFunctionName != "" {
				resp.Connections = append(resp.Connections, infraConnection{
					SourceFunction: api.APIID,
					TargetID:       integ.LambdaFunctionName,
					TargetName:     integ.LambdaFunctionName,
					TargetKind:     "apigw-lambda",
					Evidence:       "integration",
					Source:         integ.IntegrationID,
				})
			}
		}
	}
	// API Gateway v1 (REST APIs)
	for _, v1api := range v1APIs {
		stages, err := h.apigwv1.ListStages(v1api.ID)
		if err != nil {
			resp.Warnings = append(resp.Warnings, "v1 gateway stages unavailable for "+v1api.ID)
			stages = nil
		}
		integrations, err := h.apigwv1.ListIntegrations(v1api.ID)
		if err != nil {
			resp.Warnings = append(resp.Warnings, "v1 gateway integrations unavailable for "+v1api.ID)
			integrations = nil
		}
		resources, err := h.apigwv1.ListResources(v1api.ID)
		if err != nil {
			resp.Warnings = append(resp.Warnings, "v1 gateway resources unavailable for "+v1api.ID)
			resources = nil
		}

		defaultStage := ""
		invokeURL := ""
		if len(stages) > 0 {
			defaultStage = stages[0].StageName
			invokeURL = stages[0].InvokeURL
		}

		// Collect route-like paths from resources (non-root)
		routeKeys := make([]string, 0)
		for _, res := range resources {
			if res.Path != "/" {
				routeKeys = append(routeKeys, res.Path)
			}
		}
		sort.Strings(routeKeys)

		// Build per-route integration detail for the UI
		resourcePathByID := make(map[string]string, len(resources))
		for _, res := range resources {
			resourcePathByID[res.ID] = res.Path
		}
		// Cache event examples per Lambda function to avoid re-reading the zip
		// for every route that targets the same function.
		lambdaEvents := make(map[string]map[string]json.RawMessage)
		v1RouteDetails := make([]routeDetailSummary, 0, len(integrations))
		for _, integ := range integrations {
			resPath := resourcePathByID[integ.ResourceID]
			detail := routeDetailSummary{
				RouteKey:        integ.MethodHTTPMethod + " " + resPath,
				Method:          integ.MethodHTTPMethod,
				Path:            resPath,
				IntegrationType: integ.Type,
			}
			switch {
			case integ.SQSQueueName != "":
				detail.IntegrationTarget = "sqs:" + integ.SQSQueueName
			case integ.LambdaFunctionName != "":
				detail.IntegrationTarget = "lambda:" + integ.LambdaFunctionName
			}
			if len(integ.RequestTemplates) > 0 {
				detail.RequestTemplates = integ.RequestTemplates
			}
			// Fetch method-level request parameters (required headers, query params, body flag).
			if method, err := h.apigwv1.GetMethod(v1api.ID, integ.ResourceID, integ.MethodHTTPMethod); err == nil && len(method.RequestParameters) > 0 {
				detail.MethodRequestParams = method.RequestParameters
			}
			// Populate body example from the Lambda's events/ folder.
			if integ.LambdaFunctionName != "" {
				if _, cached := lambdaEvents[integ.LambdaFunctionName]; !cached {
					lambdaEvents[integ.LambdaFunctionName] = h.lambda.GetEventExamples(integ.LambdaFunctionName)
				}
				if example := pickBodyExample(lambdaEvents[integ.LambdaFunctionName], integ.MethodHTTPMethod); example != nil {
					detail.BodyExample = example
				}
			}
			v1RouteDetails = append(v1RouteDetails, detail)
		}
		sort.Slice(v1RouteDetails, func(i, j int) bool {
			return v1RouteDetails[i].RouteKey < v1RouteDetails[j].RouteKey
		})

		resp.Gateways = append(resp.Gateways, gatewaySummary{
			APIID:        v1api.ID,
			Name:         v1api.Name,
			Description:  v1api.Description,
			ProtocolType: "REST",
			Version:      "v1",
			Arn:          v1api.APIArn,
			ApiEndpoint:  invokeURL,
			DefaultStage: defaultStage,
			InvokeURL:    invokeURL,
			Routes:       len(resources) - 1, // exclude root
			Integrations: len(integrations),
			Stages:       len(stages),
			Tags:         cloneStringMap(v1api.Tags),
			TagCount:     len(v1api.Tags),
			RouteKeys:    routeKeys,
			RouteDetails: v1RouteDetails,
		})

		// APIGW v1 → SQS/Lambda connections
		for _, integ := range integrations {
			if integ.SQSQueueName != "" {
				resp.Connections = append(resp.Connections, infraConnection{
					SourceFunction: v1api.ID,
					TargetID:       integ.SQSQueueName,
					TargetName:     integ.SQSQueueName,
					TargetKind:     "apigw-sqs",
					Evidence:       "integration",
					Source:         integ.ResourceID + ":" + integ.MethodHTTPMethod,
				})
			}
			if integ.LambdaFunctionName != "" {
				resp.Connections = append(resp.Connections, infraConnection{
					SourceFunction: v1api.ID,
					TargetID:       integ.LambdaFunctionName,
					TargetName:     integ.LambdaFunctionName,
					TargetKind:     "apigw-lambda",
					Evidence:       "integration",
					Source:         integ.ResourceID + ":" + integ.MethodHTTPMethod,
				})
			}
		}
	}

	sort.Slice(resp.Gateways, func(i, j int) bool {
		if resp.Gateways[i].Name == resp.Gateways[j].Name {
			return resp.Gateways[i].APIID < resp.Gateways[j].APIID
		}
		return resp.Gateways[i].Name < resp.Gateways[j].Name
	})

	for _, fn := range functions {
		metrics := h.lambda.GetFunctionMetrics(fn.FunctionName)
		resp.Functions = append(resp.Functions, functionSummary{
			Name:              fn.FunctionName,
			Arn:               fn.FunctionArn,
			Runtime:           string(fn.Runtime),
			State:             string(fn.State),
			TimeoutSec:        fn.Timeout,
			MemoryMB:          fn.MemorySize,
			CodeSize:          fn.CodeSize,
			MessagesProcessed: metrics.MessagesProcessed,
			Version:           fn.Version,
			LastModified:      fn.LastModified,
			Layers:            len(fn.Layers),
			Tags:              cloneStringMap(fn.Tags),
			TagCount:          len(fn.Tags),
		})
	}
	sort.Slice(resp.Functions, func(i, j int) bool { return resp.Functions[i].Name < resp.Functions[j].Name })

	for _, q := range queues {
		dlqName := ""
		if q.DeadLetterTargetArn != "" {
			parts := strings.Split(q.DeadLetterTargetArn, ":")
			if len(parts) > 0 {
				dlqName = parts[len(parts)-1]
			}
		}
		summary := queueSummary{
			Name:             q.QueueName,
			URL:              q.QueueUrl,
			Arn:              q.QueueArn,
			FIFO:             q.FifoQueue,
			VisibilitySec:    q.VisibilityTimeout,
			WaitTimeSec:      q.ReceiveMessageWaitTimeSeconds,
			CreatedTimestamp: q.CreatedTimestamp,
			DLQName:          dlqName,
			Tags:             cloneStringMap(q.Tags),
			TagCount:         len(q.Tags),
		}

		if dlqName != "" {
			resp.Connections = append(resp.Connections, infraConnection{
				SourceFunction: q.QueueName,
				TargetID:       dlqName,
				TargetName:     dlqName,
				TargetKind:     "queue-dlq",
				Evidence:       "dlq-config",
				Source:         q.QueueArn,
			})
		}

		attrs, err := h.sqs.GetQueueAttributes(q.QueueName, []string{
			"ApproximateNumberOfMessages",
			"ApproximateNumberOfMessagesNotVisible",
			"ApproximateNumberOfMessagesDelayed",
			"ApproximateNumberOfMessagesStale",
		})
		if err != nil {
			resp.Warnings = append(resp.Warnings, "queue metrics unavailable for "+q.QueueName)
		} else {
			summary.ApproxVisible = parseInt(attrs["ApproximateNumberOfMessages"])
			summary.ApproxInFlight = parseInt(attrs["ApproximateNumberOfMessagesNotVisible"])
			summary.ApproxDelayed = parseInt(attrs["ApproximateNumberOfMessagesDelayed"])
			summary.ApproxStale = parseInt(attrs["ApproximateNumberOfMessagesStale"])
		}

		msgs, err := h.sqs.PeekMessages(q.QueueName, 8)
		if err != nil {
			resp.Warnings = append(resp.Warnings, "queue messages unavailable for "+q.QueueName)
		} else {
			now := time.Now().UnixMilli()
			summary.RecentMessages = make([]queueMessage, 0, len(msgs))
			for _, msg := range msgs {
				state := "visible"
				if msg.Stale {
					state = "stale"
				} else if msg.DelayUntil > now {
					state = "delayed"
				} else if msg.VisibleAt > now {
					state = "inflight"
				}
				summary.RecentMessages = append(summary.RecentMessages, queueMessage{
					ID:           msg.MessageId,
					Body:         truncateText(msg.Body, 160),
					State:        state,
					SentAt:       msg.SentTimestamp,
					ReceiveCount: msg.ApproximateReceiveCount,
				})
			}
		}

		resp.Queues = append(resp.Queues, summary)
	}
	sort.Slice(resp.Queues, func(i, j int) bool { return resp.Queues[i].Name < resp.Queues[j].Name })

	subscriptionsByTopicArn := make(map[string]int, len(topics))
	for _, sub := range subscriptions {
		subscriptionsByTopicArn[sub.TopicArn]++
	}
	for _, topic := range topics {
		resp.Topics = append(resp.Topics, topicSummary{
			Name:             topic.Name,
			Arn:              topic.TopicArn,
			FIFO:             topic.FifoTopic,
			Subscriptions:    subscriptionsByTopicArn[topic.TopicArn],
			CreatedTimestamp: topic.CreatedTimestamp,
			Tags:             cloneStringMap(topic.Tags),
			TagCount:         len(topic.Tags),
		})
	}
	sort.Slice(resp.Topics, func(i, j int) bool { return resp.Topics[i].Name < resp.Topics[j].Name })

	for _, sub := range subscriptions {
		topicName := topicNameFromARN(sub.TopicArn)
		resp.Subscriptions = append(resp.Subscriptions, subscriptionSummary{
			SubscriptionArn:    sub.SubscriptionArn,
			TopicArn:           sub.TopicArn,
			TopicName:          topicName,
			Protocol:           sub.Protocol,
			Endpoint:           sub.Endpoint,
			RawMessageDelivery: sub.RawMessageDelivery,
			FilterPolicy:       sub.FilterPolicy,
			FilterPolicyScope:  sub.FilterPolicyScope,
		})

		kind := ""
		targetID := sub.Endpoint
		targetName := sub.Endpoint
		switch strings.ToLower(sub.Protocol) {
		case "sqs":
			kind = "sns-sqs"
			targetName = queueNameFromEndpoint(sub.Endpoint)
			targetID = targetName
		case "lambda":
			kind = "sns-lambda"
			targetName = lambdaNameFromARNOrName(sub.Endpoint)
			targetID = targetName
		default:
			// unsupported target types are omitted from topology connections.
		}
		if kind != "" {
			resp.Connections = append(resp.Connections, infraConnection{
				SourceFunction: topicName,
				TargetID:       targetID,
				TargetName:     targetName,
				TargetKind:     kind,
				Evidence:       "subscription",
				Source:         sub.SubscriptionArn,
			})
		}
	}
	sort.Slice(resp.Subscriptions, func(i, j int) bool {
		if resp.Subscriptions[i].TopicName == resp.Subscriptions[j].TopicName {
			return resp.Subscriptions[i].SubscriptionArn < resp.Subscriptions[j].SubscriptionArn
		}
		return resp.Subscriptions[i].TopicName < resp.Subscriptions[j].TopicName
	})

	for _, secret := range secrets {
		resp.Secrets = append(resp.Secrets, secretSummary{
			Name:            secret.Name,
			Arn:             secret.ARN,
			Description:     secret.Description,
			VersionID:       secret.VersionId,
			Tags:            secretTagsToMap(secret.Tags),
			TagCount:        len(secret.Tags),
			CreatedDate:     secret.CreatedDate,
			LastChangedDate: secret.LastChangedDate,
		})
	}
	sort.Slice(resp.Secrets, func(i, j int) bool { return resp.Secrets[i].Name < resp.Secrets[j].Name })

	for _, b := range buckets {
		resp.Buckets = append(resp.Buckets, s3BucketSummary{
			Name:        b.Name,
			Objects:     h.s3.ObjectCount(b.Name),
			TotalSize:   h.s3.TotalSize(b.Name),
			CreatedDate: b.CreationDate,
		})
	}
	sort.Slice(resp.Buckets, func(i, j int) bool { return resp.Buckets[i].Name < resp.Buckets[j].Name })

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// recentTraces returns the last 50 traces from the store, or nil if none.
func (h *Handler) recentTraces() []*tracesvc.Trace {
	if h.traceStore == nil {
		return nil
	}
	t := h.traceStore.Recent(50)
	if len(t) == 0 {
		return nil
	}
	return t
}

// SecretValue returns a single secret value by secret name.
func (h *Handler) SecretValue(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "secret name is required")
		return
	}
	if h.secrets == nil {
		writeError(w, http.StatusInternalServerError, "secrets service unavailable")
		return
	}

	secret, err := h.secrets.GetSecretValue(name)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	value := secret.SecretString
	valueType := "string"
	if value == "" && len(secret.SecretBinary) > 0 {
		value = base64.StdEncoding.EncodeToString(secret.SecretBinary)
		valueType = "binary"
	} else if value == "" {
		valueType = "empty"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"name":      secret.Name,
		"value":     value,
		"valueType": valueType,
	})
}

// QueueMessages returns recent queue messages for a single queue.
func (h *Handler) QueueMessages(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "queue name is required")
		return
	}

	limit := 20
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = parsed
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	msgs, err := h.sqs.PeekMessages(name, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	now := time.Now().UnixMilli()
	result := make([]queueMessage, 0, len(msgs))
	for _, msg := range msgs {
		state := "visible"
		if msg.Stale {
			state = "stale"
		} else if msg.DelayUntil > now {
			state = "delayed"
		} else if msg.VisibleAt > now {
			state = "inflight"
		}
		result = append(result, queueMessage{
			ID:           msg.MessageId,
			Body:         msg.Body,
			State:        state,
			SentAt:       msg.SentTimestamp,
			ReceiveCount: msg.ApproximateReceiveCount,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"queue":    name,
		"messages": result,
	})
}

// LogGroups returns all log group summaries.
func (h *Handler) LogGroups(w http.ResponseWriter, r *http.Request) {
	groups := h.logs.ListGroups()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(groups)
}

// LogGroupDetail returns a single log group summary plus its streams.
func (h *Handler) LogGroupDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "group name is required")
		return
	}

	summary, err := h.logs.GetGroup(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if summary == nil {
		writeError(w, http.StatusNotFound, "log group not found")
		return
	}

	streams := h.logs.ListStreams(name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"group":   summary,
		"streams": streams,
	})
}

// LogEvents returns filtered log events from a group.
func (h *Handler) LogEvents(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "group name is required")
		return
	}

	q := r.URL.Query()
	filter := &logssvc.LogFilter{}

	if v := q.Get("level"); v != "" {
		filter.Level = logssvc.LogLevel(strings.ToUpper(v))
	}
	if v := q.Get("pattern"); v != "" {
		filter.Pattern = v
	}
	if v := q.Get("stream"); v != "" {
		filter.StreamName = v
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	if v := q.Get("cursor"); v != "" {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			filter.Cursor = &ts
		}
	}

	events, total := h.logs.GetLogEvents(name, filter)
	if events == nil {
		events = []logssvc.LogEvent{}
	}

	var nextCursor string
	if len(events) > 0 {
		last := events[len(events)-1]
		nextCursor = last.Timestamp.Format(time.RFC3339Nano)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"events":     events,
		"total":      total,
		"nextCursor": nextCursor,
	})
}

// Infrastructure triggers a fresh probe and returns results.
func (h *Handler) Infrastructure(w http.ResponseWriter, r *http.Request) {
	results := h.infra.ProbeAll(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(results)
}

// RunChaos accepts a list of routes and fires one probe per route,
// streaming each ProbeRound result as an NDJSON line.
func (h *Handler) RunChaos(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InvokeBase string `json:"invokeBase"`
		Routes     []struct {
			RouteKey        string                 `json:"routeKey"`
			Method          string                 `json:"method"`
			Path            string                 `json:"path"`
			SeedBody        json.RawMessage        `json:"seedBody,omitempty"`
			RequiredHeaders map[string]string      `json:"requiredHeaders,omitempty"`
			FieldOverrides  map[string]interface{} `json:"fieldOverrides,omitempty"`
			ProbeBodies     []collection.ProbeBody `json:"probeBodies,omitempty"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	if req.InvokeBase == "" || len(req.Routes) == 0 {
		writeError(w, http.StatusBadRequest, "invokeBase and routes are required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	specs := make([]collection.RouteSpec, len(req.Routes))
	for i, rt := range req.Routes {
		specs[i] = collection.RouteSpec{
			RouteKey:        rt.RouteKey,
			Method:          rt.Method,
			Path:            rt.Path,
			InvokeBase:      req.InvokeBase,
			SeedBody:        rt.SeedBody,
			RequiredHeaders: rt.RequiredHeaders,
			FieldOverrides:  rt.FieldOverrides,
			ProbeBodies:     rt.ProbeBodies,
		}
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch := make(chan collection.ProbeRound, len(specs))
	go func() {
		collection.RunProbes(r.Context(), specs, ch)
		close(ch)
	}()

	enc := json.NewEncoder(w)
	for round := range ch {
		if err := enc.Encode(round); err != nil {
			return
		}
		flusher.Flush()
	}
}

// FireEventBridgeRule manually triggers one EventBridge rule now.
func (h *Handler) FireEventBridgeRule(w http.ResponseWriter, r *http.Request) {
	if h.eventbridge == nil {
		writeError(w, http.StatusServiceUnavailable, "eventbridge service unavailable")
		return
	}
	var req struct {
		RuleName string `json:"ruleName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	req.RuleName = strings.TrimSpace(req.RuleName)
	if req.RuleName == "" {
		writeError(w, http.StatusBadRequest, "ruleName is required")
		return
	}

	result, err := h.eventbridge.FireRuleNow(req.RuleName, map[string]string{"trigger": "manual"})
	if err != nil {
		if se, ok := err.(*eventbridgesvc.ServiceError); ok {
			writeError(w, se.StatusCode(), se.Message)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}

// RunEventBridgeRace triggers one rule repeatedly with bounded concurrency.
func (h *Handler) RunEventBridgeRace(w http.ResponseWriter, r *http.Request) {
	if h.eventbridge == nil {
		writeError(w, http.StatusServiceUnavailable, "eventbridge service unavailable")
		return
	}
	var req struct {
		RuleName    string `json:"ruleName"`
		Runs        int    `json:"runs"`
		Concurrency int    `json:"concurrency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	req.RuleName = strings.TrimSpace(req.RuleName)
	if req.RuleName == "" {
		writeError(w, http.StatusBadRequest, "ruleName is required")
		return
	}
	if req.Runs == 0 {
		req.Runs = 10
	}
	if req.Concurrency == 0 {
		req.Concurrency = 2
	}

	result, err := h.eventbridge.RunRuleRace(req.RuleName, req.Runs, req.Concurrency)
	if err != nil {
		if se, ok := err.(*eventbridgesvc.ServiceError); ok {
			writeError(w, se.StatusCode(), se.Message)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}

// ScanChaosSource scans a local directory for Lambda project directories,
// parses any schemas.ts files found, and generates probe bodies for each
// function matched to the provided function names.
func (h *Handler) ScanChaosSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseDir       string   `json:"baseDir"`
		FunctionNames []string `json:"functionNames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	baseDir, err := sanitizeLocalSourceDir(req.BaseDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := collection.ScanLocalSource(baseDir, req.FunctionNames)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}

	type matchResult struct {
		collection.ScanMatch
		Schemas        []collection.SchemaExport         `json:"schemas,omitempty"`
		ProbeBodies    []collection.ProbeBody            `json:"probeBodies,omitempty"`
		ProbesByMethod map[string][]collection.ProbeBody `json:"probesByMethod,omitempty"`
	}

	matches := make([]matchResult, 0, len(result.Matches))
	for _, m := range result.Matches {
		mr := matchResult{ScanMatch: m}

		if m.SchemasTs != "" {
			exports, err := collection.ParseSchemasFile(m.SchemasTs)
			if err == nil {
				mr.Schemas = exports
				// Try method-specific probe generation first (eliminates duplicates).
				byMethod := collection.GenerateProbesByMethod(exports, m.EventFiles)
				if len(byMethod) > 0 {
					mr.ProbesByMethod = byMethod
				}
				// Always generate the combined fallback for routes whose method
				// doesn't match any schema naming convention.
				mr.ProbeBodies = collection.GenerateProbesFromExports(exports, m.EventFiles)
			}
		}
		// Always include event-file probes and structural probes (malformed/empty),
		// even when no schema was found.
		if len(mr.ProbeBodies) == 0 {
			mr.ProbeBodies = collection.GenerateProbes(nil, m.EventFiles)
		}

		matches = append(matches, mr)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"matches":    matches,
		"unmatched":  result.Unmatched,
		"discovered": result.Discovered,
	})
}

func sanitizeLocalSourceDir(baseDir string) (string, error) {
	trimmed := strings.TrimSpace(baseDir)
	if trimmed == "" {
		return "", fmt.Errorf("baseDir is required")
	}
	if len(trimmed) > 1024 {
		return "", fmt.Errorf("baseDir is too long")
	}
	if strings.IndexFunc(trimmed, func(r rune) bool {
		return r == 0 || r < 32 || r == 127
	}) != -1 {
		return "", fmt.Errorf("baseDir contains invalid control characters")
	}
	if len(trimmed) >= 2 {
		if (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') || (trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') {
			trimmed = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		}
	}

	cleaned := filepath.Clean(trimmed)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("baseDir is invalid")
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("baseDir does not exist")
		}
		return "", fmt.Errorf("baseDir is unavailable")
	}
	if !info.IsDir() {
		return "", fmt.Errorf("baseDir must be a directory")
	}

	return abs, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

func topicNameFromARN(topicArn string) string {
	parts := strings.Split(topicArn, ":")
	if len(parts) == 0 {
		return topicArn
	}
	return parts[len(parts)-1]
}

func queueNameFromEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return endpoint
	}
	if strings.HasPrefix(endpoint, "arn:aws:sqs:") {
		parts := strings.Split(endpoint, ":")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	if idx := strings.LastIndex(endpoint, "/"); idx >= 0 && idx+1 < len(endpoint) {
		return endpoint[idx+1:]
	}
	return endpoint
}

func lambdaNameFromARNOrName(value string) string {
	value = strings.TrimSpace(value)
	const marker = ":function:"
	if !strings.Contains(value, marker) {
		return value
	}
	i := strings.Index(value, marker)
	name := value[i+len(marker):]
	if j := strings.IndexByte(name, ':'); j != -1 {
		name = name[:j]
	}
	if name == "" {
		return value
	}
	return name
}

func parseInt(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

func truncateText(s string, max int) string {
	if max <= 3 || len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func secretTagsToMap(tags []types.SecretTag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	result := make(map[string]string, len(tags))
	for _, tag := range tags {
		result[tag.Key] = tag.Value
	}
	return result
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}

type connectionHint struct {
	kind   string
	host   string
	port   int
	source string
}

func inferInfraConnections(functions []*types.FunctionConfig, probes []infrasvc.ProbeResult) []infraConnection {
	if len(functions) == 0 || len(probes) == 0 {
		return nil
	}

	connections := make([]infraConnection, 0)
	seen := make(map[string]struct{})

	for _, fn := range functions {
		hints := inferEnvConnectionHints(fn.Environment)
		for _, hint := range hints {
			for _, probe := range probes {
				if !isDatabaseKind(probe.Kind) || !probeMatchesHint(probe, hint) {
					continue
				}
				key := fn.FunctionName + "|" + probe.Kind + "|" + normalizeConnectionHost(probe.Host) + "|" + strconv.Itoa(probe.Port)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				connections = append(connections, infraConnection{
					SourceFunction: fn.FunctionName,
					TargetID:       infraProbeID(probe.Kind, probe.Host, probe.Port),
					TargetName:     probe.Name,
					TargetKind:     probe.Kind,
					TargetHost:     probe.Host,
					TargetPort:     probe.Port,
					Evidence:       "env",
					Source:         hint.source,
				})
			}
		}
	}

	sort.Slice(connections, func(i, j int) bool {
		if connections[i].SourceFunction == connections[j].SourceFunction {
			if connections[i].TargetName == connections[j].TargetName {
				return connections[i].TargetPort < connections[j].TargetPort
			}
			return connections[i].TargetName < connections[j].TargetName
		}
		return connections[i].SourceFunction < connections[j].SourceFunction
	})

	return connections
}

func inferEnvConnectionHints(env map[string]string) []connectionHint {
	if len(env) == 0 {
		return nil
	}

	hints := make([]connectionHint, 0, 8)
	seen := make(map[string]struct{})
	genericDBKind := parseDatabaseKind(env["DATABASE_KIND"])
	if genericDBKind == "" {
		genericDBKind = parseDatabaseKind(env["DATABASE_TYPE"])
	}
	if genericDBKind == "" {
		genericDBKind = parseDatabaseKind(env["DB_KIND"])
	}
	if genericDBKind == "" {
		genericDBKind = parseDatabaseKind(env["DB_TYPE"])
	}

	addHint := func(kind, host string, port int, source string) {
		host = strings.TrimSpace(host)
		if host == "" {
			return
		}
		kind = normalizeDatabaseKind(kind)
		if port == 0 {
			port = defaultPortForKind(kind)
		}
		if port == 0 {
			return
		}
		key := kind + "|" + normalizeConnectionHost(host) + "|" + strconv.Itoa(port) + "|" + source
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		hints = append(hints, connectionHint{
			kind:   kind,
			host:   host,
			port:   port,
			source: source,
		})
	}

	for key, value := range env {
		upperKey := strings.ToUpper(key)
		value = strings.TrimSpace(value)
		if value == "" || !looksLikeDatabaseEnvKey(upperKey) {
			continue
		}
		kindHint := kindHintFromEnvKey(upperKey)
		if kindHint == "" {
			kindHint = genericDBKind
		}
		if kind, host, port, ok := parseDBConnectionValue(value, kindHint); ok {
			addHint(kind, host, port, key)
		}
	}

	addHint("postgresql", env["PGHOST"], parseConnectionPort(env["PGPORT"]), "PGHOST")
	addHint("postgresql", env["POSTGRES_HOST"], parseConnectionPort(env["POSTGRES_PORT"]), "POSTGRES_HOST")
	addHint("mysql", env["MYSQL_HOST"], parseConnectionPort(env["MYSQL_PORT"]), "MYSQL_HOST")
	addHint("redis", env["REDIS_HOST"], parseConnectionPort(env["REDIS_PORT"]), "REDIS_HOST")
	addHint(genericDBKind, env["DATABASE_HOST"], parseConnectionPort(env["DATABASE_PORT"]), "DATABASE_HOST")
	addHint(genericDBKind, env["DB_HOST"], parseConnectionPort(env["DB_PORT"]), "DB_HOST")

	return hints
}

func looksLikeDatabaseEnvKey(key string) bool {
	return strings.Contains(key, "DATABASE") ||
		strings.Contains(key, "DB_") ||
		strings.HasPrefix(key, "DB") ||
		strings.Contains(key, "POSTGRES") ||
		strings.HasPrefix(key, "PG") ||
		strings.Contains(key, "MYSQL") ||
		strings.Contains(key, "REDIS") ||
		strings.Contains(key, "MONGO")
}

func kindHintFromEnvKey(key string) string {
	switch {
	case strings.Contains(key, "POSTGRES"), strings.HasPrefix(key, "PG"):
		return "postgresql"
	case strings.Contains(key, "MYSQL"):
		return "mysql"
	case strings.Contains(key, "REDIS"):
		return "redis"
	case strings.Contains(key, "MONGO"):
		return "mongodb"
	default:
		return ""
	}
}

func parseDBConnectionValue(value, kindHint string) (kind string, host string, port int, ok bool) {
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return "", "", 0, false
		}
		host = u.Hostname()
		if host == "" {
			return "", "", 0, false
		}
		kind = normalizeDatabaseKind(u.Scheme)
		if kind == "" {
			kind = normalizeDatabaseKind(kindHint)
		}
		port = parseConnectionPort(u.Port())
		if port == 0 {
			port = defaultPortForKind(kind)
		}
		return kind, host, port, true
	}

	if idx := strings.Index(value, "@tcp("); idx >= 0 {
		start := idx + len("@tcp(")
		end := strings.Index(value[start:], ")")
		if end > 0 {
			hostPort := value[start : start+end]
			host, port = splitHostPort(hostPort)
			if host != "" {
				if port == 0 {
					port = defaultPortForKind(kindHint)
				}
				return normalizeDatabaseKind(kindHint), host, port, true
			}
		}
	}

	if strings.Count(value, ":") == 1 && !strings.Contains(value, "/") && !strings.Contains(value, "@") {
		host, port = splitHostPort(value)
		if host != "" && port > 0 {
			return normalizeDatabaseKind(kindHint), host, port, true
		}
	}

	return "", "", 0, false
}

func splitHostPort(value string) (string, int) {
	host, rawPort, found := strings.Cut(strings.TrimSpace(value), ":")
	if !found {
		return strings.TrimSpace(value), 0
	}
	return strings.TrimSpace(host), parseConnectionPort(rawPort)
}

func parseConnectionPort(raw string) int {
	port, _ := strconv.Atoi(strings.TrimSpace(raw))
	return port
}

func parseDatabaseKind(raw string) string {
	return normalizeDatabaseKind(strings.TrimSpace(raw))
}

func normalizeDatabaseKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "postgres", "postgresql", "pg":
		return "postgresql"
	case "mysql", "mysql2":
		return "mysql"
	case "redis", "rediss":
		return "redis"
	case "mongodb", "mongo":
		return "mongodb"
	default:
		return ""
	}
}

func defaultPortForKind(kind string) int {
	switch normalizeDatabaseKind(kind) {
	case "postgresql":
		return 5432
	case "mysql":
		return 3306
	case "redis":
		return 6379
	case "mongodb":
		return 27017
	default:
		return 0
	}
}

func isDatabaseKind(kind string) bool {
	switch normalizeDatabaseKind(kind) {
	case "postgresql", "mysql", "redis", "mongodb":
		return true
	default:
		return false
	}
}

func infraProbeID(kind, host string, port int) string {
	return kind + "-" + host + "-" + strconv.Itoa(port)
}

func probeMatchesHint(probe infrasvc.ProbeResult, hint connectionHint) bool {
	if hint.kind != "" && normalizeDatabaseKind(probe.Kind) != hint.kind {
		return false
	}
	if hint.port != 0 && probe.Port != hint.port {
		return false
	}
	return connectionHostsMatch(probe.Host, hint.host)
}

func connectionHostsMatch(a, b string) bool {
	aNorm := normalizeConnectionHost(a)
	bNorm := normalizeConnectionHost(b)
	if aNorm == bNorm {
		return true
	}
	return isLocalAlias(aNorm) && isLocalAlias(bNorm)
}

func normalizeConnectionHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

func isLocalAlias(host string) bool {
	switch normalizeConnectionHost(host) {
	case "localhost", "127.0.0.1", "0.0.0.0", "host.docker.internal":
		return true
	default:
		return false
	}
}
