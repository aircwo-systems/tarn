package sns

import "github.com/spf13/cobra"

// NewSNSCmd creates the `openstack sns` command group.
func NewSNSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sns",
		Short: "Manage SNS topics and subscriptions",
		Long:  "Create, publish, subscribe, list, and delete SNS topics on your local OpenStack instance.",
	}

	cmd.AddCommand(newCreateTopicCmd())
	cmd.AddCommand(newDeleteTopicCmd())
	cmd.AddCommand(newListTopicsCmd())
	cmd.AddCommand(newPublishCmd())
	cmd.AddCommand(newSubscribeCmd())
	cmd.AddCommand(newUnsubscribeCmd())
	cmd.AddCommand(newListSubscriptionsCmd())

	return cmd
}
