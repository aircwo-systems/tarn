package sqs

import (
	"github.com/spf13/cobra"
)

// NewSQSCmd creates the `tarn sqs` command group.
func NewSQSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sqs",
		Short: "Manage SQS queues",
		Long:  "Create, send, receive, list, and delete SQS queues on your local Tarn instance.",
	}

	cmd.AddCommand(newCreateQueueCmd())
	cmd.AddCommand(newSendCmd())
	cmd.AddCommand(newReceiveCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newDeleteQueueCmd())
	cmd.AddCommand(newSetDLQCmd())

	return cmd
}
