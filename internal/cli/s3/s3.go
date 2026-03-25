package s3

import (
	"github.com/spf13/cobra"
)

// NewS3Cmd creates the `tarn s3` command group.
func NewS3Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "s3",
		Short: "Manage S3 buckets and objects",
		Long:  "Create, list, upload, download, and delete S3 buckets and objects on your local Tarn instance.",
	}

	cmd.AddCommand(newMBCmd())
	cmd.AddCommand(newRBCmd())
	cmd.AddCommand(newLsCmd())
	cmd.AddCommand(newCpCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newRmCmd())

	return cmd
}
