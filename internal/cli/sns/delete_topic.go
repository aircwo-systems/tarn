package sns

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newDeleteTopicCmd() *cobra.Command {
	var topicArn string

	cmd := &cobra.Command{
		Use:   "delete-topic",
		Short: "Delete an SNS topic",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			form := url.Values{
				"Action":   {"DeleteTopic"},
				"TopicArn": {topicArn},
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

			fmt.Printf("Topic deleted: %s\n", topicArn)
			return nil
		},
	}

	cmd.Flags().StringVar(&topicArn, "topic-arn", "", "Topic ARN (required)")
	_ = cmd.MarkFlagRequired("topic-arn")
	return cmd
}
