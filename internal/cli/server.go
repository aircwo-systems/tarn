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
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/aircwo-systems/tarn/internal/api"
	"github.com/aircwo-systems/tarn/internal/apigateway"
	"github.com/aircwo-systems/tarn/internal/apigatewayv1"
	"github.com/aircwo-systems/tarn/internal/config"
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

func startServer(cfg *config.Config) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize container engine
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

	// Initialize warm pool
	pool := engine.NewWarmPool(eng, cfg.LambdaKeepAliveMS)
	pool.Start()
	defer pool.Stop()

	// Initialize Logs service
	logsSvc := logs.NewService(cfg)

	// Initialize Lambda store and service
	store := lambda.NewStore(cfg)
	if err := store.Init(); err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}
	lambdaSvc := lambda.NewService(cfg, store, eng, pool, logsSvc)
	lambdaSvc.ActivatePendingFunctions()

	// Initialize SQS service
	sqsSvc := sqs.NewService(cfg)
	if err := sqsSvc.Init(); err != nil {
		return fmt.Errorf("failed to initialize sqs store: %w", err)
	}
	sqsSvc.Start()
	defer sqsSvc.Stop()

	// Initialize Secrets Manager service
	secretsSvc := secrets.NewService(cfg)
	if cfg.VaultKeyPath != "" {
		vault, err := secrets.LoadOrCreateVault(cfg.VaultKeyPath)
		if err != nil {
			return fmt.Errorf("failed to initialize vault: %w", err)
		}
		secretsSvc.SetVault(vault)
		log.Printf("[vault] secrets encryption enabled (key: %s)", cfg.VaultKeyPath)
	}
	if err := secretsSvc.Init(); err != nil {
		return fmt.Errorf("failed to initialize secrets store: %w", err)
	}

	// Initialize SNS service
	snsSvc := sns.NewService(cfg, sqsSvc, lambdaSvc)
	if err := snsSvc.Init(); err != nil {
		return fmt.Errorf("failed to initialize sns store: %w", err)
	}

	// Initialize S3 service
	s3Svc := s3store.NewService(cfg)
	if err := s3Svc.Init(); err != nil {
		return fmt.Errorf("failed to initialize s3 store: %w", err)
	}

	// Initialize API Gateway services (with shared SQS send function)
	sqsSendFn := func(queueName, body, groupId, dedupId string) (string, string, error) {
		msg, err := sqsSvc.SendMessage(queueName, body, 0, nil, groupId, dedupId)
		if err != nil {
			return "", "", err
		}
		return msg.MessageId, msg.MD5OfBody, nil
	}
	sqsSend := apigateway.SQSSendFunc(sqsSendFn)
	gatewaySvc := apigateway.NewService(cfg, lambdaSvc, sqsSend)
	if err := gatewaySvc.Init(); err != nil {
		return fmt.Errorf("failed to initialize api gateway store: %w", err)
	}

	// Initialize API Gateway v1 (REST API) service
	sqsSendV1 := apigatewayv1.SQSSendFunc(func(queueName, body, groupId, dedupId string) (string, string, error) {
		msg, err := sqsSvc.SendMessage(queueName, body, 0, nil, groupId, dedupId)
		if err != nil {
			return "", "", err
		}
		return msg.MessageId, msg.MD5OfBody, nil
	})
	gatewayV1Svc := apigatewayv1.NewService(cfg, lambdaSvc, sqsSendV1)
	if err := gatewayV1Svc.Init(); err != nil {
		return fmt.Errorf("failed to initialize api gateway v1 store: %w", err)
	}

	// Initialize event source mapping service
	esmStore := eventsource.NewStore(cfg)
	esmSvc := eventsource.NewService(cfg, esmStore, lambdaSvc, sqsSvc)
	if err := esmSvc.Init(); err != nil {
		return fmt.Errorf("failed to initialize event source store: %w", err)
	}

	// Initialize request trace store and sub-span collector. Attach both to gateway
	// and ESM before starting pollers so boot-time invocations are fully traced.
	traceStore := trace.NewStore()
	collector := trace.NewCollector()
	gatewaySvc.SetTraceStore(traceStore)
	gatewaySvc.SetCollector(collector)
	gatewayV1Svc.SetTraceStore(traceStore)
	gatewayV1Svc.SetCollector(collector)
	snsSvc.SetTraceStore(traceStore)
	esmSvc.SetTraceStore(traceStore)
	esmSvc.SetCollector(collector)

	// Initialize EventBridge scheduled-rules service.
	eventbridgeStore := eventbridge.NewStore(cfg)
	eventbridgeSvc := eventbridge.NewService(cfg, eventbridgeStore, lambdaSvc)
	if err := eventbridgeSvc.Init(); err != nil {
		return fmt.Errorf("failed to initialize eventbridge store: %w", err)
	}
	eventbridgeSvc.SetTraceStore(traceStore)
	eventbridgeSvc.SetCollector(collector)
	eventbridgeSvc.Start()
	defer eventbridgeSvc.Stop()

	esmSvc.Start()
	defer esmSvc.Stop()

	// Wire S3 event callback for bucket notifications
	s3Svc.SetEventCallback(func(eventName string, bucket, key string, size int64, etag string) {
		notifCfg := s3Svc.GetBucketNotificationConfiguration(bucket)
		if notifCfg == nil {
			return
		}
		for _, lc := range notifCfg.LambdaConfigurations {
			if !matchesNotification(lc, eventName, key) {
				continue
			}
			payload := buildS3EventPayload(eventName, bucket, key, size, etag, cfg.Region)
			go func(fnName string, p []byte) {
				collector.Begin(fnName)
				traceStart := time.Now()
				invokeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				out, err := lambdaSvc.Invoke(invokeCtx, &types.InvokeInput{
					FunctionName:   fnName,
					Payload:        p,
					InvocationType: "Event",
				})
				durationMs := time.Since(traceStart).Milliseconds()
				subSpans := trace.SubSpansToSpans(collector.CollectWithFlush(fnName))
				status := 200
				spanStatus := "ok"
				if err != nil || (out != nil && out.FunctionError != "") {
					status = 500
					spanStatus = "error"
				}
				traceStore.Add(&trace.Trace{
					ID:         uuid.NewString()[:8],
					StartedAt:  traceStart,
					DurationMs: durationMs,
					Status:     status,
					Spans: append([]trace.Span{
						{Kind: "s3", Name: bucket + "/" + key, DurationMs: 0, Status: "ok", Meta: map[string]string{"event": eventName}},
						{Kind: "lambda", Name: fnName, DurationMs: durationMs, Status: spanStatus},
					}, subSpans...),
				})
			}(lc.LambdaFunctionName, payload)
		}
	})

	// Initialize infrastructure probe service
	infraSvc := infrastructure.NewService(cfg.InfraProbeTargets, cfg.InfraProbeEnabled)
	infraSvc.Start(ctx)
	defer infraSvc.Stop()

	// Inject Docker engine connectivity as an infra probe result
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

	var secretsProxyServer *http.Server
	if cfg.ExposeSecretsProxy {
		logsSvc.CreateLogGroup("/tarn/secrets-proxy")
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
	server := api.NewServer(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, snsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, eventbridgeSvc, traceStore, collector)

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
	// http.ErrServerClosed is expected on graceful shutdown
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

func buildS3EventPayload(eventName, bucket, key string, size int64, etag, region string) []byte {
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
	}
	data, _ := json.Marshal(map[string]any{"Records": []any{record}})
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
