package secrets

import (
	"github.com/spf13/cobra"
)

// NewSecretsCmd creates the `openstack secrets` command group.
func NewSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage Secrets Manager secrets",
		Long:  "Create, get, update, list, and delete secrets on your local OpenStack instance.",
	}

	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newDeleteCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newProxyCmd())

	return cmd
}
