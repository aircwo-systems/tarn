package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type fakeStorePool struct {
	cognitoFlushPool
	protected bool
}

type fakeStoreTable struct {
	dynamodbFlushTable
	protected bool
}

// fakeStoreTarn is a stateful stand-in for the Tarn overview plus the Cognito,
// DynamoDB, KMS and ECS calls flush makes, so a second flush sees the result
// of the first.
type fakeStoreTarn struct {
	t        *testing.T
	pools    []*fakeStorePool
	tables   []*fakeStoreTable
	keys     []*kmsFlushKey
	clusters []ecsFlushCluster
	calls    []string
}

const fakeStoreEndpoint = "http://tarn.test"

func (f *fakeStoreTarn) install(t *testing.T) {
	t.Helper()
	t.Setenv("TARN_ENDPOINT", fakeStoreEndpoint)
	prev := cliHTTPClient
	cliHTTPClient = &http.Client{Transport: roundTripperFunc(f.roundTrip)}
	t.Cleanup(func() { cliHTTPClient = prev })
}

func (f *fakeStoreTarn) roundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && r.URL.String() == fakeStoreEndpoint+"/_tarn/admin/overview" {
		return f.overview()
	}
	if r.Method != http.MethodPost || r.URL.String() != fakeStoreEndpoint+"/" {
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		return jsonResponse(http.StatusNotFound, `{}`)
	}
	target := r.Header.Get("X-Amz-Target")
	f.calls = append(f.calls, target)
	body, _ := io.ReadAll(r.Body)
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		f.t.Fatalf("decode %s: %v", target, err)
	}
	str := func(k string) string { s, _ := in[k].(string); return s }

	switch target {
	case "AWSCognitoIdentityProviderService.DeleteUserPool":
		for i, p := range f.pools {
			if p.ID != str("UserPoolId") {
				continue
			}
			if p.protected {
				return jsonResponse(http.StatusBadRequest, `{"__type":"InvalidParameterException","message":"The user pool cannot be deleted because deletion protection is activated. Deletion protection must be inactivated first."}`)
			}
			f.pools = slices.Delete(f.pools, i, i+1)
			return jsonResponse(http.StatusOK, `{}`)
		}
		return jsonResponse(http.StatusBadRequest, `{"__type":"ResourceNotFoundException"}`)
	case "AWSCognitoIdentityProviderService.UpdateUserPool":
		for _, p := range f.pools {
			if p.ID == str("UserPoolId") && str("DeletionProtection") == "INACTIVE" {
				p.protected = false
			}
		}
		return jsonResponse(http.StatusOK, `{}`)
	case "DynamoDB_20120810.DeleteTable":
		if ct := r.Header.Get("Content-Type"); ct != "application/x-amz-json-1.0" {
			f.t.Errorf("DynamoDB content type = %q", ct)
		}
		for i, tb := range f.tables {
			if tb.Name != str("TableName") {
				continue
			}
			if tb.protected {
				return jsonResponse(http.StatusBadRequest, `{"__type":"com.amazon.coral.validate#ValidationException","message":"Resource cannot be deleted as it is currently protected against deletion. Disable deletion protection first."}`)
			}
			f.tables = slices.Delete(f.tables, i, i+1)
			return jsonResponse(http.StatusOK, `{}`)
		}
		return jsonResponse(http.StatusBadRequest, `{"__type":"com.amazonaws.dynamodb.v20120810#ResourceNotFoundException"}`)
	case "DynamoDB_20120810.UpdateTable":
		for _, tb := range f.tables {
			if tb.Name == str("TableName") && in["DeletionProtectionEnabled"] == false {
				tb.protected = false
			}
		}
		return jsonResponse(http.StatusOK, `{}`)
	case "TrentService.DeleteAlias":
		for _, k := range f.keys {
			if i := slices.Index(k.Aliases, str("AliasName")); i >= 0 {
				k.Aliases = slices.Delete(k.Aliases, i, i+1)
				return jsonResponse(http.StatusOK, `{}`)
			}
		}
		return jsonResponse(http.StatusBadRequest, `{"__type":"NotFoundException"}`)
	case "TrentService.ScheduleKeyDeletion":
		if in["PendingWindowInDays"] != float64(7) {
			f.t.Errorf("PendingWindowInDays = %v, want 7", in["PendingWindowInDays"])
		}
		for _, k := range f.keys {
			if k.ID == str("KeyId") {
				if len(k.Aliases) > 0 {
					f.t.Errorf("key %s scheduled before its aliases were deleted", k.ID)
				}
				k.State = "PendingDeletion"
				return jsonResponse(http.StatusOK, `{}`)
			}
		}
		return jsonResponse(http.StatusBadRequest, `{"__type":"NotFoundException"}`)
	case "AmazonEC2ContainerServiceV20141113.DeleteCluster":
		return jsonResponse(http.StatusOK, `{}`)
	}
	f.t.Errorf("unexpected action %s", target)
	return jsonResponse(http.StatusBadRequest, `{}`)
}

func (f *fakeStoreTarn) overview() (*http.Response, error) {
	pools := make([]cognitoFlushPool, 0, len(f.pools))
	for _, p := range f.pools {
		pools = append(pools, p.cognitoFlushPool)
	}
	tables := make([]dynamodbFlushTable, 0, len(f.tables))
	for _, tb := range f.tables {
		tables = append(tables, tb.dynamodbFlushTable)
	}
	keys := make([]kmsFlushKey, 0, len(f.keys))
	for _, k := range f.keys {
		keys = append(keys, *k)
	}
	body, _ := json.Marshal(map[string]any{
		"config":         map[string]string{"accountId": "000000000000"},
		"cognitoPools":   pools,
		"dynamodbTables": tables,
		"kmsKeys":        keys,
		"ecs":            map[string]any{"clusters": f.clusters},
	})
	return jsonResponse(http.StatusOK, string(body))
}

func (f *fakeStoreTarn) flush(t *testing.T, opts flushOptions) string {
	t.Helper()
	var out bytes.Buffer
	if err := runFlush(&cobra.Command{Use: "tarn"}, &out, opts); err != nil {
		t.Fatalf("runFlush(%+v): %v\n%s", opts, err, out.String())
	}
	return out.String()
}

func newFakeStoreTarn(t *testing.T) *fakeStoreTarn {
	f := &fakeStoreTarn{
		t: t,
		pools: []*fakeStorePool{
			{cognitoFlushPool: cognitoFlushPool{ID: "eu-west-2_app", Name: "app-users"}},
			{cognitoFlushPool: cognitoFlushPool{ID: "eu-west-2_admin", Name: "admin-users"}, protected: true},
		},
		tables: []*fakeStoreTable{
			{dynamodbFlushTable: dynamodbFlushTable{Name: "app-orders", ItemCount: 3}},
			{dynamodbFlushTable: dynamodbFlushTable{Name: "admin-audit"}, protected: true},
		},
		keys: []*kmsFlushKey{
			{ID: "key-pool", Manager: "CUSTOMER", State: "Enabled", Aliases: []string{"alias/app-sender"},
				UsedBy: []kmsFlushKeyRef{{Kind: "userpool", Name: "app-users", ID: "eu-west-2_app"}}},
			{ID: "key-table", Manager: "CUSTOMER", State: "Enabled",
				UsedBy: []kmsFlushKeyRef{{Kind: "table", Name: "app-orders"}}},
			{ID: "key-loose", Manager: "CUSTOMER", State: "Enabled"},
			{ID: "key-gone", Manager: "CUSTOMER", State: "PendingDeletion"},
			{ID: "key-aws", Manager: "AWS", State: "Enabled", Aliases: []string{"alias/aws/secretsmanager"}},
		},
		clusters: []ecsFlushCluster{{Name: "default", Arn: "arn:aws:ecs:eu-west-2:000000000000:cluster/default"}},
	}
	f.install(t)
	return f
}

func keyStates(f *fakeStoreTarn) map[string]string {
	out := map[string]string{}
	for _, k := range f.keys {
		out[k.ID] = k.State
	}
	return out
}

func TestRunFlushDeletesCognitoPoolsAndKeepsTablesWithoutStorage(t *testing.T) {
	f := newFakeStoreTarn(t)

	out := f.flush(t, flushOptions{})

	if len(f.pools) != 0 {
		t.Fatalf("pools left after flush: %+v\n%s", f.pools, out)
	}
	if len(f.tables) != 2 {
		t.Fatalf("flush without --storage deleted DynamoDB tables: %+v", f.tables)
	}
	want := map[string]string{
		"key-pool":  "PendingDeletion",
		"key-table": "Enabled", // its table survives without --storage
		"key-loose": "PendingDeletion",
		"key-gone":  "PendingDeletion",
		"key-aws":   "Enabled",
	}
	if got := keyStates(f); !maps.Equal(got, want) {
		t.Fatalf("key states = %v, want %v", got, want)
	}
	for _, s := range []string{
		"Deleted Cognito User Pool: admin-users (eu-west-2_admin)",
		"Scheduled KMS Key Deletion: key-pool (alias/app-sender)",
	} {
		if !strings.Contains(out, s) {
			t.Fatalf("output missing %q:\n%s", s, out)
		}
	}
	for _, s := range []string{"DynamoDB", "ECS Cluster", "key-aws", "key-gone"} {
		if strings.Contains(out, s) {
			t.Fatalf("output mentions %q:\n%s", s, out)
		}
	}
	if slices.Contains(f.calls, "AmazonEC2ContainerServiceV20141113.DeleteCluster") {
		t.Fatalf("flush deleted the implicit default ECS cluster")
	}

	if again := f.flush(t, flushOptions{}); !strings.Contains(again, "No resources found.") {
		t.Fatalf("second flush matched resources:\n%s", again)
	}
}

func TestRunFlushWithStorageDeletesProtectedTablesAndTheirKeys(t *testing.T) {
	f := newFakeStoreTarn(t)

	dry := f.flush(t, flushOptions{Storage: true, DryRun: true})
	if len(f.calls) != 0 {
		t.Fatalf("dry run made calls: %v", f.calls)
	}
	for _, s := range []string{"Cognito User Pools:", "DynamoDB Tables:", "app-orders (3 items)", "KMS Keys (scheduled for deletion in 7 days):", "key-table"} {
		if !strings.Contains(dry, s) {
			t.Fatalf("dry-run plan missing %q:\n%s", s, dry)
		}
	}

	out := f.flush(t, flushOptions{Storage: true})
	if len(f.tables) != 0 || len(f.pools) != 0 {
		t.Fatalf("left tables %+v pools %+v\n%s", f.tables, f.pools, out)
	}
	if got := keyStates(f)["key-table"]; got != "PendingDeletion" {
		t.Fatalf("key-table state = %s, want PendingDeletion", got)
	}
	if !strings.Contains(out, "Deleted DynamoDB Table: admin-audit") {
		t.Fatalf("protected table not deleted:\n%s", out)
	}

	if again := f.flush(t, flushOptions{Storage: true}); !strings.Contains(again, "No resources found.") {
		t.Fatalf("second flush matched resources:\n%s", again)
	}
}

func TestRunFlushGroupLeavesUnrelatedPoolsTablesAndKeys(t *testing.T) {
	f := newFakeStoreTarn(t)
	f.keys = append(f.keys, &kmsFlushKey{ID: "key-named", Manager: "CUSTOMER", State: "Enabled", Aliases: []string{"alias/app-tokens"}})

	out := f.flush(t, flushOptions{Group: "app", Storage: true})

	if len(f.pools) != 1 || f.pools[0].Name != "admin-users" {
		t.Fatalf("pools after --group app: %+v\n%s", f.pools, out)
	}
	if len(f.tables) != 1 || f.tables[0].Name != "admin-audit" {
		t.Fatalf("tables after --group app: %+v\n%s", f.tables, out)
	}
	want := map[string]string{
		"key-pool":  "PendingDeletion", // only user is the flushed app-users pool
		"key-table": "PendingDeletion", // only user is the flushed app-orders table
		"key-loose": "Enabled",         // nothing ties it to the group
		"key-gone":  "PendingDeletion",
		"key-aws":   "Enabled",
		"key-named": "PendingDeletion", // alias matches the group
	}
	if got := keyStates(f); !maps.Equal(got, want) {
		t.Fatalf("key states = %v, want %v", got, want)
	}
}

func TestRunFlushTagSkipsKeysUsedByKeptResources(t *testing.T) {
	f := newFakeStoreTarn(t)
	f.pools[0].Tags = map[string]string{"feature": "r10"}
	f.keys = append(f.keys, &kmsFlushKey{ID: "key-shared", Manager: "CUSTOMER", State: "Enabled", Tags: map[string]string{"feature": "r10"},
		UsedBy: []kmsFlushKeyRef{{Kind: "userpool", Name: "admin-users", ID: "eu-west-2_admin"}}})

	out := f.flush(t, flushOptions{TagFilter: "feature=r10"})

	if len(f.pools) != 1 || f.pools[0].Name != "admin-users" {
		t.Fatalf("pools after --tag: %+v\n%s", f.pools, out)
	}
	states := keyStates(f)
	if states["key-shared"] != "Enabled" {
		t.Fatalf("flush scheduled a tagged key still used by a kept pool:\n%s", out)
	}
	if states["key-pool"] != "PendingDeletion" || states["key-loose"] != "Enabled" {
		t.Fatalf("key states = %v\n%s", states, out)
	}
}

func TestMatchesKMSKeyIgnoresKeyID(t *testing.T) {
	key := kmsFlushKey{ID: "fe42a0b1-42aa-4c3d-9e8f-0123456789ab", Manager: "CUSTOMER", State: "Enabled"}
	for _, tc := range []struct{ query, group string }{
		{group: "fe"},
		{group: "42"},
		{query: "pr=42"},
		{query: "fe42"},
	} {
		if matchesKMSKey(key, tc.query, tc.group) {
			t.Errorf("key ID matched --tag %q --group %q", tc.query, tc.group)
		}
	}

	tagged := key
	tagged.Tags = map[string]string{"pr": "42"}
	if !matchesKMSKey(tagged, "pr=42", "") {
		t.Errorf("tagged key did not match its tag")
	}
	aliased := key
	aliased.Aliases = []string{"alias/fe-tokens"}
	if !matchesKMSKey(aliased, "", "fe") {
		t.Errorf("aliased key did not match its alias")
	}
	if matchesKMSKey(tagged, "pr=42", "fe") {
		t.Errorf("tag match ignored a --group that only the key ID contains")
	}
}
