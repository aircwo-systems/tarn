package test

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const serverPort = "14566" // use a non-default port to avoid conflicts

var endpoint = "http://127.0.0.1:" + serverPort

// TestMain starts the Tarn server before running tests and stops it after.
func TestMain(m *testing.M) {
	// Check Docker is available
	if err := exec.Command("docker", "info").Run(); err != nil {
		fmt.Println("SKIP: Docker not available")
		os.Exit(0)
	}

	// Find the project root (parent of test/)
	// When `go test ./test/` runs, the working directory is the test/ folder
	wd, _ := os.Getwd()
	projectRoot := wd
	// If we're inside the test/ directory, go up one level
	if _, err := os.Stat(filepath.Join(wd, "e2e_test.go")); err == nil {
		projectRoot = filepath.Dir(wd)
	}

	binaryPath := filepath.Join(projectRoot, "build", "tarn-test")

	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/tarn")
	build.Dir = projectRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Printf("Failed to build: %s\n%s\n", err, out)
		os.Exit(1)
	}

	// Start the server
	dataDir, _ := os.MkdirTemp("", "tarn-e2e-*")
	defer os.RemoveAll(dataDir)

	server := exec.Command(binaryPath, "start",
		"--port", serverPort,
		"--data-dir", dataDir,
	)
	server.Stdout = os.Stdout
	server.Stderr = os.Stderr
	// Set process group so we can kill all children
	server.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := server.Start(); err != nil {
		fmt.Printf("Failed to start server: %s\n", err)
		os.Exit(1)
	}

	// Wait for server to be ready
	ready := false
	for i := 0; i < 30; i++ {
		resp, err := http.Get(endpoint + "/_tarn/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		fmt.Println("Server did not become ready in time")
		_ = syscall.Kill(-server.Process.Pid, syscall.SIGTERM)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Stop server
	_ = syscall.Kill(-server.Process.Pid, syscall.SIGTERM)
	_ = server.Wait()

	os.Exit(code)
}

func TestHealthCheck(t *testing.T) {
	resp, err := http.Get(endpoint + "/_tarn/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "running" {
		t.Fatalf("expected status 'running', got %v", result["status"])
	}
}

func TestListFunctionsEmpty(t *testing.T) {
	resp, err := http.Get(endpoint + "/2015-03-31/functions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result struct {
		Functions []interface{} `json:"Functions"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Functions) != 0 {
		t.Fatalf("expected empty function list, got %d", len(result.Functions))
	}
}

func TestNodeJSLambdaE2E(t *testing.T) {
	handlerCode := `exports.handler = async (event) => {
  return {
    statusCode: 200,
    body: JSON.stringify({ message: "hello from node", input: event }),
  };
};`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	// Create function
	createReq := map[string]interface{}{
		"FunctionName": "e2e-node-test",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Timeout":      30,
		"MemorySize":   128,
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 201 {
		t.Fatalf("create failed (%d): %s", resp.StatusCode, string(respBody))
	}

	t.Log("Function created, invoking...")

	// Invoke
	invokePayload := `{"name": "tarn"}`
	invokeResp, err := http.Post(
		endpoint+"/2015-03-31/functions/e2e-node-test/invocations",
		"application/json",
		strings.NewReader(invokePayload),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = invokeResp.Body.Close()
	}()
	invokeBody, _ := io.ReadAll(invokeResp.Body)

	t.Logf("Invoke response (%d): %s", invokeResp.StatusCode, string(invokeBody))

	if invokeResp.StatusCode != 200 {
		t.Fatalf("invoke failed (%d): %s", invokeResp.StatusCode, string(invokeBody))
	}

	// Check the response contains our expected data
	var result map[string]interface{}
	if err := json.Unmarshal(invokeBody, &result); err != nil {
		t.Fatalf("failed to parse response: %v (body: %s)", err, string(invokeBody))
	}

	bodyStr, ok := result["body"].(string)
	if !ok {
		t.Fatalf("expected 'body' field in response, got: %s", string(invokeBody))
	}
	if !strings.Contains(bodyStr, "hello from node") {
		t.Fatalf("expected 'hello from node' in body, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "tarn") {
		t.Fatalf("expected 'tarn' in body, got: %s", bodyStr)
	}

	// Second invoke should be warm
	t.Log("Invoking again (should be warm)...")
	invokeResp2, err := http.Post(
		endpoint+"/2015-03-31/functions/e2e-node-test/invocations",
		"application/json",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = invokeResp2.Body.Close()
	}()
	invokeBody2, _ := io.ReadAll(invokeResp2.Body)
	t.Logf("Warm invoke response (%d): %s", invokeResp2.StatusCode, string(invokeBody2))

	if invokeResp2.StatusCode != 200 {
		t.Fatalf("warm invoke failed (%d): %s", invokeResp2.StatusCode, string(invokeBody2))
	}

	// Delete
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-node-test", nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = delResp.Body.Close()
	if delResp.StatusCode != 204 {
		t.Fatalf("delete failed: %d", delResp.StatusCode)
	}
}

func TestLambdaLogsAppearInAdminLogs(t *testing.T) {
	handlerCode := `exports.handler = async () => {
  console.log("[e2e-log-test] hello from lambda logs");
  return {
    statusCode: 200,
    body: JSON.stringify({ ok: true }),
  };
};`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	createReq := map[string]interface{}{
		"FunctionName": "e2e-log-test",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Timeout":      30,
		"MemorySize":   128,
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("create failed (%d): %s", resp.StatusCode, string(respBody))
	}

	invokeResp, err := http.Post(
		endpoint+"/2015-03-31/functions/e2e-log-test/invocations",
		"application/json",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer invokeResp.Body.Close()

	if invokeResp.StatusCode != 200 {
		invokeBody, _ := io.ReadAll(invokeResp.Body)
		t.Fatalf("invoke failed (%d): %s", invokeResp.StatusCode, string(invokeBody))
	}

	logGroupPath := endpoint + "/_tarn/admin/logs/events/" + url.PathEscape("/aws/lambda/e2e-log-test") + "?limit=200"
	found := false

	for attempt := 0; attempt < 20; attempt++ {
		logResp, err := http.Get(logGroupPath)
		if err != nil {
			t.Fatal(err)
		}

		var payload struct {
			Events []struct {
				Message string `json:"message"`
			} `json:"events"`
		}
		if err := json.NewDecoder(logResp.Body).Decode(&payload); err != nil {
			_ = logResp.Body.Close()
			t.Fatalf("decode logs response: %v", err)
		}
		_ = logResp.Body.Close()

		for _, event := range payload.Events {
			if strings.Contains(event.Message, "[e2e-log-test] hello from lambda logs") {
				found = true
				break
			}
		}
		if found {
			break
		}

		time.Sleep(250 * time.Millisecond)
	}

	if !found {
		t.Fatal("expected lambda console log to appear in admin logs")
	}

	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-log-test", nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = delResp.Body.Close()
	if delResp.StatusCode != 204 {
		t.Fatalf("delete failed: %d", delResp.StatusCode)
	}
}

func TestPythonLambdaE2E(t *testing.T) {
	handlerCode := `import json

def lambda_handler(event, context):
    return {
        "statusCode": 200,
        "body": json.dumps({"message": "hello from python", "input": event}),
    }
`
	zipData := createZip(t, map[string]string{"lambda_function.py": handlerCode})

	createReq := map[string]interface{}{
		"FunctionName": "e2e-python-test",
		"Runtime":      "python3.12",
		"Handler":      "lambda_function.lambda_handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Timeout":      30,
		"MemorySize":   128,
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 201 {
		t.Fatalf("create failed (%d): %s", resp.StatusCode, string(respBody))
	}

	t.Log("Python function created, invoking...")

	invokePayload := `{"language": "python"}`
	invokeResp, err := http.Post(
		endpoint+"/2015-03-31/functions/e2e-python-test/invocations",
		"application/json",
		strings.NewReader(invokePayload),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = invokeResp.Body.Close()
	}()
	invokeBody, _ := io.ReadAll(invokeResp.Body)

	t.Logf("Python invoke response (%d): %s", invokeResp.StatusCode, string(invokeBody))

	if invokeResp.StatusCode != 200 {
		t.Fatalf("invoke failed (%d): %s", invokeResp.StatusCode, string(invokeBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(invokeBody, &result); err != nil {
		t.Fatalf("failed to parse response: %v (body: %s)", err, string(invokeBody))
	}

	bodyStr, ok := result["body"].(string)
	if !ok {
		t.Fatalf("expected 'body' field in response, got: %s", string(invokeBody))
	}
	if !strings.Contains(bodyStr, "hello from python") {
		t.Fatalf("expected 'hello from python' in body, got: %s", bodyStr)
	}

	// Cleanup
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-python-test", nil)
	delResp, _ := http.DefaultClient.Do(delReq)
	_ = delResp.Body.Close()
}

func TestDryRunInvoke(t *testing.T) {
	handlerCode := `exports.handler = async () => ({ statusCode: 200 });`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	createReq := map[string]interface{}{
		"FunctionName": "e2e-dryrun",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, _ := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	resp.Body.Close()

	// DryRun invoke — should return 204 without starting a container
	req, _ := http.NewRequest("POST", endpoint+"/2015-03-31/functions/e2e-dryrun/invocations", strings.NewReader("{}"))
	req.Header.Set("X-Amz-Invocation-Type", "DryRun")
	dryResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = dryResp.Body.Close()

	if dryResp.StatusCode != 204 {
		t.Fatalf("expected 204 for DryRun, got %d", dryResp.StatusCode)
	}

	// Cleanup
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-dryrun", nil)
	delResp, _ := http.DefaultClient.Do(delReq)
	_ = delResp.Body.Close()
}

func TestAPIGatewayLambdaE2E(t *testing.T) {
	handlerCode := `exports.handler = async (event) => {
  return {
    statusCode: 200,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      ok: true,
      routeKey: event.routeKey,
      path: event.rawPath,
      id: event.pathParameters?.id || null,
      name: event.queryStringParameters?.name || ""
    }),
  };
};`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	functionName := "e2e-apigw-handler"
	createReq := map[string]interface{}{
		"FunctionName": functionName,
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	createBody, _ := json.Marshal(createReq)
	createResp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != 201 {
		t.Fatalf("create lambda failed: %d", createResp.StatusCode)
	}

	lambdaArn := "arn:aws:lambda:us-east-1:000000000000:function:" + functionName

	apiCreateReq := map[string]interface{}{
		"Name":         "e2e-http-api",
		"ProtocolType": "HTTP",
	}
	apiCreateBody, _ := json.Marshal(apiCreateReq)
	apiResp, err := http.Post(endpoint+"/v2/apis", "application/json", bytes.NewReader(apiCreateBody))
	if err != nil {
		t.Fatal(err)
	}
	defer apiResp.Body.Close()
	apiRespBody, _ := io.ReadAll(apiResp.Body)
	if apiResp.StatusCode != 201 {
		t.Fatalf("create api failed (%d): %s", apiResp.StatusCode, string(apiRespBody))
	}
	var apiCreated struct {
		APIID string `json:"ApiId"`
	}
	if err := json.Unmarshal(apiRespBody, &apiCreated); err != nil {
		t.Fatalf("decode api create: %v", err)
	}

	integrationReq := map[string]interface{}{
		"IntegrationType":      "AWS_PROXY",
		"IntegrationUri":       lambdaArn,
		"PayloadFormatVersion": "2.0",
		"TimeoutInMillis":      30000,
	}
	integrationBody, _ := json.Marshal(integrationReq)
	integrationResp, err := http.Post(endpoint+"/v2/apis/"+apiCreated.APIID+"/integrations", "application/json", bytes.NewReader(integrationBody))
	if err != nil {
		t.Fatal(err)
	}
	defer integrationResp.Body.Close()
	integrationRespBody, _ := io.ReadAll(integrationResp.Body)
	if integrationResp.StatusCode != 201 {
		t.Fatalf("create integration failed (%d): %s", integrationResp.StatusCode, string(integrationRespBody))
	}
	var integrationCreated struct {
		IntegrationID string `json:"IntegrationId"`
	}
	if err := json.Unmarshal(integrationRespBody, &integrationCreated); err != nil {
		t.Fatalf("decode integration create: %v", err)
	}

	routeReq := map[string]interface{}{
		"RouteKey": "GET /hello/{id}",
		"Target":   "integrations/" + integrationCreated.IntegrationID,
	}
	routeBody, _ := json.Marshal(routeReq)
	routeResp, err := http.Post(endpoint+"/v2/apis/"+apiCreated.APIID+"/routes", "application/json", bytes.NewReader(routeBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = routeResp.Body.Close()
	}()
	routeRespBody, _ := io.ReadAll(routeResp.Body)
	if routeResp.StatusCode != 201 {
		t.Fatalf("create route failed (%d): %s", routeResp.StatusCode, string(routeRespBody))
	}
	var routeCreated struct {
		RouteID string `json:"RouteId"`
	}
	if err := json.Unmarshal(routeRespBody, &routeCreated); err != nil {
		t.Fatalf("decode route create: %v", err)
	}

	invokeURL := endpoint + "/_apigateway/" + apiCreated.APIID + "/$default/hello/42?name=tarn"
	invokeResp, err := http.Get(invokeURL)
	if err != nil {
		t.Fatal(err)
	}
	invokeBody, _ := io.ReadAll(invokeResp.Body)
	invokeResp.Body.Close()
	if invokeResp.StatusCode != 200 {
		t.Fatalf("invoke failed (%d): %s", invokeResp.StatusCode, string(invokeBody))
	}

	var invokeResult struct {
		OK       bool   `json:"ok"`
		RouteKey string `json:"routeKey"`
		Path     string `json:"path"`
		ID       string `json:"id"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(invokeBody, &invokeResult); err != nil {
		t.Fatalf("decode invoke response: %v (%s)", err, string(invokeBody))
	}
	if !invokeResult.OK || invokeResult.ID != "42" || invokeResult.Name != "tarn" {
		t.Fatalf("unexpected invoke payload: %+v", invokeResult)
	}

	patchRouteReq := map[string]interface{}{
		"RouteKey": "GET /hi/{id}",
		"Target":   "integrations/" + integrationCreated.IntegrationID,
	}
	patchRouteBody, _ := json.Marshal(patchRouteReq)
	patchReq, _ := http.NewRequest("PATCH", endpoint+"/v2/apis/"+apiCreated.APIID+"/routes/"+routeCreated.RouteID, bytes.NewReader(patchRouteBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = patchResp.Body.Close()
	if patchResp.StatusCode != 200 {
		t.Fatalf("patch route failed: %d", patchResp.StatusCode)
	}

	oldRouteResp, err := http.Get(endpoint + "/_apigateway/" + apiCreated.APIID + "/$default/hello/42")
	if err != nil {
		t.Fatal(err)
	}
	oldRouteResp.Body.Close()
	if oldRouteResp.StatusCode != 404 {
		t.Fatalf("old route should return 404 after patch, got %d", oldRouteResp.StatusCode)
	}

	newRouteResp, err := http.Get(endpoint + "/_apigateway/" + apiCreated.APIID + "/$default/hi/99?name=patched")
	if err != nil {
		t.Fatal(err)
	}
	newRouteBody, _ := io.ReadAll(newRouteResp.Body)
	newRouteResp.Body.Close()
	if newRouteResp.StatusCode != 200 {
		t.Fatalf("new route invoke failed (%d): %s", newRouteResp.StatusCode, string(newRouteBody))
	}

	delAPIReq, _ := http.NewRequest("DELETE", endpoint+"/v2/apis/"+apiCreated.APIID, nil)
	delAPIResp, _ := http.DefaultClient.Do(delAPIReq)
	delAPIResp.Body.Close()
	if delAPIResp.StatusCode != 204 {
		t.Fatalf("delete api failed: %d", delAPIResp.StatusCode)
	}

	delFnReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/"+functionName, nil)
	delFnResp, _ := http.DefaultClient.Do(delFnReq)
	delFnResp.Body.Close()
	if delFnResp.StatusCode != 204 {
		t.Fatalf("delete lambda failed: %d", delFnResp.StatusCode)
	}
}

func TestAPIGatewayV1SQSFIFOE2E(t *testing.T) {
	queueName := "e2e-apigwv1-fifo.fifo"
	queueURL := endpoint + "/000000000000/" + queueName

	sqsRequest(t, url.Values{
		"Action":            {"CreateQueue"},
		"QueueName":         {queueName},
		"Attribute.1.Name":  {"FifoQueue"},
		"Attribute.1.Value": {"true"},
	})
	defer func() {
		sqsQueueRequest(t, queueName, url.Values{
			"Action":   {"DeleteQueue"},
			"QueueUrl": {queueURL},
		})
	}()

	doJSON := func(method, rawURL string, payload any, headers map[string]string) (int, []byte) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, rawURL, body)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, respBody
	}

	status, body := doJSON(http.MethodPost, endpoint+"/restapis", map[string]any{
		"name": "e2e-v1-fifo-api",
	}, nil)
	if status != 201 {
		t.Fatalf("create rest api failed (%d): %s", status, string(body))
	}

	var api struct {
		ID             string `json:"id"`
		RootResourceID string `json:"rootResourceId"`
	}
	if err := json.Unmarshal(body, &api); err != nil {
		t.Fatalf("decode rest api create: %v", err)
	}
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, endpoint+"/restapis/"+api.ID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	createResource := func(parentID, pathPart string) string {
		t.Helper()
		status, body := doJSON(http.MethodPost, endpoint+"/restapis/"+api.ID+"/resources/"+parentID, map[string]any{
			"pathPart": pathPart,
		}, nil)
		if status != 201 {
			t.Fatalf("create resource %q failed (%d): %s", pathPart, status, string(body))
		}
		var res struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatalf("decode resource create: %v", err)
		}
		return res.ID
	}

	v1ResourceID := createResource(api.RootResourceID, "v1")
	eventsResourceID := createResource(v1ResourceID, "events")
	aggregateResourceID := createResource(eventsResourceID, "{aggregateId}")

	status, body = doJSON(http.MethodPut, endpoint+"/restapis/"+api.ID+"/resources/"+aggregateResourceID+"/methods/DELETE", map[string]any{
		"authorizationType": "NONE",
		"requestParameters": map[string]bool{
			"method.request.path.aggregateId": true,
		},
	}, nil)
	if status != 201 {
		t.Fatalf("put method failed (%d): %s", status, string(body))
	}

	requestTemplate := `#set($aggregateId = $input.params().path.get('aggregateId'))
#set($serviceName = $input.params().header.get('service-name'))
#set($correlationId = $input.params().header.get('correlation-id'))
#set($channel = $input.params().header.get('channel'))
#set($payload = {
  "event": {
    "service-name": "$serviceName"
  },
  "aggregate": {
    "aggregateId": "$aggregateId"
  },
  "source": {
    "correlationId": "$correlationId",
    "channel": "$channel"
  }
})
Action=SendMessage&MessageGroupId=$util.urlEncode($aggregateId)&MessageBody=$util.urlEncode($util.toJson($payload))`

	status, body = doJSON(http.MethodPut, endpoint+"/restapis/"+api.ID+"/resources/"+aggregateResourceID+"/methods/DELETE/integration", map[string]any{
		"type":       "AWS",
		"httpMethod": "POST",
		"uri":        "arn:aws:apigateway:us-east-1:sqs:path/000000000000/" + queueName,
		"requestParameters": map[string]string{
			"integration.request.header.Content-Type": "'application/x-www-form-urlencoded'",
		},
		"requestTemplates": map[string]string{
			"application/json": requestTemplate,
		},
	}, nil)
	if status != 201 {
		t.Fatalf("put integration failed (%d): %s", status, string(body))
	}

	status, body = doJSON(http.MethodPost, endpoint+"/restapis/"+api.ID+"/deployments", map[string]any{
		"stageName": "local",
	}, nil)
	if status != 201 {
		t.Fatalf("create deployment failed (%d): %s", status, string(body))
	}

	status, body = doJSON(http.MethodDelete, endpoint+"/_aws/execute-api/"+api.ID+"/local/v1/events/agg-42", map[string]any{
		"ignored": true,
	}, map[string]string{
		"service-name":   "orders-service",
		"correlation-id": "corr-123",
		"channel":        "web",
	})
	if status != 200 {
		t.Fatalf("invoke failed (%d): %s", status, string(body))
	}

	recvBody := sqsQueueRequest(t, queueName, url.Values{
		"Action":              {"ReceiveMessage"},
		"QueueUrl":            {queueURL},
		"MaxNumberOfMessages": {"1"},
	})
	recv := string(recvBody)
	if !strings.Contains(recv, `"aggregateId":"agg-42"`) {
		t.Fatalf("expected mapped aggregateId in message body, got: %s", recv)
	}
	if !strings.Contains(recv, `"service-name":"orders-service"`) {
		t.Fatalf("expected mapped service-name in message body, got: %s", recv)
	}
	if !strings.Contains(recv, `"correlationId":"corr-123"`) {
		t.Fatalf("expected mapped correlationId in message body, got: %s", recv)
	}
}

func TestAPIGatewayV2SQSFIFOE2E(t *testing.T) {
	queueName := "e2e-apigwv2-fifo.fifo"
	queueURL := endpoint + "/000000000000/" + queueName

	sqsRequest(t, url.Values{
		"Action":            {"CreateQueue"},
		"QueueName":         {queueName},
		"Attribute.1.Name":  {"FifoQueue"},
		"Attribute.1.Value": {"true"},
	})
	defer func() {
		sqsQueueRequest(t, queueName, url.Values{
			"Action":   {"DeleteQueue"},
			"QueueUrl": {queueURL},
		})
	}()

	doJSON := func(method, rawURL string, payload any, headers map[string]string) (int, []byte) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, rawURL, body)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, respBody
	}

	status, body := doJSON(http.MethodPost, endpoint+"/v2/apis", map[string]any{
		"name":         "e2e-v2-fifo-api",
		"protocolType": "HTTP",
	}, nil)
	if status != 201 {
		t.Fatalf("create http api failed (%d): %s", status, string(body))
	}
	var api struct {
		APIID string `json:"apiId"`
	}
	if err := json.Unmarshal(body, &api); err != nil {
		t.Fatalf("decode api create: %v", err)
	}
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, endpoint+"/v2/apis/"+api.APIID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	status, body = doJSON(http.MethodPost, endpoint+"/v2/apis/"+api.APIID+"/integrations", map[string]any{
		"integrationType": "AWS",
		"integrationUri":  "arn:aws:sqs:us-east-1:000000000000:" + queueName,
		"requestParameters": map[string]string{
			"MessageBody":            "$request.body.payload",
			"MessageGroupId":         "$request.path.aggregateId",
			"MessageDeduplicationId": "$request.header.x-dedup-id",
		},
	}, nil)
	if status != 201 {
		t.Fatalf("create fifo integration failed (%d): %s", status, string(body))
	}
	var fifoIntegration struct {
		IntegrationID string `json:"integrationId"`
	}
	if err := json.Unmarshal(body, &fifoIntegration); err != nil {
		t.Fatalf("decode fifo integration create: %v", err)
	}

	status, body = doJSON(http.MethodPost, endpoint+"/v2/apis/"+api.APIID+"/routes", map[string]any{
		"routeKey": "POST /events/{aggregateId}",
		"target":   "integrations/" + fifoIntegration.IntegrationID,
	}, nil)
	if status != 201 {
		t.Fatalf("create fifo route failed (%d): %s", status, string(body))
	}

	status, body = doJSON(http.MethodPost, endpoint+"/_apigateway/"+api.APIID+"/$default/events/agg-v2", map[string]any{
		"payload": "hello-from-v2",
	}, map[string]string{
		"x-dedup-id": "dedup-v2-1",
	})
	if status != 200 {
		t.Fatalf("invoke fifo route failed (%d): %s", status, string(body))
	}

	recvBody := sqsQueueRequest(t, queueName, url.Values{
		"Action":              {"ReceiveMessage"},
		"QueueUrl":            {queueURL},
		"MaxNumberOfMessages": {"1"},
	})
	if !strings.Contains(string(recvBody), "hello-from-v2") {
		t.Fatalf("expected mapped v2 message body, got: %s", string(recvBody))
	}

	status, body = doJSON(http.MethodPost, endpoint+"/v2/apis/"+api.APIID+"/integrations", map[string]any{
		"integrationType": "AWS",
		"integrationUri":  "arn:aws:sqs:us-east-1:000000000000:" + queueName,
		"requestParameters": map[string]string{
			"MessageBody": "$request.body",
		},
	}, nil)
	if status != 201 {
		t.Fatalf("create no-group integration failed (%d): %s", status, string(body))
	}
	var noGroupIntegration struct {
		IntegrationID string `json:"integrationId"`
	}
	if err := json.Unmarshal(body, &noGroupIntegration); err != nil {
		t.Fatalf("decode no-group integration create: %v", err)
	}

	status, body = doJSON(http.MethodPost, endpoint+"/v2/apis/"+api.APIID+"/routes", map[string]any{
		"routeKey": "POST /events-no-group",
		"target":   "integrations/" + noGroupIntegration.IntegrationID,
	}, nil)
	if status != 201 {
		t.Fatalf("create no-group route failed (%d): %s", status, string(body))
	}

	status, body = doJSON(http.MethodPost, endpoint+"/_apigateway/"+api.APIID+"/$default/events-no-group", map[string]any{
		"payload": "should-fail",
	}, nil)
	if status != 500 {
		t.Fatalf("expected fifo validation failure status 500, got %d: %s", status, string(body))
	}
	if !strings.Contains(string(body), "MessageGroupId is required for FIFO queues") {
		t.Fatalf("expected fifo validation error in body, got: %s", string(body))
	}
}

func TestCreateDuplicate(t *testing.T) {
	handlerCode := `exports.handler = async () => ({});`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	createReq := map[string]interface{}{
		"FunctionName": "e2e-dupe",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, _ := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	resp.Body.Close()

	// Try to create again — we override the resource
	resp2, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 201 {
		t.Fatalf("expected 201 for duplicate override, got %d", resp2.StatusCode)
	}

	// Cleanup
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-dupe", nil)
	delResp, _ := http.DefaultClient.Do(delReq)
	_ = delResp.Body.Close()
}

func TestGetFunction(t *testing.T) {
	handlerCode := `exports.handler = async () => ({});`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	createReq := map[string]interface{}{
		"FunctionName": "e2e-getfunc",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Description":  "test function",
		"Timeout":      10,
		"MemorySize":   256,
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// Define polling parameters
	timeout := 15 * time.Second
	interval := 1 * time.Second
	deadline := time.Now().Add(timeout)

	var result struct {
		Configuration struct {
			FunctionName string `json:"FunctionName"`
			Runtime      string `json:"Runtime"`
			Handler      string `json:"Handler"`
			MemorySize   int    `json:"MemorySize"`
			Timeout      int    `json:"Timeout"`
			Description  string `json:"Description"`
			State        string `json:"State"`
		} `json:"Configuration"`
		Code struct {
			RepositoryType string `json:"RepositoryType"`
		} `json:"Code"`
	}

	// Polling loop to wait for "Active" state
	activated := false
	for time.Now().Before(deadline) {
		getResp, err := http.Get(endpoint + "/2015-03-31/functions/e2e-getfunc")
		if err != nil {
			t.Fatal(err)
		}

		getBody, _ := io.ReadAll(getResp.Body)
		_ = getResp.Body.Close()

		if getResp.StatusCode != 200 {
			t.Fatalf("get failed (%d): %s", getResp.StatusCode, string(getBody))
		}

		err = json.Unmarshal(getBody, &result)
		if err != nil {
			t.Fatal(err)
		}

		if result.Configuration.State == "Active" {
			activated = true
			break
		}

		t.Logf("Function state is %q, retrying...", result.Configuration.State)
		time.Sleep(interval)
	}

	if !activated {
		t.Fatalf("timed out waiting for 'Active' state; last state was %q", result.Configuration.State)
	}

	// Final Assertions
	cfg := result.Configuration
	if cfg.FunctionName != "e2e-getfunc" {
		t.Fatalf("expected name 'e2e-getfunc', got %q", cfg.FunctionName)
	}
	if cfg.Runtime != "nodejs20.x" {
		t.Fatalf("expected runtime 'nodejs20.x', got %q", cfg.Runtime)
	}
	if cfg.MemorySize != 256 {
		t.Fatalf("expected memory 256, got %d", cfg.MemorySize)
	}
	if cfg.Timeout != 10 {
		t.Fatalf("expected timeout 10, got %d", cfg.Timeout)
	}
	if result.Code.RepositoryType == "" {
		t.Fatalf("expected non-empty Code.RepositoryType")
	}

	// Cleanup
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-getfunc", nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err == nil {
		_ = delResp.Body.Close()
	}
}

func TestUpdateFunctionConfiguration(t *testing.T) {
	handlerCode := `exports.handler = async (event) => ({
		statusCode: 200,
		body: JSON.stringify({ timeout: process.env.AWS_LAMBDA_FUNCTION_TIMEOUT, memory: process.env.AWS_LAMBDA_FUNCTION_MEMORY_SIZE }),
	});`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	createReq := map[string]interface{}{
		"FunctionName": "e2e-update-config",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Timeout":      5,
		"MemorySize":   128,
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Update configuration
	updateReq := map[string]interface{}{
		"Timeout":     30,
		"MemorySize":  512,
		"Description": "updated desc",
		"Environment": map[string]interface{}{
			"Variables": map[string]string{"MY_VAR": "hello"},
		},
	}
	updateBody, _ := json.Marshal(updateReq)
	putReq, _ := http.NewRequest("PUT", endpoint+"/2015-03-31/functions/e2e-update-config/configuration", bytes.NewReader(updateBody))
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	defer putResp.Body.Close()
	putBody, _ := io.ReadAll(putResp.Body)

	if putResp.StatusCode != 200 {
		t.Fatalf("update config failed (%d): %s", putResp.StatusCode, string(putBody))
	}

	var result map[string]interface{}
	json.Unmarshal(putBody, &result)

	if int(result["Timeout"].(float64)) != 30 {
		t.Fatalf("expected timeout 30, got %v", result["Timeout"])
	}
	if int(result["MemorySize"].(float64)) != 512 {
		t.Fatalf("expected memory 512, got %v", result["MemorySize"])
	}
	if result["Description"] != "updated desc" {
		t.Fatalf("expected description 'updated desc', got %v", result["Description"])
	}

	// Verify via GET
	getResp, err := http.Get(endpoint + "/2015-03-31/functions/e2e-update-config")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	getBody, _ := io.ReadAll(getResp.Body)

	var getResult struct {
		Configuration struct {
			Timeout     int               `json:"Timeout"`
			MemorySize  int               `json:"MemorySize"`
			Description string            `json:"Description"`
			Environment map[string]string `json:"Environment"`
		} `json:"Configuration"`
	}
	json.Unmarshal(getBody, &getResult)

	if getResult.Configuration.Timeout != 30 {
		t.Fatalf("GET: expected timeout 30, got %d", getResult.Configuration.Timeout)
	}
	if getResult.Configuration.Environment["MY_VAR"] != "hello" {
		t.Fatalf("GET: expected MY_VAR=hello, got %v", getResult.Configuration.Environment)
	}

	// Cleanup
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-update-config", nil)
	delResp, _ := http.DefaultClient.Do(delReq)
	delResp.Body.Close()
}

func TestFunctionTags(t *testing.T) {
	handlerCode := `exports.handler = async () => ({});`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	// Create with tags
	createReq := map[string]interface{}{
		"FunctionName": "e2e-tags",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
		"Tags": map[string]string{
			"env":     "test",
			"project": "tarn",
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// List tags
	tagResp, err := http.Get(endpoint + "/2015-03-31/functions/e2e-tags/tags")
	if err != nil {
		t.Fatal(err)
	}
	defer tagResp.Body.Close()
	tagBody, _ := io.ReadAll(tagResp.Body)

	var tagResult struct {
		Tags map[string]string `json:"Tags"`
	}
	json.Unmarshal(tagBody, &tagResult)

	if tagResult.Tags["env"] != "test" {
		t.Fatalf("expected tag env=test, got %v", tagResult.Tags)
	}
	if tagResult.Tags["project"] != "tarn" {
		t.Fatalf("expected tag project=tarn, got %v", tagResult.Tags)
	}

	// Add tags
	addTagReq := map[string]interface{}{
		"Tags": map[string]string{"version": "1.0"},
	}
	addBody, _ := json.Marshal(addTagReq)
	addResp, err := http.Post(endpoint+"/2015-03-31/functions/e2e-tags/tags", "application/json", bytes.NewReader(addBody))
	if err != nil {
		t.Fatal(err)
	}
	addResp.Body.Close()

	if addResp.StatusCode != 204 {
		t.Fatalf("tag resource failed: %d", addResp.StatusCode)
	}

	// Verify new tag exists
	tagResp2, _ := http.Get(endpoint + "/2015-03-31/functions/e2e-tags/tags")
	tagBody2, _ := io.ReadAll(tagResp2.Body)
	tagResp2.Body.Close()

	var tagResult2 struct {
		Tags map[string]string `json:"Tags"`
	}
	json.Unmarshal(tagBody2, &tagResult2)
	if tagResult2.Tags["version"] != "1.0" {
		t.Fatalf("expected tag version=1.0, got %v", tagResult2.Tags)
	}

	// Remove tag
	untagReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-tags/tags?tagKeys=env", nil)
	untagResp, err := http.DefaultClient.Do(untagReq)
	if err != nil {
		t.Fatal(err)
	}
	untagResp.Body.Close()

	if untagResp.StatusCode != 204 {
		t.Fatalf("untag failed: %d", untagResp.StatusCode)
	}

	// Verify tag removed
	tagResp3, _ := http.Get(endpoint + "/2015-03-31/functions/e2e-tags/tags")
	tagBody3, _ := io.ReadAll(tagResp3.Body)
	tagResp3.Body.Close()

	var tagResult3 struct {
		Tags map[string]string `json:"Tags"`
	}
	json.Unmarshal(tagBody3, &tagResult3)
	if _, exists := tagResult3.Tags["env"]; exists {
		t.Fatal("tag 'env' should have been removed")
	}
	if tagResult3.Tags["project"] != "tarn" {
		t.Fatal("tag 'project' should still exist")
	}

	// Cleanup
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-tags", nil)
	delResp, _ := http.DefaultClient.Do(delReq)
	delResp.Body.Close()
}

func TestGetAccountSettings(t *testing.T) {
	resp, err := http.Get(endpoint + "/2015-03-31/account-settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccountLimit struct {
			ConcurrentExecutions int64 `json:"ConcurrentExecutions"`
			TotalCodeSize        int64 `json:"TotalCodeSize"`
		} `json:"AccountLimit"`
		AccountUsage struct {
			FunctionCount int `json:"FunctionCount"`
		} `json:"AccountUsage"`
	}
	json.Unmarshal(body, &result)

	if result.AccountLimit.ConcurrentExecutions != 1000 {
		t.Fatalf("expected ConcurrentExecutions 1000, got %d", result.AccountLimit.ConcurrentExecutions)
	}
	if result.AccountLimit.TotalCodeSize != 80530636800 {
		t.Fatalf("expected TotalCodeSize 80530636800, got %d", result.AccountLimit.TotalCodeSize)
	}
}

func TestRapidSequentialInvokes(t *testing.T) {
	// Tests rapid sequential invokes on a warm container.
	// Note: The RIE handles one request at a time (matching real Lambda
	// which uses one container per concurrent invocation). This test
	// verifies warm containers handle rapid sequential calls correctly.
	handlerCode := `exports.handler = async (event) => ({
		statusCode: 200,
		body: JSON.stringify({ id: event.id }),
	});`
	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	createReq := map[string]interface{}{
		"FunctionName": "e2e-rapid",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Timeout":      30,
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Fire 5 rapid sequential invokes
	for i := 0; i < 5; i++ {
		payload := fmt.Sprintf(`{"id":%d}`, i)
		invokeResp, err := http.Post(
			endpoint+"/2015-03-31/functions/e2e-rapid/invocations",
			"application/json",
			strings.NewReader(payload),
		)
		if err != nil {
			t.Fatalf("invoke %d failed: %v", i, err)
		}
		invokeBody, _ := io.ReadAll(invokeResp.Body)
		invokeResp.Body.Close()

		if invokeResp.StatusCode != 200 {
			t.Fatalf("invoke %d: expected 200, got %d: %s", i, invokeResp.StatusCode, string(invokeBody))
		}
		t.Logf("invoke %d: OK (%d)", i, invokeResp.StatusCode)
	}

	// Cleanup
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-rapid", nil)
	delResp, _ := http.DefaultClient.Do(delReq)
	delResp.Body.Close()
}

func TestLambdaLayers(t *testing.T) {
	// Publish a layer with a helper module
	layerContent := createZip(t, map[string]string{
		"nodejs/node_modules/helper/index.js": `module.exports.greet = () => "hello from layer";`,
	})

	publishReq := map[string]interface{}{
		"Description":        "test helper layer",
		"CompatibleRuntimes": []string{"nodejs20.x"},
		"Content": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(layerContent),
		},
	}
	publishBody, _ := json.Marshal(publishReq)
	publishResp, err := http.Post(endpoint+"/2015-03-31/layers/e2e-helper/versions", "application/json", bytes.NewReader(publishBody))
	if err != nil {
		t.Fatal(err)
	}
	defer publishResp.Body.Close()
	publishRespBody, _ := io.ReadAll(publishResp.Body)

	if publishResp.StatusCode != 201 {
		t.Fatalf("publish layer failed (%d): %s", publishResp.StatusCode, string(publishRespBody))
	}

	var layerResult struct {
		LayerVersionArn string `json:"LayerVersionArn"`
		Version         int64  `json:"Version"`
	}
	json.Unmarshal(publishRespBody, &layerResult)
	t.Logf("Published layer: %s (version %d)", layerResult.LayerVersionArn, layerResult.Version)

	if layerResult.LayerVersionArn == "" {
		t.Fatal("expected non-empty LayerVersionArn")
	}

	// List layers
	listResp, err := http.Get(endpoint + "/2015-03-31/layers")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	listBody, _ := io.ReadAll(listResp.Body)

	var listResult struct {
		Layers []interface{} `json:"Layers"`
	}
	json.Unmarshal(listBody, &listResult)
	if len(listResult.Layers) == 0 {
		t.Fatal("expected at least 1 layer")
	}

	// List layer versions
	versionsResp, err := http.Get(endpoint + "/2015-03-31/layers/e2e-helper/versions")
	if err != nil {
		t.Fatal(err)
	}
	defer versionsResp.Body.Close()
	versionsBody, _ := io.ReadAll(versionsResp.Body)

	var versionsResult struct {
		LayerVersions []interface{} `json:"LayerVersions"`
	}
	json.Unmarshal(versionsBody, &versionsResult)
	if len(versionsResult.LayerVersions) != 1 {
		t.Fatalf("expected 1 layer version, got %d", len(versionsResult.LayerVersions))
	}

	// Get layer version
	getLayerResp, err := http.Get(endpoint + "/2015-03-31/layers/e2e-helper/versions/1")
	if err != nil {
		t.Fatal(err)
	}
	defer getLayerResp.Body.Close()

	if getLayerResp.StatusCode != 200 {
		t.Fatalf("get layer version failed: %d", getLayerResp.StatusCode)
	}

	// Delete layer version
	delLayerReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/layers/e2e-helper/versions/1", nil)
	delLayerResp, err := http.DefaultClient.Do(delLayerReq)
	if err != nil {
		t.Fatal(err)
	}
	delLayerResp.Body.Close()

	if delLayerResp.StatusCode != 204 {
		t.Fatalf("delete layer failed: %d", delLayerResp.StatusCode)
	}

	// Verify deleted
	getDeleted, _ := http.Get(endpoint + "/2015-03-31/layers/e2e-helper/versions/1")
	getDeleted.Body.Close()
	if getDeleted.StatusCode != 404 {
		t.Fatalf("expected 404 after deletion, got %d", getDeleted.StatusCode)
	}
}

// --- SQS Tests ---

func sqsRequest(t *testing.T, params url.Values) []byte {
	t.Helper()
	resp, err := http.PostForm(endpoint, params)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("SQS request failed (%d): %s", resp.StatusCode, string(body))
	}
	return body
}

func sqsQueueRequest(t *testing.T, queueName string, params url.Values) []byte {
	t.Helper()
	resp, err := http.PostForm(endpoint+"/000000000000/"+queueName, params)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("SQS request failed (%d): %s", resp.StatusCode, string(body))
	}
	return body
}

func TestSQSCreateAndListQueues(t *testing.T) {
	// Create queue
	body := sqsRequest(t, url.Values{
		"Action":    {"CreateQueue"},
		"QueueName": {"e2e-sqs-queue"},
	})
	if !strings.Contains(string(body), "e2e-sqs-queue") {
		t.Fatalf("expected queue URL in response, got: %s", string(body))
	}

	// List queues
	listBody := sqsRequest(t, url.Values{
		"Action": {"ListQueues"},
	})
	if !strings.Contains(string(listBody), "e2e-sqs-queue") {
		t.Fatalf("expected queue in list, got: %s", string(listBody))
	}

	// Get queue URL
	urlBody := sqsRequest(t, url.Values{
		"Action":    {"GetQueueUrl"},
		"QueueName": {"e2e-sqs-queue"},
	})
	if !strings.Contains(string(urlBody), "e2e-sqs-queue") {
		t.Fatalf("expected queue URL, got: %s", string(urlBody))
	}

	// Cleanup
	sqsQueueRequest(t, "e2e-sqs-queue", url.Values{
		"Action":   {"DeleteQueue"},
		"QueueUrl": {endpoint + "/000000000000/e2e-sqs-queue"},
	})
}

func TestSQSSendReceiveDelete(t *testing.T) {
	// Create
	sqsRequest(t, url.Values{
		"Action":    {"CreateQueue"},
		"QueueName": {"e2e-sqs-msg"},
	})

	queueURL := endpoint + "/000000000000/e2e-sqs-msg"

	// Send
	sendBody := sqsQueueRequest(t, "e2e-sqs-msg", url.Values{
		"Action":      {"SendMessage"},
		"QueueUrl":    {queueURL},
		"MessageBody": {"hello from e2e"},
	})
	if !strings.Contains(string(sendBody), "MessageId") {
		t.Fatalf("expected MessageId in response, got: %s", string(sendBody))
	}
	if !strings.Contains(string(sendBody), "MD5OfMessageBody") {
		t.Fatalf("expected MD5 in response, got: %s", string(sendBody))
	}

	// Receive
	recvBody := sqsQueueRequest(t, "e2e-sqs-msg", url.Values{
		"Action":              {"ReceiveMessage"},
		"QueueUrl":            {queueURL},
		"MaxNumberOfMessages": {"1"},
	})
	if !strings.Contains(string(recvBody), "hello from e2e") {
		t.Fatalf("expected message body in response, got: %s", string(recvBody))
	}
	if !strings.Contains(string(recvBody), "ReceiptHandle") {
		t.Fatalf("expected ReceiptHandle in response, got: %s", string(recvBody))
	}

	// Extract receipt handle for delete
	receiptStart := strings.Index(string(recvBody), "<ReceiptHandle>") + len("<ReceiptHandle>")
	receiptEnd := strings.Index(string(recvBody), "</ReceiptHandle>")
	receiptHandle := string(recvBody)[receiptStart:receiptEnd]

	// Delete message
	sqsQueueRequest(t, "e2e-sqs-msg", url.Values{
		"Action":        {"DeleteMessage"},
		"QueueUrl":      {queueURL},
		"ReceiptHandle": {receiptHandle},
	})

	// Verify empty
	recvBody2 := sqsQueueRequest(t, "e2e-sqs-msg", url.Values{
		"Action":              {"ReceiveMessage"},
		"QueueUrl":            {queueURL},
		"MaxNumberOfMessages": {"1"},
	})
	if strings.Contains(string(recvBody2), "hello from e2e") {
		t.Fatal("expected no messages after delete")
	}

	// Cleanup
	sqsQueueRequest(t, "e2e-sqs-msg", url.Values{
		"Action":   {"DeleteQueue"},
		"QueueUrl": {queueURL},
	})
}

func TestSQSFIFOQueue(t *testing.T) {
	// Create FIFO queue
	sqsRequest(t, url.Values{
		"Action":            {"CreateQueue"},
		"QueueName":         {"e2e-fifo.fifo"},
		"Attribute.1.Name":  {"FifoQueue"},
		"Attribute.1.Value": {"true"},
	})

	queueURL := endpoint + "/000000000000/e2e-fifo.fifo"

	// Send ordered messages
	for i := 0; i < 3; i++ {
		sqsQueueRequest(t, "e2e-fifo.fifo", url.Values{
			"Action":                 {"SendMessage"},
			"QueueUrl":               {queueURL},
			"MessageBody":            {fmt.Sprintf("msg-%d", i)},
			"MessageGroupId":         {"group1"},
			"MessageDeduplicationId": {fmt.Sprintf("dedup-%d", i)},
		})
	}

	// Receive first message (FIFO returns one per group)
	recvBody := sqsQueueRequest(t, "e2e-fifo.fifo", url.Values{
		"Action":              {"ReceiveMessage"},
		"QueueUrl":            {queueURL},
		"MaxNumberOfMessages": {"10"},
	})
	if !strings.Contains(string(recvBody), "msg-0") {
		t.Fatalf("expected first FIFO message 'msg-0', got: %s", string(recvBody))
	}

	// Cleanup
	sqsQueueRequest(t, "e2e-fifo.fifo", url.Values{
		"Action":   {"DeleteQueue"},
		"QueueUrl": {queueURL},
	})
}

func TestSQSPurgeQueue(t *testing.T) {
	sqsRequest(t, url.Values{
		"Action":    {"CreateQueue"},
		"QueueName": {"e2e-sqs-purge"},
	})

	queueURL := endpoint + "/000000000000/e2e-sqs-purge"

	// Send 3 messages
	for i := 0; i < 3; i++ {
		sqsQueueRequest(t, "e2e-sqs-purge", url.Values{
			"Action":      {"SendMessage"},
			"QueueUrl":    {queueURL},
			"MessageBody": {fmt.Sprintf("purge-msg-%d", i)},
		})
	}

	// Purge
	sqsQueueRequest(t, "e2e-sqs-purge", url.Values{
		"Action":   {"PurgeQueue"},
		"QueueUrl": {queueURL},
	})

	// Verify empty
	recvBody := sqsQueueRequest(t, "e2e-sqs-purge", url.Values{
		"Action":              {"ReceiveMessage"},
		"QueueUrl":            {queueURL},
		"MaxNumberOfMessages": {"10"},
	})
	if strings.Contains(string(recvBody), "purge-msg") {
		t.Fatal("expected no messages after purge")
	}

	// Cleanup
	sqsQueueRequest(t, "e2e-sqs-purge", url.Values{
		"Action":   {"DeleteQueue"},
		"QueueUrl": {queueURL},
	})
}

func TestSQSBatchOperations(t *testing.T) {
	sqsRequest(t, url.Values{
		"Action":    {"CreateQueue"},
		"QueueName": {"e2e-sqs-batch"},
	})

	queueURL := endpoint + "/000000000000/e2e-sqs-batch"

	// SendMessageBatch
	sendBody := sqsQueueRequest(t, "e2e-sqs-batch", url.Values{
		"Action":                            {"SendMessageBatch"},
		"QueueUrl":                          {queueURL},
		"SendMessageBatchRequestEntry.1.Id": {"msg1"},
		"SendMessageBatchRequestEntry.1.MessageBody": {"batch-1"},
		"SendMessageBatchRequestEntry.2.Id":          {"msg2"},
		"SendMessageBatchRequestEntry.2.MessageBody": {"batch-2"},
	})
	if !strings.Contains(string(sendBody), "msg1") || !strings.Contains(string(sendBody), "msg2") {
		t.Fatalf("expected both batch message IDs, got: %s", string(sendBody))
	}

	// Receive both
	recvBody := sqsQueueRequest(t, "e2e-sqs-batch", url.Values{
		"Action":              {"ReceiveMessage"},
		"QueueUrl":            {queueURL},
		"MaxNumberOfMessages": {"10"},
	})
	if !strings.Contains(string(recvBody), "batch-1") || !strings.Contains(string(recvBody), "batch-2") {
		t.Fatalf("expected both batch messages, got: %s", string(recvBody))
	}

	// Extract receipt handles for batch delete
	parts := strings.Split(string(recvBody), "<ReceiptHandle>")
	var handles []string
	for _, p := range parts[1:] {
		end := strings.Index(p, "</ReceiptHandle>")
		handles = append(handles, p[:end])
	}

	// DeleteMessageBatch
	delParams := url.Values{
		"Action":   {"DeleteMessageBatch"},
		"QueueUrl": {queueURL},
	}
	for i, h := range handles {
		delParams.Set(fmt.Sprintf("DeleteMessageBatchRequestEntry.%d.Id", i+1), fmt.Sprintf("del%d", i+1))
		delParams.Set(fmt.Sprintf("DeleteMessageBatchRequestEntry.%d.ReceiptHandle", i+1), h)
	}
	sqsQueueRequest(t, "e2e-sqs-batch", delParams)

	// Cleanup
	sqsQueueRequest(t, "e2e-sqs-batch", url.Values{
		"Action":   {"DeleteQueue"},
		"QueueUrl": {queueURL},
	})
}

func TestSQSGetQueueAttributes(t *testing.T) {
	sqsRequest(t, url.Values{
		"Action":    {"CreateQueue"},
		"QueueName": {"e2e-sqs-attrs"},
	})

	queueURL := endpoint + "/000000000000/e2e-sqs-attrs"

	// Send 2 messages
	for i := 0; i < 2; i++ {
		sqsQueueRequest(t, "e2e-sqs-attrs", url.Values{
			"Action":      {"SendMessage"},
			"QueueUrl":    {queueURL},
			"MessageBody": {fmt.Sprintf("attr-msg-%d", i)},
		})
	}

	// Get attributes
	attrBody := sqsQueueRequest(t, "e2e-sqs-attrs", url.Values{
		"Action":          {"GetQueueAttributes"},
		"QueueUrl":        {queueURL},
		"AttributeName.1": {"All"},
	})

	attrStr := string(attrBody)
	if !strings.Contains(attrStr, "ApproximateNumberOfMessages") {
		t.Fatalf("expected ApproximateNumberOfMessages, got: %s", attrStr)
	}
	if !strings.Contains(attrStr, "VisibilityTimeout") {
		t.Fatalf("expected VisibilityTimeout, got: %s", attrStr)
	}

	// Cleanup
	sqsQueueRequest(t, "e2e-sqs-attrs", url.Values{
		"Action":   {"DeleteQueue"},
		"QueueUrl": {queueURL},
	})
}

// --- Secrets Manager Tests ---

// secretsRequest sends a JSON-RPC style request to the Secrets Manager API.
func secretsRequest(t *testing.T, action string, body interface{}) map[string]interface{} {
	t.Helper()
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", endpoint+"/", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager."+action)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("secrets %s failed (%d): %s", action, resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	return result
}

func secretsRequestRaw(t *testing.T, action string, body interface{}) (*http.Response, []byte) {
	t.Helper()
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", endpoint+"/", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager."+action)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody
}

func TestSecretsCreateAndGet(t *testing.T) {
	// Create a secret
	createResult := secretsRequest(t, "CreateSecret", map[string]interface{}{
		"Name":         "e2e-secret-1",
		"SecretString": "my-password-123",
		"Description":  "e2e test secret",
	})

	if createResult["Name"] != "e2e-secret-1" {
		t.Fatalf("expected name 'e2e-secret-1', got %v", createResult["Name"])
	}
	if createResult["ARN"] == nil || createResult["ARN"] == "" {
		t.Fatal("expected non-empty ARN")
	}
	if createResult["VersionId"] == nil || createResult["VersionId"] == "" {
		t.Fatal("expected non-empty VersionId")
	}

	// Get secret value
	getResult := secretsRequest(t, "GetSecretValue", map[string]string{
		"SecretId": "e2e-secret-1",
	})

	if getResult["SecretString"] != "my-password-123" {
		t.Fatalf("expected secret value 'my-password-123', got %v", getResult["SecretString"])
	}
	if getResult["Name"] != "e2e-secret-1" {
		t.Fatalf("expected name 'e2e-secret-1', got %v", getResult["Name"])
	}

	// Get by ARN
	arn := createResult["ARN"].(string)
	getByArn := secretsRequest(t, "GetSecretValue", map[string]string{
		"SecretId": arn,
	})
	if getByArn["SecretString"] != "my-password-123" {
		t.Fatalf("expected secret value when fetched by ARN, got %v", getByArn["SecretString"])
	}

	// Cleanup
	secretsRequest(t, "DeleteSecret", map[string]interface{}{
		"SecretId":                   "e2e-secret-1",
		"ForceDeleteWithoutRecovery": true,
	})
}

func TestSecretsListAndDelete(t *testing.T) {
	// Create multiple secrets
	secretsRequest(t, "CreateSecret", map[string]interface{}{
		"Name":         "e2e-list-secret-a",
		"SecretString": "value-a",
	})
	secretsRequest(t, "CreateSecret", map[string]interface{}{
		"Name":         "e2e-list-secret-b",
		"SecretString": "value-b",
	})

	// List secrets
	listResult := secretsRequest(t, "ListSecrets", map[string]string{})
	secretList, ok := listResult["SecretList"].([]interface{})
	if !ok {
		t.Fatalf("expected SecretList array, got %T", listResult["SecretList"])
	}

	// Find our test secrets
	found := map[string]bool{}
	for _, item := range secretList {
		s, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := s["Name"].(string)
		if name == "e2e-list-secret-a" || name == "e2e-list-secret-b" {
			found[name] = true
		}
	}
	if !found["e2e-list-secret-a"] || !found["e2e-list-secret-b"] {
		t.Fatalf("expected both secrets in list, found: %v", found)
	}

	// Delete one
	secretsRequest(t, "DeleteSecret", map[string]interface{}{
		"SecretId":                   "e2e-list-secret-a",
		"ForceDeleteWithoutRecovery": true,
	})

	// Verify deleted
	resp, body := secretsRequestRaw(t, "GetSecretValue", map[string]string{
		"SecretId": "e2e-list-secret-a",
	})
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 after delete, got %d: %s", resp.StatusCode, string(body))
	}

	// Cleanup
	secretsRequest(t, "DeleteSecret", map[string]interface{}{
		"SecretId":                   "e2e-list-secret-b",
		"ForceDeleteWithoutRecovery": true,
	})
}

func TestSecretsUpdate(t *testing.T) {
	// Create
	secretsRequest(t, "CreateSecret", map[string]interface{}{
		"Name":         "e2e-update-secret",
		"SecretString": "old-value",
	})

	// Update via PutSecretValue
	updateResult := secretsRequest(t, "PutSecretValue", map[string]string{
		"SecretId":     "e2e-update-secret",
		"SecretString": "new-value",
	})
	if updateResult["Name"] != "e2e-update-secret" {
		t.Fatalf("expected name 'e2e-update-secret', got %v", updateResult["Name"])
	}

	// Get and verify
	getResult := secretsRequest(t, "GetSecretValue", map[string]string{
		"SecretId": "e2e-update-secret",
	})
	if getResult["SecretString"] != "new-value" {
		t.Fatalf("expected 'new-value', got %v", getResult["SecretString"])
	}

	// Cleanup
	secretsRequest(t, "DeleteSecret", map[string]interface{}{
		"SecretId":                   "e2e-update-secret",
		"ForceDeleteWithoutRecovery": true,
	})
}

func TestSecretsDescribeAndTags(t *testing.T) {
	// Create with tags
	secretsRequest(t, "CreateSecret", map[string]interface{}{
		"Name":         "e2e-tags-secret",
		"SecretString": "value",
		"Description":  "tagged secret",
		"Tags": []map[string]string{
			{"Key": "env", "Value": "test"},
		},
	})

	// Describe
	descResult := secretsRequest(t, "DescribeSecret", map[string]string{
		"SecretId": "e2e-tags-secret",
	})
	if descResult["Description"] != "tagged secret" {
		t.Fatalf("expected description 'tagged secret', got %v", descResult["Description"])
	}

	tags, ok := descResult["Tags"].([]interface{})
	if !ok || len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %v", descResult["Tags"])
	}

	// Add tag
	secretsRequest(t, "TagResource", map[string]interface{}{
		"SecretId": "e2e-tags-secret",
		"Tags": []map[string]string{
			{"Key": "version", "Value": "1.0"},
		},
	})

	// Verify 2 tags
	descResult2 := secretsRequest(t, "DescribeSecret", map[string]string{
		"SecretId": "e2e-tags-secret",
	})
	tags2, _ := descResult2["Tags"].([]interface{})
	if len(tags2) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags2))
	}

	// Remove tag
	secretsRequest(t, "UntagResource", map[string]interface{}{
		"SecretId": "e2e-tags-secret",
		"TagKeys":  []string{"env"},
	})

	// Verify 1 tag remaining
	descResult3 := secretsRequest(t, "DescribeSecret", map[string]string{
		"SecretId": "e2e-tags-secret",
	})
	tags3, _ := descResult3["Tags"].([]interface{})
	if len(tags3) != 1 {
		t.Fatalf("expected 1 tag after untag, got %d", len(tags3))
	}

	// Cleanup
	secretsRequest(t, "DeleteSecret", map[string]interface{}{
		"SecretId":                   "e2e-tags-secret",
		"ForceDeleteWithoutRecovery": true,
	})
}

func TestSecretsDuplicate(t *testing.T) {
	// Create
	secretsRequest(t, "CreateSecret", map[string]interface{}{
		"Name":         "e2e-dupe-secret",
		"SecretString": "value",
	})

	// Try to create again — should fail
	resp, _ := secretsRequestRaw(t, "CreateSecret", map[string]interface{}{
		"Name":         "e2e-dupe-secret",
		"SecretString": "value2",
	})
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for duplicate, got %d", resp.StatusCode)
	}

	// Cleanup
	secretsRequest(t, "DeleteSecret", map[string]interface{}{
		"SecretId":                   "e2e-dupe-secret",
		"ForceDeleteWithoutRecovery": true,
	})
}

func TestLambdaSecretsExtension(t *testing.T) {
	// This is the key differentiator test: create a secret, then create a Lambda
	// function that retrieves it via the secrets cache extension (localhost:2773).

	// Create a secret first
	secretsRequest(t, "CreateSecret", map[string]interface{}{
		"Name":         "e2e-lambda-secret",
		"SecretString": "super-secret-value",
	})

	// Create a Lambda function that fetches the secret via the extension endpoint
	handlerCode := `const http = require('http');

exports.handler = async (event) => {
  return new Promise((resolve, reject) => {
    const options = {
      hostname: 'localhost',
      port: 2773,
      path: '/secretsmanager/get?secretId=e2e-lambda-secret',
      method: 'GET',
      headers: {
        'X-Aws-Parameters-Secrets-Token': process.env.AWS_SESSION_TOKEN || 'test'
      }
    };

    const req = http.request(options, (res) => {
      let data = '';
      res.on('data', (chunk) => { data += chunk; });
      res.on('end', () => {
        try {
          const parsed = JSON.parse(data);
          resolve({
            statusCode: res.statusCode,
            body: JSON.stringify({
              secretValue: parsed.SecretString,
              secretName: parsed.Name,
              source: 'extension'
            })
          });
        } catch (e) {
          resolve({
            statusCode: 500,
            body: JSON.stringify({ error: 'parse error', raw: data })
          });
        }
      });
    });

    req.on('error', (e) => {
      resolve({
        statusCode: 500,
        body: JSON.stringify({ error: e.message })
      });
    });

    req.end();
  });
};`

	zipData := createZip(t, map[string]string{"index.js": handlerCode})

	createReq := map[string]interface{}{
		"FunctionName": "e2e-secrets-ext",
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::000000000000:role/test",
		"Timeout":      30,
		"MemorySize":   128,
		"Code": map[string]string{
			"ZipFile": base64.StdEncoding.EncodeToString(zipData),
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := http.Post(endpoint+"/2015-03-31/functions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 201 {
		t.Fatalf("create failed (%d): %s", resp.StatusCode, string(respBody))
	}

	t.Log("Lambda function with secrets extension created, invoking...")

	// Invoke the function
	invokeResp, err := http.Post(
		endpoint+"/2015-03-31/functions/e2e-secrets-ext/invocations",
		"application/json",
		strings.NewReader("{}"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer invokeResp.Body.Close()
	invokeBody, _ := io.ReadAll(invokeResp.Body)

	t.Logf("Secrets extension invoke response (%d): %s", invokeResp.StatusCode, string(invokeBody))

	if invokeResp.StatusCode != 200 {
		t.Fatalf("invoke failed (%d): %s", invokeResp.StatusCode, string(invokeBody))
	}

	// Parse the response
	var invokeResult map[string]interface{}
	if err := json.Unmarshal(invokeBody, &invokeResult); err != nil {
		t.Fatalf("failed to parse invoke response: %v", err)
	}

	bodyStr, ok := invokeResult["body"].(string)
	if !ok {
		t.Fatalf("expected 'body' field in response, got: %s", string(invokeBody))
	}

	// The function should have received the secret via the extension
	if !strings.Contains(bodyStr, "super-secret-value") {
		t.Fatalf("expected secret value 'super-secret-value' in response body, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "extension") {
		t.Fatalf("expected 'extension' source marker in response body, got: %s", bodyStr)
	}

	t.Log("Lambda successfully retrieved secret via the secrets cache extension!")

	// Cleanup
	delReq, _ := http.NewRequest("DELETE", endpoint+"/2015-03-31/functions/e2e-secrets-ext", nil)
	delResp, _ := http.DefaultClient.Do(delReq)
	delResp.Body.Close()

	secretsRequest(t, "DeleteSecret", map[string]interface{}{
		"SecretId":                   "e2e-lambda-secret",
		"ForceDeleteWithoutRecovery": true,
	})
}

// --- Helpers ---

func createZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}
