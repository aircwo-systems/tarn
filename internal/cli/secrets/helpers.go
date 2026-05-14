package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

func getEndpoint(cmd *cobra.Command) string {
	if v := os.Getenv("TARN_ENDPOINT"); v != "" {
		return v
	}

	host, _ := cmd.Root().Flags().GetString("host")
	port, _ := cmd.Root().Flags().GetInt("port")

	if host == "0.0.0.0" {
		host = "localhost"
	}

	return fmt.Sprintf("http://%s:%d", host, port)
}

// secretsRequest sends a JSON-RPC style request to the Secrets Manager API.
func secretsRequest(endpoint, action string, body interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint+"/", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager."+action)
	common.SetAccountHeader(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		if json.Unmarshal(respBody, &errResp) == nil {
			if msg, ok := errResp["Message"]; ok {
				return nil, fmt.Errorf("%s", msg)
			}
		}
		return nil, fmt.Errorf("error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return result, nil
}
