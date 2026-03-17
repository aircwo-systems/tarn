package sns

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newListTopicsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list-topics",
		Aliases: []string{"ls"},
		Short:   "List SNS topics",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)
			resp, err := http.PostForm(endpoint, url.Values{"Action": {"ListTopics"}})
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			var result struct {
				Topics []string `xml:"ListTopicsResult>Topics>member>TopicArn"`
			}
			if err := xml.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if len(result.Topics) == 0 {
				fmt.Println("No topics found.")
				return nil
			}
			for _, arn := range result.Topics {
				fmt.Println(arn)
			}
			return nil
		},
	}
}
