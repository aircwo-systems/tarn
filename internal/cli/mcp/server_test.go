package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect wires a client to a server for endpoint over an in-memory transport,
// which exercises the real handshake without spawning a process.
func connect(t *testing.T, endpoint string) *mcp.ClientSession {
	t.Helper()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	server := newServer(endpoint, "test")
	go func() { _ = server.Run(context.Background(), serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// fakeTarn stands in for a running instance, serving the overview payload
// shape measured against a real one.
func fakeTarn(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_tarn/admin/overview" {
			http.NotFound(w, r)
			return
		}
		account := "000000000000"
		if auth := r.Header.Get("Authorization"); strings.Contains(auth, "Credential=") {
			account = strings.SplitN(strings.SplitN(auth, "Credential=", 2)[1], "/", 2)[0]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []string{"lambda", "sqs", "s3"},
			"config": map[string]any{
				"region": "us-east-1", "accountId": account,
				"endpoint": "http://127.0.0.1:4566", "dataDir": "/tmp/data",
			},
			"counts":    map[string]int{"functions": 1, "buckets": 1},
			"functions": []map[string]any{{"name": "order-processor", "runtime": "nodejs20.x", "state": "Active"}},
			"buckets":   []map[string]any{{"name": "uploads"}},
			"queues":    []map[string]any{},
			"secrets":   []map[string]any{{"name": "api-key"}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func callStatus(t *testing.T, session *mcp.ClientSession, args map[string]any) StatusOutput {
	t.Helper()

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "tarn_status", Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("tarn_status returned an error result: %+v", res.Content)
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("failed to remarshal structured content: %v", err)
	}
	var out StatusOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("failed to decode structured content: %v", err)
	}
	return out
}

func TestServerAdvertisesToolsAndInstructions(t *testing.T) {
	session := connect(t, "http://127.0.0.1:1")

	init := session.InitializeResult()
	if init.Instructions == "" {
		t.Fatal("server sent no instructions; a client with no AGENTS.md has nothing to orient on")
	}
	if !strings.Contains(init.Instructions, "tarn_status") {
		t.Error("instructions do not point at the entry-point tool")
	}
	if init.ServerInfo.Name != serverName {
		t.Errorf("server name is %q, want %q", init.ServerInfo.Name, serverName)
	}

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	// Clients cap how many tools can be active across all configured servers,
	// so the size of this surface is a budget worth noticing when it grows.
	want := map[string]bool{
		"tarn_status": true, "tarn_deploy_lambda": false, "tarn_invoke_lambda": false,
		"tarn_get_logs": true, "tarn_peek_queue": true, "tarn_send_message": false,
		"tarn_publish": false, "tarn_list_objects": true, "tarn_get_object": true,
		"tarn_fire_rule": false,
	}
	if len(tools.Tools) != len(want) {
		t.Errorf("advertised %d tools, want %d", len(tools.Tools), len(want))
	}

	seen := map[string]bool{}
	for _, tool := range tools.Tools {
		readOnly, known := want[tool.Name]
		if !known {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		seen[tool.Name] = true

		// With no AGENTS.md in the project, descriptions are the only
		// documentation a model gets.
		if len(tool.Description) < 100 {
			t.Errorf("%s description is %d bytes; too thin to orient a model that has never heard of Tarn",
				tool.Name, len(tool.Description))
		}
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
		if tool.Annotations == nil {
			t.Errorf("%s has no annotations, so clients cannot tell whether it mutates state", tool.Name)
			continue
		}
		if tool.Annotations.ReadOnlyHint != readOnly {
			t.Errorf("%s readOnlyHint = %v, want %v", tool.Name, tool.Annotations.ReadOnlyHint, readOnly)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("tool %q was not advertised", name)
		}
	}
}

// TestEveryToolTakesAccount guards Tarn's per-account isolation. A tool without
// the argument silently operates on the default namespace, and a model then
// reports resources as missing.
func TestEveryToolTakesAccount(t *testing.T) {
	session := connect(t, "http://127.0.0.1:1")

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	for _, tool := range tools.Tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Errorf("%s input schema is not an object", tool.Name)
			continue
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Errorf("%s input schema has no properties", tool.Name)
			continue
		}
		if _, ok := props["account"]; !ok {
			t.Errorf("%s takes no account argument", tool.Name)
		}
	}
}

func TestStatusAgainstRunningInstance(t *testing.T) {
	tarn := fakeTarn(t)
	out := callStatus(t, connect(t, tarn.URL), nil)

	if !out.Running {
		t.Fatal("running = false against a live instance")
	}
	if out.Endpoint != tarn.URL {
		t.Errorf("endpoint = %q, want %q", out.Endpoint, tarn.URL)
	}
	if out.AccountID != "000000000000" {
		t.Errorf("accountId = %q, want the default account", out.AccountID)
	}
	if len(out.Functions) != 1 || out.Functions[0].Name != "order-processor" {
		t.Errorf("functions = %+v, want order-processor", out.Functions)
	}
	if out.Functions[0].State != "Active" {
		t.Errorf("function state = %q; a model needs this to know the function is invokable", out.Functions[0].State)
	}
	if len(out.Buckets) != 1 || out.Buckets[0] != "uploads" {
		t.Errorf("buckets = %v, want [uploads]", out.Buckets)
	}
	if len(out.Secrets) != 1 || out.Secrets[0] != "api-key" {
		t.Errorf("secrets = %v, want names only", out.Secrets)
	}
	if out.Remediation != "" {
		t.Errorf("remediation = %q on a running instance, want empty", out.Remediation)
	}
}

// TestStatusAddressesRequestedAccount covers Tarn's per-account isolation,
// which is resolved from the SigV4 access key rather than a query parameter.
func TestStatusAddressesRequestedAccount(t *testing.T) {
	tarn := fakeTarn(t)
	out := callStatus(t, connect(t, tarn.URL), map[string]any{"account": "123456789012"})

	if out.AccountID != "123456789012" {
		t.Fatalf("accountId = %q, want the requested account; the SigV4 header was not sent", out.AccountID)
	}
}

// TestStatusWhenNotRunning is the point of the tool. A transport failure gives
// a model nothing to act on, so a dead endpoint must come back as a normal
// result carrying the remediation.
func TestStatusWhenNotRunning(t *testing.T) {
	// Port 1 is reserved and never listening.
	out := callStatus(t, connect(t, "http://127.0.0.1:1"), nil)

	if out.Running {
		t.Fatal("running = true against a dead endpoint")
	}
	if out.Remediation == "" {
		t.Fatal("no remediation returned; the model has nothing to act on")
	}
	if !strings.Contains(out.Remediation, "tarn start") {
		t.Errorf("remediation = %q, want the start command", out.Remediation)
	}
	if out.Endpoint == "" {
		t.Error("endpoint missing; the model cannot tell which instance was tried")
	}
}
