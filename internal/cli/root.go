package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/openstack-project/openstack/internal/cli/lambda"
)

var version = "0.1.0-dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "openstack",
		Short: "OpenStack — open-source AWS emulator",
		Long: `OpenStack is a fully open-source AWS cloud emulator for local development and testing.
It provides high-fidelity emulation of AWS services starting with Lambda.

Start the server:
  openstack start

Manage Lambda functions:
  openstack lambda create --name my-func --runtime nodejs20.x --handler index.handler --zip ./code.zip
  openstack lambda invoke --name my-func --payload '{"key": "value"}'
  openstack lambda list
  openstack lambda delete --name my-func`,
	}

	root.AddCommand(newStartCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(lambda.NewLambdaCmd())

	root.PersistentFlags().String("host", "0.0.0.0", "API server bind address")
	root.PersistentFlags().Int("port", 4566, "API server port")
	root.PersistentFlags().String("data-dir", "", "Data directory (default: ~/.openstack/data)")
	root.PersistentFlags().String("region", "us-east-1", "Emulated AWS region")

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
	fmt.Fprintln(os.Stderr, "Services: lambda")
	fmt.Fprintln(os.Stderr, "")

	return startServer(cfg)
}
