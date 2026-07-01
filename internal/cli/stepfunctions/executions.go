package stepfunctions

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newExecutionsCmd() *cobra.Command {
	var (
		stateMachineArn string
		statusFilter    string
	)

	cmd := &cobra.Command{
		Use:   "executions",
		Short: "List executions for a state machine",
		Example: `  tarn stepfunctions executions --state-machine-arn arn:aws:states:us-east-1:000000000000:stateMachine:my-machine
  tarn sfn executions --state-machine-arn arn:aws:states:us-east-1:000000000000:stateMachine:my-machine --status RUNNING`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			body := map[string]interface{}{}
			if stateMachineArn != "" {
				body["stateMachineArn"] = stateMachineArn
			}
			if statusFilter != "" {
				body["statusFilter"] = statusFilter
			}

			result, err := stepFunctionsRequest(endpoint, "ListExecutions", body)
			if err != nil {
				return err
			}

			executions, ok := result["executions"].([]interface{})
			if !ok || len(executions) == 0 {
				fmt.Println("No executions found.")
				return nil
			}

			fmt.Printf("%-30s %-10s %-60s %s\n", "NAME", "STATUS", "EXECUTION ARN", "START DATE")
			fmt.Printf("%-30s %-10s %-60s %s\n", "----", "------", "-------------", "----------")

			for _, item := range executions {
				e, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				fmt.Printf("%-30s %-10s %-60s %s\n",
					getString(e, "name"),
					getString(e, "status"),
					getString(e, "executionArn"),
					formatEpoch(e["startDate"]),
				)
			}

			fmt.Printf("\nTotal: %d execution(s)\n", len(executions))
			return nil
		},
	}

	cmd.Flags().StringVar(&stateMachineArn, "state-machine-arn", "", "Filter by state machine ARN")
	cmd.Flags().StringVar(&statusFilter, "status", "", "Filter by status (RUNNING, SUCCEEDED, FAILED, TIMED_OUT, ABORTED)")

	return cmd
}
