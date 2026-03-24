package secrets

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:     "delete",
		Short:   "Delete a secret",
		Example: `  openstack secrets delete --name my-secret`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			result, err := secretsRequest(endpoint, "DeleteSecret", map[string]interface{}{
				"SecretId":                   name,
				"ForceDeleteWithoutRecovery": true,
			})
			if err != nil {
				return err
			}

			fmt.Printf("Secret deleted:\n")
			fmt.Printf("  Name: %s\n", result["Name"])
			fmt.Printf("  ARN:  %s\n", result["ARN"])
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Secret name or ARN (required)")
	cmd.MarkFlagRequired("name")

	return cmd
}
