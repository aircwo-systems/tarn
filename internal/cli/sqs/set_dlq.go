package sqs

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newSetDLQCmd() *cobra.Command {
	var (
		queue           string
		dlq             string
		maxReceiveCount int
		remove          bool
	)

	cmd := &cobra.Command{
		Use:   "set-dlq",
		Short: "Configure a dead-letter queue (redrive policy) for an SQS queue",
		Example: `  # Attach a DLQ — move messages after 3 failed receives
  openstack sqs set-dlq --queue my-queue --dlq my-queue-dlq --max-receive-count 3

  # Remove the DLQ configuration from a queue
  openstack sqs set-dlq --queue my-queue --remove`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remove && dlq == "" {
				return fmt.Errorf("either --dlq or --remove is required")
			}

			endpoint := getEndpoint(cmd)
			queueURL := queueURL(endpoint, queue)

			form := url.Values{
				"Action":   {"SetQueueAttributes"},
				"QueueUrl": {queueURL},
			}

			if remove {
				form.Set("Attribute.1.Name", "RedrivePolicy")
				form.Set("Attribute.1.Value", "")
			} else {
				if maxReceiveCount <= 0 {
					maxReceiveCount = 3
				}
				region := "us-east-1"
				accountID := getAccountID()
				dlqArn := fmt.Sprintf("arn:aws:sqs:%s:%s:%s", region, accountID, dlq)
				redrivePolicy := fmt.Sprintf(`{"deadLetterTargetArn":"%s","maxReceiveCount":%d}`, dlqArn, maxReceiveCount)
				form.Set("Attribute.1.Name", "RedrivePolicy")
				form.Set("Attribute.1.Value", redrivePolicy)
			}

			resp, err := http.PostForm(queueURL, form)
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			var result struct {
				XMLName xml.Name `xml:"SetQueueAttributesResponse"`
			}
			if err := xml.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if remove {
				fmt.Printf("DLQ configuration removed from queue: %s\n", queue)
			} else {
				fmt.Printf("DLQ configured: %s -> %s (maxReceiveCount=%d)\n", queue, dlq, maxReceiveCount)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&queue, "queue", "", "Source queue name (required)")
	cmd.Flags().StringVar(&dlq, "dlq", "", "Dead-letter queue name")
	cmd.Flags().IntVar(&maxReceiveCount, "max-receive-count", 3, "Number of receives before routing to DLQ")
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove the DLQ configuration from the queue")
	_ = cmd.MarkFlagRequired("queue")

	return cmd
}
