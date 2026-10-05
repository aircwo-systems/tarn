package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/secretsproxy"
)

func TestConnectionsMCPSessionsExpireAfterMissedHeartbeats(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	c := NewConnections()
	c.now = func() time.Time { return now }
	cfg := config.Default()

	c.heartbeat(mcpSession{ID: "a", ClientName: "Claude Code"})
	now = now.Add(30 * time.Second)
	c.heartbeat(mcpSession{ID: "b", ClientName: "VS Code"})
	c.heartbeat(mcpSession{ID: "a", ClientName: "Claude Code"})

	sessions := c.snapshot(cfg).MCP.Sessions
	if len(sessions) != 2 || sessions[0].ID != "a" {
		t.Fatalf("want sessions [a b], got %+v", sessions)
	}
	if !sessions[0].ConnectedAt.Equal(now.Add(-30 * time.Second)) {
		t.Fatalf("re-beat should keep first ConnectedAt, got %v", sessions[0].ConnectedAt)
	}

	now = now.Add(mcpSessionTTL + time.Second)
	if got := c.snapshot(cfg).MCP.Sessions; len(got) != 0 {
		t.Fatalf("stale sessions should drop, got %+v", got)
	}

	c.heartbeat(mcpSession{ID: "c"})
	c.disconnect("c")
	if got := c.snapshot(cfg).MCP.Sessions; len(got) != 0 {
		t.Fatalf("disconnected session should drop, got %+v", got)
	}
}

func TestConnectionsSecretsProxyStatsHideToken(t *testing.T) {
	c := NewConnections()
	cfg := config.Default()
	cfg.ExposeSecretsProxy = true
	cfg.SecretsProxySessionToken = "super-secret-value"

	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	c.RecordSecretsProxy(secretsproxy.RequestEvent{StartedAt: at, SecretID: "db", FunctionName: "api", TokenValid: true, StatusCode: 200})
	c.RecordSecretsProxy(secretsproxy.RequestEvent{StartedAt: at.Add(time.Second), SecretID: "db", ClientIP: "10.0.0.1", TokenValid: false, StatusCode: 403})

	view := c.snapshot(cfg).SecretsProxy
	if !view.Enabled || view.Address != "127.0.0.1:2773" || view.DefaultToken {
		t.Fatalf("unexpected config view %+v", view)
	}
	if view.Requests != 2 || view.Denied != 1 || view.LastCaller != "10.0.0.1" || view.LastStatus != 403 {
		t.Fatalf("unexpected stats %+v", view)
	}
	raw, _ := json.Marshal(c.snapshot(cfg))
	if strings.Contains(string(raw), "super-secret-value") {
		t.Fatalf("token leaked into view: %s", raw)
	}
}
