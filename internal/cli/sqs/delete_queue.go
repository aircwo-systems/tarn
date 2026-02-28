package sqs

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newDeleteQueueCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "delete-queue",
		Short: "Delete an SQS queue",
		Example: `  openstack sqs delete-queue --name my-queue`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			form := url.Values{
				"Action":   {"DeleteQueue"},
				"QueueUrl": {queueURL(endpoint, name)},
			}

			resp, err := http.PostForm(endpoint+"/"+getAccountID()+"/"+name, form)
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
			}

			fmt.Printf("Queue %s deleted\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Queue name (required)")
	cmd.MarkFlagRequired("name")

	return cmd
}
