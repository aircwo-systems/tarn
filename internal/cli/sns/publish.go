package sns

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

func newPublishCmd() *cobra.Command {
	var (
		topicArn string
		message  string
		subject  string
	)

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a message to an SNS topic",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			form := url.Values{
				"Action":   {"Publish"},
				"TopicArn": {topicArn},
				"Message":  {message},
			}
			if subject != "" {
				form.Set("Subject", subject)
			}

			resp, err := common.PostForm(endpoint, form)
			if err != nil {
				return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			var result struct {
				MessageID string `xml:"PublishResult>MessageId"`
			}
			if err := xml.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("Message published: %s\n", result.MessageID)
			return nil
		},
	}

	cmd.Flags().StringVar(&topicArn, "topic-arn", "", "Topic ARN (required)")
	cmd.Flags().StringVar(&message, "message", "", "Message body (required)")
	cmd.Flags().StringVar(&subject, "subject", "", "Optional message subject")
	_ = cmd.MarkFlagRequired("topic-arn")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}
