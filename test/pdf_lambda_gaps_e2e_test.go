package test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestPDFLambdaGapsE2E(t *testing.T) {
	t.Run("standard layer publishing and mounting", func(t *testing.T) {
		name := fmt.Sprintf("e2e-pdf-layer-%d", time.Now().UnixNano())
		layerPath := "/2018-10-31/layers/" + name + "/versions"
		code := createZip(t, map[string]string{"chromium/marker.txt": "mounted chromium directory"})
		published := functionHTTP(t, http.MethodPost, layerPath, map[string]any{"Content": map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(code)}, "CompatibleRuntimes": []string{"nodejs20.x"}}, 201)
		var layer struct {
			LayerVersionArn string
			Version         int64
		}
		if err := json.Unmarshal(published.body, &layer); err != nil {
			t.Fatal(err)
		}
		if layer.LayerVersionArn == "" || layer.Version != 1 {
			t.Fatalf("invalid layer response: %s", published.body)
		}
		cleanupFunctionResource(t, layerPath+"/1")
		functionHTTP(t, http.MethodGet, layerPath+"/1", nil, 200)
		path := "/2015-03-31/functions/" + name
		code = createZip(t, map[string]string{"index.js": `exports.handler = async () => ({marker:require("fs").readFileSync("/opt/chromium/marker.txt","utf8")});`})
		functionHTTP(t, http.MethodPost, "/2015-03-31/functions", map[string]any{"FunctionName": name, "Runtime": "nodejs20.x", "Architectures": []string{testLambdaArchitecture}, "Handler": "index.handler", "Timeout": 10, "MemorySize": 128, "Layers": []string{layer.LayerVersionArn}, "Code": map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(code)}}, 201)
		cleanupFunctionResource(t, path)
		for range 2 {
			result := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 200))
			if result["marker"] != "mounted chromium directory" {
				t.Fatalf("cold/warm layer mount missing: %+v", result)
			}
		}
	})
	t.Run("synchronous handler errors", func(t *testing.T) {
		name := fmt.Sprintf("e2e-pdf-error-%d", time.Now().UnixNano())
		path := "/2015-03-31/functions/" + name
		code := createZip(t, map[string]string{"index.js": `exports.handler = async event => { if (event.fail) throw new Error("pdf handler regression"); return {errorType:"Error",errorMessage:"ordinary successful result"}; };`})
		functionHTTP(t, http.MethodPost, "/2015-03-31/functions", map[string]any{"FunctionName": name, "Runtime": "nodejs20.x", "Architectures": []string{testLambdaArchitecture}, "Handler": "index.handler", "Timeout": 10, "MemorySize": 128, "Code": map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(code)}}, 201)
		cleanupFunctionResource(t, path)
		for _, fail := range []bool{true, false, true, false} {
			result := callFunctionHTTP(http.MethodPost, path+"/invocations", map[string]bool{"fail": fail}, http.Header{"X-Amz-Invocation-Type": []string{"RequestResponse"}})
			if result.err != nil || result.status != http.StatusOK {
				t.Fatalf("invoke status=%d body=%s err=%v", result.status, result.body, result.err)
			}
			wantHeader, wantMessage := "", "ordinary successful result"
			if fail {
				wantHeader, wantMessage = "Unhandled", "pdf handler regression"
			}
			if got := result.headers.Get("X-Amz-Function-Error"); got != wantHeader {
				t.Errorf("fail=%t FunctionError=%q, want %q; body=%s", fail, got, wantHeader, result.body)
			}
			var body struct{ ErrorType, ErrorMessage string }
			if err := json.Unmarshal(result.body, &body); err != nil {
				t.Fatal(err)
			}
			if body.ErrorType != "Error" || body.ErrorMessage != wantMessage {
				t.Fatalf("fail=%t unexpected payload: %s", fail, result.body)
			}
		}
	})
}
