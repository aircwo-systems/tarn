package admin

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	apigatewaysvc "github.com/openstack-project/openstack/internal/apigateway"
	"github.com/openstack-project/openstack/internal/config"
	eventsourcesvc "github.com/openstack-project/openstack/internal/eventsource"
	infrasvc "github.com/openstack-project/openstack/internal/infrastructure"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	logssvc "github.com/openstack-project/openstack/internal/logs"
	s3svc "github.com/openstack-project/openstack/internal/s3"
	secretssvc "github.com/openstack-project/openstack/internal/secrets"
	sqssvc "github.com/openstack-project/openstack/internal/sqs"
	tracesvc "github.com/openstack-project/openstack/internal/trace"
	"github.com/openstack-project/openstack/pkg/types"
)

// Handler serves JSON endpoints used by the dashboard UI.
type Handler struct {
	cfg        *config.Config
	apigw      *apigatewaysvc.Service
	lambda     *lambdasvc.Service
	logs       *logssvc.Service
	s3         *s3svc.Service
	sqs        *sqssvc.Service
	secrets    *secretssvc.Service
	infra      *infrasvc.Service
	esm        *eventsourcesvc.Service
	traceStore *tracesvc.Store
}

func NewHandler(cfg *config.Config, apigw *apigatewaysvc.Service, lambda *lambdasvc.Service, logs *logssvc.Service, sqs *sqssvc.Service, secrets *secretssvc.Service, infra *infrasvc.Service, s3 *s3svc.Service, esm *eventsourcesvc.Service, traceStore *tracesvc.Store) *Handler {
	return &Handler{
		cfg:        cfg,
		apigw:      apigw,
		lambda:     lambda,
		logs:       logs,
		s3:         s3,
		sqs:        sqs,
		secrets:    secrets,
		infra:      infra,
		esm:        esm,
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
	Secrets             []secretSummary        `json:"secrets"`
	Buckets             []s3BucketSummary      `json:"buckets"`
	EventSourceMappings []esmSummary           `json:"eventSourceMappings"`
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
	Secrets             int `json:"secrets"`
	Buckets             int `json:"buckets"`
	LogGroups           int `json:"logGroups"`
	EventSourceMappings int `json:"eventSourceMappings"`
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

type s3BucketSummary struct {
	Name        string    `json:"name"`
	Objects     int       `json:"objects"`
	TotalSize   int64     `json:"totalSize"`
	CreatedDate time.Time `json:"createdDate"`
}

type gatewaySummary struct {
	APIID        string            `json:"apiId"`
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	ProtocolType string            `json:"protocolType"`
	Arn          string            `json:"arn"`
	ApiEndpoint  string            `json:"apiEndpoint"`
	DefaultStage string            `json:"defaultStage"`
	InvokeURL    string            `json:"invokeUrl"`
	Routes       int               `json:"routes"`
	Integrations int               `json:"integrations"`
	Stages       int               `json:"stages"`
	Tags         map[string]string `json:"tags,omitempty"`
	TagCount     int               `json:"tagCount"`
	RouteKeys    []string          `json:"routeKeys,omitempty"`
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
	CreatedTimestamp int64             `json:"createdTimestamp"`
	DLQName          string            `json:"dlqName,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
	TagCount         int               `json:"tagCount"`
	RecentMessages   []queueMessage    `json:"recentMessages,omitempty"`
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

// Overview returns a dashboard-friendly snapshot of current resources.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	gateways := h.apigw.ListAPIs()

	functions, err := h.lambda.ListFunctions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list functions")
		return
	}

	queues := h.sqs.ListQueues("")
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

	resp := overviewResponse{
		Status:    "running",
		Timestamp: time.Now().UTC(),
		Services:  []string{"apigatewayv2", "lambda", "s3", "sqs", "secretsmanager", "eventsource"},
		Config: overviewConfig{
			Region:    h.cfg.Region,
			AccountID: h.cfg.AccountID,
			Endpoint:  h.cfg.Endpoint(),
			DataDir:   h.cfg.DataDir,
			UIEnabled: h.cfg.UIEnabled,
		},
		Counts: overviewCounts{
			Gateways:            len(gateways),
			Functions:           len(functions),
			Queues:              len(queues),
			Secrets:             len(secrets),
			Buckets:             len(buckets),
			LogGroups:           len(logGroups),
			EventSourceMappings: len(esmMappings),
		},
		Gateways:            make([]gatewaySummary, 0, len(gateways)),
		Functions:           make([]functionSummary, 0, len(functions)),
		Queues:              make([]queueSummary, 0, len(queues)),
		Secrets:             make([]secretSummary, 0, len(secrets)),
		Buckets:             make([]s3BucketSummary, 0, len(buckets)),
		EventSourceMappings: make([]esmSummary, 0, len(esmMappings)),
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

		resp.Gateways = append(resp.Gateways, gatewaySummary{
			APIID:        api.APIID,
			Name:         api.Name,
			Description:  api.Description,
			ProtocolType: api.ProtocolType,
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
		})
		if err != nil {
			resp.Warnings = append(resp.Warnings, "queue metrics unavailable for "+q.QueueName)
		} else {
			summary.ApproxVisible = parseInt(attrs["ApproximateNumberOfMessages"])
			summary.ApproxInFlight = parseInt(attrs["ApproximateNumberOfMessagesNotVisible"])
			summary.ApproxDelayed = parseInt(attrs["ApproximateNumberOfMessagesDelayed"])
		}

		msgs, err := h.sqs.PeekMessages(q.QueueName, 8)
		if err != nil {
			resp.Warnings = append(resp.Warnings, "queue messages unavailable for "+q.QueueName)
		} else {
			now := time.Now().UnixMilli()
			summary.RecentMessages = make([]queueMessage, 0, len(msgs))
			for _, msg := range msgs {
				state := "visible"
				if msg.DelayUntil > now {
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
		if msg.DelayUntil > now {
			state = "delayed"
		} else if msg.VisibleAt > now {
			state = "inflight"
		}
		result = append(result, queueMessage{
			ID:           msg.MessageId,
			Body:         truncateText(msg.Body, 160),
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

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
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
