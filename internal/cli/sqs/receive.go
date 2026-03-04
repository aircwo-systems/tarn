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

func newReceiveCmd() *cobra.Command {
	var (
		queue string
		max   int
		wait  int
	)

	cmd := &cobra.Command{
		Use:   "receive",
		Short: "Receive messages from an SQS queue",
		Example: `  openstack sqs receive --queue my-queue
  openstack sqs receive --queue my-queue --max 5 --wait 10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			form := url.Values{
				"Action":              {"ReceiveMessage"},
				"QueueUrl":            {queueURL(endpoint, queue)},
				"MaxNumberOfMessages": {strconv.Itoa(max)},
			}
			if wait > 0 {
				form.Set("WaitTimeSeconds", strconv.Itoa(wait))
			}

			resp, err := http.PostForm(endpoint+"/"+getAccountID()+"/"+queue, form)
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			var result struct {
				XMLName  xml.Name `xml:"ReceiveMessageResponse"`
				Messages []struct {
					MessageId     string `xml:"MessageId"`
					ReceiptHandle string `xml:"ReceiptHandle"`
					Body          string `xml:"Body"`
					MD5           string `xml:"MD5OfBody"`
				} `xml:"ReceiveMessageResult>Message"`
			}
			if err := xml.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if len(result.Messages) == 0 {
				fmt.Println("No messages available.")
				return nil
			}

			for i, msg := range result.Messages {
				if i > 0 {
					fmt.Println("---")
				}
				fmt.Printf("MessageId:     %s\n", msg.MessageId)
				fmt.Printf("ReceiptHandle: %s\n", msg.ReceiptHandle)
				fmt.Printf("MD5:           %s\n", msg.MD5)
				fmt.Printf("Body:          %s\n", msg.Body)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&queue, "queue", "", "Queue name (required)")
	cmd.Flags().IntVar(&max, "max", 1, "Maximum number of messages to receive (1-10)")
	cmd.Flags().IntVar(&wait, "wait", 0, "Long poll wait time in seconds (0-20)")
	_ = cmd.MarkFlagRequired("queue")

	return cmd
}
