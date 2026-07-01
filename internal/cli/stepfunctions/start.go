package stepfunctions

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	var (
		stateMachineArn string
		name            string
		input           string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a state machine execution",
		Example: `  tarn stepfunctions start --state-machine-arn arn:aws:states:us-east-1:000000000000:stateMachine:my-machine
  tarn sfn start --state-machine-arn arn:aws:states:us-east-1:000000000000:stateMachine:my-machine --name my-exec --input '{"key":"value"}'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			if input == "" {
				input = "{}"
			}

			body := map[string]interface{}{
				"stateMachineArn": stateMachineArn,
				"input":           input,
			}
			if name != "" {
				body["name"] = name
			}

			result, err := stepFunctionsRequest(endpoint, "StartExecution", body)
			if err != nil {
				return err
			}

			fmt.Printf("Execution started:\n")
			fmt.Printf("  ExecutionArn: %s\n", getString(result, "executionArn"))
			fmt.Printf("  StartDate:    %s\n", formatEpoch(result["startDate"]))
			return nil
		},
	}

	cmd.Flags().StringVar(&stateMachineArn, "state-machine-arn", "", "State machine ARN (required)")
	cmd.Flags().StringVar(&name, "name", "", "Execution name (optional)")
	cmd.Flags().StringVar(&input, "input", "", "Execution input as JSON (default: {})")
	cmd.MarkFlagRequired("state-machine-arn")

	return cmd
}
