package stepfunctions

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List all state machines",
		Example: `  tarn stepfunctions list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			result, err := stepFunctionsRequest(endpoint, "ListStateMachines", map[string]string{})
			if err != nil {
				return err
			}

			machines, ok := result["stateMachines"].([]interface{})
			if !ok || len(machines) == 0 {
				fmt.Println("No state machines found.")
				return nil
			}

			fmt.Printf("%-30s %-10s %-60s %s\n", "NAME", "TYPE", "ARN", "CREATION DATE")
			fmt.Printf("%-30s %-10s %-60s %s\n", "----", "----", "---", "-------------")

			for _, item := range machines {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				fmt.Printf("%-30s %-10s %-60s %s\n",
					getString(m, "name"),
					getString(m, "type"),
					getString(m, "stateMachineArn"),
					formatEpoch(m["creationDate"]),
				)
			}

			fmt.Printf("\nTotal: %d state machine(s)\n", len(machines))
			return nil
		},
	}

	return cmd
}
