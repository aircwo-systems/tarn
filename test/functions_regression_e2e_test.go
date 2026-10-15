package test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type functionHTTPResult struct {
	status  int
	headers http.Header
	body    []byte
	err     error
}

func callFunctionHTTP(method, path string, body any, headers ...http.Header) functionHTTPResult {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return functionHTTPResult{err: err}
		}
	}
	r, err := http.NewRequest(method, endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return functionHTTPResult{err: err}
	}
	r.Header.Set("Content-Type", "application/json")
	for _, values := range headers {
		for name, entries := range values {
			for _, value := range entries {
				r.Header.Add(name, value)
			}
		}
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(r)
	if err != nil {
		return functionHTTPResult{err: err}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	return functionHTTPResult{status: response.StatusCode, headers: response.Header, body: data, err: err}
}

func functionHTTP(t *testing.T, method, path string, body any, want int) functionHTTPResult {
	t.Helper()
	result := callFunctionHTTP(method, path, body)
	if result.err != nil || result.status != want {
		t.Fatalf("%s %s: status=%d body=%s err=%v", method, path, result.status, result.body, result.err)
	}
	return result
}

func cleanupFunctionResource(t *testing.T, path string) {
	t.Helper()
	t.Cleanup(func() {
		result := callFunctionHTTP(http.MethodDelete, path, nil)
		if result.err != nil || (result.status != 204 && result.status != 404) {
			t.Errorf("cleanup %s: %d %s %v", path, result.status, result.body, result.err)
		}
	})
}

func awaitFunctionLog(t *testing.T, name, marker string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ids, err := exec.Command("docker", "ps", "-q", "--filter", "label=tarn.function="+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range strings.Fields(string(ids)) {
			logs, err := exec.Command("docker", "logs", id).CombinedOutput()
			if err == nil && strings.Contains(string(logs), marker) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("function never started invocation %s", marker)
}

func decodeFunctionResult(t *testing.T, result functionHTTPResult) map[string]any {
	t.Helper()
	if result.err != nil || result.status != 200 || result.headers.Get("X-Amz-Function-Error") != "" {
		t.Fatalf("invoke failed: %d functionError=%s body=%s err=%v", result.status, result.headers.Get("X-Amz-Function-Error"), result.body, result.err)
	}
	var body map[string]any
	if err := json.Unmarshal(result.body, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestFunctionLayersVersionsUpdatesAndConcurrencyE2E(t *testing.T) {
	name := fmt.Sprintf("e2e-fn-revisions-%d", time.Now().UnixNano())
	var layers []string
	for i, word := range []string{"first", "second"} {
		layer := fmt.Sprintf("%s-layer%d", name, i)
		code := createZip(t, map[string]string{fmt.Sprintf("nodejs/node_modules/helper%d/index.js", i): fmt.Sprintf("module.exports.value = %d;", i), "shared.txt": word, "nested/data/value.txt": word})
		result := functionHTTP(t, http.MethodPost, "/2015-03-31/layers/"+layer+"/versions", map[string]any{"Content": map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(code)}}, 201)
		var published struct {
			LayerVersionArn string
			Version         int
		}
		if err := json.Unmarshal(result.body, &published); err != nil {
			t.Fatal(err)
		}
		layers = append(layers, published.LayerVersionArn)
		cleanupFunctionResource(t, fmt.Sprintf("/2015-03-31/layers/%s/versions/%d", layer, published.Version))
	}
	t.Run("one local layer", func(t *testing.T) {
		oneName := name + "-one"
		onePath := "/2015-03-31/functions/" + oneName
		code := createZip(t, map[string]string{"index.js": `const fs = require("fs"); exports.handler = async () => ({helper:require("helper0").value, shared:fs.readFileSync("/opt/shared.txt","utf8"), nested:fs.readFileSync("/opt/nested/data/value.txt","utf8")});`})
		functionHTTP(t, http.MethodPost, "/2015-03-31/functions", map[string]any{"FunctionName": oneName, "Runtime": "nodejs20.x", "Architectures": []string{testLambdaArchitecture}, "Handler": "index.handler", "Timeout": 10, "MemorySize": 128, "Layers": layers[:1], "Code": map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(code)}}, 201)
		cleanupFunctionResource(t, onePath)
		for range 2 {
			result := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, onePath+"/invocations", map[string]any{}, 200))
			if result["helper"] != float64(0) || result["shared"] != "first" || result["nested"] != "first" {
				t.Fatalf("one-layer cold/warm execution lost content: %+v", result)
			}
		}
	})
	codeFor := func(version string) []byte {
		return createZip(t, map[string]string{"index.js": fmt.Sprintf(`const fs = require("fs"); exports.handler = async event => { if (event.marker) console.log(event.marker); if (event.sleep) await new Promise(resolve => setTimeout(resolve,event.sleep)); return {code:%q, marker:fs.readFileSync("/var/task/marker.txt","utf8"), environment:process.env.KEY, version:process.env.AWS_LAMBDA_FUNCTION_VERSION, layers:require("helper0").value+require("helper1").value, shared:fs.readFileSync("/opt/shared.txt","utf8")}; };`, version), "marker.txt": version})
	}
	path := "/2015-03-31/functions/" + name
	functionHTTP(t, http.MethodPost, "/2015-03-31/functions", map[string]any{"FunctionName": name, "Runtime": "nodejs20.x", "Architectures": []string{testLambdaArchitecture}, "Handler": "index.handler", "Timeout": 10, "MemorySize": 128, "Layers": layers, "Environment": map[string]any{"Variables": map[string]string{"KEY": "original"}}, "Code": map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(codeFor("v1"))}}, 201)
	cleanupFunctionResource(t, path)
	first := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 200))
	if first["layers"] != float64(1) || first["shared"] != "second" {
		t.Fatalf("two layers not merged: %+v", first)
	}
	functionHTTP(t, http.MethodPost, path+"/versions", map[string]any{}, 201)
	functionHTTP(t, http.MethodPost, path+"/aliases", map[string]string{"Name": "live", "FunctionVersion": "1"}, 201)
	functionHTTP(t, http.MethodPut, path+"/configuration", map[string]any{"Layers": []string{layers[1], layers[0]}, "Environment": map[string]any{"Variables": map[string]string{"KEY": "updated"}}}, 200)
	reversed := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 200))
	if reversed["shared"] != "first" || reversed["environment"] != "updated" {
		t.Fatalf("layer order/environment update ignored: %+v", reversed)
	}
	old := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations?Qualifier=live", map[string]any{}, 200))
	if old["shared"] != "second" || old["environment"] != "original" || old["version"] != "1" {
		t.Fatalf("published configuration changed: %+v", old)
	}

	// A running function reads its code file after the update commits. It must
	// still read the deployment it acquired rather than the replacement files.
	marker := "update-in-flight-" + name
	oldCall := make(chan functionHTTPResult, 1)
	go func() {
		oldCall <- callFunctionHTTP(http.MethodPost, path+"/invocations", map[string]any{"marker": marker, "sleep": 1800})
	}()
	awaitFunctionLog(t, name, marker)
	functionHTTP(t, http.MethodPut, path+"/code", map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(codeFor("v2"))}, 200)
	current := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 200))
	if current["code"] != "v2" || current["marker"] != "v2" || current["shared"] != "first" {
		t.Fatalf("new deployment did not execute: %+v", current)
	}
	old = decodeFunctionResult(t, <-oldCall)
	if old["code"] != "v1" || old["marker"] != "v1" {
		t.Fatalf("running invocation changed deployment: %+v", old)
	}
	for range 2 {
		old = decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations?Qualifier=1", map[string]any{}, 200))
		if old["code"] != "v1" || old["marker"] != "v1" || old["shared"] != "second" {
			t.Fatalf("published code changed: %+v", old)
		}
	}
	functionHTTP(t, http.MethodPost, path+"/versions", map[string]any{}, 201)
	functionHTTP(t, http.MethodPut, path+"/aliases/live", map[string]string{"FunctionVersion": "2"}, 200)
	alias := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations?Qualifier=live", map[string]any{}, 200))
	if alias["code"] != "v2" || alias["version"] != "2" {
		t.Fatalf("alias target ignored: %+v", alias)
	}

	concurrency := "/2017-10-31/functions/" + name + "/concurrency"
	functionHTTP(t, http.MethodPut, concurrency, map[string]int{"ReservedConcurrentExecutions": 1}, 200)
	marker = "concurrency-in-flight-" + name
	go func() {
		oldCall <- callFunctionHTTP(http.MethodPost, path+"/invocations", map[string]any{"marker": marker, "sleep": 1500})
	}()
	awaitFunctionLog(t, name, marker)
	throttled := functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 429)
	if throttled.headers.Get("X-Amzn-ErrorType") != "TooManyRequestsException" {
		t.Fatalf("wrong concurrency error: %s", throttled.body)
	}
	decodeFunctionResult(t, <-oldCall)
	functionHTTP(t, http.MethodPut, concurrency, map[string]int{"ReservedConcurrentExecutions": 0}, 200)
	functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 429)
	asyncMarker := "queued-while-paused-" + name
	accepted := callFunctionHTTP(http.MethodPost, path+"/invocations", map[string]any{"marker": asyncMarker}, http.Header{"X-Amz-Invocation-Type": []string{"Event"}})
	if accepted.err != nil || accepted.status != 202 {
		t.Fatalf("async enqueue while paused: %d %s %v", accepted.status, accepted.body, accepted.err)
	}
	time.Sleep(50 * time.Millisecond)
	functionHTTP(t, http.MethodDelete, concurrency, nil, 204)
	awaitFunctionLog(t, name, asyncMarker)
	decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 200))

	marker = "delete-in-flight-" + name
	go func() {
		oldCall <- callFunctionHTTP(http.MethodPost, path+"/invocations", map[string]any{"marker": marker, "sleep": 1500})
	}()
	awaitFunctionLog(t, name, marker)
	functionHTTP(t, http.MethodDelete, path, nil, 204)
	functionHTTP(t, http.MethodGet, path, nil, 404)
	old = decodeFunctionResult(t, <-oldCall)
	if old["code"] != "v2" || old["marker"] != "v2" {
		t.Fatalf("deletion interrupted running invocation: %+v", old)
	}
}

func TestImageFunctionAndPublishedImageVersionE2E(t *testing.T) {
	name := fmt.Sprintf("e2e-fn-image-%d", time.Now().UnixNano())
	image := "tarn-test-lambda-" + name + ":latest"
	buildDir := t.TempDir()
	var imageIDs []string
	t.Cleanup(func() {
		// Function deletion retires containers asynchronously. Wait before
		// removing this test's images, without touching other daemon resources.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			ids, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=tarn.function="+name).Output()
			if len(strings.TrimSpace(string(ids))) == 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		for _, ref := range append([]string{image}, imageIDs...) {
			_ = exec.Command("docker", "image", "rm", "--force", ref).Run()
		}
	})
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte("FROM public.ecr.aws/lambda/nodejs:20\nCOPY index.js /var/task/index.js\nCMD [\"index.handler\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	build := func(value string) string {
		t.Helper()
		code := fmt.Sprintf(`exports.handler = async () => ({value:%q, version:process.env.AWS_LAMBDA_FUNCTION_VERSION});`, value)
		if err := os.WriteFile(filepath.Join(buildDir, "index.js"), []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
		command := exec.Command("docker", "build", "--platform", "linux/"+map[string]string{"arm64": "arm64", "x86_64": "amd64"}[testLambdaArchitecture], "--quiet", "-t", image, buildDir)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			t.Fatalf("build test image: %v %s", err, stderr.String())
		}
		id := strings.TrimSpace(string(output))
		imageIDs = append(imageIDs, id)
		return id
	}
	firstID := build("first")
	path := "/2015-03-31/functions/" + name
	functionHTTP(t, http.MethodPost, "/2015-03-31/functions", map[string]any{"FunctionName": name, "PackageType": "Image", "Architectures": []string{testLambdaArchitecture}, "Timeout": 10, "MemorySize": 128, "Code": map[string]string{"ImageUri": image}}, 201)
	cleanupFunctionResource(t, path)
	first := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 200))
	if first["value"] != "first" {
		t.Fatal(first)
	}
	functionHTTP(t, http.MethodPost, path+"/versions", map[string]any{}, 201)
	secondID := build("second")
	if firstID == secondID {
		t.Fatal("fixture images have identical IDs")
	}
	functionHTTP(t, http.MethodPut, path+"/code", map[string]string{"ImageUri": image}, 200)
	second := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 200))
	if second["value"] != "second" {
		t.Fatalf("image update ignored: %+v", second)
	}
	for range 2 {
		first = decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations?Qualifier=1", map[string]any{}, 200))
		if first["value"] != "first" || first["version"] != "1" {
			t.Fatalf("published image changed after retagging: %+v", first)
		}
	}
	metadata := functionHTTP(t, http.MethodGet, path, nil, 200)
	var stored struct {
		Code struct {
			ImageUri         string
			ResolvedImageUri string
		}
	}
	if err := json.Unmarshal(metadata.body, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Code.ImageUri != image || stored.Code.ResolvedImageUri != secondID {
		t.Fatalf("image metadata not pinned: %s", metadata.body)
	}
}
