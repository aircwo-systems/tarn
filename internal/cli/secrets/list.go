package secrets

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List all secrets",
		Example: `  openstack secrets list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			result, err := secretsRequest(endpoint, "ListSecrets", map[string]string{})
			if err != nil {
				return err
			}

			secretList, ok := result["SecretList"].([]interface{})
			if !ok || len(secretList) == 0 {
				fmt.Println("No secrets found.")
				return nil
			}

			fmt.Printf("%-30s %-60s %s\n", "NAME", "ARN", "DESCRIPTION")
			fmt.Printf("%-30s %-60s %s\n", "----", "---", "-----------")

			for _, item := range secretList {
				s, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				name := getString(s, "Name")
				arn := getString(s, "ARN")
				desc := getString(s, "Description")

				if len(desc) > 40 {
					desc = desc[:37] + "..."
				}

				fmt.Printf("%-30s %-60s %s\n", name, arn, desc)
			}

			fmt.Printf("\nTotal: %d secret(s)\n", len(secretList))
			return nil
		},
	}

	return cmd
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}
