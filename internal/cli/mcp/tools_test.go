package mcp

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// call invokes a tool and decodes its structured result into out.
func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: CallTool failed: %v", name, err)
	}
	if res.IsError {
		var text string
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				text = tc.Text
			}
		}
		t.Fatalf("%s returned an error result: %s", name, text)
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("%s: failed to remarshal result: %v", name, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: failed to decode result: %v", name, err)
	}
}

// recordedRequest captures what a tool sent to the instance.
type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   []byte
	Auth   string
}

// instance is a scriptable stand-in for a running Tarn, recording requests so
// tests can assert on what a tool actually asked for.
type instance struct {
	t        *testing.T
	server   *httptest.Server
	requests []recordedRequest
	routes   map[string]http.HandlerFunc
}

func newInstance(t *testing.T) *instance {
	t.Helper()

	inst := &instance{t: t, routes: map[string]http.HandlerFunc{}}
	inst.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		inst.requests = append(inst.requests, recordedRequest{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			Body: body, Auth: r.Header.Get("Authorization"),
		})
		r.Body = io.NopCloser(bytes.NewReader(body))

		if h, ok := inst.routes[r.Method+" "+r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(inst.server.Close)
	return inst
}

func (i *instance) handle(methodPath string, h http.HandlerFunc) *instance {
	i.routes[methodPath] = h
	return i
}

func (i *instance) json(methodPath string, payload any) *instance {
	return i.handle(methodPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})
}

func (i *instance) session() *mcp.ClientSession { return connect(i.t, i.server.URL) }

func (i *instance) lastRequest() recordedRequest {
	i.t.Helper()
	if len(i.requests) == 0 {
		i.t.Fatal("no requests were recorded")
	}
	return i.requests[len(i.requests)-1]
}

// TestInvokeReportsFailureWithoutHeader mirrors what a real runtime does:
// a thrown handler comes back as HTTP 200 with the error envelope in the body
// and, on some builds, no X-Amz-Function-Error header. A model reading only the
// status code would call this a success.
func TestInvokeReportsFailureWithoutHeader(t *testing.T) {
	inst := newInstance(t).handle("POST /2015-03-31/functions/order-processor/invocations",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"errorType":"TypeError","errorMessage":"Cannot read properties of undefined (reading 'reduce')","trace":["TypeError: Cannot read properties of undefined","    at exports.handler (/var/task/index.js:3:29)"]}`)
		})

	var out InvokeOutput
	call(t, inst.session(), "tarn_invoke_lambda",
		map[string]any{"name": "order-processor", "payload": `{"orderId":"ord-991"}`}, &out)

	if out.Succeeded {
		t.Fatal("succeeded = true for a handler that threw")
	}
	if out.ErrorType != "TypeError" {
		t.Errorf("errorType = %q, want TypeError", out.ErrorType)
	}
	if len(out.Stack) == 0 || !strings.Contains(strings.Join(out.Stack, "\n"), "index.js:3:29") {
		t.Errorf("stack = %v, want the failing file and line", out.Stack)
	}
	if out.LogGroup != "/aws/lambda/order-processor" {
		t.Errorf("logGroup = %q, want the function's log group so the model can read logs next", out.LogGroup)
	}
	if out.Response != "" {
		t.Errorf("response = %q on a failed invocation, want empty", out.Response)
	}
}

func TestInvokeReportsSuccess(t *testing.T) {
	inst := newInstance(t).handle("POST /2015-03-31/functions/order-processor/invocations",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-amzn-RequestId", "52182d81-5f35-4e5f-9fc2-b492d6a655ff")
			_, _ = io.WriteString(w, `{"statusCode":200,"body":"{\"total\":15}"}`)
		})

	var out InvokeOutput
	call(t, inst.session(), "tarn_invoke_lambda", map[string]any{"name": "order-processor"}, &out)

	if !out.Succeeded {
		t.Fatalf("succeeded = false for a clean return: %+v", out)
	}
	if !strings.Contains(out.Response, "total") {
		t.Errorf("response = %q, want the handler's return value", out.Response)
	}
	if out.RequestID == "" {
		t.Error("requestId not surfaced from the response header")
	}
	if out.ErrorType != "" {
		t.Errorf("errorType = %q on success, want empty", out.ErrorType)
	}
}

// TestInvokeDefaultsPayload covers the common case of a model invoking with no
// event at all.
func TestInvokeDefaultsPayload(t *testing.T) {
	inst := newInstance(t).handle("POST /2015-03-31/functions/f/invocations",
		func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `"ok"`) })

	var out InvokeOutput
	call(t, inst.session(), "tarn_invoke_lambda", map[string]any{"name": "f"}, &out)

	if got := string(inst.lastRequest().Body); got != "{}" {
		t.Errorf("payload sent = %q, want an empty object", got)
	}
}

// TestDeployZipsFiles covers deploying source a model just wrote, without it
// having to build a zip on disk.
func TestDeployZipsFiles(t *testing.T) {
	inst := newInstance(t).
		handle("GET /2015-03-31/functions/order-processor", func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		}).
		json("POST /2015-03-31/functions", map[string]any{
			"FunctionName": "order-processor", "Runtime": "nodejs20.x",
			"State": "Active", "CodeSize": 340,
			"FunctionArn": "arn:aws:lambda:us-east-1:000000000000:function:order-processor",
		})

	source := "exports.handler = async () => ({ ok: true });"
	var out DeployOutput
	call(t, inst.session(), "tarn_deploy_lambda", map[string]any{
		"name":  "order-processor",
		"files": map[string]any{"index.js": source},
	}, &out)

	if out.State != "Active" {
		t.Errorf("state = %q, want Active", out.State)
	}
	if out.LogGroup != "/aws/lambda/order-processor" {
		t.Errorf("logGroup = %q, want the function's log group", out.LogGroup)
	}

	var req struct {
		Runtime string `json:"Runtime"`
		Handler string `json:"Handler"`
		Code    struct {
			ZipFile string `json:"ZipFile"`
		} `json:"Code"`
	}
	if err := json.Unmarshal(inst.lastRequest().Body, &req); err != nil {
		t.Fatalf("failed to decode create request: %v", err)
	}
	if req.Runtime != "nodejs20.x" || req.Handler != "index.handler" {
		t.Errorf("runtime/handler defaults not applied: %+v", req)
	}

	raw, err := base64.StdEncoding.DecodeString(req.Code.ZipFile)
	if err != nil {
		t.Fatalf("code was not base64: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("code was not a zip: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "index.js" {
		t.Fatalf("zip contains %d entries, want just index.js", len(zr.File))
	}
	f, _ := zr.File[0].Open()
	content, _ := io.ReadAll(f)
	if string(content) != source {
		t.Errorf("zipped source = %q, want the supplied text", content)
	}
}

func TestDeployRejectsAmbiguousCode(t *testing.T) {
	inst := newInstance(t)

	res, err := inst.session().CallTool(context.Background(), &mcp.CallToolParams{
		Name: "tarn_deploy_lambda",
		Arguments: map[string]any{
			"name": "f", "files": map[string]any{"index.js": "x"}, "zipPath": "/tmp/f.zip",
		},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !res.IsError {
		t.Fatal("supplying both files and zipPath was accepted")
	}
}

// TestLogsFilterRuntimeNoiseAndOrder covers the measured shape of a single
// invocation's logs: 16 events of which 2 carry signal, served oldest-first by
// default. A thin proxy would hand a model container boot chatter.
func TestLogsFilterRuntimeNoiseAndOrder(t *testing.T) {
	events := []map[string]any{
		{"timestamp": "2026-09-04T21:33:20.502763Z", "message": "04 Sep 2026 [INFO] (rapid) INIT START(type: on-demand)", "level": "INFO", "source": "runtime"},
		{"timestamp": "2026-09-04T21:33:20.502764Z", "message": "START RequestId: d028b611 Version: $LATEST", "level": "INFO", "source": "runtime"},
		{"timestamp": "2026-09-04T21:33:20.492Z", "message": `processing order {"orderId":"ord-991"}`, "level": "INFO", "source": "output"},
		{"timestamp": "2026-09-04T21:33:20.497Z", "message": `Invoke Error {"errorType":"TypeError"}`, "level": "ERROR", "source": "output"},
		{"timestamp": "2026-09-04T21:33:20.505Z", "message": "REPORT RequestId: d028b611\tDuration: 12 ms", "level": "INFO", "source": "runtime"},
		{"timestamp": "2026-09-04T21:33:20.506Z", "message": "2026/09/04 [secrets-proxy] listening on :2773", "level": "INFO", "source": "runtime"},
	}

	inst := newInstance(t).json("GET /_tarn/admin/logs/events//aws/lambda/order-processor",
		map[string]any{"events": events, "total": len(events)})

	var out LogsOutput
	call(t, inst.session(), "tarn_get_logs", map[string]any{"function": "order-processor"}, &out)

	if out.LogGroup != "/aws/lambda/order-processor" {
		t.Errorf("logGroup = %q", out.LogGroup)
	}
	if out.Returned != 2 {
		t.Fatalf("returned %d events, want the 2 the function produced: %+v", out.Returned, out.Events)
	}
	if out.RuntimeFiltered != 4 {
		t.Errorf("runtimeFiltered = %d, want 4", out.RuntimeFiltered)
	}
	if !strings.Contains(out.Events[0].Message, "processing order") {
		t.Errorf("first event = %q, want the earliest one; results should read chronologically", out.Events[0].Message)
	}
	if !strings.Contains(out.Events[1].Message, "Invoke Error") {
		t.Errorf("second event = %q, want the failure", out.Events[1].Message)
	}

	// The tail is what matters when debugging, so the request must not ask for
	// the oldest events, which is what the API returns by default.
	if !strings.Contains(inst.lastRequest().Query, "order=desc") {
		t.Errorf("query = %q, want order=desc so the newest events are fetched", inst.lastRequest().Query)
	}
}

func TestLogsIncludeRuntimeWhenAsked(t *testing.T) {
	events := []map[string]any{
		{"timestamp": "2026-09-04T21:33:20.502Z", "message": "START RequestId: d028b611", "level": "INFO", "source": "runtime"},
		{"timestamp": "2026-09-04T21:33:20.492Z", "message": "hello", "level": "INFO", "source": "output"},
	}
	inst := newInstance(t).json("GET /_tarn/admin/logs/events//aws/lambda/f",
		map[string]any{"events": events, "total": len(events)})

	var out LogsOutput
	call(t, inst.session(), "tarn_get_logs",
		map[string]any{"function": "f", "includeRuntime": true}, &out)

	if out.Returned != 2 {
		t.Fatalf("returned %d events, want both", out.Returned)
	}
	if out.RuntimeFiltered != 0 {
		t.Errorf("runtimeFiltered = %d, want 0", out.RuntimeFiltered)
	}
}

func TestPeekQueueIsReadOnly(t *testing.T) {
	// Shape taken from a real instance: an object with a messages array, not a
	// bare array.
	inst := newInstance(t).json("GET /_tarn/admin/queues/jobs/messages", map[string]any{
		"queue": "jobs",
		"messages": []map[string]any{
			{"id": "m-1", "body": `{"orderId":"ord-1"}`, "state": "visible", "receiveCount": 3},
		},
	})

	var out PeekQueueOutput
	call(t, inst.session(), "tarn_peek_queue", map[string]any{"queue": "jobs"}, &out)

	if out.Returned != 1 || out.Messages[0].ID != "m-1" {
		t.Fatalf("messages = %+v", out.Messages)
	}
	if out.Messages[0].ReceiveCount != 3 {
		t.Errorf("receiveCount = %d; a model needs it to spot a consumer that keeps failing", out.Messages[0].ReceiveCount)
	}
	if got := inst.lastRequest().Method; got != http.MethodGet {
		t.Errorf("peek used %s, want GET; peeking must not consume messages", got)
	}
}

func TestGetObjectFlagsBinary(t *testing.T) {
	inst := newInstance(t).handle("GET /uploads/logo.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe})
	})

	var out GetObjectOutput
	call(t, inst.session(), "tarn_get_object",
		map[string]any{"bucket": "uploads", "key": "logo.png"}, &out)

	if !out.Binary {
		t.Fatal("binary = false for PNG bytes")
	}
	if out.Content != "" {
		t.Error("binary object returned content; that is noise in a model's context")
	}
}

func TestGetObjectReturnsText(t *testing.T) {
	inst := newInstance(t).handle("GET /reports/summary.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"orders":12}`)
	})

	var out GetObjectOutput
	call(t, inst.session(), "tarn_get_object",
		map[string]any{"bucket": "reports", "key": "summary.json"}, &out)

	if out.Binary {
		t.Fatal("binary = true for JSON text")
	}
	if out.Content != `{"orders":12}` {
		t.Errorf("content = %q", out.Content)
	}
}

// TestSendMessageAddressesAccountQueue covers the account-scoped queue URL the
// SQS query protocol requires.
func TestSendMessageAddressesAccountQueue(t *testing.T) {
	inst := newInstance(t).handle("POST /123456789012/jobs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<SendMessageResponse><SendMessageResult><MessageId>m-9</MessageId></SendMessageResult></SendMessageResponse>`)
	})

	var out SendMessageOutput
	call(t, inst.session(), "tarn_send_message", map[string]any{
		"queue": "jobs", "body": `{"orderId":"ord-1"}`, "account": "123456789012",
	}, &out)

	if out.MessageID != "m-9" {
		t.Fatalf("messageId = %q", out.MessageID)
	}
	req := inst.lastRequest()
	if !strings.Contains(req.Auth, "Credential=123456789012/") {
		t.Errorf("Authorization = %q, want the requested account's access key", req.Auth)
	}
	if !strings.Contains(string(req.Body), "Action=SendMessage") {
		t.Errorf("body = %q, want the SendMessage action", req.Body)
	}
}

func TestFireRuleReportsTargets(t *testing.T) {
	inst := newInstance(t).json("POST /_tarn/admin/eventbridge/fire", map[string]any{
		"ruleName": "nightly", "targets": 2, "successful": 1, "failed": 1,
		"firedAt": "2026-09-04T21:00:00Z",
	})

	var out FireRuleOutput
	call(t, inst.session(), "tarn_fire_rule", map[string]any{"rule": "nightly"}, &out)

	if out.Targets != 2 || out.Failed != 1 {
		t.Fatalf("result = %+v", out)
	}
}

// TestLogsKeepUnrecognizedRuntimeLines pins the filter as fail-open. A model
// cannot ask what was withheld, so a runtime-sourced line that matches no known
// marker has to stay visible.
func TestLogsKeepUnrecognizedRuntimeLines(t *testing.T) {
	events := []map[string]any{
		{"timestamp": "2026-09-04T21:33:20.500Z", "message": "START RequestId: abc", "level": "INFO", "source": "runtime"},
		{"timestamp": "2026-09-04T21:33:20.501Z", "message": "OOM killed: container exceeded memory limit", "level": "ERROR", "source": "runtime"},
	}
	inst := newInstance(t).json("GET /_tarn/admin/logs/events//aws/lambda/f",
		map[string]any{"events": events, "total": len(events)})

	var out LogsOutput
	call(t, inst.session(), "tarn_get_logs", map[string]any{"function": "f"}, &out)

	if out.Returned != 1 {
		t.Fatalf("returned %d events, want the unrecognized one kept: %+v", out.Returned, out.Events)
	}
	if !strings.Contains(out.Events[0].Message, "OOM killed") {
		t.Errorf("kept %q, want the unrecognized runtime line", out.Events[0].Message)
	}
}

// TestDeployReportsReplacement pins both branches of the existence check.
// Reporting "replaced" wrongly would let a model clobber a function it did not
// create and not say so.
func TestDeployReportsReplacement(t *testing.T) {
	created := map[string]any{
		"FunctionName": "f", "Runtime": "nodejs20.x", "State": "Active",
	}

	t.Run("new function", func(t *testing.T) {
		inst := newInstance(t).json("POST /2015-03-31/functions", created)
		// GET is unregistered, so it 404s the way a missing function does.

		var out DeployOutput
		call(t, inst.session(), "tarn_deploy_lambda",
			map[string]any{"name": "f", "files": map[string]any{"index.js": "x"}}, &out)

		if out.Replaced {
			t.Error("replaced = true for a function that did not exist")
		}
	})

	t.Run("existing function", func(t *testing.T) {
		inst := newInstance(t).
			json("GET /2015-03-31/functions/f", map[string]any{
				"Configuration": map[string]any{"FunctionName": "f"},
			}).
			json("POST /2015-03-31/functions", created)

		var out DeployOutput
		call(t, inst.session(), "tarn_deploy_lambda",
			map[string]any{"name": "f", "files": map[string]any{"index.js": "x"}}, &out)

		if !out.Replaced {
			t.Error("replaced = false when the function already existed")
		}
	})
}
