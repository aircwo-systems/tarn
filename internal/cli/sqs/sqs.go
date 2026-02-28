package sqs

import (
	"github.com/spf13/cobra"
)

// NewSQSCmd creates the `openstack sqs` command group.
func NewSQSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sqs",
		Short: "Manage SQS queues",
		Long:  "Create, send, receive, list, and delete SQS queues on your local OpenStack instance.",
	}

	cmd.AddCommand(newCreateQueueCmd())
	cmd.AddCommand(newSendCmd())
	cmd.AddCommand(newReceiveCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newDeleteQueueCmd())

	return cmd
}
