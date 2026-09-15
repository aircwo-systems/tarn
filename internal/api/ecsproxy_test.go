package api

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ecshandler "github.com/aircwo-systems/tarn/internal/api/ecs"
	"github.com/aircwo-systems/tarn/internal/config"
	ecssvc "github.com/aircwo-systems/tarn/internal/ecs"
	"github.com/aircwo-systems/tarn/internal/logs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// ecsProxyFixture bundles the ECS service state and HTTP handler needed to
// exercise the /_ecs/ reverse proxy without Docker: tasks are seeded
// directly into the store with network bindings pointing at httptest
// backends, the way internal/ecs/runner.go would after a real container
// start.
type ecsProxyFixture struct {
	t         *testing.T
	accountID string
	ecsSvc    *ecssvc.Service
	handler   http.Handler
	cluster   *types.Cluster
}

func newECSProxyFixture(t *testing.T, accountID string) *ecsProxyFixture {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AccountID = accountID
	cfg.PersistenceEnabled = false

	store := ecssvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init ecs store: %v", err)
	}
	ecsSvc := ecssvc.NewService(cfg, store)
	if err := ecsSvc.Init(); err != nil {
		t.Fatalf("init ecs service: %v", err)
	}
	ecsHandler := ecshandler.NewHandler(ecsSvc)

	hs := &HandlerSet{ECS: ecsHandler}
	bundle := NewAccountBundle(hs, nil)

	registry := NewHandlerRegistry(func(id string) (*AccountBundle, error) {
		if id != accountID {
			return nil, fmt.Errorf("unexpected account %s", id)
		}
		return bundle, nil
	})

	s := NewServer(cfg, registry, logs.NewService(cfg), nil)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	// Wrap with withLogging, matching the real server's handler chain
	// (s.httpServer.Handler = s.withLogging(mux)): withLogging's
	// dispatchProtocolRequest runs first and, without its /_ecs/ bailout,
	// would ParseForm() a proxied body or hijack it into SNS/DynamoDB. Using
	// the bare mux here would hide exactly that bug.
	handler := s.withLogging(mux)

	cluster, err := ecsSvc.ResolveCluster("")
	if err != nil {
		t.Fatalf("resolve default cluster: %v", err)
	}

	return &ecsProxyFixture{t: t, accountID: accountID, ecsSvc: ecsSvc, handler: handler, cluster: cluster}
}

// runningTaskWithBackend registers a task definition (one essential
// container, one port mapping), creates a service, and starts a task record
// bound to backend's actual listening port — mimicking what T7's runner
// records via SetContainerNetworkBindings after a real container starts.
func (f *ecsProxyFixture) runningTaskWithBackend(serviceName string, backend *httptest.Server) *types.Task {
	f.t.Helper()

	tdOut, err := f.ecsSvc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: serviceName,
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:         "web",
				Image:        "nginx:latest",
				PortMappings: []types.PortMapping{{ContainerPort: 8080}},
			},
		},
	})
	if err != nil {
		f.t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	if _, err := f.ecsSvc.CreateService(&types.CreateServiceInput{
		ServiceName:    serviceName,
		TaskDefinition: serviceName,
		DesiredCount:   1,
		Cluster:        f.cluster.ClusterArn,
	}); err != nil && !strings.Contains(err.Error(), "already exist") {
		f.t.Fatalf("CreateService: %v", err)
	}

	task, err := f.ecsSvc.NewTaskRecord(f.cluster, tdOut.TaskDefinition, nil, "", "service:"+serviceName)
	if err != nil {
		f.t.Fatalf("NewTaskRecord: %v", err)
	}
	if _, err := f.ecsSvc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
		f.t.Fatalf("SetTaskStatus: %v", err)
	}

	port := backendPort(f.t, backend)
	bindings := []types.NetworkBinding{{ContainerPort: 8080, HostPort: port, Protocol: "tcp", BindIP: "127.0.0.1"}}
	updated, err := f.ecsSvc.SetContainerNetworkBindings(task.TaskArn, "web", bindings)
	if err != nil {
		f.t.Fatalf("SetContainerNetworkBindings: %v", err)
	}
	return updated
}

func backendPort(t *testing.T, backend *httptest.Server) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(backend.URL, "http://"), "https://"))
	if err != nil {
		t.Fatalf("parse backend URL %s: %v", backend.URL, err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("parse backend port %s: %v", portStr, err)
	}
	return port
}

func TestECSProxyUnknownAccountReturns404(t *testing.T) {
	f := newECSProxyFixture(t, "111111111111")

	req := httptest.NewRequest(http.MethodGet, "/_ecs/222222222222/default/svc/", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestECSProxyUnknownClusterReturns404(t *testing.T) {
	f := newECSProxyFixture(t, "111111111111")

	req := httptest.NewRequest(http.MethodGet, "/_ecs/111111111111/no-such-cluster/svc/", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestECSProxyUnknownServiceReturns404(t *testing.T) {
	f := newECSProxyFixture(t, "111111111111")

	req := httptest.NewRequest(http.MethodGet, "/_ecs/111111111111/default/no-such-service/", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no-such-service") {
		t.Fatalf("body = %q, want it to mention the service name", rec.Body.String())
	}
}

func TestECSProxyNoRunningTaskReturns503(t *testing.T) {
	f := newECSProxyFixture(t, "111111111111")

	if _, err := f.ecsSvc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family:               "idle-svc",
		ContainerDefinitions: []types.ContainerDefinition{{Name: "web", Image: "nginx:latest"}},
	}); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	if _, err := f.ecsSvc.CreateService(&types.CreateServiceInput{
		ServiceName:    "idle-svc",
		TaskDefinition: "idle-svc",
		DesiredCount:   1,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_ecs/111111111111/default/idle-svc/", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestECSProxyNoTrailingPathRedirects(t *testing.T) {
	f := newECSProxyFixture(t, "111111111111")

	req := httptest.NewRequest(http.MethodGet, "/_ecs/111111111111/default/some-svc", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/_ecs/111111111111/default/some-svc/" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestECSProxyForwardsMethodBodyQueryAndPrefixHeader(t *testing.T) {
	f := newECSProxyFixture(t, "111111111111")

	var gotPath, gotQuery, gotMethod, gotBody, gotPrefix, gotForwardedHost, gotForwardedProto string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotMethod = r.Method
		gotPrefix = r.Header.Get("X-Forwarded-Prefix")
		gotForwardedHost = r.Header.Get("X-Forwarded-Host")
		gotForwardedProto = r.Header.Get("X-Forwarded-Proto")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "ok")
	}))
	defer backend.Close()

	f.runningTaskWithBackend("web-svc", backend)

	req := httptest.NewRequest(http.MethodPost, "/_ecs/111111111111/default/web-svc/api/items?x=1", strings.NewReader("hello"))
	req.Host = "tarn.local:4566"
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("upstream method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/items" {
		t.Fatalf("upstream path = %q, want /api/items", gotPath)
	}
	if gotQuery != "x=1" {
		t.Fatalf("upstream query = %q, want x=1", gotQuery)
	}
	if gotBody != "hello" {
		t.Fatalf("upstream body = %q, want hello", gotBody)
	}
	if gotPrefix != "/_ecs/111111111111/default/web-svc" {
		t.Fatalf("X-Forwarded-Prefix = %q", gotPrefix)
	}
	if gotForwardedHost != "tarn.local:4566" {
		t.Fatalf("X-Forwarded-Host = %q", gotForwardedHost)
	}
	if gotForwardedProto != "http" {
		t.Fatalf("X-Forwarded-Proto = %q", gotForwardedProto)
	}
}

func TestECSProxyRoundRobinsAcrossRunningTasks(t *testing.T) {
	f := newECSProxyFixture(t, "111111111111")

	hit := map[string]int{}
	newBackend := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hit[name]++
			w.WriteHeader(http.StatusOK)
		}))
	}
	backendA := newBackend("a")
	defer backendA.Close()
	backendB := newBackend("b")
	defer backendB.Close()

	// One service, two independently-started tasks (as a DesiredCount:2
	// service would have), each bound to its own backend.
	tdOut, err := f.ecsSvc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family:               "rr-svc",
		ContainerDefinitions: []types.ContainerDefinition{{Name: "web", Image: "nginx:latest", PortMappings: []types.PortMapping{{ContainerPort: 8080}}}},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	if _, err := f.ecsSvc.CreateService(&types.CreateServiceInput{
		ServiceName:    "rr-svc",
		TaskDefinition: "rr-svc",
		DesiredCount:   2,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	for _, backend := range []*httptest.Server{backendA, backendB} {
		task, err := f.ecsSvc.NewTaskRecord(f.cluster, tdOut.TaskDefinition, nil, "", "service:rr-svc")
		if err != nil {
			t.Fatalf("NewTaskRecord: %v", err)
		}
		if _, err := f.ecsSvc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
			t.Fatalf("SetTaskStatus: %v", err)
		}
		port := backendPort(t, backend)
		if _, err := f.ecsSvc.SetContainerNetworkBindings(task.TaskArn, "web", []types.NetworkBinding{{ContainerPort: 8080, HostPort: port, Protocol: "tcp"}}); err != nil {
			t.Fatalf("SetContainerNetworkBindings: %v", err)
		}
	}

	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/_ecs/111111111111/default/rr-svc/", nil)
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200; body=%s", i, rec.Code, rec.Body.String())
		}
	}

	if hit["a"] == 0 || hit["b"] == 0 {
		t.Fatalf("expected both backends to receive at least one request, got %+v", hit)
	}
	if hit["a"]+hit["b"] != 4 {
		t.Fatalf("expected 4 total requests, got %+v", hit)
	}
}

// TestECSProxyPreservesFormEncodedBodyThroughLoggingMiddleware guards against
// a specific failure mode: the real server's handler chain is
// s.withLogging(mux), and withLogging's dispatchProtocolRequest calls
// r.ParseForm() for POSTs while probing for SNS/DynamoDB/etc requests. For a
// form-urlencoded body, ParseForm() drains r.Body — if dispatchProtocolRequest
// doesn't bail out for /_ecs/ paths first, the proxied backend would see an
// empty body (or the request could even get hijacked into SNS if the form
// happened to carry an "Action" parameter SNS recognizes).
func TestECSProxyPreservesFormEncodedBodyThroughLoggingMiddleware(t *testing.T) {
	f := newECSProxyFixture(t, "111111111111")

	var gotBody, gotContentType string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	f.runningTaskWithBackend("form-svc", backend)

	formBody := "name=widget&Action=Publish"
	req := httptest.NewRequest(http.MethodPost, "/_ecs/111111111111/default/form-svc/submit", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if gotBody != formBody {
		t.Fatalf("upstream body = %q, want %q (dispatchProtocolRequest must not ParseForm/hijack /_ecs/ requests)", gotBody, formBody)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("upstream Content-Type = %q", gotContentType)
	}
}
