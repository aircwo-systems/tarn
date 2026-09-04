// Package mcp exposes a running Tarn instance to LLM tooling over the Model
// Context Protocol.
//
// The server is a client of Tarn's HTTP API rather than an in-process embed.
// That keeps it a thin translation layer, lets it target any instance the CLI
// can reach, and means it carries no dependency on the service packages — so
// it builds in both the full and lite binaries.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// numericAKID matches the 12-digit access key Tarn treats as an account ID.
// See internal/account.FromRequest, which resolves the account per request
// from the SigV4 Authorization header.
var numericAKID = regexp.MustCompile(`^\d{12}$`)

// client talks to a Tarn instance's admin API.
type client struct {
	endpoint string
	http     *http.Client
}

func newClient(endpoint string) *client {
	return &client{
		endpoint: endpoint,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// errNotRunning reports that nothing answered at the endpoint. Callers turn
// this into a structured tool result rather than a tool error, so the model
// gets a remediation instead of an opaque failure it will retry blindly.
type errNotRunning struct {
	endpoint string
	cause    error
}

func (e *errNotRunning) Error() string {
	return fmt.Sprintf("no Tarn instance answered at %s: %v", e.endpoint, e.cause)
}

// get issues a GET against the instance and decodes the JSON body into out.
//
// account, when set to a 12-digit ID, is sent as a SigV4 Authorization header.
// Tarn resolves per-account isolation from the access key in that header, so
// this is the only way to address a non-default account.
func (c *client) get(ctx context.Context, path, account string, query url.Values, out any) error {
	u := c.endpoint + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if numericAKID.MatchString(account) {
		req.Header.Set("Authorization", fmt.Sprintf(
			"AWS4-HMAC-SHA256 Credential=%s/00000000/us-east-1/tarn/aws4_request", account))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &errNotRunning{endpoint: c.endpoint, cause: err}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// do issues a request against the instance, returning the response for the
// caller to read. Callers own closing the body.
//
// account, when set to a 12-digit ID, is sent as a SigV4 Authorization header.
// Tarn resolves per-account isolation from the access key in that header, so
// this is the only way to address a non-default account.
func (c *client) do(ctx context.Context, method, path, account, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if numericAKID.MatchString(account) {
		req.Header.Set("Authorization", fmt.Sprintf(
			"AWS4-HMAC-SHA256 Credential=%s/00000000/us-east-1/tarn/aws4_request", account))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &errNotRunning{endpoint: c.endpoint, cause: err}
	}
	return resp, nil
}

// postJSON sends in as JSON and decodes the response body into out.
// out may be nil when the caller does not need the body.
func (c *client) postJSON(ctx context.Context, path, account string, in, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}

	resp, err := c.do(ctx, http.MethodPost, path, account, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s returned HTTP %d: %s", path, resp.StatusCode, readSnippet(resp.Body))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// postForm sends form as an AWS query-protocol request. Those APIs answer with
// XML, so the raw body is returned for the caller to parse.
func (c *client) postForm(ctx context.Context, path, account string, form url.Values) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodPost, path, account,
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s returned HTTP %d: %s", path, resp.StatusCode, truncate(string(body), 400))
	}
	return body, nil
}

// readSnippet reads a bounded prefix of r for use in an error message.
func readSnippet(r io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(r, 2048))
	return truncate(strings.TrimSpace(string(body)), 400)
}

// truncate shortens s to at most n bytes, marking that it was cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "… (truncated)"
}
