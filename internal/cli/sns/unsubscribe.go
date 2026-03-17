package sns

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newUnsubscribeCmd() *cobra.Command {
	var subscriptionArn string

	cmd := &cobra.Command{
		Use:   "unsubscribe",
		Short: "Delete an SNS subscription",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)
			form := url.Values{
				"Action":          {"Unsubscribe"},
				"SubscriptionArn": {subscriptionArn},
			}

			resp, err := http.PostForm(endpoint, form)
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			fmt.Printf("Subscription deleted: %s\n", subscriptionArn)
			return nil
		},
	}

	cmd.Flags().StringVar(&subscriptionArn, "subscription-arn", "", "Subscription ARN (required)")
	_ = cmd.MarkFlagRequired("subscription-arn")
	return cmd
}
