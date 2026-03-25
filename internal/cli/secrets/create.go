package secrets

import (
	"fmt"

	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var (
		name        string
		value       string
		description string
		tags        string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new secret",
		Example: `  tarn secrets create --name my-secret --value "password123"
  tarn secrets create --name db-creds --value '{"user":"admin","pass":"secret"}' --description "Database credentials"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			body := map[string]interface{}{
				"Name":         name,
				"SecretString": value,
			}
			if description != "" {
				body["Description"] = description
			}
			if tags != "" {
				tagMap, err := common.ParseTagMap(tags)
				if err != nil {
					return err
				}
				if len(tagMap) > 0 {
					body["Tags"] = common.ToSecretTags(tagMap)
				}
			}

			result, err := secretsRequest(endpoint, "CreateSecret", body)
			if err != nil {
				return err
			}

			fmt.Printf("Secret created:\n")
			fmt.Printf("  Name: %s\n", result["Name"])
			fmt.Printf("  ARN:  %s\n", result["ARN"])
			if vid, ok := result["VersionId"]; ok {
				fmt.Printf("  VersionId: %s\n", vid)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Secret name (required)")
	cmd.Flags().StringVar(&value, "value", "", "Secret value (required)")
	cmd.Flags().StringVar(&description, "description", "", "Secret description")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags in KEY=VALUE form")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("value")

	return cmd
}
