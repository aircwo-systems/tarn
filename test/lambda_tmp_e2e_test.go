package test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestLambdaTmpExecutablesE2E(t *testing.T) {
	name := fmt.Sprintf("e2e-lambda-tmp-exec-%d", time.Now().UnixNano())
	path := "/2015-03-31/functions/" + name
	code := createZip(t, map[string]string{"index.js": `exports.handler = async () => {
  const fs = require("fs");
  fs.copyFileSync("/bin/true", "/tmp/lambda-executable");
  fs.chmodSync("/tmp/lambda-executable", 0o700);
  require("child_process").execFileSync("/tmp/lambda-executable");
  return {executed:true};
};`})
	functionHTTP(t, http.MethodPost, "/2015-03-31/functions", map[string]any{"FunctionName": name, "Runtime": "nodejs20.x", "Architectures": []string{testLambdaArchitecture}, "Handler": "index.handler", "Timeout": 10, "MemorySize": 128, "Code": map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(code)}}, 201)
	cleanupFunctionResource(t, path)
	for range 2 {
		result := decodeFunctionResult(t, functionHTTP(t, http.MethodPost, path+"/invocations", map[string]any{}, 200))
		if result["executed"] != true {
			t.Fatalf("cold/warm invocation could not execute its temporary binary: %+v", result)
		}
	}
}
