package cli

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/openstack-project/openstack/internal/api"
	"github.com/openstack-project/openstack/internal/apigateway"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/internal/engine"
	"github.com/openstack-project/openstack/internal/infrastructure"
	"github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/internal/logs"
	"github.com/openstack-project/openstack/internal/secrets"
	"github.com/openstack-project/openstack/internal/sqs"
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

	// Initialize API Gateway service
	gatewaySvc := apigateway.NewService(cfg, lambdaSvc)
	if err := gatewaySvc.Init(); err != nil {
		return fmt.Errorf("failed to initialize api gateway store: %w", err)
	}

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
	server := api.NewServer(cfg, gatewaySvc, lambdaSvc, logsSvc, sqsSvc, secretsSvc, infraSvc)

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

func displayHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "localhost"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return "localhost"
	}
	return host
}
