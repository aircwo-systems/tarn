package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/openstack-project/openstack/internal/api"
	"github.com/openstack-project/openstack/internal/apigateway"
	"github.com/openstack-project/openstack/internal/apigatewayv1"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/internal/engine"
	"github.com/openstack-project/openstack/internal/eventsource"
	"github.com/openstack-project/openstack/internal/infrastructure"
	"github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/internal/logs"
	s3store "github.com/openstack-project/openstack/internal/s3"
	"github.com/openstack-project/openstack/internal/secrets"
	"github.com/openstack-project/openstack/internal/sqs"
	"github.com/openstack-project/openstack/internal/trace"
	"github.com/openstack-project/openstack/pkg/types"
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
	if cmd.Flags().Changed("persist") || os.Getenv("OPENSTACK_PERSIST") == "" {
		if v, err := cmd.Flags().GetBool("persist"); err == nil {
			cfg.PersistenceEnabled = v
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
	if err := secretsSvc.Init(); err != nil {
		return fmt.Errorf("failed to initialize secrets store: %w", err)
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
	esmSvc.SetTraceStore(traceStore)
	esmSvc.SetCollector(collector)

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

	// Create and start API server
	server := api.NewServer(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, traceStore, collector)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("\nShutting down OpenStack...")
		eng.Cleanup(ctx)
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
