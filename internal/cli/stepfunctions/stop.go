package stepfunctions

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <executionArn>",
		Short: "Stop a running execution",
		Example: `  tarn stepfunctions stop arn:aws:states:us-east-1:000000000000:execution:my-machine:my-exec
  tarn sfn stop arn:aws:states:us-east-1:000000000000:execution:my-machine:my-exec`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			_, err := stepFunctionsRequest(endpoint, "StopExecution", map[string]interface{}{
				"executionArn": args[0],
			})
			if err != nil {
				return err
			}

			fmt.Printf("Execution stopped: %s\n", args[0])
			return nil
		},
	}

	return cmd
}
