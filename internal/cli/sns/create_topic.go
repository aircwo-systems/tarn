package sns

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

func newCreateTopicCmd() *cobra.Command {
	var (
		name string
		fifo bool
		tags string
	)

	cmd := &cobra.Command{
		Use:   "create-topic",
		Short: "Create an SNS topic",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			form := url.Values{
				"Action": {"CreateTopic"},
				"Name":   {name},
			}
			if fifo {
				form.Set("Attributes.entry.1.key", "FifoTopic")
				form.Set("Attributes.entry.1.value", "true")
			}
			if tags != "" {
				tagMap, err := common.ParseTagMap(tags)
				if err != nil {
					return err
				}
				keys := make([]string, 0, len(tagMap))
				for key := range tagMap {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for i, key := range keys {
					n := i + 1
					form.Set(fmt.Sprintf("Tags.member.%d.Key", n), key)
					form.Set(fmt.Sprintf("Tags.member.%d.Value", n), tagMap[key])
				}
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
				TopicArn string `xml:"CreateTopicResult>TopicArn"`
			}
			if err := xml.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			fmt.Printf("Topic created: %s\n", result.TopicArn)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Topic name (required)")
	cmd.Flags().BoolVar(&fifo, "fifo", false, "Create a FIFO topic (name must end with .fifo)")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags in KEY=VALUE form")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}
