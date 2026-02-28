package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var cliHTTPClient = http.DefaultClient

type flushOptions struct {
	TagFilter string
	DryRun    bool
}

type flushOverview struct {
	Config struct {
		AccountID string `json:"accountId"`
	} `json:"config"`
	Gateways []struct {
		APIID string            `json:"apiId"`
		Name  string            `json:"name"`
		Tags  map[string]string `json:"tags"`
	} `json:"gateways"`
	Functions []struct {
		Name string            `json:"name"`
		Tags map[string]string `json:"tags"`
	} `json:"functions"`
	Queues []struct {
		Name string            `json:"name"`
		URL  string            `json:"url"`
		Tags map[string]string `json:"tags"`
	} `json:"queues"`
	Secrets []struct {
		Name string            `json:"name"`
		Tags map[string]string `json:"tags"`
	} `json:"secrets"`
}

func newFlushCmd() *cobra.Command {
	var opts flushOptions

	cmd := &cobra.Command{
		Use:   "flush",
		Short: "Delete provisioned resources from the current OpenStack instance",
		Long: `Flush deletes provisioned API Gateways, Lambda functions, SQS queues, and Secrets Manager secrets
from the current OpenStack instance.

Use --tag to scope deletion to a feature slice such as feature=r10.`,
		Example: `  openstack flush
  openstack flush --tag feature=r10
  openstack flush --tag r10 --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFlush(cmd, os.Stdout, opts)
		},
	}

	cmd.Flags().StringVar(&opts.TagFilter, "tag", "", "Filter resources by tag query, e.g. feature=r10 or r10")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Print resources that would be deleted without deleting them")
	return cmd
}

func runFlush(cmd *cobra.Command, out io.Writer, opts flushOptions) error {
	endpoint := getCLIEndpoint(cmd)
	overview, err := fetchFlushOverview(endpoint)
	if err != nil {
		return err
	}

	filteredGateways := filterGateways(overview.Gateways, opts.TagFilter)
	filteredFunctions := filterFunctions(overview.Functions, opts.TagFilter)
	filteredQueues := filterQueues(overview.Queues, opts.TagFilter)
	filteredSecrets := filterSecrets(overview.Secrets, opts.TagFilter)

	total := len(filteredGateways) + len(filteredFunctions) + len(filteredQueues) + len(filteredSecrets)
	if total == 0 {
		if opts.TagFilter != "" {
			_, _ = fmt.Fprintf(out, "No resources matched tag filter %q\n", opts.TagFilter)
		} else {
			_, _ = fmt.Fprintln(out, "No resources found.")
		}
		return nil
	}

	_, _ = fmt.Fprintf(out, "Matched %d resources", total)
	if opts.TagFilter != "" {
		_, _ = fmt.Fprintf(out, " for tag filter %q", opts.TagFilter)
	}
	_, _ = fmt.Fprintln(out)

	if err := printFlushPlan(out, filteredGateways, filteredFunctions, filteredQueues, filteredSecrets); err != nil {
		return err
	}
	if opts.DryRun {
		_, _ = fmt.Fprintln(out, "Dry run only. No resources were deleted.")
		return nil
	}

	for _, api := range filteredGateways {
		if err := deleteAPIGateway(endpoint, api.APIID); err != nil {
			return fmt.Errorf("delete api gateway %s: %w", api.Name, err)
		}
		_, _ = fmt.Fprintf(out, "Deleted API Gateway: %s\n", api.Name)
	}
	for _, queue := range filteredQueues {
		if err := deleteQueue(queue.URL); err != nil {
			return fmt.Errorf("delete queue %s: %w", queue.Name, err)
		}
		_, _ = fmt.Fprintf(out, "Deleted Queue: %s\n", queue.Name)
	}
	for _, secret := range filteredSecrets {
		if err := deleteSecret(endpoint, secret.Name); err != nil {
			return fmt.Errorf("delete secret %s: %w", secret.Name, err)
		}
		_, _ = fmt.Fprintf(out, "Deleted Secret: %s\n", secret.Name)
	}
	for _, fn := range filteredFunctions {
		if err := deleteFunction(endpoint, fn.Name); err != nil {
			return fmt.Errorf("delete function %s: %w", fn.Name, err)
		}
		_, _ = fmt.Fprintf(out, "Deleted Lambda: %s\n", fn.Name)
	}

	_, _ = fmt.Fprintln(out, "Flush complete.")
	return nil
}

func fetchFlushOverview(endpoint string) (*flushOverview, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint+"/_openstack/admin/overview", nil)
	if err != nil {
		return nil, err
	}
	resp, err := cliHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
	}

	var overview flushOverview
	if err := json.Unmarshal(body, &overview); err != nil {
		return nil, fmt.Errorf("failed to parse overview response: %w", err)
	}
	return &overview, nil
}

func printFlushPlan(out io.Writer, gateways []struct {
	APIID string
	Name  string
	Tags  map[string]string
}, functions []struct {
	Name string
	Tags map[string]string
}, queues []struct {
	Name string
	URL  string
	Tags map[string]string
}, secrets []struct {
	Name string
	Tags map[string]string
}) error {
	if len(gateways) > 0 {
		if _, err := fmt.Fprintln(out, "API Gateways:"); err != nil {
			return err
		}
		for _, api := range gateways {
			if _, err := fmt.Fprintf(out, "  - %s (%s)\n", api.Name, api.APIID); err != nil {
				return err
			}
		}
	}
	if len(functions) > 0 {
		if _, err := fmt.Fprintln(out, "Lambda Functions:"); err != nil {
			return err
		}
		for _, fn := range functions {
			if _, err := fmt.Fprintf(out, "  - %s\n", fn.Name); err != nil {
				return err
			}
		}
	}
	if len(queues) > 0 {
		if _, err := fmt.Fprintln(out, "SQS Queues:"); err != nil {
			return err
		}
		for _, queue := range queues {
			if _, err := fmt.Fprintf(out, "  - %s\n", queue.Name); err != nil {
				return err
			}
		}
	}
	if len(secrets) > 0 {
		if _, err := fmt.Fprintln(out, "Secrets:"); err != nil {
			return err
		}
		for _, secret := range secrets {
			if _, err := fmt.Fprintf(out, "  - %s\n", secret.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func filterGateways(items []struct {
	APIID string            `json:"apiId"`
	Name  string            `json:"name"`
	Tags  map[string]string `json:"tags"`
}, query string) []struct {
	APIID string
	Name  string
	Tags  map[string]string
} {
	var out []struct {
		APIID string
		Name  string
		Tags  map[string]string
	}
	for _, item := range items {
		if matchesTagSelector(item.Tags, query) {
			out = append(out, struct {
				APIID string
				Name  string
				Tags  map[string]string
			}{APIID: item.APIID, Name: item.Name, Tags: item.Tags})
		}
	}
	return out
}

func filterFunctions(items []struct {
	Name string            `json:"name"`
	Tags map[string]string `json:"tags"`
}, query string) []struct {
	Name string
	Tags map[string]string
} {
	var out []struct {
		Name string
		Tags map[string]string
	}
	for _, item := range items {
		if matchesTagSelector(item.Tags, query) {
			out = append(out, struct {
				Name string
				Tags map[string]string
			}{Name: item.Name, Tags: item.Tags})
		}
	}
	return out
}

func filterQueues(items []struct {
	Name string            `json:"name"`
	URL  string            `json:"url"`
	Tags map[string]string `json:"tags"`
}, query string) []struct {
	Name string
	URL  string
	Tags map[string]string
} {
	var out []struct {
		Name string
		URL  string
		Tags map[string]string
	}
	for _, item := range items {
		if matchesTagSelector(item.Tags, query) {
			out = append(out, struct {
				Name string
				URL  string
				Tags map[string]string
			}{Name: item.Name, URL: item.URL, Tags: item.Tags})
		}
	}
	return out
}

func filterSecrets(items []struct {
	Name string            `json:"name"`
	Tags map[string]string `json:"tags"`
}, query string) []struct {
	Name string
	Tags map[string]string
} {
	var out []struct {
		Name string
		Tags map[string]string
	}
	for _, item := range items {
		if matchesTagSelector(item.Tags, query) {
			out = append(out, struct {
				Name string
				Tags map[string]string
			}{Name: item.Name, Tags: item.Tags})
		}
	}
	return out
}

func matchesTagSelector(tags map[string]string, query string) bool {
	normalized := strings.TrimSpace(strings.ToLower(query))
	if normalized == "" {
		return true
	}
	if len(tags) == 0 {
		return false
	}

	pairSeparator := ""
	switch {
	case strings.Contains(normalized, ":"):
		pairSeparator = ":"
	case strings.Contains(normalized, "="):
		pairSeparator = "="
	}

	if pairSeparator != "" {
		parts := strings.Split(normalized, pairSeparator)
		keyQuery := strings.TrimSpace(parts[0])
		valueQuery := strings.TrimSpace(strings.Join(parts[1:], pairSeparator))
		for key, value := range tags {
			keyLower := strings.ToLower(key)
			valueLower := strings.ToLower(value)
			if keyQuery != "" && !strings.Contains(keyLower, keyQuery) {
				continue
			}
			if valueQuery != "" && !strings.Contains(valueLower, valueQuery) {
				continue
			}
			return true
		}
		return false
	}

	for key, value := range tags {
		keyLower := strings.ToLower(key)
		valueLower := strings.ToLower(value)
		if strings.Contains(keyLower, normalized) ||
			strings.Contains(valueLower, normalized) ||
			strings.Contains(keyLower+":"+valueLower, normalized) {
			return true
		}
	}
	return false
}

func deleteAPIGateway(endpoint, apiID string) error {
	req, err := http.NewRequest(http.MethodDelete, endpoint+"/v2/apis/"+apiID, nil)
	if err != nil {
		return err
	}
	resp, err := cliHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func deleteFunction(endpoint, name string) error {
	req, err := http.NewRequest(http.MethodDelete, endpoint+"/2015-03-31/functions/"+name, nil)
	if err != nil {
		return err
	}
	resp, err := cliHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func deleteQueue(queueURL string) error {
	form := url.Values{
		"Action":   {"DeleteQueue"},
		"QueueUrl": {queueURL},
	}
	req, err := http.NewRequest(http.MethodPost, queueURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := cliHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func deleteSecret(endpoint, name string) error {
	body, _ := json.Marshal(map[string]any{
		"SecretId":                   name,
		"ForceDeleteWithoutRecovery": true,
	})
	req, err := http.NewRequest(http.MethodPost, endpoint+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.DeleteSecret")

	resp, err := cliHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error (%d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func getCLIEndpoint(cmd *cobra.Command) string {
	if v := os.Getenv("OPENSTACK_ENDPOINT"); v != "" {
		return v
	}

	host, _ := cmd.Root().Flags().GetString("host")
	port, _ := cmd.Root().Flags().GetInt("port")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}
