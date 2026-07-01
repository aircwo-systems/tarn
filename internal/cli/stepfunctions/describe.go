package stepfunctions

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newDescribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe <stateMachineArn>",
		Short: "Describe a state machine",
		Example: `  tarn stepfunctions describe arn:aws:states:us-east-1:000000000000:stateMachine:my-machine
  tarn sfn describe arn:aws:states:us-east-1:000000000000:stateMachine:my-machine`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			result, err := stepFunctionsRequest(endpoint, "DescribeStateMachine", map[string]interface{}{
				"stateMachineArn": args[0],
			})
			if err != nil {
				return err
			}

			fmt.Printf("Name:            %s\n", getString(result, "name"))
			fmt.Printf("ARN:             %s\n", getString(result, "stateMachineArn"))
			fmt.Printf("Status:          %s\n", getString(result, "status"))
			fmt.Printf("Type:            %s\n", getString(result, "type"))
			fmt.Printf("RoleArn:         %s\n", getString(result, "roleArn"))
			fmt.Printf("CreationDate:    %s\n", formatEpoch(result["creationDate"]))

			if defRaw := getString(result, "definition"); defRaw != "" {
				var pretty interface{}
				if err := json.Unmarshal([]byte(defRaw), &pretty); err == nil {
					prettyBytes, _ := json.MarshalIndent(pretty, "", "  ")
					fmt.Printf("Definition:\n%s\n", string(prettyBytes))
				} else {
					fmt.Printf("Definition:\n%s\n", defRaw)
				}
			}

			return nil
		},
	}

	return cmd
}
