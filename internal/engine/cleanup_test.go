package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// fakeDocker is a minimal Docker Engine API server that records the calls
// the engine makes, so cleanup paths can be tested without a daemon.
type fakeDocker struct {
	mu        sync.Mutex
	listed    []container.Summary
	listQuery filters.Args
	created   container.Config
	stopped   []string
	removed   []string
}

func (f *fakeDocker) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	if i := strings.Index(path, "/containers"); i >= 0 {
		path = path[i:]
	}
	switch {
	case r.Method == http.MethodGet && path == "/containers/json":
		f.listQuery, _ = filters.FromJSON(r.URL.Query().Get("filters"))
		_ = json.NewEncoder(w).Encode(f.listed)
	case r.Method == http.MethodPost && path == "/containers/create":
		_ = json.NewDecoder(r.Body).Decode(&f.created)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"created-id"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/stop"):
		f.stopped = append(f.stopped, strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/stop"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/containers/"):
		f.removed = append(f.removed, strings.TrimPrefix(path, "/containers/"))
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func newFakeEngine(t *testing.T, port int) (*Engine, *fakeDocker) {
	t.Helper()
	fake := &fakeDocker{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(
		client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")),
		client.WithVersion("1.45"),
		client.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return &Engine{
		client:     cli,
		cfg:        &config.Config{Port: port, Region: "us-east-1"},
		containers: make(map[string][]*ContainerInfo),
	}, fake
}

func TestCreateContainerLabelsLambdaOwnership(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	fn := &types.FunctionConfig{FunctionName: "orders", Runtime: types.RuntimeNodeJS20, Handler: "index.handler", MemorySize: 128}

	if _, err := eng.CreateContainer(context.Background(), fn, t.TempDir(), nil, "111111111111"); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		labelManaged:  labelManagedLambda,
		labelPort:     "4566",
		labelAccount:  "111111111111",
		labelFunction: "orders",
	}
	for k, v := range want {
		if fake.created.Labels[k] != v {
			t.Fatalf("label %s = %q, want %q (labels %v)", k, fake.created.Labels[k], v, fake.created.Labels)
		}
	}
}

func TestSweepOrphanedLambdaContainersRemovesOnlyThisInstance(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	fake.listed = []container.Summary{{ID: "orphan-running", State: "running"}, {ID: "orphan-exited", State: "exited"}}

	n, err := eng.SweepOrphanedLambdaContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("removed %d, want 2", n)
	}
	if !fake.listQuery.ExactMatch("label", labelManaged+"="+labelManagedLambda) || !fake.listQuery.ExactMatch("label", labelPort+"=4566") {
		t.Fatalf("list must filter by owner labels, got %v", fake.listQuery)
	}
	if strings.Join(fake.removed, ",") != "orphan-running,orphan-exited" {
		t.Fatalf("removed %v", fake.removed)
	}
}

func TestRemoveContainerSurvivesCancelledContext(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	eng.containers["orders"] = []*ContainerInfo{{ID: "cold-start", FunctionName: "orders"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // invoking client disconnected mid cold start

	if err := eng.RemoveContainer(ctx, "cold-start"); err != nil {
		t.Fatal(err)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "cold-start" {
		t.Fatalf("container untracked but not removed from Docker: removed=%v", fake.removed)
	}
	if eng.CountContainers("orders") != 0 {
		t.Fatal("container still tracked")
	}
}

func TestCleanupStopsAndRemovesEveryPool(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	eng.containers["orders"] = []*ContainerInfo{{ID: "a"}, {ID: "b", Busy: true}}
	eng.containers["billing"] = []*ContainerInfo{{ID: "c"}}

	eng.Cleanup(context.Background())

	if len(fake.stopped) != 3 || len(fake.removed) != 3 {
		t.Fatalf("stopped=%v removed=%v, want all 3", fake.stopped, fake.removed)
	}
	if len(eng.containers) != 0 {
		t.Fatalf("pools still tracked: %v", eng.containers)
	}
}

func TestSweepLegacyLambdaContainersRemovesOnlyStoppedUnlabelled(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	lambdaImage := "public.ecr.aws/lambda/nodejs:20"
	fake.listed = []container.Summary{
		{ID: "old-tarn", Names: []string{"/tarn-lambda-orders-1790340836404"}, Image: lambdaImage, State: "exited"},
		{ID: "old-openstack", Names: []string{"/openstack-lambda-billing-1772917465172"}, Image: lambdaImage, State: "created"},
		{ID: "old-running", Names: []string{"/tarn-lambda-live-1790340836404"}, Image: lambdaImage, State: "running"},
		{ID: "labelled", Names: []string{"/tarn-lambda-orders-1790340836405"}, Image: lambdaImage, State: "exited", Labels: map[string]string{labelManaged: labelManagedLambda}},
		{ID: "other-image", Names: []string{"/tarn-lambda-orders-1790340836406"}, Image: "node:20", State: "exited"},
		{ID: "user-container", Names: []string{"/my-postgres"}, Image: "postgres", State: "exited"},
	}

	names, err := eng.SweepLegacyLambdaContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "tarn-lambda-orders-1790340836404,openstack-lambda-billing-1772917465172" {
		t.Fatalf("names = %v", names)
	}
	if strings.Join(fake.removed, ",") != "old-tarn,old-openstack" {
		t.Fatalf("removed %v", fake.removed)
	}
}
