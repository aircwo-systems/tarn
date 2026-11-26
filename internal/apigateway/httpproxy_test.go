package apigateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/reqlog"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// echoRequest is what the echo upstream saw.
type echoRequest struct {
	Method     string
	RequestURI string
	Host       string
	Header     http.Header
	Body       []byte
}

type echoUpstream struct {
	mu       sync.Mutex
	requests []echoRequest
	calls    atomic.Int32
	respond  func(w http.ResponseWriter, r *http.Request)
}

func (e *echoUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.calls.Add(1)
	body, _ := io.ReadAll(r.Body)
	e.mu.Lock()
	e.requests = append(e.requests, echoRequest{Method: r.Method, RequestURI: r.RequestURI, Host: r.Host, Header: r.Header.Clone(), Body: body})
	e.mu.Unlock()
	if e.respond != nil {
		e.respond(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (e *echoUpstream) last(t *testing.T) echoRequest {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.requests) == 0 {
		t.Fatal("upstream saw no request")
	}
	return e.requests[len(e.requests)-1]
}

func newHTTPProxyAPI(t *testing.T, svc *Service, routeKey string, input IntegrationCreateInput) (*types.APIGatewayAPI, *types.APIGatewayIntegration) {
	t.Helper()
	api, err := svc.CreateAPI("gw", "", "HTTP", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	input.IntegrationType = integrationTypeHTTPProxy
	integration, err := svc.CreateIntegration(api.APIID, input)
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	if _, err := svc.CreateRoute(api.APIID, RouteCreateInput{RouteKey: routeKey, Target: "integrations/" + integration.IntegrationID}); err != nil {
		t.Fatal(err)
	}
	return api, integration
}

func invokePath(t *testing.T, svc *Service, apiID, method, target string, header http.Header, body string) *InvokeOutput {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	if header == nil {
		header = http.Header{}
	}
	out, err := svc.Invoke(t.Context(), &InvokeInput{
		APIID: apiID, Stage: "$default", Method: method, Path: u.Path, RawQuery: u.RawQuery,
		Query: u.Query(), Headers: header, RemoteAddr: "203.0.113.9:5555", Body: []byte(body),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	return out
}

func TestCreateHTTPProxyIntegrationRoundTrip(t *testing.T) {
	svc := newTestService(t)
	api, integration := newHTTPProxyAPI(t, svc, "ANY /admin/{proxy+}", IntegrationCreateInput{
		IntegrationURI:    "http://127.0.0.1:9/_ecs/000000000000/admin/identity/admin/{proxy}",
		IntegrationMethod: "any",
		TimeoutInMillis:   5000,
		RequestParameters: map[string]string{"overwrite:header.x-api-key": "gateway-key"},
	})
	got, err := svc.GetIntegration(api.APIID, integration.IntegrationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IntegrationType != "HTTP_PROXY" || got.IntegrationMethod != "ANY" || got.ConnectionType != "INTERNET" ||
		got.PayloadFormatVersion != "1.0" || got.TimeoutInMillis != 5000 || got.RequestParameters["overwrite:header.x-api-key"] != "gateway-key" {
		t.Fatalf("GetIntegration = %+v", got)
	}

	newURI := "https://backend.example/v2/{proxy}"
	method := "POST"
	timeout := 800
	updated, err := svc.UpdateIntegration(api.APIID, integration.IntegrationID, IntegrationUpdateInput{
		IntegrationURI: &newURI, IntegrationMethod: &method, TimeoutInMillis: &timeout,
		RequestParameters: map[string]string{"append:querystring.src": "gw"},
	})
	if err != nil {
		t.Fatalf("UpdateIntegration: %v", err)
	}
	// An update merges: mappings it leaves out are kept.
	if updated.IntegrationURI != newURI || updated.IntegrationMethod != "POST" || updated.TimeoutInMillis != 800 ||
		len(updated.RequestParameters) != 2 || updated.RequestParameters["append:querystring.src"] != "gw" ||
		updated.RequestParameters["overwrite:header.x-api-key"] != "gateway-key" {
		t.Fatalf("UpdateIntegration = %+v", updated)
	}

	// An empty value deletes a mapping, as AWS documents and Terraform sends.
	// It cannot leave an empty static overwrite behind.
	updated, err = svc.UpdateIntegration(api.APIID, integration.IntegrationID, IntegrationUpdateInput{
		RequestParameters: map[string]string{
			"append:querystring.src": "", "overwrite:header.x-api-key": "",
			"overwrite:header.x-a": "1", "remove:header.x-b": "",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.RequestParameters) != 2 || updated.RequestParameters["overwrite:header.x-a"] != "1" {
		t.Fatalf("RequestParameters = %v, want the append and x-api-key mappings deleted", updated.RequestParameters)
	}
	if _, ok := updated.RequestParameters["remove:header.x-b"]; !ok {
		t.Fatalf("RequestParameters = %v, want remove:header.x-b kept", updated.RequestParameters)
	}

	for name, in := range map[string]IntegrationUpdateInput{
		"bad uri":         {IntegrationURI: ptr("ftp://x")},
		"bad method":      {IntegrationMethod: ptr("BREW")},
		"payload 2.0":     {PayloadFormatVersion: ptr("2.0")},
		"vpc link":        {ConnectionType: ptr("VPC_LINK")},
		"reserved header": {RequestParameters: map[string]string{"overwrite:header.Authorization": "x"}},
	} {
		if _, err := svc.UpdateIntegration(api.APIID, integration.IntegrationID, in); err == nil {
			t.Errorf("%s: UpdateIntegration accepted it", name)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestCreateHTTPProxyIntegrationValidation(t *testing.T) {
	svc := newTestService(t)
	api, err := svc.CreateAPI("gw", "", "HTTP", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		input IntegrationCreateInput
		want  string
	}{
		{"missing uri", IntegrationCreateInput{}, "IntegrationUri is required"},
		{"relative uri", IntegrationCreateInput{IntegrationURI: "/admin"}, "absolute http"},
		{"non-http uri", IntegrationCreateInput{IntegrationURI: "arn:aws:lambda:us-east-1:000000000000:function:x"}, "absolute http"},
		{"payload 2.0", IntegrationCreateInput{IntegrationURI: "http://x", PayloadFormatVersion: "2.0"}, "1.0 only"},
		{"vpc link", IntegrationCreateInput{IntegrationURI: "http://x", ConnectionType: "VPC_LINK"}, "VPC_LINK is not supported"},
		{"bad method", IntegrationCreateInput{IntegrationURI: "http://x", IntegrationMethod: "BREW"}, "IntegrationMethod"},
		{"reserved header", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"overwrite:header.X-Forwarded-For": "1"}}, "reserved"},
		{"reserved prefix", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"append:header.x-amzn-trace": "1"}}, "reserved"},
		{"body source", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"overwrite:header.x-id": "$request.body.id"}}, "unsupported source"},
		{"stage variable", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"overwrite:header.x-id": "$stageVariables.key"}}, "unsupported source"},
		{"other context", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"overwrite:header.x-id": "$context.identity.sourceIp"}}, "unsupported source"},
		{"mixed value", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"overwrite:header.x-id": "Bearer $request.header.token"}}, "mixes text"},
		{"overwrite and remove", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"overwrite:header.X-Api-Key": "k", "remove:header.x-api-key": ""}}, "both overwrite and remove"},
		{"bad action", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"set:header.x": "k"}}, "unknown action"},
		{"remove path", IntegrationCreateInput{IntegrationURI: "http://x", RequestParameters: map[string]string{"remove:path": ""}}, "only be overwritten"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.input.IntegrationType = integrationTypeHTTPProxy
			_, err := svc.CreateIntegration(api.APIID, tc.input)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestHTTPProxyForwardsRequest(t *testing.T) {
	upstream := &echoUpstream{respond: func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "a=1; Path=/")
		w.Header().Add("Set-Cookie", "b=2; Path=/")
		w.Header().Set("X-Upstream", "yes")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`))
	}}
	backend := httptest.NewServer(upstream)
	defer backend.Close()

	svc := newTestService(t)
	api, _ := newHTTPProxyAPI(t, svc, "ANY /admin/{proxy+}", IntegrationCreateInput{
		IntegrationURI:    backend.URL + "/admin/{proxy}",
		IntegrationMethod: "ANY",
		RequestParameters: map[string]string{"overwrite:header.x-api-key": "gateway-key"},
	})

	header := http.Header{}
	header.Set("Authorization", "Bearer t")
	header.Set("X-Api-Key", "wrong")
	header.Set("Connection", "keep-alive, X-Hop")
	header.Set("X-Hop", "drop-me")
	header.Set("Keep-Alive", "timeout=5")
	header.Set("Content-Type", "application/json")
	body := `{"name":"smith","n":[1,2,3]}`
	out := invokePath(t, svc, api.APIID, http.MethodPost, "/admin/users/42/roles?q=smith&q=jones&z=%2F", header, body)

	if out.StatusCode != http.StatusCreated || string(out.Body) != `{"created":true}` {
		t.Fatalf("response = %d %s", out.StatusCode, out.Body)
	}
	if cookies := out.Headers.Values("Set-Cookie"); len(cookies) != 2 || cookies[0] != "a=1; Path=/" || cookies[1] != "b=2; Path=/" {
		t.Fatalf("Set-Cookie = %q, want two headers", cookies)
	}
	if out.Headers.Get("X-Upstream") != "yes" {
		t.Fatalf("response headers = %v", out.Headers)
	}

	got := upstream.last(t)
	if got.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", got.Method)
	}
	if got.RequestURI != "/admin/users/42/roles?q=smith&q=jones&z=%2F" {
		t.Errorf("request URI = %q", got.RequestURI)
	}
	if string(got.Body) != body {
		t.Errorf("body = %q", got.Body)
	}
	if got.Host != strings.TrimPrefix(backend.URL, "http://") {
		t.Errorf("Host = %q, want the target's", got.Host)
	}
	if got.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("Authorization = %q", got.Header.Get("Authorization"))
	}
	if keys := got.Header.Values("X-Api-Key"); len(keys) != 1 || keys[0] != "gateway-key" {
		t.Errorf("x-api-key = %q, want exactly the gateway's", keys)
	}
	for _, h := range []string{"X-Hop", "Keep-Alive"} {
		if got.Header.Get(h) != "" {
			t.Errorf("hop-by-hop header %s forwarded", h)
		}
	}
	if got.Header.Get("X-Forwarded-For") != "203.0.113.9" || got.Header.Get("X-Forwarded-Proto") != "http" {
		t.Errorf("forwarded headers = %q %q", got.Header.Get("X-Forwarded-For"), got.Header.Get("X-Forwarded-Proto"))
	}
	if got.Header.Get("X-Correlation-Id") == "" {
		t.Error("no X-Correlation-Id sent")
	}
	if upstream.calls.Load() != 1 {
		t.Errorf("upstream calls = %d", upstream.calls.Load())
	}
}

func TestHTTPProxyMethodAndPath(t *testing.T) {
	upstream := &echoUpstream{}
	backend := httptest.NewServer(upstream)
	defer backend.Close()
	svc := newTestService(t)

	t.Run("fixed method", func(t *testing.T) {
		api, _ := newHTTPProxyAPI(t, svc, "ANY /items", IntegrationCreateInput{IntegrationURI: backend.URL + "/fixed", IntegrationMethod: "POST"})
		invokePath(t, svc, api.APIID, http.MethodGet, "/items?x=1", nil, "")
		if got := upstream.last(t); got.Method != http.MethodPost || got.RequestURI != "/fixed?x=1" {
			t.Fatalf("got %s %s, want POST /fixed?x=1", got.Method, got.RequestURI)
		}
	})
	t.Run("any method", func(t *testing.T) {
		api, _ := newHTTPProxyAPI(t, svc, "ANY /items", IntegrationCreateInput{IntegrationURI: backend.URL + "/any"})
		invokePath(t, svc, api.APIID, http.MethodDelete, "/items", nil, "")
		if got := upstream.last(t); got.Method != http.MethodDelete || got.RequestURI != "/any" {
			t.Fatalf("got %s %s, want DELETE /any", got.Method, got.RequestURI)
		}
	})
	t.Run("no path variable keeps the URI path", func(t *testing.T) {
		api, _ := newHTTPProxyAPI(t, svc, "GET /a/{id}", IntegrationCreateInput{IntegrationURI: backend.URL + "/static?k=v"})
		invokePath(t, svc, api.APIID, http.MethodGet, "/a/7?q=1", nil, "")
		if got := upstream.last(t); got.RequestURI != "/static?k=v&q=1" {
			t.Fatalf("request URI = %q", got.RequestURI)
		}
	})
	t.Run("default route has no path parameters", func(t *testing.T) {
		api, _ := newHTTPProxyAPI(t, svc, "$default", IntegrationCreateInput{IntegrationURI: backend.URL + "/base/{proxy}"})
		invokePath(t, svc, api.APIID, http.MethodGet, "/anything", nil, "")
		if got := upstream.last(t); got.RequestURI != "/base/" {
			t.Fatalf("request URI = %q, want /base/", got.RequestURI)
		}
	})
	t.Run("named variable escaped", func(t *testing.T) {
		api, _ := newHTTPProxyAPI(t, svc, "GET /users/{id}", IntegrationCreateInput{IntegrationURI: backend.URL + "/u/{id}/x"})
		invokePath(t, svc, api.APIID, http.MethodGet, "/users/a b", nil, "")
		if got := upstream.last(t); got.RequestURI != "/u/a%20b/x" {
			t.Fatalf("request URI = %q", got.RequestURI)
		}
	})
}

func TestHTTPProxyMappings(t *testing.T) {
	upstream := &echoUpstream{}
	backend := httptest.NewServer(upstream)
	defer backend.Close()
	svc := newTestService(t)
	api, _ := newHTTPProxyAPI(t, svc, "GET /orders/{id}", IntegrationCreateInput{
		IntegrationURI: backend.URL + "/orders",
		RequestParameters: map[string]string{
			"overwrite:header.X-Tenant":     "$request.header.x-org",
			"append:header.X-Multi":         "added",
			"remove:header.X-Secret":        "",
			"append:header.X-Order":         "$request.path.id",
			"overwrite:header.X-Api":        "$context.apiId",
			"overwrite:header.X-Stage":      "$context.stage",
			"overwrite:header.X-Request-Id": "$context.requestId",
			"overwrite:header.X-Q":          "$request.querystring.q",
			"overwrite:header.X-Path":       "$request.path",
			"overwrite:querystring.page":    "1",
			"append:querystring.tag":        "gw",
			"remove:querystring.debug":      "",
			"overwrite:path":                "/v2/orders/view",
		},
	})
	header := http.Header{}
	header.Set("X-Org", "acme")
	header.Set("X-Tenant", "spoofed")
	header.Add("X-Multi", "inbound")
	header.Set("X-Secret", "s3cret")
	header.Set("X-Correlation-Id", "corr-1")
	invokePath(t, svc, api.APIID, http.MethodGet, "/orders/42?q=hello&page=9&page=8&debug=1&tag=a", header, "")

	got := upstream.last(t)
	for name, want := range map[string]string{
		"X-Tenant": "acme", "X-Order": "42", "X-Api": api.APIID, "X-Stage": "$default",
		"X-Request-Id": "corr-1", "X-Q": "hello", "X-Path": "/orders/42",
	} {
		if v := got.Header.Values(name); len(v) != 1 || v[0] != want {
			t.Errorf("%s = %q, want [%q]", name, v, want)
		}
	}
	if v := got.Header.Values("X-Multi"); len(v) != 2 || v[0] != "inbound" || v[1] != "added" {
		t.Errorf("X-Multi = %q, want inbound then added", v)
	}
	if got.Header.Get("X-Secret") != "" {
		t.Error("X-Secret not removed")
	}
	if got.RequestURI != "/v2/orders/view?q=hello&tag=a&page=1&tag=gw" {
		t.Errorf("request URI = %q", got.RequestURI)
	}
}

func TestHTTPProxyTimeoutAndUnreachable(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		upstream := &echoUpstream{respond: func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-time.After(5 * time.Second):
			}
		}}
		backend := httptest.NewServer(upstream)
		defer backend.Close()
		defer close(release)

		svc := newTestService(t)
		api, _ := newHTTPProxyAPI(t, svc, "GET /slow", IntegrationCreateInput{IntegrationURI: backend.URL + "/slow", TimeoutInMillis: 300})
		start := time.Now()
		out := invokePath(t, svc, api.APIID, http.MethodGet, "/slow", nil, "")
		elapsed := time.Since(start)
		if out.StatusCode != http.StatusGatewayTimeout {
			t.Fatalf("status = %d, want 504", out.StatusCode)
		}
		if elapsed < 300*time.Millisecond || elapsed > 330*time.Millisecond+50*time.Millisecond {
			t.Errorf("took %s, want about 300ms", elapsed)
		}
		assertJSONMessage(t, out)
		if n := upstream.calls.Load(); n != 1 {
			t.Errorf("upstream calls = %d, want 1", n)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		ln.Close()

		svc := newTestService(t)
		api, _ := newHTTPProxyAPI(t, svc, "GET /down", IntegrationCreateInput{IntegrationURI: "http://" + addr + "/down"})
		ctx, fields := reqlog.WithFields(context.Background())
		out, err := svc.Invoke(ctx, &InvokeInput{APIID: api.APIID, Stage: "$default", Method: http.MethodGet, Path: "/down", Headers: http.Header{}})
		if err != nil {
			t.Fatal(err)
		}
		if out.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", out.StatusCode)
		}
		assertJSONMessage(t, out)
		if line := fields.String(); !strings.Contains(line, `cause="connection refused"`) {
			t.Errorf("log fields = %s", line)
		}
	})

	t.Run("request too large", func(t *testing.T) {
		upstream := &echoUpstream{}
		backend := httptest.NewServer(upstream)
		defer backend.Close()
		svc := newTestService(t)
		api, _ := newHTTPProxyAPI(t, svc, "POST /up", IntegrationCreateInput{IntegrationURI: backend.URL})
		out := invokePath(t, svc, api.APIID, http.MethodPost, "/up", nil, strings.Repeat("x", maxHTTPProxyPayload+1))
		if out.StatusCode != http.StatusRequestEntityTooLarge || upstream.calls.Load() != 0 {
			t.Fatalf("status = %d, upstream calls = %d", out.StatusCode, upstream.calls.Load())
		}
	})

	t.Run("response too large", func(t *testing.T) {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxHTTPProxyPayload+1)))
		}))
		defer backend.Close()
		svc := newTestService(t)
		api, _ := newHTTPProxyAPI(t, svc, "GET /big", IntegrationCreateInput{IntegrationURI: backend.URL})
		if out := invokePath(t, svc, api.APIID, http.MethodGet, "/big", nil, ""); out.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", out.StatusCode)
		}
	})
}

func assertJSONMessage(t *testing.T, out *InvokeOutput) {
	t.Helper()
	var body struct{ Message string }
	if err := json.Unmarshal(out.Body, &body); err != nil || body.Message == "" || out.Headers.Get("Content-Type") != "application/json" {
		t.Fatalf("body %q (Content-Type %q) is not a JSON message", out.Body, out.Headers.Get("Content-Type"))
	}
}

func TestHTTPProxyPassesCompressedBodiesThrough(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write([]byte("not-really-gzip"))
	}))
	defer backend.Close()
	svc := newTestService(t)
	api, _ := newHTTPProxyAPI(t, svc, "GET /z", IntegrationCreateInput{IntegrationURI: backend.URL})
	out := invokePath(t, svc, api.APIID, http.MethodGet, "/z", nil, "")
	if out.Headers.Get("Content-Encoding") != "gzip" || string(out.Body) != "not-really-gzip" {
		t.Fatalf("headers %v body %q", out.Headers, out.Body)
	}
}

func TestHTTPProxyDoesNotFollowRedirects(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer backend.Close()
	svc := newTestService(t)
	api, _ := newHTTPProxyAPI(t, svc, "GET /r", IntegrationCreateInput{IntegrationURI: backend.URL + "/r"})
	out := invokePath(t, svc, api.APIID, http.MethodGet, "/r", nil, "")
	if out.StatusCode != http.StatusFound || out.Headers.Get("Location") != "/elsewhere" {
		t.Fatalf("status %d Location %q", out.StatusCode, out.Headers.Get("Location"))
	}
}

// unboundPort returns a port nothing listens on, so a request that succeeds
// against it cannot have used a socket.
func unboundPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestHTTPProxyLocalDispatch(t *testing.T) {
	port := unboundPort(t)
	cfg := config.Default()
	cfg.Port = port

	var seen atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/_ecs/{account}/{cluster}/{service}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Method + " " + r.URL.RequestURI() + " key=" + r.Header.Get("X-Api-Key"))
		if r.PathValue("service") == "scaled-to-zero" {
			http.Error(w, "no running task", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-Service", r.PathValue("service"))
		_, _ = w.Write([]byte("hello from " + r.PathValue("rest")))
	})
	dispatcher := NewLocalDispatcher(cfg)
	dispatcher.SetHandler(mux)

	for _, host := range []string{"127.0.0.1", "localhost", "host.docker.internal"} {
		t.Run(host, func(t *testing.T) {
			svc := newTestService(t)
			svc.SetLocalDispatcher(dispatcher)
			traces := tracesvc.NewStore()
			svc.SetTraceStore(traces)
			api, _ := newHTTPProxyAPI(t, svc, "ANY /admin/{proxy+}", IntegrationCreateInput{
				IntegrationURI:    "http://" + host + ":" + strconv.Itoa(port) + "/_ecs/000000000000/admin/identity/admin/{proxy}",
				RequestParameters: map[string]string{"overwrite:header.x-api-key": "gateway-key"},
			})
			out := invokePath(t, svc, api.APIID, http.MethodGet, "/admin/users?q=a&q=b", nil, "")
			if out.StatusCode != http.StatusOK || string(out.Body) != "hello from admin/users" || out.Headers.Get("X-Service") != "identity" {
				t.Fatalf("response %d %q %v", out.StatusCode, out.Body, out.Headers)
			}
			if got := seen.Load(); got != "GET /_ecs/000000000000/admin/identity/admin/users?q=a&q=b key=gateway-key" {
				t.Fatalf("handler saw %q", got)
			}
			recent := traces.Recent(1)
			if len(recent) != 1 || len(recent[0].Spans) == 0 {
				t.Fatalf("traces = %+v", recent)
			}
			span := recent[0].Spans[len(recent[0].Spans)-1]
			if span.Kind != "ecs" || span.Name != "identity" {
				t.Fatalf("span = %+v, want ecs identity", span)
			}
		})
	}

	t.Run("upstream errors pass through", func(t *testing.T) {
		svc := newTestService(t)
		svc.SetLocalDispatcher(dispatcher)
		api, _ := newHTTPProxyAPI(t, svc, "ANY /{proxy+}", IntegrationCreateInput{
			IntegrationURI: "http://127.0.0.1:" + strconv.Itoa(port) + "/_ecs/000000000000/admin/scaled-to-zero/{proxy}",
		})
		if out := invokePath(t, svc, api.APIID, http.MethodGet, "/x", nil, ""); out.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want the proxy's 503", out.StatusCode)
		}
	})

	t.Run("other port is not local", func(t *testing.T) {
		svc := newTestService(t)
		svc.SetLocalDispatcher(dispatcher)
		api, _ := newHTTPProxyAPI(t, svc, "GET /x", IntegrationCreateInput{
			IntegrationURI: "http://127.0.0.1:" + strconv.Itoa(unboundPort(t)) + "/_ecs/000000000000/admin/identity/",
		})
		if out := invokePath(t, svc, api.APIID, http.MethodGet, "/x", nil, ""); out.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 from the closed port", out.StatusCode)
		}
	})
}

func TestLocalDispatchTimeoutAndLoop(t *testing.T) {
	port := unboundPort(t)
	cfg := config.Default()
	cfg.Port = port
	dispatcher := NewLocalDispatcher(cfg)

	calls := atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	})
	svc := newTestService(t)
	svc.SetLocalDispatcher(dispatcher)
	// The loop route sends the gateway's request back to the gateway itself.
	mux.HandleFunc("/_apigateway/{apiId}/{stage}/{proxy...}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		out, err := svc.Invoke(r.Context(), &InvokeInput{APIID: r.PathValue("apiId"), Stage: r.PathValue("stage"), Method: r.Method, Path: "/" + r.PathValue("proxy"), Headers: r.Header, Body: body})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(out.StatusCode)
		_, _ = w.Write(out.Body)
	})
	dispatcher.SetHandler(mux)

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	api, _ := newHTTPProxyAPI(t, svc, "GET /slow", IntegrationCreateInput{IntegrationURI: base + "/slow", TimeoutInMillis: 200})
	start := time.Now()
	out := invokePath(t, svc, api.APIID, http.MethodGet, "/slow", nil, "")
	if out.StatusCode != http.StatusGatewayTimeout || time.Since(start) > time.Second {
		t.Fatalf("status = %d after %s, want 504 after about 200ms", out.StatusCode, time.Since(start))
	}
	assertJSONMessage(t, out)
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d", calls.Load())
	}

	loopAPI, err := svc.CreateAPI("loop", "", "HTTP", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	integration, err := svc.CreateIntegration(loopAPI.APIID, IntegrationCreateInput{
		IntegrationType: integrationTypeHTTPProxy,
		IntegrationURI:  base + "/_apigateway/" + loopAPI.APIID + "/$default/loop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRoute(loopAPI.APIID, RouteCreateInput{RouteKey: "GET /loop", Target: "integrations/" + integration.IntegrationID}); err != nil {
		t.Fatal(err)
	}
	if out := invokePath(t, svc, loopAPI.APIID, http.MethodGet, "/loop", nil, ""); out.StatusCode != http.StatusBadGateway {
		t.Fatalf("loop status = %d, want 502", out.StatusCode)
	}
}

func TestHTTPProxyPersistsAcrossRestart(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	svc := NewService(cfg, testLambda{}, nil)
	if err := svc.Init(); err != nil {
		t.Fatal(err)
	}
	api, integration := newHTTPProxyAPI(t, svc, "ANY /{proxy+}", IntegrationCreateInput{
		IntegrationURI:    "http://127.0.0.1:4566/_ecs/000000000000/c/s/{proxy}",
		IntegrationMethod: "GET",
		TimeoutInMillis:   1234,
		RequestParameters: map[string]string{"overwrite:header.x-api-key": "k"},
	})
	svc.Close()

	reloaded := NewService(cfg, testLambda{}, nil)
	if err := reloaded.Init(); err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	got, err := reloaded.GetIntegration(api.APIID, integration.IntegrationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IntegrationType != "HTTP_PROXY" || got.IntegrationMethod != "GET" || got.ConnectionType != "INTERNET" ||
		got.TimeoutInMillis != 1234 || got.IntegrationURI != integration.IntegrationURI || got.RequestParameters["overwrite:header.x-api-key"] != "k" {
		t.Fatalf("after restart = %+v", got)
	}
}

func TestInvokeRejectsUnknownIntegrationType(t *testing.T) {
	svc := newTestService(t)
	api, integration := newHTTPProxyAPI(t, svc, "GET /x", IntegrationCreateInput{IntegrationURI: "http://127.0.0.1:9/"})
	integration.IntegrationType = "MOCK"
	if err := svc.store.SaveIntegration(api.APIID, integration); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(t.Context(), &InvokeInput{APIID: api.APIID, Stage: "$default", Method: http.MethodGet, Path: "/x"}); err == nil || !strings.Contains(err.Error(), "unsupported integration type") {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPProxyURIVariablesOnlyInPath(t *testing.T) {
	svc := newTestService(t)
	api, err := svc.CreateAPI("gw", "", "HTTP", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{"http://{host}/x", "http://api.{env}.example/x", "http://x/a?id={id}", "http://x/a#{f}"} {
		_, err := svc.CreateIntegration(api.APIID, IntegrationCreateInput{IntegrationType: integrationTypeHTTPProxy, IntegrationURI: uri})
		if err == nil || !strings.Contains(err.Error(), "only in the URL's path") {
			t.Errorf("%s: err = %v, want a path-only error", uri, err)
		}
	}
	existing, err := svc.CreateIntegration(api.APIID, IntegrationCreateInput{IntegrationType: integrationTypeHTTPProxy, IntegrationURI: "http://x/{id}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateIntegration(api.APIID, existing.IntegrationID, IntegrationUpdateInput{IntegrationURI: ptr("http://{id}/")}); err == nil {
		t.Error("UpdateIntegration accepted a variable in the host")
	}

	// A captured value is escaped, so it cannot add a query or fragment.
	upstream := &echoUpstream{}
	backend := httptest.NewServer(upstream)
	defer backend.Close()
	gw, _ := newHTTPProxyAPI(t, svc, "GET /u/{id}", IntegrationCreateInput{IntegrationURI: backend.URL + "/u/{id}?fixed=1"})
	// The handler passes the decoded path, so a caller's /u/a%3Fb%23c arrives as /u/a?b#c.
	if _, err := svc.Invoke(t.Context(), &InvokeInput{APIID: gw.APIID, Stage: "$default", Method: http.MethodGet, Path: "/u/a?b#c", Headers: http.Header{}}); err != nil {
		t.Fatal(err)
	}
	if got := upstream.last(t); got.RequestURI != "/u/a%3Fb%23c?fixed=1" {
		t.Fatalf("request URI = %q", got.RequestURI)
	}
}

func TestHTTPProxyQueryMappingKeepsUntouchedPairs(t *testing.T) {
	upstream := &echoUpstream{}
	backend := httptest.NewServer(upstream)
	defer backend.Close()
	svc := newTestService(t)
	api, _ := newHTTPProxyAPI(t, svc, "GET /q", IntegrationCreateInput{
		IntegrationURI:    backend.URL + "/q",
		RequestParameters: map[string]string{"remove:querystring.debug": "", "append:querystring.src": "a b"},
	})
	header := http.Header{}
	u, _ := url.Parse("/q?flag&z=%2F&debug=1&sp=a+b&x=%7e")
	if _, err := svc.Invoke(t.Context(), &InvokeInput{APIID: api.APIID, Stage: "$default", Method: http.MethodGet, Path: u.Path, RawQuery: u.RawQuery, Query: u.Query(), Headers: header}); err != nil {
		t.Fatal(err)
	}
	if got := upstream.last(t); got.RequestURI != "/q?flag&z=%2F&sp=a+b&x=%7e&src=a+b" {
		t.Fatalf("request URI = %q", got.RequestURI)
	}
}

func TestLocalDispatchDeadlineCoversBody(t *testing.T) {
	cfg := config.Default()
	cfg.Port = unboundPort(t)
	release := make(chan struct{})
	defer close(release)
	dispatcher := NewLocalDispatcher(cfg)
	dispatcher.SetHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	svc := newTestService(t)
	svc.SetLocalDispatcher(dispatcher)
	api, _ := newHTTPProxyAPI(t, svc, "GET /slow", IntegrationCreateInput{
		IntegrationURI:  "http://127.0.0.1:" + strconv.Itoa(cfg.Port) + "/slow",
		TimeoutInMillis: 50,
	})
	start := time.Now()
	out := invokePath(t, svc, api.APIID, http.MethodGet, "/slow", nil, "")
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("took %s with a 50ms timeout", elapsed)
	}
	if out.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", out.StatusCode)
	}
}
