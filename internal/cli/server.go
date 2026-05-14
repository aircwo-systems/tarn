package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aircwo-systems/tarn/internal/api"
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
	"github.com/aircwo-systems/tarn/internal/apigateway"
	"github.com/aircwo-systems/tarn/internal/apigatewayv1"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/dynamodb"
	"github.com/aircwo-systems/tarn/internal/engine"
	"github.com/aircwo-systems/tarn/internal/eventbridge"
	"github.com/aircwo-systems/tarn/internal/eventsource"
	"github.com/aircwo-systems/tarn/internal/infrastructure"
	"github.com/aircwo-systems/tarn/internal/lambda"
	"github.com/aircwo-systems/tarn/internal/logs"
	s3store "github.com/aircwo-systems/tarn/internal/s3"
	"github.com/aircwo-systems/tarn/internal/secrets"
	"github.com/aircwo-systems/tarn/internal/secretsproxy"
	"github.com/aircwo-systems/tarn/internal/sns"
	"github.com/aircwo-systems/tarn/internal/sqs"
	"github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func buildConfig(cmd *cobra.Command) (*config.Config, error) {
	cfg := config.Default()
	cfg.LoadFromEnv()

	if v, _ := cmd.Flags().GetString("host"); v != "" {
		cfg.Host = v
	}
	if v, _ := cmd.Flags().GetInt("port"); v != 0 {
		cfg.Port = v
	}
	if v, _ := cmd.Flags().GetString("data-dir"); v != "" {
		cfg.DataDir = v
	}
	if v, _ := cmd.Flags().GetString("region"); v != "" {
		cfg.Region = v
	}
	if cmd.Flags().Changed("ui") {
		if v, err := cmd.Flags().GetBool("ui"); err == nil {
			cfg.UIEnabled = v
		}
	}
	if v, _ := cmd.Flags().GetString("ui-dir"); v != "" {
		cfg.UIDir = v
	}
	if cmd.Flags().Changed("persist") || os.Getenv("TARN_PERSIST") == "" {
		if v, err := cmd.Flags().GetBool("persist"); err == nil {
			cfg.PersistenceEnabled = v
		}
	}
	if cmd.Flags().Changed("expose-secrets-proxy") || os.Getenv("TARN_EXPOSE_SECRETS_PROXY") == "" {
		if v, err := cmd.Flags().GetBool("expose-secrets-proxy"); err == nil {
			cfg.ExposeSecretsProxy = v
		}
	}
	if cmd.Flags().Changed("secrets-proxy-host") || os.Getenv("TARN_SECRETS_PROXY_HOST") == "" {
		if v, err := cmd.Flags().GetString("secrets-proxy-host"); err == nil && v != "" {
			cfg.SecretsProxyHost = v
		}
	}
	if cmd.Flags().Changed("secrets-proxy-port") || os.Getenv("TARN_SECRETS_PROXY_PORT") == "" {
		if v, err := cmd.Flags().GetInt("secrets-proxy-port"); err == nil && v > 0 {
			cfg.SecretsProxyPort = v
		}
	}
	if cmd.Flags().Changed("secrets-proxy-token") || os.Getenv("TARN_SECRETS_PROXY_TOKEN") == "" {
		if v, err := cmd.Flags().GetString("secrets-proxy-token"); err == nil && v != "" {
			cfg.SecretsProxySessionToken = v
		}
	}
	if cmd.Flags().Changed("secrets-proxy-require-token") || os.Getenv("TARN_SECRETS_PROXY_REQUIRE_TOKEN") == "" {
		if v, err := cmd.Flags().GetBool("secrets-proxy-require-token"); err == nil {
			cfg.SecretsProxyRequireToken = v
		}
	}
	if cmd.Flags().Changed("vault-key") {
		if v, err := cmd.Flags().GetString("vault-key"); err == nil {
			cfg.VaultKeyPath = v
		}
	}

	if err := cfg.EnsureDataDir(); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	return cfg, nil
}

func writePIDFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644)
}

// sharedDeps holds services that are global across all accounts.
type sharedDeps struct {
	eng        *engine.Engine
	pool       *engine.WarmPool
	logsSvc    *logs.Service
	traceStore *trace.Store
	collector  *trace.Collector
	infraSvc   *infrastructure.Service
	vault      *secrets.Vault
}

// initAccountBundle creates all per-account services and handlers.
// It is called once per account ID, either at startup (default) or lazily.
func initAccountBundle(acctCfg *config.Config, shared *sharedDeps) (*api.AccountBundle, error) {
	log.Printf("[account] initializing %s (data: %s)", acctCfg.AccountID, acctCfg.DataDir)

	if err := acctCfg.EnsureDataDir(); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// Lambda
	lambdaStore := lambda.NewStore(acctCfg)
	if err := lambdaStore.Init(); err != nil {
		return nil, fmt.Errorf("lambda store: %w", err)
	}
	lambdaSvc := lambda.NewService(acctCfg, lambdaStore, shared.eng, shared.pool, shared.logsSvc)
	lambdaSvc.ActivatePendingFunctions()

	// SQS
	sqsSvc := sqs.NewService(acctCfg)
	if err := sqsSvc.Init(); err != nil {
		return nil, fmt.Errorf("sqs store: %w", err)
	}
	sqsSvc.Start()

	// Secrets
	secretsSvc := secrets.NewService(acctCfg)
	if shared.vault != nil {
		secretsSvc.SetVault(shared.vault)
	}
	if err := secretsSvc.Init(); err != nil {
		return nil, fmt.Errorf("secrets store: %w", err)
	}

	// DynamoDB
	dynamoSvc := dynamodb.NewService(acctCfg)
	if err := dynamoSvc.Init(); err != nil {
		return nil, fmt.Errorf("dynamodb store: %w", err)
	}

	// SNS
	snsSvc := sns.NewService(acctCfg, sqsSvc, lambdaSvc)
	if err := snsSvc.Init(); err != nil {
		return nil, fmt.Errorf("sns store: %w", err)
	}
	snsSvc.SetTraceStore(shared.traceStore)

	// S3
	s3Svc := s3store.NewService(acctCfg)
	if err := s3Svc.Init(); err != nil {
		return nil, fmt.Errorf("s3 store: %w", err)
	}

	// SQS send helper shared by both API Gateway versions
	sqsSendFn := func(queueName, body string, attrs map[string]*types.MessageAttribute, groupId, dedupId string) (string, string, error) {
		msg, err := sqsSvc.SendMessage(queueName, body, 0, attrs, groupId, dedupId)
		if err != nil {
			return "", "", err
		}
		return msg.MessageId, msg.MD5OfBody, nil
	}

	// API Gateway v2
	gatewaySvc := apigateway.NewService(acctCfg, lambdaSvc, apigateway.SQSSendFunc(sqsSendFn))
	if err := gatewaySvc.Init(); err != nil {
		return nil, fmt.Errorf("api gateway store: %w", err)
	}
	gatewaySvc.SetTraceStore(shared.traceStore)
	gatewaySvc.SetCollector(shared.collector)

	// API Gateway v1
	gatewayV1Svc := apigatewayv1.NewService(acctCfg, lambdaSvc, apigatewayv1.SQSSendFunc(sqsSendFn))
	if err := gatewayV1Svc.Init(); err != nil {
		return nil, fmt.Errorf("api gateway v1 store: %w", err)
	}
	gatewayV1Svc.SetTraceStore(shared.traceStore)
	gatewayV1Svc.SetCollector(shared.collector)

	// Event Source Mapping
	esmStore := eventsource.NewStore(acctCfg)
	esmSvc := eventsource.NewService(acctCfg, esmStore, lambdaSvc, sqsSvc, dynamoSvc)
	if err := esmSvc.Init(); err != nil {
		return nil, fmt.Errorf("eventsource store: %w", err)
	}
	esmSvc.SetTraceStore(shared.traceStore)
	esmSvc.SetCollector(shared.collector)

	// EventBridge
	eventbridgeStore := eventbridge.NewStore(acctCfg)
	eventbridgeSvc := eventbridge.NewService(acctCfg, eventbridgeStore, lambdaSvc)
	if err := eventbridgeSvc.Init(); err != nil {
		return nil, fmt.Errorf("eventbridge store: %w", err)
	}
	eventbridgeSvc.SetTraceStore(shared.traceStore)
	eventbridgeSvc.SetCollector(shared.collector)

	// S3 event callback — routes object events to this account's Lambda functions.
	s3Svc.SetEventCallback(func(eventName string, bucket, key string, size int64, etag string) {
		notifCfg := s3Svc.GetBucketNotificationConfiguration(bucket)
		if notifCfg == nil {
			return
		}
		for _, lc := range notifCfg.LambdaConfigurations {
			if !matchesNotification(lc, eventName, key) {
				continue
			}
			correlationID := trace.NewCorrelationID()
			payload := buildS3EventPayload(eventName, bucket, key, size, etag, acctCfg.Region, correlationID)
			go func(fnName string, p []byte, corr string) {
				shared.collector.Begin(fnName)
				traceStart := time.Now()
				invokeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				out, err := lambdaSvc.Invoke(invokeCtx, &types.InvokeInput{
					FunctionName:   fnName,
					Payload:        p,
					InvocationType: "Event",
				})
				durationMs := time.Since(traceStart).Milliseconds()
				subSpans := trace.SubSpansToSpans(shared.collector.CollectWithFlush(fnName))
				status := 200
				spanStatus := "ok"
				if err != nil || (out != nil && out.FunctionError != "") {
					status = 500
					spanStatus = "error"
				}
				shared.traceStore.Add(&trace.Trace{
					ID:            uuid.NewString()[:8],
					CorrelationID: corr,
					StartedAt:     traceStart,
					DurationMs:    durationMs,
					Status:        status,
					Spans: append([]trace.Span{
						{Kind: "s3", Name: bucket + "/" + key, DurationMs: 0, Status: "ok", Meta: map[string]string{"event": eventName}},
						{Kind: "lambda", Name: fnName, DurationMs: durationMs, Status: spanStatus},
					}, subSpans...),
				})
			}(lc.LambdaFunctionName, payload, correlationID)
		}
	})

	// Start background workers
	eventbridgeSvc.Start()
	esmSvc.Start()

	// Build HTTP handlers
	lh := lambdahandler.NewHandler(lambdaSvc, s3Svc)
	lh.SetTraceStore(shared.traceStore)
	lh.SetCollector(shared.collector)

	sh := secretshandler.NewHandler(acctCfg, secretsSvc)
	sh.SetCollector(shared.collector)

	hs := &api.HandlerSet{
		APIGateway:  apigatewayhandler.NewHandler(gatewaySvc),
		APIGatewayV1: apigatewayv1handler.NewHandler(gatewayV1Svc),
		Lambda:      lh,
		S3:          s3handler.NewHandler(s3Svc),
		SQS:         sqshandler.NewHandler(sqsSvc),
		SNS:         snshandler.NewHandler(snsSvc),
		DynamoDB:    dynamodbhandler.NewHandler(dynamoSvc),
		Secrets:     sh,
		EventSource: eventsourcehandler.NewHandler(esmSvc),
		EventBridge: eventbridgehandler.NewHandler(eventbridgeSvc),
		IAM:         iamhandler.NewHandler(acctCfg.AccountID),
		Admin: adminhandler.NewHandler(
			acctCfg, gatewaySvc, gatewayV1Svc, lambdaSvc, shared.logsSvc,
			sqsSvc, snsSvc, dynamoSvc, secretsSvc, shared.infraSvc,
			s3Svc, esmSvc, eventbridgeSvc, shared.traceStore,
		),
	}

	stop := func() {
		sqsSvc.Stop()
		esmSvc.Stop()
		eventbridgeSvc.Stop()
	}

	return api.NewAccountBundle(hs, stop), nil
}

func startServer(cfg *config.Config) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pidPath := cfg.PIDFilePath()
	if err := writePIDFile(pidPath); err != nil {
		log.Printf("WARNING: could not write PID file %s: %v", pidPath, err)
	} else {
		defer os.Remove(pidPath)
	}

	// Initialize container engine (shared across all accounts)
	eng, err := engine.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize container engine: %w", err)
	}
	defer eng.Close()

	dockerPingErr := eng.Ping(ctx)
	if dockerPingErr != nil {
		log.Printf("WARNING: %v", dockerPingErr)
		log.Println("Lambda functions requiring Docker will not work until Docker is available.")
	}

	// Warm pool is shared so containers can be reused across accounts
	pool := engine.NewWarmPool(eng, cfg.LambdaKeepAliveMS)
	pool.Start()
	defer pool.Stop()

	// Shared services
	logsSvc := logs.NewService(cfg)
	traceStore := trace.OpenStore(cfg.DataDir)
	defer traceStore.Close()
	collector := trace.NewCollector()

	infraSvc := infrastructure.NewService(cfg.InfraProbeTargets, cfg.InfraProbeEnabled)
	infraSvc.Start(ctx)
	defer infraSvc.Stop()

	dockerStatus := "connected"
	dockerErr := ""
	if dockerPingErr != nil {
		dockerStatus = "unreachable"
		dockerErr = dockerPingErr.Error()
	}
	infraSvc.SetResult(infrastructure.ProbeResult{
		Name:   "Docker",
		Kind:   "docker",
		Host:   "localhost",
		Port:   0,
		Status: dockerStatus,
		Error:  dockerErr,
	})

	// Load vault once for all accounts (secrets encryption key is global)
	var vault *secrets.Vault
	if cfg.VaultKeyPath != "" {
		v, err := secrets.LoadOrCreateVault(cfg.VaultKeyPath)
		if err != nil {
			return fmt.Errorf("failed to initialize vault: %w", err)
		}
		vault = v
		log.Printf("[vault] secrets encryption enabled (key: %s)", cfg.VaultKeyPath)
	}

	shared := &sharedDeps{
		eng:        eng,
		pool:       pool,
		logsSvc:    logsSvc,
		traceStore: traceStore,
		collector:  collector,
		infraSvc:   infraSvc,
		vault:      vault,
	}

	// Build the per-account factory closure. It captures all shared deps.
	factory := func(accountID string) (*api.AccountBundle, error) {
		acctCfg := cfg.ForAccount(accountID)
		return initAccountBundle(acctCfg, shared)
	}

	registry := api.NewHandlerRegistry(factory)

	// Pre-initialize the default account so it's ready before the first request.
	// Any startup errors (e.g. corrupt state files) surface here rather than on
	// the first API call.
	if _, err := registry.PreInit(cfg.AccountID); err != nil {
		return fmt.Errorf("failed to initialize default account: %w", err)
	}

	// Secrets proxy (uses default account's secrets service — single account proxy)
	var secretsProxyServer *http.Server
	if cfg.ExposeSecretsProxy {
		// Get the default account's secrets service for the proxy by initializing
		// it eagerly — the admin handler wires up the proxy telemetry via logsSvc.
		logsSvc.CreateLogGroup("/tarn/secrets-proxy")

		// Wire secrets proxy using the upstream endpoint
		addr := fmt.Sprintf("%s:%d", cfg.SecretsProxyHost, cfg.SecretsProxyPort)
		upstream := cfg.Endpoint()
		token := cfg.SecretsProxySessionToken
		if token == "" {
			token = "local-dev-token"
		}

		secretsProxyServer = &http.Server{
			Addr: addr,
			Handler: secretsproxy.NewHandler(secretsproxy.Options{
				UpstreamURL:  upstream,
				SessionToken: token,
				RequireToken: cfg.SecretsProxyRequireToken,
				OnRequest: func(event secretsproxy.RequestEvent) {
					recordSecretsProxyTelemetry(logsSvc, traceStore, event)
				},
			}),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to start secrets proxy listener on %s: %w", addr, err)
		}
		go func() {
			log.Printf("[secrets-proxy] listening on %s, upstream: %s", addr, upstream)
			if err := secretsProxyServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("[secrets-proxy] server error: %v", err)
			}
		}()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = secretsProxyServer.Shutdown(ctx)
		}()
	}

	// Create and start API server
	server := api.NewServer(cfg, registry, logsSvc, collector)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("\nShutting down Tarn...")
		eng.Cleanup(ctx)
		if secretsProxyServer != nil {
			if err := secretsProxyServer.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("[secrets-proxy] shutdown error: %v", err)
			}
		}
		server.Shutdown(ctx)
		cancel()
	}()

	err = server.Start()
	if err != nil && err.Error() == "http: Server closed" {
		return nil
	}
	return err
}

func recordSecretsProxyTelemetry(logsSvc *logs.Service, traceStore *trace.Store, event secretsproxy.RequestEvent) {
	startedAt := event.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	statusCode := event.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	spanStatus := "ok"
	switch {
	case statusCode >= 500:
		spanStatus = "error"
	case statusCode >= 400:
		spanStatus = "client_error"
	}

	secretID := event.SecretID
	if secretID == "" {
		secretID = "(missing secretId)"
	}
	path := "/secretsmanager/get?secretId=" + url.QueryEscape(secretID)

	if logsSvc != nil {
		level := logs.LevelINFO
		if statusCode >= 500 {
			level = logs.LevelERROR
		} else if statusCode >= 400 {
			level = logs.LevelWARN
		}
		fnName := event.FunctionName
		if fnName == "" {
			fnName = "unknown-local"
		}
		msg := fmt.Sprintf(
			"GET /secretsmanager/get status=%d duration=%dms secretId=%s function=%s tokenValid=%t",
			statusCode,
			event.DurationMs,
			secretID,
			fnName,
			event.TokenValid,
		)
		if event.CallerName != "" {
			msg += " caller=" + event.CallerName
		}
		if event.Error != "" {
			msg += " error=" + event.Error
		}
		logsSvc.PutLogEvents("/tarn/secrets-proxy", "requests", []logs.LogEvent{
			{
				Timestamp: startedAt,
				Message:   msg,
				Level:     level,
				Source:    logs.SourceAPI,
			},
		})
	}

	if traceStore != nil {
		spanMeta := map[string]string{
			"source": event.Source,
		}
		if event.FunctionName != "" {
			spanMeta["functionName"] = event.FunctionName
		}
		spans := []trace.Span{
			{
				Kind:       "cache_extension",
				Name:       "secrets-proxy",
				DurationMs: event.DurationMs,
				Status:     spanStatus,
				Meta:       spanMeta,
			},
			{
				Kind:       "secrets",
				Name:       secretID,
				DurationMs: event.DurationMs,
				Status:     spanStatus,
			},
		}
		if event.FunctionName != "" {
			spans = append([]trace.Span{
				{
					Kind:       "lambda",
					Name:       event.FunctionName,
					DurationMs: event.DurationMs,
					Status:     spanStatus,
					Meta: map[string]string{
						"source": "secrets-proxy",
					},
				},
			}, spans...)
		} else if event.CallerName != "" {
			meta := map[string]string{
				"sourceKind": event.CallerKind,
			}
			if event.UserAgent != "" {
				meta["userAgent"] = event.UserAgent
			}
			if event.Origin != "" {
				meta["origin"] = event.Origin
			}
			if event.ClientIP != "" {
				meta["clientIp"] = event.ClientIP
			}
			spans = append([]trace.Span{
				{
					Kind:       "external",
					Name:       event.CallerName,
					DurationMs: 0,
					Status:     spanStatus,
					Meta:       meta,
				},
			}, spans...)
		}
		traceStore.Add(&trace.Trace{
			ID:         uuid.NewString()[:8],
			StartedAt:  startedAt,
			DurationMs: event.DurationMs,
			Status:     statusCode,
			Method:     http.MethodGet,
			Path:       path,
			Spans:      spans,
		})
	}
}

func matchesNotification(lc types.S3LambdaNotification, eventName, key string) bool {
	eventMatch := false
	for _, evt := range lc.Events {
		if matchEventPattern(string(evt), eventName) {
			eventMatch = true
			break
		}
	}
	if !eventMatch {
		return false
	}
	if lc.FilterPrefix != "" && !strings.HasPrefix(key, lc.FilterPrefix) {
		return false
	}
	if lc.FilterSuffix != "" && !strings.HasSuffix(key, lc.FilterSuffix) {
		return false
	}
	return true
}

func matchEventPattern(pattern, eventName string) bool {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return false
	}
	if p == "*" {
		return true
	}
	if strings.HasSuffix(p, "*") {
		return strings.HasPrefix(eventName, strings.TrimSuffix(p, "*"))
	}
	return p == eventName
}

func buildS3EventPayload(eventName, bucket, key string, size int64, etag, region, correlationID string) []byte {
	record := map[string]any{
		"eventVersion": "2.1",
		"eventSource":  "aws:s3",
		"awsRegion":    region,
		"eventTime":    time.Now().UTC().Format(time.RFC3339),
		"eventName":    eventName,
		"s3": map[string]any{
			"bucket": map[string]any{
				"name": bucket,
				"arn":  "arn:aws:s3:::" + bucket,
			},
			"object": map[string]any{
				"key":  key,
				"size": size,
				"eTag": etag,
			},
		},
		"correlationId": correlationID,
	}
	data, _ := json.Marshal(map[string]any{
		"correlationId": correlationID,
		"Records":       []any{record},
	})
	return data
}

func displayHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "localhost"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return "localhost"
	}
	return host
}
