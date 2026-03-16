package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
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
	Storage   bool
}

type flushOverview struct {
	Config struct {
		AccountID string `json:"accountId"`
	} `json:"config"`
	Gateways []struct {
		APIID   string            `json:"apiId"`
		Name    string            `json:"name"`
		Tags    map[string]string `json:"tags"`
		Version string            `json:"version"`
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
	EventSourceMappings []struct {
		UUID         string `json:"uuid"`
		QueueName    string `json:"queueName"`
		FunctionName string `json:"functionName"`
	} `json:"eventSourceMappings"`
	Buckets []struct {
		Name      string `json:"name"`
		Objects   int    `json:"objects"`
		TotalSize int64  `json:"totalSize"`
	} `json:"buckets"`
}

func newFlushCmd() *cobra.Command {
	var opts flushOptions

	cmd := &cobra.Command{
		Use:   "flush",
		Short: "Delete provisioned resources from the current OpenStack instance",
		Long: `Flush deletes provisioned API Gateways, Lambda functions, SQS queues, event source mappings, and Secrets Manager secrets
from the current OpenStack instance.

Use --tag to scope deletion to a feature slice such as feature=r10.
Use --storage to also purge S3 bucket contents and delete buckets.`,
		Example: `  openstack flush
  openstack flush --storage
  openstack flush --tag feature=r10
  openstack flush --tag r10 --dry-run
  openstack flush --tag develop-mvp --storage`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFlush(cmd, os.Stdout, opts)
		},
	}

	cmd.Flags().StringVar(&opts.TagFilter, "tag", "", "Filter resources by tag query, e.g. feature=r10 or r10")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Print resources that would be deleted without deleting them")
	cmd.Flags().BoolVar(&opts.Storage, "storage", false, "Also flush S3 buckets and objects")
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
	filteredMappings := filterEventSourceMappings(overview.EventSourceMappings, opts.TagFilter, filteredQueues, filteredFunctions)
	filteredBuckets := []struct {
		Name      string
		Objects   int
		TotalSize int64
	}{}
	if opts.Storage {
		filteredBuckets = filterBuckets(overview.Buckets, opts.TagFilter)
	}

	total := len(filteredGateways) + len(filteredFunctions) + len(filteredQueues) + len(filteredSecrets) + len(filteredMappings) + len(filteredBuckets)
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

	if err := printFlushPlan(out, filteredGateways, filteredFunctions, filteredQueues, filteredSecrets, filteredMappings, filteredBuckets); err != nil {
		return err
	}
	if opts.DryRun {
		_, _ = fmt.Fprintln(out, "Dry run only. No resources were deleted.")
		return nil
	}

	failures := make([]string, 0)
	recordFailure := func(kind, name string, err error) {
		msg := fmt.Sprintf("%s %s: %v", kind, name, err)
		failures = append(failures, msg)
		_, _ = fmt.Fprintf(out, "Warning: failed to delete %s %s: %v\n", kind, name, err)
	}

	for _, mapping := range filteredMappings {
		if err := deleteEventSourceMapping(endpoint, mapping.UUID); err != nil {
			recordFailure("trigger", mapping.UUID, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "Deleted Trigger: %s (%s -> %s)\n", mapping.UUID, mapping.QueueName, mapping.FunctionName)
	}
	for _, api := range filteredGateways {
		if err := deleteAPIGateway(endpoint, api.APIID, api.Version); err != nil {
			recordFailure("api gateway", api.Name, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "Deleted API Gateway: %s\n", api.Name)
	}
	for _, queue := range filteredQueues {
		if err := deleteQueue(queue.URL); err != nil {
			recordFailure("queue", queue.Name, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "Deleted Queue: %s\n", queue.Name)
	}
	for _, secret := range filteredSecrets {
		if err := deleteSecret(endpoint, secret.Name); err != nil {
			recordFailure("secret", secret.Name, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "Deleted Secret: %s\n", secret.Name)
	}
	for _, fn := range filteredFunctions {
		if err := deleteFunction(endpoint, fn.Name); err != nil {
			recordFailure("lambda", fn.Name, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "Deleted Lambda: %s\n", fn.Name)
	}
	for _, bucket := range filteredBuckets {
		if err := flushS3Bucket(endpoint, bucket.Name); err != nil {
			recordFailure("s3 bucket", bucket.Name, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "Deleted S3 Bucket: %s\n", bucket.Name)
	}

	if len(failures) == 0 {
		_, _ = fmt.Fprintln(out, "Flush complete.")
		return nil
	}

	_, _ = fmt.Fprintf(out, "Flush completed with %d warning(s).\n", len(failures))
	return fmt.Errorf("flush completed with %d warning(s)", len(failures))
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
	APIID   string
	Name    string
	Tags    map[string]string
	Version string
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
}, mappings []struct {
	UUID         string
	QueueName    string
	FunctionName string
}, buckets []struct {
	Name      string
	Objects   int
	TotalSize int64
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
	if len(mappings) > 0 {
		if _, err := fmt.Fprintln(out, "Event Source Mappings:"); err != nil {
			return err
		}
		for _, mapping := range mappings {
			if _, err := fmt.Fprintf(out, "  - %s (%s -> %s)\n", mapping.UUID, mapping.QueueName, mapping.FunctionName); err != nil {
				return err
			}
		}
	}
	if len(buckets) > 0 {
		if _, err := fmt.Fprintln(out, "S3 Buckets:"); err != nil {
			return err
		}
		for _, bucket := range buckets {
			if _, err := fmt.Fprintf(out, "  - %s (%d objects, %d bytes)\n", bucket.Name, bucket.Objects, bucket.TotalSize); err != nil {
				return err
			}
		}
	}
	return nil
}

func filterGateways(items []struct {
	APIID   string            `json:"apiId"`
	Name    string            `json:"name"`
	Tags    map[string]string `json:"tags"`
	Version string            `json:"version"`
}, query string) []struct {
	APIID   string
	Name    string
	Tags    map[string]string
	Version string
} {
	var out []struct {
		APIID   string
		Name    string
		Tags    map[string]string
		Version string
	}
	for _, item := range items {
		if matchesTagSelector(item.Tags, query) {
			out = append(out, struct {
				APIID   string
				Name    string
				Tags    map[string]string
				Version string
			}{APIID: item.APIID, Name: item.Name, Tags: item.Tags, Version: item.Version})
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

func filterBuckets(items []struct {
	Name      string `json:"name"`
	Objects   int    `json:"objects"`
	TotalSize int64  `json:"totalSize"`
}, query string) []struct {
	Name      string
	Objects   int
	TotalSize int64
} {
	var out []struct {
		Name      string
		Objects   int
		TotalSize int64
	}
	for _, item := range items {
		if matchesBucketSelector(item.Name, query) {
			out = append(out, struct {
				Name      string
				Objects   int
				TotalSize int64
			}{Name: item.Name, Objects: item.Objects, TotalSize: item.TotalSize})
		}
	}
	return out
}

func matchesBucketSelector(name, query string) bool {
	nameLower := strings.ToLower(strings.TrimSpace(name))
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return true
	}
	if strings.Contains(nameLower, normalized) {
		return true
	}
	if key, value, ok := splitTagSelector(normalized); ok {
		if key != "" && strings.Contains(nameLower, key) {
			return true
		}
		if value != "" && strings.Contains(nameLower, value) {
			return true
		}
	}
	return false
}

func splitTagSelector(query string) (key, value string, ok bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", "", false
	}
	var sep string
	if strings.Contains(query, ":") {
		sep = ":"
	} else if strings.Contains(query, "=") {
		sep = "="
	}
	if sep == "" {
		return "", "", false
	}
	parts := strings.SplitN(query, sep, 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key = strings.TrimSpace(parts[0])
	value = strings.TrimSpace(parts[1])
	ok = key != "" || value != ""
	return
}

func filterEventSourceMappings(items []struct {
	UUID         string `json:"uuid"`
	QueueName    string `json:"queueName"`
	FunctionName string `json:"functionName"`
}, query string, queues []struct {
	Name string
	URL  string
	Tags map[string]string
}, functions []struct {
	Name string
	Tags map[string]string
}) []struct {
	UUID         string
	QueueName    string
	FunctionName string
} {
	var out []struct {
		UUID         string
		QueueName    string
		FunctionName string
	}

	// Without a tag filter, flush all mappings.
	if strings.TrimSpace(query) == "" {
		for _, item := range items {
			out = append(out, struct {
				UUID         string
				QueueName    string
				FunctionName string
			}{
				UUID:         item.UUID,
				QueueName:    item.QueueName,
				FunctionName: item.FunctionName,
			})
		}
		return out
	}

	// With a tag filter, mappings are selected when attached to a selected
	// queue or function. Event source mappings are currently untagged.
	queueNames := make(map[string]struct{}, len(queues))
	for _, queue := range queues {
		queueNames[queue.Name] = struct{}{}
	}
	functionNames := make(map[string]struct{}, len(functions))
	for _, fn := range functions {
		functionNames[fn.Name] = struct{}{}
	}

	for _, item := range items {
		_, queueMatch := queueNames[item.QueueName]

		normalizedFunctionName := normalizeLambdaRef(item.FunctionName)
		_, functionMatch := functionNames[item.FunctionName]
		if !functionMatch {
			_, functionMatch = functionNames[normalizedFunctionName]
		}

		if queueMatch || functionMatch {
			out = append(out, struct {
				UUID         string
				QueueName    string
				FunctionName string
			}{
				UUID:         item.UUID,
				QueueName:    item.QueueName,
				FunctionName: item.FunctionName,
			})
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

func deleteAPIGateway(endpoint, apiID, version string) error {
	path := "/v2/apis/" + apiID
	if version == "v1" {
		path = "/restapis/" + apiID
	}
	req, err := http.NewRequest(http.MethodDelete, endpoint+path, nil)
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
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Deleting a queue should be idempotent for flush purposes.
	// If the queue already disappeared, continue as success.
	if resp.StatusCode == http.StatusBadRequest {
		bodyText := string(body)
		if strings.Contains(bodyText, "AWS.SimpleQueueService.NonExistentQueue") ||
			strings.Contains(bodyText, "QueueDoesNotExist") {
			return nil
		}
	}

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

func deleteEventSourceMapping(endpoint, uuid string) error {
	req, err := http.NewRequest(http.MethodDelete, endpoint+"/2015-03-31/event-source-mappings/"+uuid, nil)
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

func normalizeLambdaRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref
	}

	const marker = ":function:"
	if !strings.HasPrefix(ref, "arn:") {
		return ref
	}
	i := strings.Index(ref, marker)
	if i == -1 {
		return ref
	}

	name := ref[i+len(marker):]
	if j := strings.IndexByte(name, ':'); j != -1 {
		name = name[:j]
	}
	if name == "" {
		return ref
	}
	return name
}

type listBucketResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

type deleteObjectsRequest struct {
	XMLName xml.Name `xml:"Delete"`
	Objects []struct {
		Key string `xml:"Key"`
	} `xml:"Object"`
}

func flushS3Bucket(endpoint, bucket string) error {
	keys, err := listAllBucketObjectKeys(endpoint, bucket)
	if err != nil {
		return err
	}

	for start := 0; start < len(keys); start += 1000 {
		end := start + 1000
		if end > len(keys) {
			end = len(keys)
		}
		if err := deleteBucketObjectBatch(endpoint, bucket, keys[start:end]); err != nil {
			return err
		}
	}

	return deleteS3Bucket(endpoint, bucket)
}

func listAllBucketObjectKeys(endpoint, bucket string) ([]string, error) {
	all := make([]string, 0)
	token := ""

	for {
		page, err := listBucketObjectPage(endpoint, bucket, token)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Contents {
			all = append(all, item.Key)
		}
		if !page.IsTruncated || strings.TrimSpace(page.NextContinuationToken) == "" {
			break
		}
		token = page.NextContinuationToken
	}

	return all, nil
}

func listBucketObjectPage(endpoint, bucket, continuationToken string) (*listBucketResult, error) {
	query := url.Values{}
	query.Set("max-keys", "1000")
	if continuationToken != "" {
		query.Set("continuation-token", continuationToken)
	}

	requestURL := endpoint + "/_s3/" + url.PathEscape(bucket)
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cliHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list bucket objects error (%d): %s", resp.StatusCode, string(body))
	}

	var result listBucketResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse bucket object list: %w", err)
	}
	return &result, nil
}

func deleteBucketObjectBatch(endpoint, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	reqBody := deleteObjectsRequest{
		Objects: make([]struct {
			Key string `xml:"Key"`
		}, len(keys)),
	}
	for i, key := range keys {
		reqBody.Objects[i] = struct {
			Key string `xml:"Key"`
		}{Key: key}
	}

	payload, err := xml.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal delete objects request: %w", err)
	}

	requestURL := endpoint + "/_s3/" + url.PathEscape(bucket) + "?delete"
	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	resp, err := cliHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete bucket objects error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func deleteS3Bucket(endpoint, bucket string) error {
	req, err := http.NewRequest(http.MethodDelete, endpoint+"/_s3/"+url.PathEscape(bucket), nil)
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
		return fmt.Errorf("delete bucket error (%d): %s", resp.StatusCode, string(body))
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
