package lambda

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

func newInvokeCmd() *cobra.Command {
	var (
		name    string
		payload string
	)

	cmd := &cobra.Command{
		Use:   "invoke",
		Short: "Invoke a Lambda function",
		Example: `  tarn lambda invoke --name my-func
  tarn lambda invoke --name my-func --payload '{"key": "value"}'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			var body io.Reader
			if payload != "" {
				body = bytes.NewBufferString(payload)
			} else {
				body = bytes.NewBufferString("{}")
			}

			url := fmt.Sprintf("%s/2015-03-31/functions/%s/invocations", endpoint, name)
			req, err := http.NewRequest("POST", url, body)
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)

			if fnErr := resp.Header.Get("X-Amz-Function-Error"); fnErr != "" {
				fmt.Printf("Function Error: %s\n", fnErr)
			}

			fmt.Println(string(respBody))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Function name (required)")
	cmd.Flags().StringVar(&payload, "payload", "", "JSON payload to send to the function")

	cmd.MarkFlagRequired("name")

	return cmd
}
