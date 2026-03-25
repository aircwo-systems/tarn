package cli

import (
	"fmt"
	"os"

	"github.com/aircwo-systems/tarn/internal/cli/lambda"
	s3cli "github.com/aircwo-systems/tarn/internal/cli/s3"
	"github.com/aircwo-systems/tarn/internal/cli/secrets"
	"github.com/aircwo-systems/tarn/internal/cli/sns"
	"github.com/aircwo-systems/tarn/internal/cli/sqs"
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
  tarn start

Manage Lambda functions:
  tarn lambda create --name my-func --runtime nodejs20.x --handler index.handler --zip ./code.zip
  tarn lambda invoke --name my-func --payload '{"key": "value"}'
  tarn lambda list
  tarn lambda delete --name my-func

Manage SQS queues:
  tarn sqs create-queue --name my-queue
  tarn sqs send --queue my-queue --body "hello world"
  tarn sqs receive --queue my-queue
  tarn sqs list
  tarn sqs delete-queue --name my-queue

Manage SNS topics:
  tarn sns create-topic --name my-topic
  tarn sns subscribe --topic-arn arn:aws:sns:us-east-1:000000000000:my-topic --protocol sqs --endpoint arn:aws:sqs:us-east-1:000000000000:my-queue
  tarn sns publish --topic-arn arn:aws:sns:us-east-1:000000000000:my-topic --message "hello"
  tarn sns list-topics
  tarn sns delete-topic --topic-arn arn:aws:sns:us-east-1:000000000000:my-topic

Manage S3 buckets:
  tarn s3 mb --name my-bucket
  tarn s3 ls
  tarn s3 cp --bucket my-bucket --key file.txt --file ./file.txt
  tarn s3 get --bucket my-bucket --key file.txt
  tarn s3 rm --bucket my-bucket --key file.txt
  tarn s3 rb --name my-bucket

Manage Secrets Manager:
  tarn secrets create --name my-secret --value "password123"
  tarn secrets get --name my-secret
  tarn secrets list
  tarn secrets update --name my-secret --value "new-value"
  tarn secrets delete --name my-secret

Flush provisioned resources:
  tarn flush
  tarn flush --tag feature=r10
  tarn flush --tag r10 --dry-run
	tarn flush --storage`,
	}

	root.AddCommand(newStartCmd())
	root.AddCommand(newFlushCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(lambda.NewLambdaCmd())
	root.AddCommand(s3cli.NewS3Cmd())
	root.AddCommand(sqs.NewSQSCmd())
	root.AddCommand(sns.NewSNSCmd())
	root.AddCommand(secrets.NewSecretsCmd())

	root.PersistentFlags().String("host", "0.0.0.0", "API server bind address")
	root.PersistentFlags().Int("port", 4566, "API server port")
	root.PersistentFlags().String("data-dir", "", "Data directory (default: ~/.tarn/data)")
	root.PersistentFlags().String("region", "us-east-1", "Emulated AWS region")
	root.PersistentFlags().Bool("ui", false, "Enable the built-in dashboard UI")
	root.PersistentFlags().String("ui-dir", "", "Path to built dashboard assets (default: ./ui/build)")
	root.PersistentFlags().Bool("persist", true, "Persist non-Lambda service configuration across Tarn sessions")

	return root
}

func newVersionCmd() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the Tarn version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("tarn %s\n", version)
			if !check {
				return nil
			}
			return runVersionUpdateCheck(cmd, os.Stdout, version)
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "Check whether a newer Tarn version is available")
	return cmd
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

func runStart(cmd *cobra.Command, args []string) error {
	// Import here to avoid circular deps at package level
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
	fmt.Fprintf(os.Stderr, "Region:   %s\n", cfg.Region)
	fmt.Fprintf(os.Stderr, "Endpoint: %s\n", cfg.Endpoint())
	fmt.Fprintf(os.Stderr, "Data Dir: %s\n", cfg.DataDir)
	fmt.Fprintln(os.Stderr, "Services: apigateway, apigatewayv2, lambda, s3, sqs, sns, secretsmanager, eventbridge")
	if cfg.UIEnabled {
		fmt.Fprintf(os.Stderr, "Dashboard: http://%s:%d/\n", displayHost(cfg.Host), cfg.Port)
		fmt.Fprintf(os.Stderr, "UI Dir:    %s\n", cfg.UIDir)
	}
	if cfg.VaultKeyPath != "" {
		fmt.Fprintf(os.Stderr, "Vault:     %s\n", cfg.VaultKeyPath)
	}
	if cfg.ExposeSecretsProxy {
		fmt.Fprintf(os.Stderr, "Secrets Proxy: http://%s:%d\n", displayHost(cfg.SecretsProxyHost), cfg.SecretsProxyPort)
	}
	fmt.Fprintln(os.Stderr, "")

	return startServer(cfg)
}
