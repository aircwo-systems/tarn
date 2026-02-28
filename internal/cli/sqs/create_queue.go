package sqs

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"

	"github.com/openstack-project/openstack/internal/cli/common"
	"github.com/spf13/cobra"
)

func newCreateQueueCmd() *cobra.Command {
	var (
		name string
		fifo bool
		tags string
	)

	cmd := &cobra.Command{
		Use:   "create-queue",
		Short: "Create a new SQS queue",
		Example: `  openstack sqs create-queue --name my-queue
  openstack sqs create-queue --name my-queue.fifo --fifo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			form := url.Values{
				"Action":    {"CreateQueue"},
				"QueueName": {name},
			}
			if fifo {
				form.Set("Attribute.1.Name", "FifoQueue")
				form.Set("Attribute.1.Value", "true")
			}
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
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

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
	cmd.MarkFlagRequired("name")

	return cmd
}
