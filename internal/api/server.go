package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/account"
	adminhandler "github.com/aircwo-systems/tarn/internal/api/admin"
	apigatewayhandler "github.com/aircwo-systems/tarn/internal/api/apigateway"
	apigatewayv1handler "github.com/aircwo-systems/tarn/internal/api/apigatewayv1"
	dynamodbhandler "github.com/aircwo-systems/tarn/internal/api/dynamodb"
	eventbridgehandler "github.com/aircwo-systems/tarn/internal/api/eventbridge"
	eventsourcehandler "github.com/aircwo-systems/tarn/internal/api/eventsource"
	iamhandler "github.com/aircwo-systems/tarn/internal/api/iam"
	lambdahandler "github.com/aircwo-systems/tarn/internal/api/lambda"
	s3handler "github.com/aircwo-systems/tarn/internal/api/s3"
	secretshandler "github.com/aircwo-systems/tarn/internal/api/secrets"
	snshandler "github.com/aircwo-systems/tarn/internal/api/sns"
	sqshandler "github.com/aircwo-systems/tarn/internal/api/sqs"
	"github.com/aircwo-systems/tarn/internal/config"
	logssvc "github.com/aircwo-systems/tarn/internal/logs"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
)

// HandlerSet groups all per-account HTTP handlers.
type HandlerSet struct {
	APIGateway  *apigatewayhandler.Handler
	APIGatewayV1 *apigatewayv1handler.Handler
	Lambda      *lambdahandler.Handler
	S3          *s3handler.Handler
	SQS         *sqshandler.Handler
	SNS         *snshandler.Handler
	DynamoDB    *dynamodbhandler.Handler
	Secrets     *secretshandler.Handler
	EventSource *eventsourcehandler.Handler
	EventBridge *eventbridgehandler.Handler
	IAM         *iamhandler.Handler
	Admin       *adminhandler.Handler
}

// AccountBundle pairs a HandlerSet with the stoppable services it owns.
type AccountBundle struct {
	handlers *HandlerSet
	stop     func() // stops all background workers for this account
}

// NewAccountBundle assembles an AccountBundle from a pre-built HandlerSet and a
// stop function that halts all background workers for this account.
func NewAccountBundle(hs *HandlerSet, stop func()) *AccountBundle {
	return &AccountBundle{handlers: hs, stop: stop}
}

// HandlerRegistry lazily creates and caches per-account HandlerSets.
type HandlerRegistry struct {
	mu      sync.RWMutex
	bundles map[string]*AccountBundle
	factory func(accountID string) (*AccountBundle, error)
}

// NewHandlerRegistry creates a registry backed by factory.
func NewHandlerRegistry(factory func(accountID string) (*AccountBundle, error)) *HandlerRegistry {
	return &HandlerRegistry{
		bundles: make(map[string]*AccountBundle),
		factory: factory,
	}
}

// PreInit eagerly initialises accountID and returns the bundle.
func (r *HandlerRegistry) PreInit(accountID string) (*AccountBundle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.bundles[accountID]; ok {
		return b, nil
	}
	b, err := r.factory(accountID)
	if err != nil {
		return nil, err
	}
	r.bundles[accountID] = b
	return b, nil
}

func (r *HandlerRegistry) get(accountID string) *HandlerSet {
	r.mu.RLock()
	b, ok := r.bundles[accountID]
	r.mu.RUnlock()
	if ok {
		return b.handlers
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok = r.bundles[accountID]; ok {
		return b.handlers
	}
	b, err := r.factory(accountID)
	if err != nil {
		log.Printf("[account] init failed for %s: %v", accountID, err)
		return nil
	}
	r.bundles[accountID] = b
	return b.handlers
}

// StopAll stops all background workers across every registered account.
func (r *HandlerRegistry) StopAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.bundles {
		if b.stop != nil {
			b.stop()
		}
	}
}

// Server is the main Tarn API server.
type Server struct {
	cfg        *config.Config
	httpServer *http.Server
	registry   *HandlerRegistry
	logsSvc    *logssvc.Service
	collector  *tracesvc.Collector
	ui         http.Handler
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, registry *HandlerRegistry, logsSvc *logssvc.Service, collector *tracesvc.Collector) *Server {
	s := &Server{
		cfg:       cfg,
		registry:  registry,
		logsSvc:   logsSvc,
		collector: collector,
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

// hs resolves the per-account HandlerSet for the incoming request.
func (s *Server) hs(r *http.Request) *HandlerSet {
	accountID := account.FromRequest(r, s.cfg.AccountID)
	hs := s.registry.get(accountID)
	if hs == nil {
		// Factory failed; fall back to the default account.
		return s.registry.get(s.cfg.AccountID)
	}
	return hs
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health check (global)
	mux.HandleFunc("GET /_tarn/health", s.healthHandler)
	// Telemetry — called by in-container proxies to report observability spans
	mux.HandleFunc("POST /_tarn/telemetry/db", s.telemetryDBHandler)

	// Admin routes — account-resolved so the dashboard always falls back to
	// the default account (no SigV4 from the browser), but API callers with
	// a 12-digit AKID get their own account's view.
	mux.HandleFunc("GET /_tarn/admin/overview", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.Overview(w, r)
	})
	mux.HandleFunc("GET /_tarn/admin/secrets/{name}/value", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.SecretValue(w, r)
	})
	mux.HandleFunc("GET /_tarn/admin/queues/{name}/messages", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.QueueMessages(w, r)
	})
	mux.HandleFunc("GET /_tarn/admin/logs/groups", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.LogGroups(w, r)
	})
	mux.HandleFunc("GET /_tarn/admin/logs/groups/{name...}", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.LogGroupDetail(w, r)
	})
	mux.HandleFunc("GET /_tarn/admin/logs/events-all", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.AllLogEvents(w, r)
	})
	mux.HandleFunc("GET /_tarn/admin/logs/events/{name...}", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.LogEvents(w, r)
	})
	mux.HandleFunc("DELETE /_tarn/admin/logs/events/{name...}", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.ClearLogGroup(w, r)
	})
	mux.HandleFunc("POST /_tarn/admin/logs/prune", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.PruneLogs(w, r)
	})
	mux.HandleFunc("GET /_tarn/admin/traces/for-log", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.TracesForLog(w, r)
	})
	mux.HandleFunc("GET /_tarn/admin/infrastructure", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.Infrastructure(w, r)
	})
	mux.HandleFunc("POST /_tarn/admin/chaos", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.RunChaos(w, r)
	})
	mux.HandleFunc("POST /_tarn/admin/chaos/source", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.ScanChaosSource(w, r)
	})
	mux.HandleFunc("POST /_tarn/admin/eventbridge/fire", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.FireEventBridgeRule(w, r)
	})
	mux.HandleFunc("POST /_tarn/admin/eventbridge/race", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Admin.RunEventBridgeRace(w, r)
	})
	// EventBridge JSON protocol endpoint used by dashboard and tooling to avoid
	// colliding with UI app-server root routes.
	mux.HandleFunc("POST /_tarn/events", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).EventBridge.Dispatch(w, r)
	})

	// S3 API — path-style REST XML protocol
	// POST is handled via postAccountDispatch to avoid conflict with SQS POST /{account}/{queue...}
	mux.HandleFunc("GET /_s3/{rest...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	mux.HandleFunc("PUT /_s3/{rest...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	mux.HandleFunc("HEAD /_s3/{rest...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	mux.HandleFunc("DELETE /_s3/{rest...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	// Root GET is shared between S3 ListBuckets and the optional dashboard UI.
	mux.HandleFunc("GET /{$}", s.getRootDispatch)
	// Compatibility surface for AWS SDK clients using path-style S3 URLs.
	mux.HandleFunc("GET /{bucket}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	mux.HandleFunc("PUT /{bucket}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	mux.HandleFunc("DELETE /{bucket}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	// Lambda 2017-10-31 concurrency endpoints — must be registered before the
	// generic S3 /{bucket}/{key...} patterns.
	mux.HandleFunc("PUT /2017-10-31/functions/{name}/concurrency", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Lambda.PutFunctionConcurrency(w, r)
	})
	mux.HandleFunc("DELETE /2017-10-31/functions/{name}/concurrency", func(w http.ResponseWriter, r *http.Request) {
		s.hs(r).Lambda.DeleteFunctionConcurrency(w, r)
	})
	for _, method := range [...]string{
		http.MethodGet, http.MethodPut, http.MethodPost,
		http.MethodDelete, http.MethodPatch,
	} {
		m := method
		mux.HandleFunc(m+" /2017-10-31/functions/{name}/{subpath...}", func(w http.ResponseWriter, r *http.Request) {
			s.hs(r).Lambda.NotFound(w, r)
		})
	}

	// Object-level S3 operations: /{bucket}/{key...}
	mux.HandleFunc("GET /{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	mux.HandleFunc("PUT /{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	mux.HandleFunc("DELETE /{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })
	mux.HandleFunc("POST /{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).S3.Dispatch(w, r) })

	// API Gateway v1 (REST API) — AWS-compatible management endpoints
	mux.HandleFunc("POST /restapis", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.CreateRestAPI(w, r) })
	mux.HandleFunc("GET /restapis", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.ListRestAPIs(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.GetRestAPI(w, r) })
	mux.HandleFunc("DELETE /restapis/{restApiId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.DeleteRestAPI(w, r) })

	mux.HandleFunc("GET /restapis/{restApiId}/resources", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.ListResources(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.GetResource(w, r) })
	mux.HandleFunc("POST /restapis/{restApiId}/resources/{parentId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.CreateResource(w, r) })
	mux.HandleFunc("DELETE /restapis/{restApiId}/resources/{resourceId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.DeleteResource(w, r) })

	mux.HandleFunc("PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.PutMethod(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.GetMethod(w, r) })
	mux.HandleFunc("PATCH /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.PatchMethod(w, r) })
	mux.HandleFunc("DELETE /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.DeleteMethod(w, r) })

	mux.HandleFunc("PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.PutIntegration(w, r) })
	mux.HandleFunc("PATCH /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.PatchIntegration(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.GetIntegration(w, r) })
	mux.HandleFunc("DELETE /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.DeleteIntegration(w, r) })

	mux.HandleFunc("PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/responses/{statusCode}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.PutMethodResponse(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/responses/{statusCode}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.GetMethodResponse(w, r) })

	mux.HandleFunc("PUT /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration/responses/{statusCode}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.PutIntegrationResponse(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/resources/{resourceId}/methods/{httpMethod}/integration/responses/{statusCode}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.GetIntegrationResponse(w, r) })

	mux.HandleFunc("POST /restapis/{restApiId}/deployments", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.CreateDeployment(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/deployments", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.ListDeployments(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/deployments/{deploymentId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.GetDeployment(w, r) })

	mux.HandleFunc("POST /restapis/{restApiId}/stages", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.CreateStage(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/stages", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.ListStages(w, r) })
	mux.HandleFunc("GET /restapis/{restApiId}/stages/{stageName}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.GetStage(w, r) })
	mux.HandleFunc("DELETE /restapis/{restApiId}/stages/{stageName}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.DeleteStage(w, r) })

	// API Gateway v1 invoke surface
	for _, method := range [...]string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodHead,
	} {
		m := method
		mux.HandleFunc(m+" /_aws/execute-api/{apiId}/{stage}/{proxy...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.Invoke(w, r) })
		mux.HandleFunc(m+" /_aws/execute-api/{apiId}/{stage}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGatewayV1.Invoke(w, r) })
	}

	// API Gateway v2 API — AWS-compatible endpoints
	mux.HandleFunc("POST /v2/apis", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.CreateAPI(w, r) })
	mux.HandleFunc("GET /v2/apis", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.ListAPIs(w, r) })
	mux.HandleFunc("GET /v2/apis/{apiId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.GetAPI(w, r) })
	mux.HandleFunc("PATCH /v2/apis/{apiId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.UpdateAPI(w, r) })
	mux.HandleFunc("DELETE /v2/apis/{apiId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.DeleteAPI(w, r) })

	mux.HandleFunc("POST /v2/apis/{apiId}/integrations", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.CreateIntegration(w, r) })
	mux.HandleFunc("GET /v2/apis/{apiId}/integrations", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.ListIntegrations(w, r) })
	mux.HandleFunc("GET /v2/apis/{apiId}/integrations/{integrationId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.GetIntegration(w, r) })
	mux.HandleFunc("PATCH /v2/apis/{apiId}/integrations/{integrationId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.UpdateIntegration(w, r) })
	mux.HandleFunc("DELETE /v2/apis/{apiId}/integrations/{integrationId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.DeleteIntegration(w, r) })

	mux.HandleFunc("POST /v2/apis/{apiId}/routes", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.CreateRoute(w, r) })
	mux.HandleFunc("GET /v2/apis/{apiId}/routes", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.ListRoutes(w, r) })
	mux.HandleFunc("GET /v2/apis/{apiId}/routes/{routeId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.GetRoute(w, r) })
	mux.HandleFunc("PATCH /v2/apis/{apiId}/routes/{routeId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.UpdateRoute(w, r) })
	mux.HandleFunc("DELETE /v2/apis/{apiId}/routes/{routeId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.DeleteRoute(w, r) })

	mux.HandleFunc("GET /v2/apis/{apiId}/stages", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.ListStages(w, r) })
	mux.HandleFunc("GET /v2/apis/{apiId}/stages/{stageName}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.GetStage(w, r) })
	mux.HandleFunc("PATCH /v2/apis/{apiId}/stages/{stageName}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.UpdateStage(w, r) })

	// API Gateway invoke surface
	for _, method := range [...]string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodHead,
	} {
		m := method
		mux.HandleFunc(m+" /_apigateway/{apiId}/{stage}/{proxy...}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.Invoke(w, r) })
		mux.HandleFunc(m+" /_apigateway/{apiId}/{stage}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).APIGateway.Invoke(w, r) })
	}

	// Event Source Mapping API
	mux.HandleFunc("POST /2015-03-31/event-source-mappings", func(w http.ResponseWriter, r *http.Request) { s.hs(r).EventSource.Create(w, r) })
	mux.HandleFunc("GET /2015-03-31/event-source-mappings", func(w http.ResponseWriter, r *http.Request) { s.hs(r).EventSource.List(w, r) })
	mux.HandleFunc("GET /2015-03-31/event-source-mappings/{uuid}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).EventSource.Get(w, r) })
	mux.HandleFunc("PUT /2015-03-31/event-source-mappings/{uuid}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).EventSource.Update(w, r) })
	mux.HandleFunc("DELETE /2015-03-31/event-source-mappings/{uuid}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).EventSource.Delete(w, r) })

	// Lambda API — AWS-compatible endpoints
	mux.HandleFunc("GET /2015-03-31/account-settings", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.GetAccountSettings(w, r) })
	mux.HandleFunc("POST /2015-03-31/functions", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.CreateFunction(w, r) })
	mux.HandleFunc("GET /2015-03-31/functions", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.ListFunctions(w, r) })
	mux.HandleFunc("GET /2015-03-31/functions/{name}/configuration", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.GetFunctionConfiguration(w, r) })
	mux.HandleFunc("GET /2015-03-31/functions/{name}/versions", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.ListVersionsByFunction(w, r) })
	mux.HandleFunc("POST /2015-03-31/functions/{name}/versions", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.PublishVersion(w, r) })
	mux.HandleFunc("GET /2015-03-31/functions/{name}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.GetFunction(w, r) })
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.DeleteFunction(w, r) })
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/code", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.UpdateFunctionCode(w, r) })
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/configuration", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.UpdateFunctionConfiguration(w, r) })
	mux.HandleFunc("POST /2015-03-31/functions/{name}/invocations", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.Invoke(w, r) })
	// Aliases
	mux.HandleFunc("POST /2015-03-31/functions/{name}/aliases", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.CreateAlias(w, r) })
	mux.HandleFunc("GET /2015-03-31/functions/{name}/aliases/{aliasName}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.GetAlias(w, r) })
	mux.HandleFunc("GET /2015-03-31/functions/{name}/aliases", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.ListAliases(w, r) })
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/aliases/{aliasName}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.UpdateAlias(w, r) })
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}/aliases/{aliasName}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.DeleteAlias(w, r) })
	// Tags
	mux.HandleFunc("GET /2015-03-31/functions/{name}/tags", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.ListTags(w, r) })
	mux.HandleFunc("POST /2015-03-31/functions/{name}/tags", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.TagResource(w, r) })
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}/tags", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.UntagResource(w, r) })
	// Resource policy compatibility (Terraform aws_lambda_permission)
	mux.HandleFunc("POST /2015-03-31/functions/{name}/policy", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.AddPermission(w, r) })
	mux.HandleFunc("GET /2015-03-31/functions/{name}/policy", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.GetPolicy(w, r) })
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}/policy/{statementId}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.RemovePermission(w, r) })
	// Layers
	mux.HandleFunc("POST /2015-03-31/layers/{layerName}/versions", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.PublishLayerVersion(w, r) })
	mux.HandleFunc("GET /2015-03-31/layers", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.ListLayers(w, r) })
	mux.HandleFunc("GET /2015-03-31/layers/{layerName}/versions", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.ListLayerVersions(w, r) })
	mux.HandleFunc("GET /2015-03-31/layers/{layerName}/versions/{version}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.GetLayerVersion(w, r) })
	mux.HandleFunc("DELETE /2015-03-31/layers/{layerName}/versions/{version}", func(w http.ResponseWriter, r *http.Request) { s.hs(r).Lambda.DeleteLayerVersion(w, r) })

	// Catch-all for Lambda function sub-resources we don't emulate.
	for _, method := range [...]string{
		http.MethodGet, http.MethodPut, http.MethodPost,
		http.MethodDelete, http.MethodPatch,
	} {
		m := method
		mux.HandleFunc(m+" /2015-03-31/functions/{name}/{subpath...}", func(w http.ResponseWriter, r *http.Request) {
			s.hs(r).Lambda.NotFound(w, r)
		})
	}

	// SQS API — POST /{accountId}/{queueName} and POST /
	mux.HandleFunc("POST /{account}/{queue}", s.postAccountDispatch)
	mux.HandleFunc("POST /", s.postRootDispatch)

	// Optional dashboard UI (SPA fallback for GET requests only)
	s.registerUIRoutes(mux)
}

// Start begins listening for requests.
func (s *Server) Start() error {
	log.Printf("Tarn API server listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server and all per-account background workers.
func (s *Server) Shutdown(ctx context.Context) error {
	s.registry.StopAll()
	return s.httpServer.Shutdown(ctx)
}

// postAccountDispatch routes POST /{account}/{queue} between S3 and SQS.
func (s *Server) postAccountDispatch(w http.ResponseWriter, r *http.Request) {
	hs := s.hs(r)
	if strings.HasPrefix(r.URL.Path, "/_s3/") {
		hs.S3.Dispatch(w, r)
		return
	}
	q := r.URL.Query()
	if q.Has("uploads") || q.Has("uploadId") || q.Has("delete") {
		hs.S3.Dispatch(w, r)
		return
	}
	hs.SQS.Dispatch(w, r)
}

func (s *Server) getRootDispatch(w http.ResponseWriter, r *http.Request) {
	if s.ui != nil && wantsHTML(r) {
		s.ui.ServeHTTP(w, r)
		return
	}
	s.hs(r).S3.Dispatch(w, r)
}

// postRootDispatch routes POST / between Secrets Manager, IAM, SNS, SQS, DynamoDB, and EventBridge.
func (s *Server) postRootDispatch(w http.ResponseWriter, r *http.Request) {
	hs := s.hs(r)
	if secretshandler.IsSecretsManagerRequest(r) {
		hs.Secrets.Dispatch(w, r)
		return
	}
	if dynamodbhandler.IsDynamoDBRequest(r) {
		hs.DynamoDB.Dispatch(w, r)
		return
	}
	if strings.HasPrefix(r.Header.Get("X-Amz-Target"), "AWSEvents.") {
		hs.EventBridge.Dispatch(w, r)
		return
	}
	if strings.HasPrefix(r.Header.Get("X-Amz-Target"), "AmazonSQS.") {
		hs.SQS.Dispatch(w, r)
		return
	}
	if iamhandler.IsIAMRequest(r) {
		hs.IAM.Dispatch(w, r)
		return
	}
	if err := r.ParseForm(); err == nil {
		if snshandler.IsSNSRequest(r) || snshandler.IsSNSAction(r.FormValue("Action")) {
			hs.SNS.Dispatch(w, r)
			return
		}
	}
	hs.SQS.Dispatch(w, r)
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
	fmt.Fprint(w, `{"status":"running","services":["apigateway","apigatewayv2","lambda","s3","sqs","sns","dynamodb","secretsmanager","eventsource","eventbridge"]}`)
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
		target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
		if target != "" {
			log.Printf("%s %s %d %s target=%s", r.Method, r.URL.Path, wrapped.status, duration, target)
		} else {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, wrapped.status, duration)
		}
		if s.logsSvc != nil && !strings.HasPrefix(r.URL.Path, "/_tarn/") {
			s.logsSvc.LogAPIRequest(r.Method, r.URL.Path, wrapped.status, duration)
		}
	})
}

func wantsHTML(r *http.Request) bool {
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept")))
	return strings.Contains(accept, "text/html")
}

// dispatchProtocolRequest routes AWS protocol requests based on headers/query
// independent of URL path. Returns true when a request was dispatched.
func (s *Server) dispatchProtocolRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}

	hs := s.hs(r)

	if secretshandler.IsSecretsManagerRequest(r) {
		hs.Secrets.Dispatch(w, r)
		return true
	}
	if dynamodbhandler.IsDynamoDBRequest(r) {
		hs.DynamoDB.Dispatch(w, r)
		return true
	}

	target := strings.TrimSpace(r.Header.Get("X-Amz-Target"))
	if strings.HasPrefix(target, "AWSEvents.") {
		hs.EventBridge.Dispatch(w, r)
		return true
	}
	if strings.HasPrefix(target, "AmazonSQS.") {
		hs.SQS.Dispatch(w, r)
		return true
	}

	if iamhandler.IsIAMRequest(r) {
		hs.IAM.Dispatch(w, r)
		return true
	}

	if strings.HasPrefix(r.URL.Path, "/_tarn/") || strings.TrimSpace(r.URL.Path) == "/_tarn" {
		return false
	}

	if err := r.ParseForm(); err == nil && (snshandler.IsSNSRequest(r) || snshandler.IsSNSAction(r.FormValue("Action"))) {
		hs.SNS.Dispatch(w, r)
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

// Flush delegates to the underlying ResponseWriter if it implements http.Flusher.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
