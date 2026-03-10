package cli

import (
	"fmt"
	"os"

	"github.com/openstack-project/openstack/internal/cli/lambda"
	s3cli "github.com/openstack-project/openstack/internal/cli/s3"
	"github.com/openstack-project/openstack/internal/cli/secrets"
	"github.com/openstack-project/openstack/internal/cli/sqs"
	"github.com/spf13/cobra"
)

var version = "0.1.0-dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "openstack",
		Short: "OpenStack — open-source AWS emulator",
		Long: `OpenStack is a fully open-source AWS cloud emulator for local development and testing.
It provides high-fidelity emulation of AWS services including API Gateway, Lambda, and SQS.

Start the server:
  openstack start

Manage Lambda functions:
  openstack lambda create --name my-func --runtime nodejs20.x --handler index.handler --zip ./code.zip
  openstack lambda invoke --name my-func --payload '{"key": "value"}'
  openstack lambda list
  openstack lambda delete --name my-func

Manage SQS queues:
  openstack sqs create-queue --name my-queue
  openstack sqs send --queue my-queue --body "hello world"
  openstack sqs receive --queue my-queue
  openstack sqs list
  openstack sqs delete-queue --name my-queue

Manage S3 buckets:
  openstack s3 mb --name my-bucket
  openstack s3 ls
  openstack s3 cp --bucket my-bucket --key file.txt --file ./file.txt
  openstack s3 get --bucket my-bucket --key file.txt
  openstack s3 rm --bucket my-bucket --key file.txt
  openstack s3 rb --name my-bucket

Manage Secrets Manager:
  openstack secrets create --name my-secret --value "password123"
  openstack secrets get --name my-secret
  openstack secrets list
  openstack secrets update --name my-secret --value "new-value"
  openstack secrets delete --name my-secret

Flush provisioned resources:
  openstack flush
  openstack flush --tag feature=r10
  openstack flush --tag r10 --dry-run
	openstack flush --storage`,
	}

	root.AddCommand(newStartCmd())
	root.AddCommand(newFlushCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(lambda.NewLambdaCmd())
	root.AddCommand(s3cli.NewS3Cmd())
	root.AddCommand(sqs.NewSQSCmd())
	root.AddCommand(secrets.NewSecretsCmd())

	root.PersistentFlags().String("host", "0.0.0.0", "API server bind address")
	root.PersistentFlags().Int("port", 4566, "API server port")
	root.PersistentFlags().String("data-dir", "", "Data directory (default: ~/.openstack/data)")
	root.PersistentFlags().String("region", "us-east-1", "Emulated AWS region")
	root.PersistentFlags().Bool("ui", false, "Enable the built-in dashboard UI")
	root.PersistentFlags().String("ui-dir", "", "Path to built dashboard assets (default: ./ui/build)")
	root.PersistentFlags().Bool("persist", true, "Persist non-Lambda service configuration across OpenStack sessions")

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the OpenStack version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("openstack %s\n", version)
		},
	}
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the OpenStack API server",
		RunE:  runStart,
	}
}

func runStart(cmd *cobra.Command, args []string) error {
	// Import here to avoid circular deps at package level
	cfg, err := buildConfig(cmd)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, `
   ____                   _____ __             __
  / __ \____  ___  ____  / ___// /_____ ______/ /__
 / / / / __ \/ _ \/ __ \ \__ \/ __/ __ '/ ___/ //_/
/ /_/ / /_/ /  __/ / / /___/ / /_/ /_/ / /__/ ,<
\____/ .___/\___/_/ /_//____/\__/\__,_/\___/_/|_|
    /_/

`)
	fmt.Fprintf(os.Stderr, "Region:   %s\n", cfg.Region)
	fmt.Fprintf(os.Stderr, "Endpoint: %s\n", cfg.Endpoint())
	fmt.Fprintf(os.Stderr, "Data Dir: %s\n", cfg.DataDir)
	fmt.Fprintln(os.Stderr, "Services: apigateway, apigatewayv2, lambda, s3, sqs, secretsmanager")
	if cfg.UIEnabled {
		fmt.Fprintf(os.Stderr, "Dashboard: http://%s:%d/\n", displayHost(cfg.Host), cfg.Port)
		fmt.Fprintf(os.Stderr, "UI Dir:    %s\n", cfg.UIDir)
	}
	fmt.Fprintln(os.Stderr, "")

	return startServer(cfg)
}
