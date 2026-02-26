package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/openstack-project/openstack/internal/api"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/internal/engine"
	"github.com/openstack-project/openstack/internal/lambda"
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

	if err := eng.Ping(ctx); err != nil {
		log.Printf("WARNING: %v", err)
		log.Println("Lambda functions requiring Docker will not work until Docker is available.")
	}

	// Initialize warm pool
	pool := engine.NewWarmPool(eng, cfg.LambdaKeepAliveMS)
	pool.Start()
	defer pool.Stop()

	// Initialize Lambda store and service
	store := lambda.NewStore(cfg)
	if err := store.Init(); err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}
	lambdaSvc := lambda.NewService(cfg, store, eng, pool)

	// Create and start API server
	server := api.NewServer(cfg, lambdaSvc)

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

	return server.Start()
}
