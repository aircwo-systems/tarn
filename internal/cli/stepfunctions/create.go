package stepfunctions

import (
	"fmt"
	"os"

	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var (
		name           string
		definition     string
		definitionFile string
		roleArn        string
		smType         string
		tags           string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new state machine",
		Example: `  tarn stepfunctions create --name my-machine --definition '{"Comment":"hello","StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}'
  tarn sfn create --name my-machine --definition-file ./asl.json --role-arn arn:aws:iam::000000000000:role/StepRole --type STANDARD`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if definition == "" && definitionFile == "" {
				return fmt.Errorf("one of --definition or --definition-file is required")
			}
			if definition != "" && definitionFile != "" {
				return fmt.Errorf("--definition and --definition-file are mutually exclusive")
			}

			if definitionFile != "" {
				data, err := os.ReadFile(definitionFile)
				if err != nil {
					return fmt.Errorf("reading --definition-file: %w", err)
				}
				definition = string(data)
			}

			endpoint := getEndpoint(cmd)

			body := map[string]interface{}{
				"name":       name,
				"definition": definition,
				"type":       smType,
			}
			if roleArn != "" {
				body["roleArn"] = roleArn
			}
			if tags != "" {
				tagMap, err := common.ParseTagMap(tags)
				if err != nil {
					return err
				}
				if len(tagMap) > 0 {
					body["tags"] = tagMap
				}
			}

			result, err := stepFunctionsRequest(endpoint, "CreateStateMachine", body)
			if err != nil {
				return err
			}

			fmt.Printf("State machine created:\n")
			fmt.Printf("  ARN:          %s\n", getString(result, "stateMachineArn"))
			fmt.Printf("  CreationDate: %s\n", formatEpoch(result["creationDate"]))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "State machine name (required)")
	cmd.Flags().StringVar(&definition, "definition", "", "ASL definition JSON (inline)")
	cmd.Flags().StringVar(&definitionFile, "definition-file", "", "Path to ASL definition JSON file")
	cmd.Flags().StringVar(&roleArn, "role-arn", "", "IAM role ARN for the state machine")
	cmd.Flags().StringVar(&smType, "type", "STANDARD", "State machine type (STANDARD or EXPRESS)")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags in KEY=VALUE form")
	cmd.MarkFlagRequired("name")

	return cmd
}
