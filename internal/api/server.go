package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	adminhandler "github.com/openstack-project/openstack/internal/api/admin"
	apigatewayhandler "github.com/openstack-project/openstack/internal/api/apigateway"
	eventsourcehandler "github.com/openstack-project/openstack/internal/api/eventsource"
	lambdahandler "github.com/openstack-project/openstack/internal/api/lambda"
	s3handler "github.com/openstack-project/openstack/internal/api/s3"
	secretshandler "github.com/openstack-project/openstack/internal/api/secrets"
	sqshandler "github.com/openstack-project/openstack/internal/api/sqs"
	apigatewaysvc "github.com/openstack-project/openstack/internal/apigateway"
	"github.com/openstack-project/openstack/internal/config"
	eventsourcesvc "github.com/openstack-project/openstack/internal/eventsource"
	infrasvc "github.com/openstack-project/openstack/internal/infrastructure"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	logssvc "github.com/openstack-project/openstack/internal/logs"
	s3svc "github.com/openstack-project/openstack/internal/s3"
	secretssvc "github.com/openstack-project/openstack/internal/secrets"
	sqssvc "github.com/openstack-project/openstack/internal/sqs"
)

// Server is the main OpenStack API server.
type Server struct {
	cfg         *config.Config
	httpServer  *http.Server
	apigw       *apigatewayhandler.Handler
	lambda      *lambdahandler.Handler
	s3          *s3handler.Handler
	sqs         *sqshandler.Handler
	secrets     *secretshandler.Handler
	eventsource *eventsourcehandler.Handler
	admin       *adminhandler.Handler
	logsSvc     *logssvc.Service
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, gatewaySvc *apigatewaysvc.Service, lambdaSvc *lambdasvc.Service, logsSvc *logssvc.Service, sqsSvc *sqssvc.Service, secretsSvc *secretssvc.Service, infraSvc *infrasvc.Service, s3Svc *s3svc.Service, esmSvc *eventsourcesvc.Service) *Server {
	s := &Server{
		cfg:         cfg,
		apigw:       apigatewayhandler.NewHandler(gatewaySvc),
		lambda:      lambdahandler.NewHandler(lambdaSvc),
		s3:          s3handler.NewHandler(s3Svc),
		sqs:         sqshandler.NewHandler(sqsSvc),
		secrets:     secretshandler.NewHandler(cfg, secretsSvc),
		eventsource: eventsourcehandler.NewHandler(esmSvc),
		admin:       adminhandler.NewHandler(cfg, gatewaySvc, lambdaSvc, logsSvc, sqsSvc, secretsSvc, infraSvc, s3Svc, esmSvc),
		logsSvc:     logsSvc,
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
	mux.HandleFunc("GET /_openstack/health", s.healthHandler)
	mux.HandleFunc("GET /_openstack/admin/overview", s.admin.Overview)
	mux.HandleFunc("GET /_openstack/admin/secrets/{name}/value", s.admin.SecretValue)
	mux.HandleFunc("GET /_openstack/admin/queues/{name}/messages", s.admin.QueueMessages)
	mux.HandleFunc("GET /_openstack/admin/logs/groups", s.admin.LogGroups)
	mux.HandleFunc("GET /_openstack/admin/logs/groups/{name...}", s.admin.LogGroupDetail)
	mux.HandleFunc("GET /_openstack/admin/logs/events/{name...}", s.admin.LogEvents)
	mux.HandleFunc("GET /_openstack/admin/infrastructure", s.admin.Infrastructure)

	// S3 API — path-style REST XML protocol
	// POST is handled via s3PostDispatch to avoid conflict with SQS POST /{account}/{queue}
	mux.HandleFunc("GET /_s3/{rest...}", s.s3.Dispatch)
	mux.HandleFunc("PUT /_s3/{rest...}", s.s3.Dispatch)
	mux.HandleFunc("HEAD /_s3/{rest...}", s.s3.Dispatch)
	mux.HandleFunc("DELETE /_s3/{rest...}", s.s3.Dispatch)
	// Compatibility surface for AWS SDK clients that normalize away endpoint paths
	// and issue bucket-level requests as /{bucket}.
	mux.HandleFunc("GET /{bucket}", s.s3.Dispatch)
	mux.HandleFunc("PUT /{bucket}", s.s3.Dispatch)
	mux.HandleFunc("HEAD /{bucket}", s.s3.Dispatch)
	mux.HandleFunc("DELETE /{bucket}", s.s3.Dispatch)

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
	mux.HandleFunc("GET /2015-03-31/functions/{name}", s.lambda.GetFunction)
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}", s.lambda.DeleteFunction)
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/code", s.lambda.UpdateFunctionCode)
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/configuration", s.lambda.UpdateFunctionConfiguration)
	mux.HandleFunc("POST /2015-03-31/functions/{name}/invocations", s.lambda.Invoke)

	// Tags
	mux.HandleFunc("GET /2015-03-31/functions/{name}/tags", s.lambda.ListTags)
	mux.HandleFunc("POST /2015-03-31/functions/{name}/tags", s.lambda.TagResource)
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}/tags", s.lambda.UntagResource)

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
	// Catch-all for POST / — dispatches between Secrets Manager (X-Amz-Target) and SQS (Action)
	mux.HandleFunc("POST /", s.postRootDispatch)

	// Optional dashboard UI (SPA fallback for GET requests only)
	s.registerUIRoutes(mux)
}

// Start begins listening for requests.
func (s *Server) Start() error {
	log.Printf("OpenStack API server listening on %s", s.httpServer.Addr)
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
	s.sqs.Dispatch(w, r)
}

// postRootDispatch routes POST / between Secrets Manager and SQS.
// Secrets Manager uses X-Amz-Target header; SQS uses Action form parameter.
func (s *Server) postRootDispatch(w http.ResponseWriter, r *http.Request) {
	if secretshandler.IsSecretsManagerRequest(r) {
		s.secrets.Dispatch(w, r)
		return
	}
	s.sqs.Dispatch(w, r)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"running","services":["apigatewayv2","lambda","s3","sqs","secretsmanager","eventsource"]}`)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, wrapped.status, duration)
		if s.logsSvc != nil {
			s.logsSvc.LogAPIRequest(r.Method, r.URL.Path, wrapped.status, duration)
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
