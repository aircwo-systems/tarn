package stepfunctions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

// formatEpoch renders an AWS-style date field — a Unix timestamp in seconds
// (possibly fractional), as returned on the wire — into a human-readable local
// time. Missing or zero values render as "-".
func formatEpoch(v any) string {
	f, ok := v.(float64)
	if !ok || f == 0 {
		return "-"
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).Format("2006-01-02 15:04:05 MST")
}

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

// stepFunctionsRequest sends a JSON request to the Step Functions API.
func stepFunctionsRequest(endpoint, action string, body interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint+"/", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AWSStepFunctions."+action)
	// Target the account embedded in the request's ARN (executionArn or
	// stateMachineArn) so commands work against any account without needing
	// TARN_ACCOUNT_ID. Falls back to TARN_ACCOUNT_ID / default when no ARN is present.
	common.SetAccountHeaderForAccount(req, accountFromBody(body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Tarn at %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		if json.Unmarshal(respBody, &errResp) == nil {
			// Step Functions JSON-1.0 uses __type and lowercase message
			if t, ok := errResp["__type"]; ok {
				msg := ""
				if m, ok := errResp["message"]; ok && m != nil {
					msg = fmt.Sprintf("%v", m)
				} else if m, ok := errResp["Message"]; ok && m != nil {
					msg = fmt.Sprintf("%v", m)
				}
				if msg != "" {
					return nil, fmt.Errorf("%v: %s", t, msg)
				}
				return nil, fmt.Errorf("%v", t)
			}
			// Fall back to Message (capitalized)
			if msg, ok := errResp["Message"]; ok && msg != nil {
				return nil, fmt.Errorf("%v", msg)
			}
			// Fall back to lowercase message
			if msg, ok := errResp["message"]; ok && msg != nil {
				return nil, fmt.Errorf("%v", msg)
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

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// accountFromBody pulls the account ID out of whichever ARN the request carries
// (executionArn or stateMachineArn), so the CLI targets that account. Returns ""
// when the body has no ARN, letting the caller fall back to TARN_ACCOUNT_ID.
func accountFromBody(body interface{}) string {
	m, ok := body.(map[string]interface{})
	if !ok {
		return ""
	}
	for _, key := range []string{"executionArn", "stateMachineArn"} {
		if v, ok := m[key].(string); ok {
			if acct := common.AccountFromARN(v); acct != "" {
				return acct
			}
		}
	}
	return ""
}
