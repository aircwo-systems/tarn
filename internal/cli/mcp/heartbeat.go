package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// heartbeatInterval is how often a running `tarn mcp` reports itself. The
// instance treats a session as gone after three missed beats.
const heartbeatInterval = 15 * time.Second

// heartbeat reports this MCP server to the instance so the dashboard can show
// which editors are connected. It is best-effort: an instance that is down or
// predates the endpoint just never lists the session.
type heartbeat struct {
	endpoint string
	version  string
	server   *mcp.Server
	id       string
	http     *http.Client
	warned   bool
}

func newHeartbeat(endpoint, version string, server *mcp.Server) *heartbeat {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return &heartbeat{
		endpoint: endpoint,
		version:  version,
		server:   server,
		id:       hex.EncodeToString(buf),
		http:     &http.Client{Timeout: 2 * time.Second},
	}
}

// run beats until ctx is done, then tells the instance the session ended.
func (h *heartbeat) run(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	h.beat(ctx)
	for {
		select {
		case <-ctx.Done():
			h.goodbye()
			return
		case <-ticker.C:
			h.beat(ctx)
		}
	}
}

func (h *heartbeat) beat(ctx context.Context) {
	body := map[string]any{
		"id":            h.id,
		"serverVersion": h.version,
		"pid":           os.Getpid(),
	}
	if info := h.clientInfo(); info != nil {
		body["clientName"] = firstNonEmptyString(info.Title, info.Name)
		body["clientVersion"] = info.Version
	}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint+"/_tarn/admin/mcp/heartbeat", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.http.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode < 300 {
			h.warned = false
			return
		}
		err = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if ctx.Err() == nil && !h.warned {
		// stdout carries the protocol; diagnostics go to stderr, once per outage.
		fmt.Fprintf(os.Stderr, "tarn mcp: heartbeat to %s failed: %v\n", h.endpoint, err)
		h.warned = true
	}
}

func (h *heartbeat) goodbye() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, h.endpoint+"/_tarn/admin/mcp/sessions/"+h.id, nil)
	if err != nil {
		return
	}
	if resp, err := h.http.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

// clientInfo returns the connected editor's identity once it has initialized.
func (h *heartbeat) clientInfo() *mcp.Implementation {
	for session := range h.server.Sessions() {
		if params := session.InitializeParams(); params != nil && params.ClientInfo != nil {
			return params.ClientInfo
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
