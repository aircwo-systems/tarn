package sns

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newListSubscriptionsCmd() *cobra.Command {
	var topicArn string

	cmd := &cobra.Command{
		Use:   "list-subscriptions",
		Short: "List SNS subscriptions",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)
			form := url.Values{}
			if topicArn == "" {
				form.Set("Action", "ListSubscriptions")
			} else {
				form.Set("Action", "ListSubscriptionsByTopic")
				form.Set("TopicArn", topicArn)
			}

			resp, err := http.PostForm(endpoint, form)
			if err != nil {
				return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			var result struct {
				Subscriptions []struct {
					SubscriptionArn string `xml:"SubscriptionArn"`
					TopicArn        string `xml:"TopicArn"`
					Protocol        string `xml:"Protocol"`
					Endpoint        string `xml:"Endpoint"`
				} `xml:"ListSubscriptionsResult>Subscriptions>member"`
			}
			if topicArn != "" {
				var scoped struct {
					Subscriptions []struct {
						SubscriptionArn string `xml:"SubscriptionArn"`
						TopicArn        string `xml:"TopicArn"`
						Protocol        string `xml:"Protocol"`
						Endpoint        string `xml:"Endpoint"`
					} `xml:"ListSubscriptionsByTopicResult>Subscriptions>member"`
				}
				if err := xml.Unmarshal(body, &scoped); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}
				result.Subscriptions = scoped.Subscriptions
			} else {
				if err := xml.Unmarshal(body, &result); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}
			}

			if len(result.Subscriptions) == 0 {
				fmt.Println("No subscriptions found.")
				return nil
			}

			for _, sub := range result.Subscriptions {
				fmt.Printf("%s\t%s\t%s\n", sub.SubscriptionArn, sub.Protocol, sub.Endpoint)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&topicArn, "topic-arn", "", "Optional topic ARN filter")
	return cmd
}
