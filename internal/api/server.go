package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	adminhandler "github.com/openstack-project/openstack/internal/api/admin"
	apigatewayhandler "github.com/openstack-project/openstack/internal/api/apigateway"
	lambdahandler "github.com/openstack-project/openstack/internal/api/lambda"
	secretshandler "github.com/openstack-project/openstack/internal/api/secrets"
	sqshandler "github.com/openstack-project/openstack/internal/api/sqs"
	apigatewaysvc "github.com/openstack-project/openstack/internal/apigateway"
	"github.com/openstack-project/openstack/internal/config"
	infrasvc "github.com/openstack-project/openstack/internal/infrastructure"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	logssvc "github.com/openstack-project/openstack/internal/logs"
	secretssvc "github.com/openstack-project/openstack/internal/secrets"
	sqssvc "github.com/openstack-project/openstack/internal/sqs"
)

// Server is the main OpenStack API server.
type Server struct {
	cfg        *config.Config
	httpServer *http.Server
	apigw      *apigatewayhandler.Handler
	lambda     *lambdahandler.Handler
	sqs        *sqshandler.Handler
	secrets    *secretshandler.Handler
	admin      *adminhandler.Handler
	logsSvc    *logssvc.Service
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, gatewaySvc *apigatewaysvc.Service, lambdaSvc *lambdasvc.Service, logsSvc *logssvc.Service, sqsSvc *sqssvc.Service, secretsSvc *secretssvc.Service, infraSvc *infrasvc.Service) *Server {
	s := &Server{
		cfg:     cfg,
		apigw:   apigatewayhandler.NewHandler(gatewaySvc),
		lambda:  lambdahandler.NewHandler(lambdaSvc),
		sqs:     sqshandler.NewHandler(sqsSvc),
		secrets: secretshandler.NewHandler(cfg, secretsSvc),
		admin:   adminhandler.NewHandler(cfg, gatewaySvc, lambdaSvc, logsSvc, sqsSvc, secretsSvc, infraSvc),
		logsSvc: logsSvc,
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

	// Lambda API — AWS-compatible endpoints
	mux.HandleFunc("GET /2015-03-31/account-settings", s.lambda.GetAccountSettings)
	mux.HandleFunc("POST /2015-03-31/functions", s.lambda.CreateFunction)
	mux.HandleFunc("GET /2015-03-31/functions", s.lambda.ListFunctions)
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

	// SQS API — query protocol (all POST with Action parameter)
	// Queue URL paths: /{accountId}/{queueName}
	mux.HandleFunc("POST /{account}/{queue}", s.sqs.Dispatch)
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
	fmt.Fprint(w, `{"status":"running","services":["apigatewayv2","lambda","sqs","secretsmanager"]}`)
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
