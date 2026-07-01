package stepfunctions

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <stateMachineArn>",
		Short: "Delete a state machine",
		Example: `  tarn stepfunctions delete arn:aws:states:us-east-1:000000000000:stateMachine:my-machine
  tarn sfn delete arn:aws:states:us-east-1:000000000000:stateMachine:my-machine`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			_, err := stepFunctionsRequest(endpoint, "DeleteStateMachine", map[string]interface{}{
				"stateMachineArn": args[0],
			})
			if err != nil {
				return err
			}

			fmt.Printf("State machine deleted: %s\n", args[0])
			return nil
		},
	}

	return cmd
}
