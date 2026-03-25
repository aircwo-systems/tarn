package secretsproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	tokenHeader        = "X-Aws-Parameters-Secrets-Token"
	functionNameHeader = "X-Tarn-Function-Name"
)

// Options configures the local secrets extension proxy behavior.
type Options struct {
	UpstreamURL  string
	SessionToken string
	RequireToken bool
	HTTPClient   *http.Client
	OnRequest    func(RequestEvent)
}

// RequestEvent records one proxy request outcome for observability.
type RequestEvent struct {
	StartedAt    time.Time
	DurationMs   int64
	SecretID     string
	FunctionName string
	Source       string
	TokenValid   bool
	StatusCode   int
	Error        string
}

// NewHandler returns an HTTP handler that mimics the AWS Parameters and Secrets
// Lambda Extension API surface required by local Lambda runtimes.
func NewHandler(opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/secretsmanager/get", func(w http.ResponseWriter, r *http.Request) {
		handleGetSecret(w, r, opts)
	})
	mux.HandleFunc("/systemsmanager/parameters/get", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "SSM Parameter Store not yet implemented",
		})
	})
	return mux
}

// ListenAndServe starts a local proxy HTTP server.
func ListenAndServe(addr string, opts Options) error {
	return http.ListenAndServe(addr, NewHandler(opts))
}

func handleGetSecret(w http.ResponseWriter, r *http.Request, opts Options) {
	start := time.Now().UTC()
	if opts.RequireToken {
		if r.Header.Get(tokenHeader) != opts.SessionToken {
			emitRequest(opts, RequestEvent{
				StartedAt:    start,
				DurationMs:   time.Since(start).Milliseconds(),
				SecretID:     r.URL.Query().Get("secretId"),
				FunctionName: strings.TrimSpace(r.Header.Get(functionNameHeader)),
				Source:       "local-secrets-proxy",
				TokenValid:   false,
				StatusCode:   http.StatusForbidden,
				Error:        "invalid secrets extension session token",
			})
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "invalid secrets extension session token",
			})
			return
		}
	}

	secretID := r.URL.Query().Get("secretId")
	if secretID == "" {
		emitRequest(opts, RequestEvent{
			StartedAt:    start,
			DurationMs:   time.Since(start).Milliseconds(),
			SecretID:     "",
			FunctionName: strings.TrimSpace(r.Header.Get(functionNameHeader)),
			Source:       "local-secrets-proxy",
			TokenValid:   true,
			StatusCode:   http.StatusBadRequest,
			Error:        "secretId parameter is required",
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "secretId parameter is required",
		})
		return
	}

	upstream := strings.TrimSuffix(opts.UpstreamURL, "/")
	if upstream == "" {
		emitRequest(opts, RequestEvent{
			StartedAt:    start,
			DurationMs:   time.Since(start).Milliseconds(),
			SecretID:     secretID,
			FunctionName: strings.TrimSpace(r.Header.Get(functionNameHeader)),
			Source:       "local-secrets-proxy",
			TokenValid:   true,
			StatusCode:   http.StatusBadRequest,
			Error:        "upstream endpoint is required",
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "upstream endpoint is required",
		})
		return
	}

	reqBody := map[string]string{
		"SecretId": secretID,
	}
	if v := r.URL.Query().Get("versionId"); v != "" {
		reqBody["VersionId"] = v
	}
	if v := r.URL.Query().Get("versionStage"); v != "" {
		reqBody["VersionStage"] = v
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		emitRequest(opts, RequestEvent{
			StartedAt:    start,
			DurationMs:   time.Since(start).Milliseconds(),
			SecretID:     secretID,
			FunctionName: strings.TrimSpace(r.Header.Get(functionNameHeader)),
			Source:       "local-secrets-proxy",
			TokenValid:   true,
			StatusCode:   http.StatusInternalServerError,
			Error:        fmt.Sprintf("failed to encode request: %v", err),
		})
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to encode request: %v", err),
		})
		return
	}

	req, err := http.NewRequest(http.MethodPost, upstream+"/", bytes.NewReader(payload))
	if err != nil {
		emitRequest(opts, RequestEvent{
			StartedAt:    start,
			DurationMs:   time.Since(start).Milliseconds(),
			SecretID:     secretID,
			FunctionName: strings.TrimSpace(r.Header.Get(functionNameHeader)),
			Source:       "local-secrets-proxy",
			TokenValid:   true,
			StatusCode:   http.StatusInternalServerError,
			Error:        err.Error(),
		})
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")

	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		emitRequest(opts, RequestEvent{
			StartedAt:    start,
			DurationMs:   time.Since(start).Milliseconds(),
			SecretID:     secretID,
			FunctionName: strings.TrimSpace(r.Header.Get(functionNameHeader)),
			Source:       "local-secrets-proxy",
			TokenValid:   true,
			StatusCode:   http.StatusBadGateway,
			Error:        fmt.Sprintf("failed to reach secrets manager: %v", err),
		})
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("failed to reach secrets manager: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
	emitRequest(opts, RequestEvent{
		StartedAt:    start,
		DurationMs:   time.Since(start).Milliseconds(),
		SecretID:     secretID,
		FunctionName: strings.TrimSpace(r.Header.Get(functionNameHeader)),
		Source:       "local-secrets-proxy",
		TokenValid:   true,
		StatusCode:   resp.StatusCode,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// EncodeSecretPath returns a query string-safe path for the extension API.
func EncodeSecretPath(secretID string) string {
	return "/secretsmanager/get?secretId=" + url.QueryEscape(secretID)
}

func emitRequest(opts Options, event RequestEvent) {
	if opts.OnRequest != nil {
		opts.OnRequest(event)
	}
}
