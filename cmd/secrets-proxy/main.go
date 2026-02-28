// secrets-proxy is a lightweight HTTP proxy that runs inside Lambda containers.
// It implements the AWS Parameters and Secrets Lambda Extension HTTP API on port 2773,
// forwarding requests to the OpenStack Secrets Manager API via host.docker.internal.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	port := os.Getenv("PARAMETERS_SECRETS_EXTENSION_HTTP_PORT")
	if port == "" {
		port = "2773"
	}

	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if endpoint == "" {
		endpoint = "http://host.docker.internal:4566"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/secretsmanager/get", func(w http.ResponseWriter, r *http.Request) {
		handleGetSecret(w, r, endpoint)
	})
	mux.HandleFunc("/systemsmanager/parameters/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(501)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "SSM Parameter Store not yet implemented",
		})
	})

	addr := ":" + port
	log.Printf("[secrets-proxy] listening on %s, upstream: %s", addr, endpoint)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[secrets-proxy] failed to start: %v", err)
	}
}

func handleGetSecret(w http.ResponseWriter, r *http.Request, endpoint string) {
	secretId := r.URL.Query().Get("secretId")
	if secretId == "" {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "secretId parameter is required"})
		return
	}

	// Build the request to OpenStack Secrets Manager API
	reqBody := map[string]string{"SecretId": secretId}
	if v := r.URL.Query().Get("versionId"); v != "" {
		reqBody["VersionId"] = v
	}
	if v := r.URL.Query().Get("versionStage"); v != "" {
		reqBody["VersionStage"] = v
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", endpoint+"/", strings.NewReader(string(bodyBytes)))
	if err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.WriteHeader(502)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("failed to reach secrets manager: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	// Forward response as-is
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
