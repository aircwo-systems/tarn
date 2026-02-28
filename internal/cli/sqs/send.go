package sqs

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

func newSendCmd() *cobra.Command {
	var (
		queue    string
		body     string
		delay    int
		groupID  string
		dedupID  string
	)

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message to an SQS queue",
		Example: `  openstack sqs send --queue my-queue --body "hello world"
  openstack sqs send --queue my-queue.fifo --body "hello" --group-id g1 --dedup-id d1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			form := url.Values{
				"Action":      {"SendMessage"},
				"QueueUrl":    {queueURL(endpoint, queue)},
				"MessageBody": {body},
			}
			if delay > 0 {
				form.Set("DelaySeconds", strconv.Itoa(delay))
			}
			if groupID != "" {
				form.Set("MessageGroupId", groupID)
			}
			if dedupID != "" {
				form.Set("MessageDeduplicationId", dedupID)
			}

			resp, err := http.PostForm(endpoint+"/"+getAccountID()+"/"+queue, form)
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(respBody))
			}

			var result struct {
				XMLName   xml.Name `xml:"SendMessageResponse"`
				MessageId string   `xml:"SendMessageResult>MessageId"`
				MD5       string   `xml:"SendMessageResult>MD5OfMessageBody"`
			}
			if err := xml.Unmarshal(respBody, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("MessageId: %s\n", result.MessageId)
			fmt.Printf("MD5:       %s\n", result.MD5)
			return nil
		},
	}

	cmd.Flags().StringVar(&queue, "queue", "", "Queue name (required)")
	cmd.Flags().StringVar(&body, "body", "", "Message body (required)")
	cmd.Flags().IntVar(&delay, "delay", 0, "Message delay in seconds")
	cmd.Flags().StringVar(&groupID, "group-id", "", "Message group ID (FIFO only)")
	cmd.Flags().StringVar(&dedupID, "dedup-id", "", "Message deduplication ID (FIFO only)")
	cmd.MarkFlagRequired("queue")
	cmd.MarkFlagRequired("body")

	return cmd
}
