package stepfunctions

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history <executionArn>",
		Short: "Get the event history of an execution",
		Example: `  tarn stepfunctions history arn:aws:states:us-east-1:000000000000:execution:my-machine:my-exec
  tarn sfn history arn:aws:states:us-east-1:000000000000:execution:my-machine:my-exec`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			result, err := stepFunctionsRequest(endpoint, "GetExecutionHistory", map[string]interface{}{
				"executionArn": args[0],
			})
			if err != nil {
				return err
			}

			events, ok := result["events"].([]interface{})
			if !ok || len(events) == 0 {
				fmt.Println("No events found.")
				return nil
			}

			fmt.Printf("%-6s %-40s %s\n", "ID", "TYPE", "TIMESTAMP")
			fmt.Printf("%-6s %-40s %s\n", "--", "----", "---------")

			for _, item := range events {
				e, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				fmt.Printf("%-6s %-40s %s\n",
					getString(e, "id"),
					getString(e, "type"),
					formatEpoch(e["timestamp"]),
				)
			}

			fmt.Printf("\nTotal: %d event(s)\n", len(events))
			return nil
		},
	}

	return cmd
}
