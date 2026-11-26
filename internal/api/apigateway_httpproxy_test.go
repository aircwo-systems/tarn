package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apigatewayhandler "github.com/aircwo-systems/tarn/internal/api/apigateway"
	ecshandler "github.com/aircwo-systems/tarn/internal/api/ecs"
	"github.com/aircwo-systems/tarn/internal/apigateway"
	"github.com/aircwo-systems/tarn/internal/config"
	ecssvc "github.com/aircwo-systems/tarn/internal/ecs"
	"github.com/aircwo-systems/tarn/internal/logs"
	"github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// TestHTTPProxyIntegrationReferenceScenario runs the PRD's reference scenario
// through Tarn's real handler chain: an HTTP API whose HTTP_PROXY integration
// points at Tarn's own /_ecs/ proxy on a port nothing listens on, so every
// call must stay in process.
func TestHTTPProxyIntegrationReferenceScenario(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	cfg.Port = port
	account := cfg.AccountID

	ecsStore := ecssvc.NewStore(cfg)
	if err := ecsStore.Init(); err != nil {
		t.Fatal(err)
	}
	ecsSvc := ecssvc.NewService(cfg, ecsStore)
	if err := ecsSvc.Init(); err != nil {
		t.Fatal(err)
	}
	gatewaySvc := apigateway.NewService(cfg, nil, nil)
	traces := trace.NewStore()
	gatewaySvc.SetTraceStore(traces)
	dispatcher := apigateway.NewLocalDispatcher(cfg)
	gatewaySvc.SetLocalDispatcher(dispatcher)
	logsSvc := logs.NewService(cfg)
	hs := &HandlerSet{ECS: ecshandler.NewHandler(ecsSvc), APIGateway: apigatewayhandler.NewHandler(gatewaySvc), Logs: logsSvc}
	bundle := NewAccountBundle(hs, nil)
	registry := NewHandlerRegistry(func(string) (*AccountBundle, error) { return bundle, nil })
	srv := NewServer(cfg, registry, logsSvc, nil)
	t.Cleanup(srv.ecsProxyTransport.CloseIdleConnections)
	dispatcher.SetHandler(srv.InProcessHandler())
	handler := srv.httpServer.Handler

	// The identity service's task.
	type seen struct {
		uri  string
		keys []string
		auth string
		body string
	}
	var mu sync.Mutex
	var requests []seen
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, seen{r.URL.RequestURI(), r.Header.Values("X-Api-Key"), r.Header.Get("Authorization"), string(body)})
		mu.Unlock()
		if r.URL.Path == "/admin/slow" {
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
			}
			return
		}
		if r.Method == http.MethodPost {
			w.Header().Add("Set-Cookie", "session=1; HttpOnly")
			w.Header().Add("Set-Cookie", "csrf=2")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write([]byte(`{"users":[]}`))
	}))
	t.Cleanup(backend.Close)
	cluster, err := ecsSvc.ResolveCluster("")
	if err != nil {
		t.Fatal(err)
	}
	startTask(t, ecsSvc, cluster, "identity", backend)
	if _, err := ecsSvc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{Family: "idle", ContainerDefinitions: []types.ContainerDefinition{{Name: "web", Image: "nginx"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ecsSvc.CreateService(&types.CreateServiceInput{ServiceName: "idle", TaskDefinition: "idle", Cluster: cluster.ClusterArn}); err != nil {
		t.Fatal(err)
	}

	api, err := gatewaySvc.CreateAPI("admin-gateway", "", "HTTP", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	route := func(routeKey, uri string, timeout int) {
		integration, err := gatewaySvc.CreateIntegration(api.APIID, apigateway.IntegrationCreateInput{
			IntegrationType:   "HTTP_PROXY",
			IntegrationMethod: "ANY",
			IntegrationURI:    uri,
			TimeoutInMillis:   timeout,
			RequestParameters: map[string]string{"overwrite:header.x-api-key": "gateway-key"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := gatewaySvc.CreateRoute(api.APIID, apigateway.RouteCreateInput{RouteKey: routeKey, Target: "integrations/" + integration.IntegrationID}); err != nil {
			t.Fatal(err)
		}
	}
	base := "http://host.docker.internal:" + strconv.Itoa(port) + "/_ecs/" + account + "/default"
	route("ANY /admin/{proxy+}", base+"/identity/admin/{proxy}", 300)
	route("ANY /idle/{proxy+}", base+"/idle/{proxy}", 300)

	call := func(method, path, body string, header http.Header) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/_apigateway/"+api.APIID+"/$default"+path, strings.NewReader(body))
		for k, v := range header {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// GET with query, a wrong caller key and a staff token.
	rec := call(http.MethodGet, "/admin/users?q=smith&q=jones", "", http.Header{
		"Authorization": {"Bearer t"}, "X-Api-Key": {"wrong"}, "X-Correlation-Id": {"corr-ref-1"},
	})
	if rec.Code != http.StatusOK || rec.Body.String() != `{"users":[]}` {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	mu.Lock()
	got := requests[len(requests)-1]
	mu.Unlock()
	if got.uri != "/admin/users?q=smith&q=jones" || got.auth != "Bearer t" || len(got.keys) != 1 || got.keys[0] != "gateway-key" {
		t.Fatalf("service saw %+v", got)
	}

	// POST: body byte for byte, 201 with two cookies.
	rec = call(http.MethodPost, "/admin/users", `{"name":"Smith"}`, http.Header{"Content-Type": {"application/json"}})
	if rec.Code != http.StatusCreated || rec.Body.String() != `{"name":"Smith"}` || len(rec.Result().Header.Values("Set-Cookie")) != 2 {
		t.Fatalf("POST = %d %s %v", rec.Code, rec.Body, rec.Result().Header)
	}

	// A form-encoded POST reaches the service intact: the listener's AWS
	// protocol detection must not parse the form first.
	rec = call(http.MethodPost, "/admin/users", "name=Smith&Action=Publish", http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
	if rec.Code != http.StatusCreated || rec.Body.String() != "name=Smith&Action=Publish" {
		t.Fatalf("form POST = %d %q", rec.Code, rec.Body)
	}

	// Scaled to zero: the ECS proxy's 503 passes through.
	if rec = call(http.MethodGet, "/idle/x", "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("idle = %d %s", rec.Code, rec.Body)
	}

	// Slow service: 504 at the integration timeout, one request upstream.
	before := calls.Load()
	start := time.Now()
	rec = call(http.MethodGet, "/admin/slow", "", nil)
	if rec.Code != http.StatusGatewayTimeout || time.Since(start) > time.Second {
		t.Fatalf("slow = %d after %s", rec.Code, time.Since(start))
	}
	var msg struct{ Message string }
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil || msg.Message == "" {
		t.Fatalf("504 body = %q", rec.Body)
	}
	if n := calls.Load() - before; n != 1 {
		t.Fatalf("slow service got %d requests, want 1", n)
	}

	// One /tarn/api line per request, carrying the correlation ID; none for
	// the in-process /_ecs/ hop.
	events, _, _ := logsSvc.GetLogEvents("/tarn/api", nil)
	if len(events) != 5 {
		var lines []string
		for _, e := range events {
			lines = append(lines, e.Message)
		}
		t.Fatalf("/tarn/api has %d lines, want 5:\n%s", len(events), strings.Join(lines, "\n"))
	}
	first := events[0].Message
	for _, want := range []string{"apiId=" + api.APIID, "stage=$default", `route="ANY /admin/{proxy+}"`, "upstream=http://host.docker.internal:", "upstreamStatus=200", "correlationId=corr-ref-1"} {
		if !strings.Contains(first, want) {
			t.Errorf("log line %q lacks %q", first, want)
		}
	}
	for _, e := range events {
		if strings.Contains(e.Message, "/admin/slow") && !strings.Contains(e.Message, "cause=timeout") {
			t.Errorf("timeout line %q lacks cause=timeout", e.Message)
		}
	}

	// The trace for the correlation ID has an ecs:identity span.
	var found bool
	for _, tr := range traces.Recent(10) {
		if tr.CorrelationID != "corr-ref-1" {
			continue
		}
		for _, span := range tr.Spans {
			if span.Kind == "ecs" && span.Name == "identity" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no ecs:identity span under corr-ref-1")
	}
}

func startTask(t *testing.T, ecsSvc *ecssvc.Service, cluster *types.Cluster, name string, backend *httptest.Server) {
	t.Helper()
	td, err := ecsSvc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family:               name,
		ContainerDefinitions: []types.ContainerDefinition{{Name: "web", Image: "nginx", PortMappings: []types.PortMapping{{ContainerPort: 8080}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ecsSvc.CreateService(&types.CreateServiceInput{ServiceName: name, TaskDefinition: name, DesiredCount: 1, Cluster: cluster.ClusterArn}); err != nil {
		t.Fatal(err)
	}
	task, err := ecsSvc.NewTaskRecord(cluster, td.TaskDefinition, nil, "", "service:"+name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ecsSvc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := ecsSvc.SetContainerNetworkBindings(task.TaskArn, "web", []types.NetworkBinding{{ContainerPort: 8080, HostPort: backendPort(t, backend), Protocol: "tcp", BindIP: "127.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPAPIInvokeRejectsOversizedBodyBeforeBuffering(t *testing.T) {
	h := newTestHTTPHandler(t)
	body := strings.NewReader(strings.Repeat("x", apigateway.MaxPayloadBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/_apigateway/anyapi/$default/upload", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"message"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
}

func TestInProcessHandlerRejectsArchivedAccount(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	const other = "111111111111"
	registry := NewHandlerRegistry(func(accountID string) (*AccountBundle, error) {
		acctCfg := cfg.ForAccount(accountID)
		if err := acctCfg.EnsureDataDir(); err != nil {
			return nil, err
		}
		return newTestBundle(t, acctCfg), nil
	})
	registry.ConfigureAccounts(cfg.DataDir, cfg.AccountID, nil)
	srv := NewServer(cfg, registry, logs.NewService(cfg), nil)
	registry.get(other)
	if err := registry.Archive(other); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/apis", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+other+"/20240101/us-east-1/apigateway/aws4_request, SignedHeaders=host, Signature=x")
	rec := httptest.NewRecorder()
	srv.InProcessHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("archived account in process = %d %s, want 403", rec.Code, rec.Body)
	}
	if events, _, _ := srv.logsSvc.GetLogEvents("/tarn/api", nil); len(events) != 0 {
		t.Fatalf("in-process request logged %d lines", len(events))
	}
}
