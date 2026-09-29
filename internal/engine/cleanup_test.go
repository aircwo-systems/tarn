package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// fakeDocker is a minimal Docker Engine API server that records the calls
// the engine makes, so cleanup paths can be tested without a daemon.
type fakeDocker struct {
	mu        sync.Mutex
	listed    []container.Summary
	listQuery filters.Args
	created   container.Config
	host      container.HostConfig
	stopped   []string
	removed   []string

	// logLines are "<RFC3339Nano timestamp> <message>\n" entries served by
	// the logs endpoint; logSince records each request's since parameter.
	logLines []string
	logSince []string
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
		var req container.CreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Config != nil {
			f.created = *req.Config
		}
		if req.HostConfig != nil {
			f.host = *req.HostConfig
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"created-id"}`))
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/logs"):
		since := r.URL.Query().Get("since")
		f.logSince = append(f.logSince, since)
		out := stdcopy.NewStdWriter(w, stdcopy.Stdout)
		for _, line := range f.logLines {
			stamp, _, _ := strings.Cut(line, " ")
			ts, _ := time.Parse(time.RFC3339Nano, stamp)
			// Docker's since filter is inclusive.
			if since != "" && ts.Before(parseDockerSince(since)) {
				continue
			}
			_, _ = out.Write([]byte(line))
		}
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

// parseDockerSince parses the "<seconds>.<nanoseconds>" form the Docker client
// sends for LogsOptions.Since.
func parseDockerSince(v string) time.Time {
	sec, nsec, _ := strings.Cut(v, ".")
	s, _ := strconv.ParseInt(sec, 10, 64)
	n, _ := strconv.ParseInt(nsec, 10, 64)
	return time.Unix(s, n)
}

func (f *fakeDocker) removedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.removed)
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
	eng.containers[poolKey("111111111111", "orders")] = []*ContainerInfo{{ID: "cold-start", FunctionName: "orders"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // invoking client disconnected mid cold start

	if err := eng.RemoveContainer(ctx, "cold-start"); err != nil {
		t.Fatal(err)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "cold-start" {
		t.Fatalf("container untracked but not removed from Docker: removed=%v", fake.removed)
	}
	if eng.CountContainers("111111111111", "orders") != 0 {
		t.Fatal("container still tracked")
	}
}

func TestCleanupRemovesEveryPool(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	eng.containers[poolKey("111111111111", "orders")] = []*ContainerInfo{{ID: "a"}, {ID: "b", Busy: true}}
	eng.containers[poolKey("222222222222", "billing")] = []*ContainerInfo{{ID: "c"}}

	eng.Cleanup(context.Background())

	// Lambda containers hold no state, so they are force-removed without a
	// graceful stop that the RIE would only time out on.
	if len(fake.removed) != 3 {
		t.Fatalf("removed=%v, want all 3", fake.removed)
	}
	if len(eng.containers) != 0 {
		t.Fatalf("pools still tracked: %v", eng.containers)
	}
}

func TestEvictAccountRemovesOnlyThatAccount(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	eng.containers[poolKey("111111111111", "orders")] = []*ContainerInfo{{ID: "a"}, {ID: "b"}}
	eng.containers[poolKey("111111111111", "billing")] = []*ContainerInfo{{ID: "c"}}
	eng.containers[poolKey("222222222222", "orders")] = []*ContainerInfo{{ID: "keep"}}

	if n := eng.EvictAccount("111111111111"); n != 3 {
		t.Fatalf("evicted %d, want 3", n)
	}
	if eng.CountContainers("222222222222", "orders") != 1 || len(eng.containers) != 1 {
		t.Fatalf("other account's pool touched: %v", eng.containers)
	}
	deadline := time.Now().Add(2 * time.Second)
	for fake.removedCount() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := fake.removedCount(); got != 3 {
		t.Fatalf("removed %d containers from Docker, want 3", got)
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

func TestCreateContainerCapsDockerLogFiles(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	fn := &types.FunctionConfig{FunctionName: "orders", Runtime: types.RuntimeNodeJS20, Handler: "index.handler", MemorySize: 128}

	if _, err := eng.CreateContainer(context.Background(), fn, t.TempDir(), nil, "111111111111"); err != nil {
		t.Fatal(err)
	}
	cfg := fake.host.LogConfig
	if cfg.Type != "json-file" || cfg.Config["max-size"] == "" || cfg.Config["max-file"] == "" {
		t.Fatalf("Lambda container log files must be capped, got %+v", cfg)
	}
}

func TestContainerLogsSinceReadsOnlyNewOutput(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	fake.logLines = []string{
		"2026-09-26T10:00:00.000000001Z START\n",
		"2026-09-26T10:00:00.000000002Z first invoke\n",
	}
	ctx := context.Background()

	text, last, err := eng.ContainerLogsSince(ctx, "c1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if text != "START\nfirst invoke\n" {
		t.Fatalf("first read = %q", text)
	}
	if fake.logSince[0] != "" {
		t.Fatalf("first read should not filter, sent since=%q", fake.logSince[0])
	}

	fake.logLines = append(fake.logLines, "2026-09-26T10:00:01.5Z second invoke\n")
	text, last, err = eng.ContainerLogsSince(ctx, "c1", last)
	if err != nil {
		t.Fatal(err)
	}
	if text != "second invoke\n" {
		t.Fatalf("second read = %q, want only the new line (the boundary line must not repeat)", text)
	}
	if fake.logSince[1] == "" {
		t.Fatal("second read must ask Docker for output since the last line")
	}

	text, _, err = eng.ContainerLogsSince(ctx, "c1", last)
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Fatalf("read with no new output = %q, want empty", text)
	}
}

func TestContainerLogsSinceJoinsPartialEntriesOfLongLines(t *testing.T) {
	eng, fake := newFakeEngine(t, 4566)
	// Docker's json-file driver splits lines over 16 KB into partial entries,
	// each carrying its own timestamp; only the last ends in a newline.
	head := strings.Repeat("a", 16*1024)
	fake.logLines = []string{
		"2026-09-26T10:00:00.000000001Z " + head,
		"2026-09-26T10:00:00.000000002Z tailEND\n",
	}

	text, _, err := eng.ContainerLogsSince(context.Background(), "c1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if text != head+"tailEND\n" {
		t.Fatalf("long line not rejoined cleanly: len=%d, suffix %q", len(text), text[max(0, len(text)-60):])
	}
}
