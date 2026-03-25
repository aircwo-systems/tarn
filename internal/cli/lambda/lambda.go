package lambda

import (
	"github.com/spf13/cobra"
)

// NewLambdaCmd creates the `tarn lambda` command group.
func NewLambdaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lambda",
		Short: "Manage Lambda functions",
		Long:  "Create, invoke, list, and delete Lambda functions on your local Tarn instance.",
	}

	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newInvokeCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newDeleteCmd())

	return cmd
}
