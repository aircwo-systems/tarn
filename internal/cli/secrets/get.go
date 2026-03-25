package secrets

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a secret's value",
		Example: `  tarn secrets get --name my-secret
  tarn secrets get --name arn:aws:secretsmanager:us-east-1:000000000000:secret:my-secret-abc123`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			result, err := secretsRequest(endpoint, "GetSecretValue", map[string]string{
				"SecretId": name,
			})
			if err != nil {
				return err
			}

			fmt.Printf("Name:         %s\n", result["Name"])
			fmt.Printf("ARN:          %s\n", result["ARN"])
			if v, ok := result["SecretString"]; ok && v != nil {
				fmt.Printf("SecretString: %s\n", v)
			}
			if v, ok := result["SecretBinary"]; ok && v != nil {
				fmt.Printf("SecretBinary: %s\n", v)
			}
			fmt.Printf("VersionId:    %s\n", result["VersionId"])
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Secret name or ARN (required)")
	cmd.MarkFlagRequired("name")

	return cmd
}
