package stepfunctions

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newExecutionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execution <executionArn>",
		Short: "Describe an execution",
		Example: `  tarn stepfunctions execution arn:aws:states:us-east-1:000000000000:execution:my-machine:my-exec
  tarn sfn execution arn:aws:states:us-east-1:000000000000:execution:my-machine:my-exec`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			result, err := stepFunctionsRequest(endpoint, "DescribeExecution", map[string]interface{}{
				"executionArn": args[0],
			})
			if err != nil {
				return err
			}

			fmt.Printf("ExecutionArn:    %s\n", getString(result, "executionArn"))
			fmt.Printf("Name:            %s\n", getString(result, "name"))
			fmt.Printf("Status:          %s\n", getString(result, "status"))
			fmt.Printf("StateMachineArn: %s\n", getString(result, "stateMachineArn"))
			fmt.Printf("StartDate:       %s\n", formatEpoch(result["startDate"]))
			fmt.Printf("StopDate:        %s\n", formatEpoch(result["stopDate"]))
			fmt.Printf("Input:           %s\n", getString(result, "input"))
			fmt.Printf("Output:          %s\n", getString(result, "output"))

			return nil
		},
	}

	return cmd
}
