package lambda

import (
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:     "delete",
		Short:   "Delete a Lambda function",
		Example: `  openstack lambda delete --name my-func`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			url := fmt.Sprintf("%s/2015-03-31/functions/%s", endpoint, name)
			req, err := http.NewRequest("DELETE", url, nil)
			if err != nil {
				return err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNoContent {
				fmt.Printf("Function %s deleted\n", name)
				return nil
			}

			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Function name (required)")
	cmd.MarkFlagRequired("name")

	return cmd
}
