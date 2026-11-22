package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// kmsFlushPendingWindowDays is the shortest deletion window KMS accepts. A
// flushed key stays listed as PendingDeletion until it passes, and later
// flushes skip it.
const kmsFlushPendingWindowDays = 7

type cognitoFlushPool struct {
	ID   string            `json:"id"`
	Name string            `json:"name"`
	Arn  string            `json:"arn"`
	Tags map[string]string `json:"tags"`
}

type dynamodbFlushTable struct {
	Name      string `json:"name"`
	Arn       string `json:"arn"`
	ItemCount int64  `json:"itemCount"`
}

type kmsFlushKeyRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	ID   string `json:"id"`
}

type kmsFlushKey struct {
	ID          string            `json:"id"`
	Arn         string            `json:"arn"`
	Description string            `json:"description"`
	State       string            `json:"state"`
	Manager     string            `json:"manager"`
	Aliases     []string          `json:"aliases"`
	Tags        map[string]string `json:"tags"`
	UsedBy      []kmsFlushKeyRef  `json:"usedBy"`
}

func filterCognitoPools(items []cognitoFlushPool, query, group string) []cognitoFlushPool {
	var out []cognitoFlushPool
	for _, item := range items {
		if matchesTagSelector(item.Tags, query) && matchesGroupName(item.Name, group) {
			out = append(out, item)
		}
	}
	return out
}

// filterDynamoDBTables selects by name, like S3 buckets: the overview carries
// no table tags.
func filterDynamoDBTables(items []dynamodbFlushTable, query, group string) []dynamodbFlushTable {
	var out []dynamodbFlushTable
	for _, item := range items {
		if matchesBucketSelector(item.Name, query) && matchesGroupName(item.Name, group) {
			out = append(out, item)
		}
	}
	return out
}

// flushedKMSUsers records the resources this flush deletes, keyed the way a
// KMS key's usedBy references name them ("kind:name").
type flushedKMSUsers map[string]bool

func (f flushedKMSUsers) add(kind, name string) {
	name = strings.TrimSpace(name)
	if name != "" {
		f[strings.ToLower(kind+":"+name)] = true
	}
}

func (f flushedKMSUsers) has(ref kmsFlushKeyRef) bool {
	if f[strings.ToLower(ref.Kind+":"+strings.TrimSpace(ref.Name))] {
		return true
	}
	return ref.ID != "" && f[strings.ToLower(ref.Kind+":"+strings.TrimSpace(ref.ID))]
}

// selectKMSFlushKeys picks the customer managed keys to schedule for
// deletion. AWS managed keys cannot be deleted, keys already pending deletion
// are done, and a key still configured on a resource this flush keeps would
// break that resource, so all three are skipped. Under --tag/--group a key is
// selected when its tags, aliases or description match, or when every
// resource using it is being flushed.
func selectKMSFlushKeys(keys []kmsFlushKey, narrow bool, query, group string, flushed flushedKMSUsers) []kmsFlushKey {
	var out []kmsFlushKey
	for _, key := range keys {
		if !strings.EqualFold(key.Manager, "CUSTOMER") || strings.EqualFold(key.State, "PendingDeletion") {
			continue
		}
		inUse := false
		for _, ref := range key.UsedBy {
			if !flushed.has(ref) {
				inUse = true
				break
			}
		}
		if inUse {
			continue
		}
		if narrow && len(key.UsedBy) == 0 && !matchesKMSKey(key, query, group) {
			continue
		}
		out = append(out, key)
	}
	return out
}

// matchesKMSKey matches --group against the key's aliases and description and
// --tag against its tags, aliases and description. The key ID is never
// matched: it is a hex UUID, so short selectors would hit it by accident.
func matchesKMSKey(key kmsFlushKey, query, group string) bool {
	var names []string
	for _, name := range append(slices.Clone(key.Aliases), key.Description) {
		if name = strings.TrimSpace(strings.TrimPrefix(name, "alias/")); name != "" {
			names = append(names, name)
		}
	}
	if strings.TrimSpace(group) != "" && !slices.ContainsFunc(names, func(n string) bool { return matchesGroupName(n, group) }) {
		return false
	}
	if strings.TrimSpace(query) == "" || matchesTagSelector(key.Tags, query) {
		return true
	}
	return slices.ContainsFunc(names, func(n string) bool { return matchesBucketSelector(n, query) })
}

func kmsKeyLabel(key kmsFlushKey) string {
	if len(key.Aliases) > 0 {
		return fmt.Sprintf("%s (%s)", key.ID, strings.Join(key.Aliases, ", "))
	}
	return key.ID
}

func printStoreFlushPlan(out io.Writer, pools []cognitoFlushPool, tables []dynamodbFlushTable, keys []kmsFlushKey) {
	if len(pools) > 0 {
		_, _ = fmt.Fprintln(out, "Cognito User Pools:")
		for _, p := range pools {
			_, _ = fmt.Fprintf(out, "  - %s (%s)\n", p.Name, p.ID)
		}
	}
	if len(tables) > 0 {
		_, _ = fmt.Fprintln(out, "DynamoDB Tables:")
		for _, t := range tables {
			_, _ = fmt.Fprintf(out, "  - %s (%d items)\n", t.Name, t.ItemCount)
		}
	}
	if len(keys) > 0 {
		_, _ = fmt.Fprintf(out, "KMS Keys (scheduled for deletion in %d days):\n", kmsFlushPendingWindowDays)
		for _, k := range keys {
			_, _ = fmt.Fprintf(out, "  - %s\n", kmsKeyLabel(k))
		}
	}
}

// callFlushJSONAction posts an AWS JSON protocol request and returns the
// response body and status. A non-200 status is returned as an error.
func callFlushJSONAction(endpoint, contentType, target string, input any) ([]byte, int, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint+"/", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Amz-Target", target)

	resp, err := cliHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return respBody, resp.StatusCode, fmt.Errorf("error (%d): %s", resp.StatusCode, string(respBody))
	}
	return respBody, resp.StatusCode, nil
}

func flushErrorMentions(body []byte, markers ...string) bool {
	message := strings.ToLower(string(body))
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func callCognitoFlushAction(endpoint, action string, input any) ([]byte, int, error) {
	return callFlushJSONAction(endpoint, "application/x-amz-json-1.1", "AWSCognitoIdentityProviderService."+action, input)
}

// deleteCognitoPool deletes a user pool, switching deletion protection off
// first when the pool has it on, as flush forces other deletions too.
func deleteCognitoPool(endpoint, poolID string) error {
	input := map[string]string{"UserPoolId": poolID}
	body, _, err := callCognitoFlushAction(endpoint, "DeleteUserPool", input)
	if err == nil || flushErrorMentions(body, "resourcenotfound") {
		return nil
	}
	if !flushErrorMentions(body, "deletion protection") {
		return err
	}
	if _, _, err := callCognitoFlushAction(endpoint, "UpdateUserPool", map[string]string{
		"UserPoolId":         poolID,
		"DeletionProtection": "INACTIVE",
	}); err != nil {
		return fmt.Errorf("disable deletion protection: %w", err)
	}
	_, _, err = callCognitoFlushAction(endpoint, "DeleteUserPool", input)
	return err
}

func callDynamoDBFlushAction(endpoint, action string, input any) ([]byte, int, error) {
	return callFlushJSONAction(endpoint, "application/x-amz-json-1.0", "DynamoDB_20120810."+action, input)
}

// deleteDynamoDBTable deletes a table, switching deletion protection off
// first when the table has it on.
func deleteDynamoDBTable(endpoint, table string) error {
	input := map[string]string{"TableName": table}
	body, _, err := callDynamoDBFlushAction(endpoint, "DeleteTable", input)
	if err == nil || flushErrorMentions(body, "resourcenotfound") {
		return nil
	}
	if !flushErrorMentions(body, "protected against deletion") {
		return err
	}
	if _, _, err := callDynamoDBFlushAction(endpoint, "UpdateTable", map[string]any{
		"TableName":                 table,
		"DeletionProtectionEnabled": false,
	}); err != nil {
		return fmt.Errorf("disable deletion protection: %w", err)
	}
	_, _, err = callDynamoDBFlushAction(endpoint, "DeleteTable", input)
	return err
}

func callKMSFlushAction(endpoint, action string, input any) ([]byte, int, error) {
	return callFlushJSONAction(endpoint, "application/x-amz-json-1.1", "TrentService."+action, input)
}

// scheduleKMSKeyDeletion removes the key's aliases, so a re-apply can create
// them again, then schedules the key for deletion after the shortest window
// KMS allows.
func scheduleKMSKeyDeletion(endpoint string, key kmsFlushKey) error {
	for _, alias := range key.Aliases {
		if !strings.HasPrefix(alias, "alias/") {
			alias = "alias/" + alias
		}
		body, _, err := callKMSFlushAction(endpoint, "DeleteAlias", map[string]string{"AliasName": alias})
		if err != nil && !flushErrorMentions(body, "notfound") {
			return fmt.Errorf("delete alias %s: %w", alias, err)
		}
	}
	body, _, err := callKMSFlushAction(endpoint, "ScheduleKeyDeletion", map[string]any{
		"KeyId":               key.ID,
		"PendingWindowInDays": kmsFlushPendingWindowDays,
	})
	if err != nil && flushErrorMentions(body, "notfound", "pending deletion") {
		return nil
	}
	return err
}
