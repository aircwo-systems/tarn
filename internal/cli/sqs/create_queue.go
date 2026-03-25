package sqs

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"

	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

func newCreateQueueCmd() *cobra.Command {
	var (
		name            string
		fifo            bool
		tags            string
		dlq             string
		maxReceiveCount int
	)

	cmd := &cobra.Command{
		Use:   "create-queue",
		Short: "Create a new SQS queue",
		Example: `  tarn sqs create-queue --name my-queue
  tarn sqs create-queue --name my-queue.fifo --fifo
  tarn sqs create-queue --name my-queue --dlq my-queue-dlq --max-receive-count 3`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			form := url.Values{
				"Action":    {"CreateQueue"},
				"QueueName": {name},
			}

			attrIdx := 1
			if fifo {
				form.Set(fmt.Sprintf("Attribute.%d.Name", attrIdx), "FifoQueue")
				form.Set(fmt.Sprintf("Attribute.%d.Value", attrIdx), "true")
				attrIdx++
			}
			if dlq != "" {
				if maxReceiveCount <= 0 {
					maxReceiveCount = 3
				}
				region := "us-east-1"
				accountID := getAccountID()
				dlqArn := fmt.Sprintf("arn:aws:sqs:%s:%s:%s", region, accountID, dlq)
				redrivePolicy := fmt.Sprintf(`{"deadLetterTargetArn":"%s","maxReceiveCount":%d}`, dlqArn, maxReceiveCount)
				form.Set(fmt.Sprintf("Attribute.%d.Name", attrIdx), "RedrivePolicy")
				form.Set(fmt.Sprintf("Attribute.%d.Value", attrIdx), redrivePolicy)
				attrIdx++
			}
			_ = attrIdx

			if tags != "" {
				tagMap, err := common.ParseTagMap(tags)
				if err != nil {
					return err
				}
				if len(tagMap) > 0 {
					keys := make([]string, 0, len(tagMap))
					for key := range tagMap {
						keys = append(keys, key)
					}
					sort.Strings(keys)
					for idx, key := range keys {
						n := idx + 1
						form.Set(fmt.Sprintf("Tag.%d.Key", n), key)
						form.Set(fmt.Sprintf("Tag.%d.Value", n), tagMap[key])
					}
				}
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
				XMLName  xml.Name `xml:"CreateQueueResponse"`
				QueueUrl string   `xml:"CreateQueueResult>QueueUrl"`
			}
			if err := xml.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("Queue created: %s\n", result.QueueUrl)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Queue name (required)")
	cmd.Flags().BoolVar(&fifo, "fifo", false, "Create a FIFO queue")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags in KEY=VALUE form")
	cmd.Flags().StringVar(&dlq, "dlq", "", "Dead-letter queue name to attach as redrive policy")
	cmd.Flags().IntVar(&maxReceiveCount, "max-receive-count", 3, "Number of receives before routing to DLQ (used with --dlq)")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}
