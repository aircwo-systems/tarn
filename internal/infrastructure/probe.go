package infrastructure

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ProbeTarget defines a service to probe.
type ProbeTarget struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
	Kind string `json:"kind"` // "postgres", "redis", "mysql", "docker"
}

// ProbeResult holds the outcome of a single probe.
type ProbeResult struct {
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Host      string  `json:"host"`
	Port      int     `json:"port"`
	Status    string  `json:"status"` // "connected", "unreachable", "refused"
	LatencyMs float64 `json:"latencyMs"`
	Version   string  `json:"version,omitempty"`
	Error     string  `json:"error,omitempty"`
	ProbedAt  string  `json:"probedAt"`
}

// Service manages infrastructure probing.
type Service struct {
	targets []ProbeTarget
	results []ProbeResult
	mu      sync.RWMutex
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewService creates a probe service from a config target string.
// Format: "kind:host:port,kind:host:port,..."
// If targets is empty, defaults are used.
func NewService(targets string, enabled bool) *Service {
	s := &Service{
		done: make(chan struct{}),
	}
	if !enabled {
		return s
	}
	s.targets = parseTargets(targets)
	return s
}

// DefaultTargets is the default probe target string.
const DefaultTargets = "postgresql:localhost:5432,redis:localhost:6379,mysql:localhost:3306"

func parseTargets(raw string) []ProbeTarget {
	if raw == "" {
		raw = DefaultTargets
	}
	var targets []ProbeTarget
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// Supports two formats:
		//   kind:host:port           (e.g. postgresql:localhost:5432)
		//   kind:name:host:port      (e.g. http:My App:localhost:5173)
		parts := strings.SplitN(entry, ":", 4)
		var kind, name, host, portStr string
		switch len(parts) {
		case 3:
			kind, host, portStr = strings.ToLower(parts[0]), parts[1], parts[2]
			name = kindDisplayName(kind)
		case 4:
			kind, name, host, portStr = strings.ToLower(parts[0]), parts[1], parts[2], parts[3]
		default:
			continue
		}
		port := 0
		if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
			continue
		}
		if port == 0 {
			continue
		}
		if name == "" {
			name = kindDisplayName(kind)
		}
		targets = append(targets, ProbeTarget{
			Name: name,
			Host: host,
			Port: port,
			Kind: kind,
		})
	}
	return targets
}

func kindDisplayName(kind string) string {
	switch kind {
	case "postgresql", "postgres":
		return "PostgreSQL"
	case "redis":
		return "Redis"
	case "mysql":
		return "MySQL"
	case "docker":
		return "Docker"
	case "http":
		return "HTTP"
	case "https":
		return "HTTPS"
	case "mongodb", "mongo":
		return "MongoDB"
	default:
		return kind
	}
}

// Start begins background probing every 30 seconds.
func (s *Service) Start(ctx context.Context) {
	if len(s.targets) == 0 {
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)

	// Initial probe
	s.ProbeAll(ctx)

	go func() {
		defer close(s.done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.ProbeAll(ctx)
			}
		}
	}()
}

// Stop cancels background probing.
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

// ProbeAll runs all probes concurrently and caches results.
func (s *Service) ProbeAll(ctx context.Context) []ProbeResult {
	results := make([]ProbeResult, len(s.targets))
	var wg sync.WaitGroup
	for i, target := range s.targets {
		wg.Add(1)
		go func(idx int, t ProbeTarget) {
			defer wg.Done()
			results[idx] = probe(ctx, t)
		}(i, target)
	}
	wg.Wait()

	s.mu.Lock()
	s.results = results
	s.mu.Unlock()
	return results
}

// Results returns the last cached probe results.
func (s *Service) Results() []ProbeResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.results == nil {
		return []ProbeResult{}
	}
	out := make([]ProbeResult, len(s.results))
	copy(out, s.results)
	return out
}

// SetResult sets a single probe result (used for Docker engine status injected externally).
func (s *Service) SetResult(r ProbeResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.results {
		if existing.Kind == r.Kind && existing.Host == r.Host && existing.Port == r.Port {
			s.results[i] = r
			return
		}
	}
	s.results = append(s.results, r)
}

// Targets returns the configured probe targets.
func (s *Service) Targets() []ProbeTarget {
	return s.targets
}

func probe(ctx context.Context, t ProbeTarget) ProbeResult {
	if t.Kind == "http" || t.Kind == "https" {
		return probeHTTP(ctx, t)
	}
	addr := net.JoinHostPort(t.Host, fmt.Sprintf("%d", t.Port))
	now := time.Now()
	result := ProbeResult{
		Name:     t.Name,
		Kind:     t.Kind,
		Host:     t.Host,
		Port:     t.Port,
		ProbedAt: now.UTC().Format(time.RFC3339),
	}

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	latency := time.Since(now)
	result.LatencyMs = float64(latency.Microseconds()) / 1000.0

	if err != nil {
		result.Status = classifyError(err)
		result.Error = err.Error()
		return result
	}
	defer func() { _ = conn.Close() }()

	result.Status = "connected"

	// Attempt protocol-specific version detection
	switch t.Kind {
	case "postgresql", "postgres":
		if v := probePgVersion(conn); v != "" {
			result.Version = v
		}
	}

	return result
}

func probeHTTP(ctx context.Context, t ProbeTarget) ProbeResult {
	targetURL := fmt.Sprintf("%s://%s:%d/", t.Kind, t.Host, t.Port)
	now := time.Now()
	result := ProbeResult{
		Name:     t.Name,
		Kind:     t.Kind,
		Host:     t.Host,
		Port:     t.Port,
		ProbedAt: now.UTC().Format(time.RFC3339),
	}

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		result.Status = "unreachable"
		result.Error = err.Error()
		return result
	}
	req.Header.Set("User-Agent", "OpenStack-Probe/1.0")

	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	result.LatencyMs = float64(time.Since(now).Microseconds()) / 1000.0

	if err != nil {
		result.Status = classifyHTTPError(err)
		result.Error = err.Error()
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	result.Status = "connected"
	if server := resp.Header.Get("Server"); server != "" {
		result.Version = server
	} else {
		result.Version = resp.Status
	}
	return result
}

func classifyHTTPError(err error) string {
	s := err.Error()
	if strings.Contains(s, "connection refused") {
		return "refused"
	}
	return "unreachable"
}

func classifyError(err error) string {
	s := err.Error()
	if strings.Contains(s, "connection refused") {
		return "refused"
	}
	return "unreachable"
}

// probePgVersion sends a minimal PostgreSQL startup message to extract the server version.
func probePgVersion(conn net.Conn) string {
	_ = conn.SetDeadline(time.Now().Add(1 * time.Second))

	// Send SSLRequest (8 bytes: length=8, code=80877103)
	sslReq := make([]byte, 8)
	binary.BigEndian.PutUint32(sslReq[0:4], 8)
	binary.BigEndian.PutUint32(sslReq[4:8], 80877103)
	if _, err := conn.Write(sslReq); err != nil {
		return ""
	}

	// Read single-byte response ('N' = no SSL, 'S' = SSL)
	resp := make([]byte, 1)
	if _, err := conn.Read(resp); err != nil {
		return ""
	}

	// Send startup message: version 3.0, user=openstack_probe
	user := "openstack_probe"
	// Startup: int32 length, int32 protocol(196608=3.0), "user\0" + user + "\0\0"
	payload := []byte("user\x00" + user + "\x00\x00")
	startupLen := 4 + 4 + len(payload)
	startup := make([]byte, startupLen)
	binary.BigEndian.PutUint32(startup[0:4], uint32(startupLen))
	binary.BigEndian.PutUint32(startup[4:8], 196608) // 3.0
	copy(startup[8:], payload)
	if _, err := conn.Write(startup); err != nil {
		return ""
	}

	// Read response — look for ParameterStatus messages containing server_version
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return ""
	}

	// Parse pg wire protocol messages
	data := buf[:n]
	for len(data) >= 5 {
		msgType := data[0]
		msgLen := int(binary.BigEndian.Uint32(data[1:5]))
		if msgLen < 4 || 1+msgLen > len(data) {
			break
		}
		body := data[5 : 1+msgLen]
		data = data[1+msgLen:]

		// 'S' = ParameterStatus
		if msgType == 'S' {
			parts := strings.SplitN(string(body), "\x00", 3)
			if len(parts) >= 2 && parts[0] == "server_version" {
				return parts[1]
			}
		}
		// 'E' = ErrorResponse — stop
		if msgType == 'E' {
			break
		}
	}

	return ""
}
