// Package mcp exposes a running Tarn instance to LLM tooling over the Model
// Context Protocol.
//
// The server is a client of Tarn's HTTP API rather than an in-process embed.
// That keeps it a thin translation layer, lets it target any instance the CLI
// can reach, and means it carries no dependency on the service packages — so
// it builds in both the full and lite binaries.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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
