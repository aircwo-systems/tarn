//go:build lite

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	mcpcli "github.com/aircwo-systems/tarn/internal/cli/mcp"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/spf13/cobra"
)

var version = "0.1.0-dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "tarn",
		Short: "Tarn — open-source AWS emulator",
		Long: `Tarn is a fully open-source AWS cloud emulator for local development and testing.
It provides high-fidelity emulation of AWS services including API Gateway, Lambda, and SQS.

Start the server:
  tarn start`,
	}

	root.AddCommand(newStartCmd())
	root.AddCommand(newStopCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newFlushCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(mcpcli.NewMCPCmd(version))

	root.PersistentFlags().String("host", "0.0.0.0", "API server bind address")
	root.PersistentFlags().Int("port", 4566, "API server port")
	root.PersistentFlags().String("data-dir", "", "Data directory (default: ~/.tarn/data)")
	root.PersistentFlags().String("region", "us-east-1", "Emulated AWS region")
	root.PersistentFlags().Bool("persist", true, "Persist non-Lambda service configuration across Tarn sessions")

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Tarn version and check for updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("tarn %s\n", version)
			return runVersionUpdateCheck(cmd, os.Stdout, version)
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop a running Tarn server",
		RunE:  runStop,
	}
}

func runStop(cmd *cobra.Command, _ []string) error {
	cfg := config.Default()
	cfg.LoadFromEnv()
	if v, err := cmd.Flags().GetString("data-dir"); err == nil && strings.TrimSpace(v) != "" {
		cfg.DataDir = strings.TrimSpace(v)
	}

	data, err := os.ReadFile(cfg.PIDFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("tarn does not appear to be running (no PID file at %s)", cfg.PIDFilePath())
		}
		return fmt.Errorf("read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid PID file contents: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop tarn (pid %d): %w", pid, err)
	}

	fmt.Fprintf(os.Stderr, "Stopping tarn (pid %d)...\n", pid)
	return nil
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of a running Tarn server",
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, _ []string) error {
	cfg := config.Default()
	cfg.LoadFromEnv()
	if v, err := cmd.Flags().GetString("host"); err == nil && strings.TrimSpace(v) != "" {
		cfg.Host = strings.TrimSpace(v)
	}
	if v, err := cmd.Flags().GetInt("port"); err == nil && v != 0 {
		cfg.Port = v
	}
	if v, err := cmd.Flags().GetString("data-dir"); err == nil && strings.TrimSpace(v) != "" {
		cfg.DataDir = strings.TrimSpace(v)
	}

	healthURL := cfg.Endpoint() + "/_tarn/health"

	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("tarn: not running\nEndpoint: %s\n", cfg.Endpoint())
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var health struct {
		Status   string   `json:"status"`
		Services []string `json:"services"`
	}
	_ = json.Unmarshal(body, &health)

	status := health.Status
	if status == "" {
		status = "running"
	}

	fmt.Printf("tarn: %s\n", status)
	fmt.Printf("Endpoint: %s\n", cfg.Endpoint())
	if len(health.Services) > 0 {
		fmt.Printf("Services: %s\n", strings.Join(health.Services, ", "))
	}

	pidData, err := os.ReadFile(cfg.PIDFilePath())
	if err == nil {
		fmt.Printf("PID:      %s\n", strings.TrimSpace(string(pidData)))
	}

	return nil
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Tarn API server",
		RunE:  runStart,
	}

	cmd.Flags().Bool("expose-secrets-proxy", false, "Run a local secrets extension-compatible proxy alongside the API server")
	cmd.Flags().String("secrets-proxy-host", "127.0.0.1", "Host interface for the local secrets proxy")
	cmd.Flags().Int("secrets-proxy-port", 2773, "Port for the local secrets proxy")
	cmd.Flags().String("secrets-proxy-token", "", "Expected X-Aws-Parameters-Secrets-Token value (defaults to TARN_SECRETS_PROXY_TOKEN or local-dev-token)")
	cmd.Flags().Bool("secrets-proxy-require-token", true, "Require X-Aws-Parameters-Secrets-Token validation in the local secrets proxy")
	cmd.Flags().String("vault-key", "", "Path to AES-256 key file for encrypting secrets at rest (default: ~/.tarn/vault.key)")

	return cmd
}

func runStart(cmd *cobra.Command, _ []string) error {
	cfg, err := buildConfig(cmd)
	if err != nil {
		return err
	}
	maybePrintUpdateNotice(os.Stderr, cfg.DataDir, version)

	fmt.Fprintf(os.Stderr, `
  _____
 |_   _|_ _ _ __ _ __
   | |/ _' | '__| '_ \
   | | (_| | |  | | | |
   |_|\__,_|_|  |_| |_|

`)
	fmt.Fprintf(os.Stderr, "Version:  %s\n", version)
	fmt.Fprintf(os.Stderr, "Region:   %s\n", cfg.Region)
	fmt.Fprintf(os.Stderr, "Endpoint: %s\n", cfg.Endpoint())
	fmt.Fprintf(os.Stderr, "Data Dir: %s\n", cfg.DataDir)
	fmt.Fprintln(os.Stderr, "Services: apigateway, apigatewayv2, lambda, s3, sqs, sns, dynamodb, secretsmanager, eventbridge")
	if cfg.VaultKeyPath != "" {
		fmt.Fprintf(os.Stderr, "Vault:     %s\n", cfg.VaultKeyPath)
	}
	if cfg.ExposeSecretsProxy {
		fmt.Fprintf(os.Stderr, "Secrets Proxy: http://%s:%d\n", displayHost(cfg.SecretsProxyHost), cfg.SecretsProxyPort)
	}
	fmt.Fprintln(os.Stderr, "")

	return startServer(cfg)
}
