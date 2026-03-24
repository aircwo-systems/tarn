package secrets

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var (
		name  string
		value string
	)

	cmd := &cobra.Command{
		Use:     "update",
		Short:   "Update a secret's value",
		Example: `  openstack secrets update --name my-secret --value "new-password"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			result, err := secretsRequest(endpoint, "PutSecretValue", map[string]string{
				"SecretId":     name,
				"SecretString": value,
			})
			if err != nil {
				return err
			}

			fmt.Printf("Secret updated:\n")
			fmt.Printf("  Name: %s\n", result["Name"])
			fmt.Printf("  ARN:  %s\n", result["ARN"])
			if vid, ok := result["VersionId"]; ok {
				fmt.Printf("  VersionId: %s\n", vid)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Secret name or ARN (required)")
	cmd.Flags().StringVar(&value, "value", "", "New secret value (required)")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("value")

	return cmd
}
