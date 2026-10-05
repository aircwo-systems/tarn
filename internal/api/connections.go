package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/secretsproxy"
)

// mcpSessionTTL is how long an MCP session counts as connected after its last
// heartbeat. `tarn mcp` beats every 15s, so this tolerates two missed beats.
const mcpSessionTTL = 45 * time.Second

// Connections tracks the instance-wide links the dashboard reports on: the
// local secrets proxy and the `tarn mcp` servers that heartbeat in. Both are
// per-instance rather than per-account, so this lives on the Server and not on
// an account's admin handler.
type Connections struct {
	mu    sync.Mutex
	now   func() time.Time
	proxy secretsProxyStats
	mcp   map[string]*mcpSession
}

type secretsProxyStats struct {
	requests     int64
	denied       int64
	lastAt       time.Time
	lastSecretID string
	lastCaller   string
	lastStatus   int
}

type mcpSession struct {
	ID            string    `json:"id"`
	ClientName    string    `json:"clientName,omitempty"`
	ClientVersion string    `json:"clientVersion,omitempty"`
	ServerVersion string    `json:"serverVersion,omitempty"`
	PID           int       `json:"pid,omitempty"`
	ConnectedAt   time.Time `json:"connectedAt"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
}

// NewConnections returns an empty tracker.
func NewConnections() *Connections {
	return &Connections{now: time.Now, mcp: map[string]*mcpSession{}}
}

// RecordSecretsProxy counts one request answered by the secrets proxy.
func (c *Connections) RecordSecretsProxy(event secretsproxy.RequestEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proxy.requests++
	if !event.TokenValid {
		c.proxy.denied++
	}
	c.proxy.lastAt = event.StartedAt
	c.proxy.lastSecretID = event.SecretID
	c.proxy.lastCaller = firstNonEmpty(event.FunctionName, event.CallerName, event.ClientIP)
	c.proxy.lastStatus = event.StatusCode
}

func (c *Connections) heartbeat(s mcpSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if existing, ok := c.mcp[s.ID]; ok {
		s.ConnectedAt = existing.ConnectedAt
	} else {
		s.ConnectedAt = now
	}
	s.LastSeenAt = now
	c.mcp[s.ID] = &s
}

func (c *Connections) disconnect(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.mcp, id)
}

type secretsProxyView struct {
	Enabled       bool       `json:"enabled"`
	Address       string     `json:"address"`
	RequireToken  bool       `json:"requireToken"`
	DefaultToken  bool       `json:"defaultToken"`
	Requests      int64      `json:"requests"`
	Denied        int64      `json:"denied"`
	LastRequestAt *time.Time `json:"lastRequestAt,omitempty"`
	LastSecretID  string     `json:"lastSecretId,omitempty"`
	LastCaller    string     `json:"lastCaller,omitempty"`
	LastStatus    int        `json:"lastStatus,omitempty"`
}

type connectionsView struct {
	SecretsProxy secretsProxyView `json:"secretsProxy"`
	MCP          struct {
		Sessions []mcpSession `json:"sessions"`
	} `json:"mcp"`
}

// snapshot reports current state, dropping MCP sessions that stopped beating.
func (c *Connections) snapshot(cfg *config.Config) connectionsView {
	c.mu.Lock()
	defer c.mu.Unlock()

	var view connectionsView
	token := cfg.SecretsProxySessionToken
	view.SecretsProxy = secretsProxyView{
		Enabled:      cfg.ExposeSecretsProxy,
		Address:      fmt.Sprintf("%s:%d", cfg.SecretsProxyHost, cfg.SecretsProxyPort),
		RequireToken: cfg.SecretsProxyRequireToken,
		// Never expose the token itself; only whether it is the well-known default.
		DefaultToken: token == "" || token == "local-dev-token",
		Requests:     c.proxy.requests,
		Denied:       c.proxy.denied,
		LastSecretID: c.proxy.lastSecretID,
		LastCaller:   c.proxy.lastCaller,
		LastStatus:   c.proxy.lastStatus,
	}
	if !c.proxy.lastAt.IsZero() {
		at := c.proxy.lastAt
		view.SecretsProxy.LastRequestAt = &at
	}

	cutoff := c.now().Add(-mcpSessionTTL)
	view.MCP.Sessions = []mcpSession{}
	for id, s := range c.mcp {
		if s.LastSeenAt.Before(cutoff) {
			delete(c.mcp, id)
			continue
		}
		view.MCP.Sessions = append(view.MCP.Sessions, *s)
	}
	sort.Slice(view.MCP.Sessions, func(i, j int) bool {
		return view.MCP.Sessions[i].ConnectedAt.Before(view.MCP.Sessions[j].ConnectedAt)
	})
	return view
}

func (s *Server) connectionsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.conns.snapshot(s.cfg))
}

func (s *Server) mcpHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	var body mcpSession
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "invalid heartbeat body", http.StatusBadRequest)
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	if body.ID == "" || len(body.ID) > 64 {
		http.Error(w, "heartbeat requires an id", http.StatusBadRequest)
		return
	}
	s.conns.heartbeat(body)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) mcpDisconnectHandler(w http.ResponseWriter, r *http.Request) {
	s.conns.disconnect(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
