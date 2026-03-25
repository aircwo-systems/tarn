package sns

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newSubscribeCmd() *cobra.Command {
	var (
		topicArn          string
		protocol          string
		endpointValue     string
		rawMessage        bool
		filterPolicy      string
		filterPolicyScope string
	)

	cmd := &cobra.Command{
		Use:   "subscribe",
		Short: "Subscribe an endpoint to an SNS topic",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)
			form := url.Values{
				"Action":   {"Subscribe"},
				"TopicArn": {topicArn},
				"Protocol": {protocol},
				"Endpoint": {endpointValue},
			}
			attrIndex := 0
			if rawMessage {
				attrIndex++
				form.Set(fmt.Sprintf("Attributes.entry.%d.key", attrIndex), "RawMessageDelivery")
				form.Set(fmt.Sprintf("Attributes.entry.%d.value", attrIndex), "true")
			}
			if filterPolicy != "" {
				attrIndex++
				form.Set(fmt.Sprintf("Attributes.entry.%d.key", attrIndex), "FilterPolicy")
				form.Set(fmt.Sprintf("Attributes.entry.%d.value", attrIndex), filterPolicy)
			}
			if filterPolicyScope != "" {
				attrIndex++
				form.Set(fmt.Sprintf("Attributes.entry.%d.key", attrIndex), "FilterPolicyScope")
				form.Set(fmt.Sprintf("Attributes.entry.%d.value", attrIndex), filterPolicyScope)
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
				SubscriptionArn string `xml:"SubscribeResult>SubscriptionArn"`
			}
			if err := xml.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			fmt.Printf("Subscription created: %s\n", result.SubscriptionArn)
			return nil
		},
	}

	cmd.Flags().StringVar(&topicArn, "topic-arn", "", "Topic ARN (required)")
	cmd.Flags().StringVar(&protocol, "protocol", "", "Subscription protocol (required)")
	cmd.Flags().StringVar(&endpointValue, "endpoint", "", "Subscription endpoint ARN/name/url (required)")
	cmd.Flags().BoolVar(&rawMessage, "raw-message", false, "Set RawMessageDelivery=true")
	cmd.Flags().StringVar(&filterPolicy, "filter-policy", "", "Filter policy JSON")
	cmd.Flags().StringVar(&filterPolicyScope, "filter-policy-scope", "", "Filter scope: MessageAttributes or MessageBody")
	_ = cmd.MarkFlagRequired("topic-arn")
	_ = cmd.MarkFlagRequired("protocol")
	_ = cmd.MarkFlagRequired("endpoint")
	return cmd
}
