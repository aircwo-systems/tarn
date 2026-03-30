package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	adminhandler "github.com/aircwo-systems/tarn/internal/api/admin"
	apigatewayhandler "github.com/aircwo-systems/tarn/internal/api/apigateway"
	apigatewayv1handler "github.com/aircwo-systems/tarn/internal/api/apigatewayv1"
	eventbridgehandler "github.com/aircwo-systems/tarn/internal/api/eventbridge"
	eventsourcehandler "github.com/aircwo-systems/tarn/internal/api/eventsource"
	iamhandler "github.com/aircwo-systems/tarn/internal/api/iam"
	lambdahandler "github.com/aircwo-systems/tarn/internal/api/lambda"
	s3handler "github.com/aircwo-systems/tarn/internal/api/s3"
	secretshandler "github.com/aircwo-systems/tarn/internal/api/secrets"
	snshandler "github.com/aircwo-systems/tarn/internal/api/sns"
	sqshandler "github.com/aircwo-systems/tarn/internal/api/sqs"
	apigatewaysvc "github.com/aircwo-systems/tarn/internal/apigateway"
	apigatewayv1svc "github.com/aircwo-systems/tarn/internal/apigatewayv1"
	"github.com/aircwo-systems/tarn/internal/config"
	eventbridgesvc "github.com/aircwo-systems/tarn/internal/eventbridge"
	eventsourcesvc "github.com/aircwo-systems/tarn/internal/eventsource"
	infrasvc "github.com/aircwo-systems/tarn/internal/infrastructure"
	lambdasvc "github.com/aircwo-systems/tarn/internal/lambda"
	logssvc "github.com/aircwo-systems/tarn/internal/logs"
	s3svc "github.com/aircwo-systems/tarn/internal/s3"
	secretssvc "github.com/aircwo-systems/tarn/internal/secrets"
	snssvc "github.com/aircwo-systems/tarn/internal/sns"
	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
)

// Server is the main Tarn API server.
type Server struct {
	cfg         *config.Config
	httpServer  *http.Server
	apigw       *apigatewayhandler.Handler
	apigwv1     *apigatewayv1handler.Handler
	lambda      *lambdahandler.Handler
	s3          *s3handler.Handler
	sqs         *sqshandler.Handler
	sns         *snshandler.Handler
	secrets     *secretshandler.Handler
	eventsource *eventsourcehandler.Handler
	eventbridge *eventbridgehandler.Handler
	admin       *adminhandler.Handler
	iam         *iamhandler.Handler
	logsSvc     *logssvc.Service
	collector   *tracesvc.Collector
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, gatewaySvc *apigatewaysvc.Service, gatewayV1Svc *apigatewayv1svc.Service, lambdaSvc *lambdasvc.Service, logsSvc *logssvc.Service, sqsSvc *sqssvc.Service, snsSvc *snssvc.Service, secretsSvc *secretssvc.Service, infraSvc *infrasvc.Service, s3Svc *s3svc.Service, esmSvc *eventsourcesvc.Service, eventbridgeSvc *eventbridgesvc.Service, traceStore *tracesvc.Store, collector *tracesvc.Collector) *Server {
	lambdaHandler := lambdahandler.NewHandler(lambdaSvc, s3Svc)
	lambdaHandler.SetTraceStore(traceStore)
	lambdaHandler.SetCollector(collector)

	secretsHandler := secretshandler.NewHandler(cfg, secretsSvc)
	secretsHandler.SetCollector(collector)

	s := &Server{
		cfg:         cfg,
		apigw:       apigatewayhandler.NewHandler(gatewaySvc),
		apigwv1:     apigatewayv1handler.NewHandler(gatewayV1Svc),
		lambda:      lambdaHandler,
		s3:          s3handler.NewHandler(s3Svc),
		sqs:         sqshandler.NewHandler(sqsSvc),
		sns:         snshandler.NewHandler(snsSvc),
		secrets:     secretsHandler,
		eventsource: eventsourcehandler.NewHandler(esmSvc),
		eventbridge: eventbridgehandler.NewHandler(eventbridgeSvc),
		admin:       adminhandler.NewHandler(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, snsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, eventbridgeSvc, traceStore),
		iam:         iamhandler.NewHandler(cfg.AccountID),
		logsSvc:     logsSvc,
		collector:   collector,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      s.withLogging(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second, // Lambda can run up to 15 min
		IdleTimeout:  120 * time.Second,
	}

	return s
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health check
	mux.HandleFunc("GET /_tarn/health", s.healthHandler)
	// Telemetry — called by in-container proxies to report observability spans
	mux.HandleFunc("POST /_tarn/telemetry/db", s.telemetryDBHandler)
	mux.HandleFunc("GET /_tarn/admin/overview", s.admin.Overview)
	mux.HandleFunc("GET /_tarn/admin/secrets/{name}/value", s.admin.SecretValue)
	mux.HandleFunc("GET /_tarn/admin/queues/{name}/messages", s.admin.QueueMessages)
	mux.HandleFunc("GET /_tarn/admin/logs/groups", s.admin.LogGroups)
	mux.HandleFunc("GET /_tarn/admin/logs/groups/{name...}", s.admin.LogGroupDetail)
	mux.HandleFunc("GET /_tarn/admin/logs/events-all", s.admin.AllLogEvents)
	mux.HandleFunc("GET /_tarn/admin/logs/events/{name...}", s.admin.LogEvents)
	mux.HandleFunc("DELETE /_tarn/admin/logs/events/{name...}", s.admin.ClearLogGroup)
	mux.HandleFunc("POST /_tarn/admin/logs/prune", s.admin.PruneLogs)
	mux.HandleFunc("GET /_tarn/admin/infrastructure", s.admin.Infrastructure)
	mux.HandleFunc("POST /_tarn/admin/chaos", s.admin.RunChaos)
	mux.HandleFunc("POST /_tarn/admin/chaos/source", s.admin.ScanChaosSource)
	mux.HandleFunc("POST /_tarn/admin/eventbridge/fire", s.admin.FireEventBridgeRule)
	mux.HandleFunc("POST /_tarn/admin/eventbridge/race", s.admin.RunEventBridgeRace)
	// EventBridge JSON protocol endpoint used by dashboard and tooling to avoid
	// colliding with UI app-server root routes.
	mux.HandleFunc("POST /_tarn/events", s.eventbridge.Dispatch)

	// S3 API — path-style REST XML protocol
	// POST is handled via postAccountDispatch to avoid conflict with SQS POST /{account}/{queue...}
	mux.HandleFunc("GET /_s3/{rest...}", s.s3.Dispatch)
	mux.HandleFunc("PUT /_s3/{rest...}", s.s3.Dispatch)
	mux.HandleFunc("HEAD /_s3/{rest...}", s.s3.Dispatch)
	mux.HandleFunc("DELETE /_s3/{rest...}", s.s3.Dispatch)
	// S3 ListBuckets: GET / (exact root path)
	mux.HandleFunc("GET /{$}", s.s3.Dispatch)
	// Compatibility surface for AWS SDK clients using path-style S3 URLs.
	// Bucket-level operations: /{bucket}
	mux.HandleFunc("GET /{bucket}", s.s3.Dispatch)
	mux.HandleFunc("PUT /{bucket}", s.s3.Dispatch)
	// HEAD is omitted — Go automatically routes HEAD to the GET handler,
	// keeping r.Method="HEAD" so Dispatch still calls headBucket.
	mux.HandleFunc("DELETE /{bucket}", s.s3.Dispatch)
	// Lambda 2017-10-31 concurrency endpoints — must be registered before the
	// generic S3 /{bucket}/{key...} patterns, which would otherwise capture
	// paths like PUT /2017-10-31/functions/{name}/concurrency and return XML.
	mux.HandleFunc("PUT /2017-10-31/functions/{name}/concurrency", s.lambda.PutFunctionConcurrency)
	mux.HandleFunc("DELETE /2017-10-31/functions/{name}/concurrency", s.lambda.DeleteFunctionConcurrency)
	for _, method := range [...]string{
		http.MethodGet, http.MethodPut, http.MethodPost,
		http.MethodDelete, http.MethodPatch,
	} {
		mux.HandleFunc(method+" /2017-10-31/functions/{name}/{subpath...}", s.lambda.NotFound)
	}

	// Object-level operations: /{bucket}/{key...}
	// HEAD is omitted — Go automatically routes HEAD to the GET handler,
	// keeping r.Method="HEAD" so Dispatch still calls headObject.
	mux.HandleFunc("GET /{bucket}/{key...}", s.s3.Dispatch)
	mux.HandleFunc("PUT /{bucket}/{key...}", s.s3.Dispatch)
	mux.HandleFunc("DELETE /{bucket}/{key...}", s.s3.Dispatch)
	mux.HandleFunc("POST /{bucket}/{key...}", s.s3.Dispatch)

	// API Gateway v1 (REST API) — AWS-compatible management endpoints
	mux.HandleFunc("POST /restapis", s.apigwv1.CreateRestAPI)
	mux.HandleFunc("GET /restapis", s.apigwv1.ListRestAPIs)
	mux.HandleFunc("GET /restapis/{restApiId}", s.apigwv1.GetRestAPI)
	mux.HandleFunc("DELETE /restapis/{restApiId}", s.apigwv1.DeleteRestAPI)

	mux.HandleFunc("GET /restapis/{restApiId}/resources", s.apigwv1.ListResources)
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}", s.apigwv1.GetResource)
	mux.HandleFunc("POST /restapis/{restApiId}/resources/{parentId}", s.apigwv1.CreateResource)
	mux.HandleFunc("DELETE /restapis/{restApiId}/resources/{resourceId}", s.apigwv1.DeleteResource)

	mux.HandleFunc("PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}", s.apigwv1.PutMethod)
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}", s.apigwv1.GetMethod)
	mux.HandleFunc("PATCH /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}", s.apigwv1.PatchMethod)
	mux.HandleFunc("DELETE /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}", s.apigwv1.DeleteMethod)

	mux.HandleFunc("PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration", s.apigwv1.PutIntegration)
	mux.HandleFunc("PATCH /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration", s.apigwv1.PatchIntegration)
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration", s.apigwv1.GetIntegration)
	mux.HandleFunc("DELETE /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration", s.apigwv1.DeleteIntegration)

	mux.HandleFunc("PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/responses/{statusCode}", s.apigwv1.PutMethodResponse)
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/responses/{statusCode}", s.apigwv1.GetMethodResponse)

	mux.HandleFunc("PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration/responses/{statusCode}", s.apigwv1.PutIntegrationResponse)
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration/responses/{statusCode}", s.apigwv1.GetIntegrationResponse)

	mux.HandleFunc("POST /restapis/{restApiId}/deployments", s.apigwv1.CreateDeployment)
	mux.HandleFunc("GET /restapis/{restApiId}/deployments", s.apigwv1.ListDeployments)
	mux.HandleFunc("GET /restapis/{restApiId}/deployments/{deploymentId}", s.apigwv1.GetDeployment)

	mux.HandleFunc("POST /restapis/{restApiId}/stages", s.apigwv1.CreateStage)
	mux.HandleFunc("GET /restapis/{restApiId}/stages", s.apigwv1.ListStages)
	mux.HandleFunc("GET /restapis/{restApiId}/stages/{stageName}", s.apigwv1.GetStage)
	mux.HandleFunc("DELETE /restapis/{restApiId}/stages/{stageName}", s.apigwv1.DeleteStage)

	// API Gateway v1 invoke surface: /_aws/execute-api/{apiId}/{stage}/{proxy...}
	for _, method := range [...]string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodHead,
	} {
		mux.HandleFunc(method+" /_aws/execute-api/{apiId}/{stage}/{proxy...}", s.apigwv1.Invoke)
		mux.HandleFunc(method+" /_aws/execute-api/{apiId}/{stage}", s.apigwv1.Invoke)
	}

	// API Gateway v2 API — AWS-compatible endpoints
	mux.HandleFunc("POST /v2/apis", s.apigw.CreateAPI)
	mux.HandleFunc("GET /v2/apis", s.apigw.ListAPIs)
	mux.HandleFunc("GET /v2/apis/{apiId}", s.apigw.GetAPI)
	mux.HandleFunc("PATCH /v2/apis/{apiId}", s.apigw.UpdateAPI)
	mux.HandleFunc("DELETE /v2/apis/{apiId}", s.apigw.DeleteAPI)

	mux.HandleFunc("POST /v2/apis/{apiId}/integrations", s.apigw.CreateIntegration)
	mux.HandleFunc("GET /v2/apis/{apiId}/integrations", s.apigw.ListIntegrations)
	mux.HandleFunc("GET /v2/apis/{apiId}/integrations/{integrationId}", s.apigw.GetIntegration)
	mux.HandleFunc("PATCH /v2/apis/{apiId}/integrations/{integrationId}", s.apigw.UpdateIntegration)
	mux.HandleFunc("DELETE /v2/apis/{apiId}/integrations/{integrationId}", s.apigw.DeleteIntegration)

	mux.HandleFunc("POST /v2/apis/{apiId}/routes", s.apigw.CreateRoute)
	mux.HandleFunc("GET /v2/apis/{apiId}/routes", s.apigw.ListRoutes)
	mux.HandleFunc("GET /v2/apis/{apiId}/routes/{routeId}", s.apigw.GetRoute)
	mux.HandleFunc("PATCH /v2/apis/{apiId}/routes/{routeId}", s.apigw.UpdateRoute)
	mux.HandleFunc("DELETE /v2/apis/{apiId}/routes/{routeId}", s.apigw.DeleteRoute)

	mux.HandleFunc("GET /v2/apis/{apiId}/stages", s.apigw.ListStages)
	mux.HandleFunc("GET /v2/apis/{apiId}/stages/{stageName}", s.apigw.GetStage)
	mux.HandleFunc("PATCH /v2/apis/{apiId}/stages/{stageName}", s.apigw.UpdateStage)

	// API Gateway invoke surface
	for _, method := range [...]string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodHead,
	} {
		mux.HandleFunc(method+" /_apigateway/{apiId}/{stage}/{proxy...}", s.apigw.Invoke)
		mux.HandleFunc(method+" /_apigateway/{apiId}/{stage}", s.apigw.Invoke)
	}

	// Event Source Mapping API
	mux.HandleFunc("POST /2015-03-31/event-source-mappings", s.eventsource.Create)
	mux.HandleFunc("GET /2015-03-31/event-source-mappings", s.eventsource.List)
	mux.HandleFunc("GET /2015-03-31/event-source-mappings/{uuid}", s.eventsource.Get)
	mux.HandleFunc("PUT /2015-03-31/event-source-mappings/{uuid}", s.eventsource.Update)
	mux.HandleFunc("DELETE /2015-03-31/event-source-mappings/{uuid}", s.eventsource.Delete)

	// Lambda API — AWS-compatible endpoints
	mux.HandleFunc("GET /2015-03-31/account-settings", s.lambda.GetAccountSettings)
	mux.HandleFunc("POST /2015-03-31/functions", s.lambda.CreateFunction)
	mux.HandleFunc("GET /2015-03-31/functions", s.lambda.ListFunctions)
	// configuration must be registered before the generic GET so that
	// /functions/foo/configuration does not match the {name} pattern first.
	mux.HandleFunc("GET /2015-03-31/functions/{name}/configuration", s.lambda.GetFunctionConfiguration)
	mux.HandleFunc("GET /2015-03-31/functions/{name}/versions", s.lambda.ListVersionsByFunction)
	mux.HandleFunc("POST /2015-03-31/functions/{name}/versions", s.lambda.PublishVersion)
	mux.HandleFunc("GET /2015-03-31/functions/{name}", s.lambda.GetFunction)
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}", s.lambda.DeleteFunction)
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/code", s.lambda.UpdateFunctionCode)
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/configuration", s.lambda.UpdateFunctionConfiguration)
	mux.HandleFunc("POST /2015-03-31/functions/{name}/invocations", s.lambda.Invoke)
	// Aliases
	mux.HandleFunc("POST /2015-03-31/functions/{name}/aliases", s.lambda.CreateAlias)
	mux.HandleFunc("GET /2015-03-31/functions/{name}/aliases/{aliasName}", s.lambda.GetAlias)
	mux.HandleFunc("GET /2015-03-31/functions/{name}/aliases", s.lambda.ListAliases)
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/aliases/{aliasName}", s.lambda.UpdateAlias)
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}/aliases/{aliasName}", s.lambda.DeleteAlias)

	// Tags
	mux.HandleFunc("GET /2015-03-31/functions/{name}/tags", s.lambda.ListTags)
	mux.HandleFunc("POST /2015-03-31/functions/{name}/tags", s.lambda.TagResource)
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}/tags", s.lambda.UntagResource)
	// Resource policy compatibility (Terraform aws_lambda_permission)
	mux.HandleFunc("POST /2015-03-31/functions/{name}/policy", s.lambda.AddPermission)
	mux.HandleFunc("GET /2015-03-31/functions/{name}/policy", s.lambda.GetPolicy)
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}/policy/{statementId}", s.lambda.RemovePermission)

	// Layers
	mux.HandleFunc("POST /2015-03-31/layers/{layerName}/versions", s.lambda.PublishLayerVersion)
	mux.HandleFunc("GET /2015-03-31/layers", s.lambda.ListLayers)
	mux.HandleFunc("GET /2015-03-31/layers/{layerName}/versions", s.lambda.ListLayerVersions)
	mux.HandleFunc("GET /2015-03-31/layers/{layerName}/versions/{version}", s.lambda.GetLayerVersion)
	mux.HandleFunc("DELETE /2015-03-31/layers/{layerName}/versions/{version}", s.lambda.DeleteLayerVersion)

	// Catch-all for Lambda function sub-resources we don't emulate
	// (e.g. code-signing-config, concurrency, policy, event-invoke-config).
	// Must be registered after all specific /functions/{name}/... routes so those
	// take precedence.  Returns a proper AWS ResourceNotFoundException so the
	// Terraform provider v5 can distinguish "not configured" from a real error.
	for _, method := range [...]string{
		http.MethodGet, http.MethodPut, http.MethodPost,
		http.MethodDelete, http.MethodPatch,
	} {
		mux.HandleFunc(method+" /2015-03-31/functions/{name}/{subpath...}", s.lambda.NotFound)
	}

	// SQS API — query protocol (all POST with Action parameter)
	// Queue URL paths: /{accountId}/{queueName}
	// Also handles POST /_s3/{bucket}?delete via prefix check
	mux.HandleFunc("POST /{account}/{queue}", s.postAccountDispatch)
	// Catch-all for POST / — dispatches between Secrets Manager, IAM, SNS, and SQS
	mux.HandleFunc("POST /", s.postRootDispatch)

	// Optional dashboard UI (SPA fallback for GET requests only)
	s.registerUIRoutes(mux)
}

// Start begins listening for requests.
func (s *Server) Start() error {
	log.Printf("Tarn API server listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// postAccountDispatch routes POST /{account}/{queue} between S3 and SQS.
// S3 POST requests target /_s3/{bucket}?delete; everything else is SQS.
func (s *Server) postAccountDispatch(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/_s3/") {
		s.s3.Dispatch(w, r)
		return
	}
	q := r.URL.Query()
	if q.Has("uploads") || q.Has("uploadId") || q.Has("delete") {
		s.s3.Dispatch(w, r)
		return
	}
	s.sqs.Dispatch(w, r)
}

// postRootDispatch routes POST / between Secrets Manager, IAM, SNS, and SQS.
// Secrets Manager uses X-Amz-Target; IAM uses Version=2010-05-08; SNS/SQS use Action.
func (s *Server) postRootDispatch(w http.ResponseWriter, r *http.Request) {
	if secretshandler.IsSecretsManagerRequest(r) {
		s.secrets.Dispatch(w, r)
		return
	}
	// EventBridge JSON protocol uses X-Amz-Target: AWSEvents.*
	if strings.HasPrefix(r.Header.Get("X-Amz-Target"), "AWSEvents.") {
		s.eventbridge.Dispatch(w, r)
		return
	}
	// SQS JSON wire protocol uses X-Amz-Target: AmazonSQS.*
	if strings.HasPrefix(r.Header.Get("X-Amz-Target"), "AmazonSQS.") {
		s.sqs.Dispatch(w, r)
		return
	}
	if iamhandler.IsIAMRequest(r) {
		s.iam.Dispatch(w, r)
		return
	}
	if err := r.ParseForm(); err == nil {
		if snshandler.IsSNSRequest(r) || snshandler.IsSNSAction(r.FormValue("Action")) {
			s.sns.Dispatch(w, r)
			return
		}
	}
	s.sqs.Dispatch(w, r)
}

func (s *Server) telemetryDBHandler(w http.ResponseWriter, r *http.Request) {
	if s.collector == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		Name       string `json:"name"`
		DurationMs int64  `json:"durationMs"`
		Status     string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Status == "" {
		req.Status = "ok"
	}
	s.collector.RecordAnon("postgres", req.Name, req.DurationMs, req.Status, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"running","services":["apigateway","apigatewayv2","lambda","s3","sqs","sns","secretsmanager","eventsource","eventbridge"]}`)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: 200}
		// AWS JSON/query protocol clients may send service requests on non-root
		// paths when custom endpoints include path components. Route these early
		// by protocol headers/params so EventBridge (and peers) still work.
		if !s.dispatchProtocolRequest(wrapped, r) {
			next.ServeHTTP(wrapped, r)
		}
		duration := time.Since(start)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, wrapped.status, duration)
		if s.logsSvc != nil && !strings.HasPrefix(r.URL.Path, "/_tarn/") {
			s.logsSvc.LogAPIRequest(r.Method, r.URL.Path, wrapped.status, duration)
		}
	})
}

// dispatchProtocolRequest routes AWS protocol requests based on headers/query
// independent of URL path. Returns true when a request was dispatched.
func (s *Server) dispatchProtocolRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}

	if secretshandler.IsSecretsManagerRequest(r) {
		s.secrets.Dispatch(w, r)
		return true
	}

	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	if strings.HasPrefix(target, "AWSEvents.") {
		s.eventbridge.Dispatch(w, r)
		return true
	}
	if strings.HasPrefix(target, "AmazonSQS.") {
		s.sqs.Dispatch(w, r)
		return true
	}

	if iamhandler.IsIAMRequest(r) {
		s.iam.Dispatch(w, r)
		return true
	}

	// Keep Tarn admin/control POST APIs on normal route matching unless
	// one of the protocol dispatchers above claimed the request.
	if strings.HasPrefix(r.URL.Path, "/_tarn/") || strings.TrimSpace(r.URL.Path) == "/_tarn" {
		return false
	}

	if err := r.ParseForm(); err == nil && (snshandler.IsSNSRequest(r) || snshandler.IsSNSAction(r.FormValue("Action"))) {
		s.sns.Dispatch(w, r)
		return true
	}

	return false
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Flush delegates to the underlying ResponseWriter if it implements http.Flusher,
// so that streaming handlers (e.g. NDJSON chaos endpoint) work through this wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
