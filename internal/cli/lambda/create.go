package lambda

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var (
		name       string
		runtime    string
		handler    string
		role       string
		zipFile    string
		timeout    int
		memorySize int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new Lambda function",
		Example: `  openstack lambda create --name my-func --runtime nodejs20.x --handler index.handler --zip ./code.zip
  openstack lambda create --name hello --runtime python3.12 --handler lambda_function.lambda_handler --zip ./hello.zip`,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			reqBody := map[string]interface{}{
				"FunctionName": name,
				"Runtime":      runtime,
				"Handler":      handler,
				"Role":         role,
			}
			if timeout > 0 {
				reqBody["Timeout"] = timeout
			}
			if memorySize > 0 {
				reqBody["MemorySize"] = memorySize
			}

			if zipFile != "" {
				data, err := os.ReadFile(zipFile)
				if err != nil {
					return fmt.Errorf("failed to read zip file: %w", err)
				}
				reqBody["Code"] = map[string]string{
					"ZipFile": base64.StdEncoding.EncodeToString(data),
				}
			}

			body, _ := json.Marshal(reqBody)
			resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusCreated {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(respBody))
			}

			fmt.Printf("Function %s created successfully\n", name)

			var result map[string]interface{}
			json.Unmarshal(respBody, &result)
			if arn, ok := result["FunctionArn"]; ok {
				fmt.Printf("ARN: %s\n", arn)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Function name (required)")
	cmd.Flags().StringVar(&runtime, "runtime", "", "Runtime (e.g., nodejs20.x, python3.12)")
	cmd.Flags().StringVar(&handler, "handler", "", "Handler (e.g., index.handler)")
	cmd.Flags().StringVar(&role, "role", "arn:aws:iam::000000000000:role/lambda-role", "Execution role ARN")
	cmd.Flags().StringVar(&zipFile, "zip", "", "Path to deployment package (zip file)")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "Function timeout in seconds")
	cmd.Flags().IntVar(&memorySize, "memory", 0, "Function memory in MB")

	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("runtime")
	cmd.MarkFlagRequired("handler")

	return cmd
}
