package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SourceUser marks probe targets and results registered through the admin API.
const SourceUser = "user"

// MaxUserTargets caps how many services can be registered.
const MaxUserTargets = 100

// UserTarget is a service registered at runtime: a local app, an API on
// another machine on the LAN, or any host reachable from the Tarn server.
type UserTarget struct {
	Name string `json:"name"`
	// URL is http(s)://host[:port][/path] or tcp://host:port.
	URL string `json:"url"`
}

// ID matches the id the admin overview and canvas use for infrastructure nodes.
func (t UserTarget) ID() string {
	pt := t.probeTarget()
	return pt.Kind + "-" + pt.Host + "-" + strconv.Itoa(pt.Port)
}

func (t UserTarget) probeTarget() ProbeTarget {
	u, _ := url.Parse(t.URL) // validated on the way in
	kind := u.Scheme
	pt := ProbeTarget{Name: t.Name, Kind: kind, Host: u.Hostname(), Port: portForURL(u), Source: SourceUser}
	if kind == "http" || kind == "https" {
		pt.URL = t.URL
	}
	return pt
}

func portForURL(u *url.URL) int {
	if p, err := strconv.Atoi(u.Port()); err == nil {
		return p
	}
	switch u.Scheme {
	case "https":
		return 443
	case "http":
		return 80
	}
	return 0
}

// NormalizeUserTarget validates a target and fills defaults. Bare "host:port"
// or "host" inputs are treated as http.
func NormalizeUserTarget(t UserTarget) (UserTarget, error) {
	raw := strings.TrimSpace(t.URL)
	if raw == "" {
		return t, errors.New("url is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return t, fmt.Errorf("invalid url %q: %w", t.URL, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "http", "https":
	case "tcp":
		if u.Port() == "" {
			return t, fmt.Errorf("tcp url %q needs a port", t.URL)
		}
		u.Path, u.RawQuery, u.Fragment = "", "", ""
	default:
		return t, fmt.Errorf("unsupported scheme %q (use http, https or tcp)", u.Scheme)
	}
	if u.Hostname() == "" {
		return t, fmt.Errorf("url %q has no host", t.URL)
	}
	if u.User != nil {
		return t, errors.New("credentials in service urls are not stored; remove user:password@")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return t, fmt.Errorf("invalid port %q", p)
		}
	}
	name := strings.TrimSpace(t.Name)
	if name == "" {
		name = u.Hostname()
	}
	return UserTarget{Name: name, URL: u.String()}, nil
}

// LoadUserTargets reads registered services from path and remembers the path
// for later saves. A missing file is not an error.
func (s *Service) LoadUserTargets(path string) error {
	s.mu.Lock()
	s.userTargetsPath = path
	s.mu.Unlock()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stored []UserTarget
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	targets := make([]UserTarget, 0, len(stored))
	for _, t := range stored {
		if nt, err := NormalizeUserTarget(t); err == nil {
			targets = append(targets, nt)
		}
	}
	s.mu.Lock()
	s.userTargets = targets
	s.mu.Unlock()
	return nil
}

// UserTargets returns the registered services.
func (s *Service) UserTargets() []UserTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]UserTarget{}, s.userTargets...)
}

// SetUserTargets validates, replaces and persists the registered services,
// then probes so results reflect the change straight away.
func (s *Service) SetUserTargets(ctx context.Context, targets []UserTarget) ([]UserTarget, error) {
	if len(targets) > MaxUserTargets {
		return nil, fmt.Errorf("at most %d services can be registered", MaxUserTargets)
	}
	normalized := make([]UserTarget, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		nt, err := NormalizeUserTarget(t)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[nt.ID()]; dup {
			continue
		}
		seen[nt.ID()] = struct{}{}
		normalized = append(normalized, nt)
	}

	s.mu.Lock()
	s.userTargets = normalized
	s.targetsGen++
	path := s.userTargetsPath
	s.mu.Unlock()

	if path != "" {
		if err := writeUserTargets(path, normalized); err != nil {
			return nil, err
		}
	}

	if s.enabled {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		s.ProbeAll(probeCtx)
	}
	return normalized, nil
}

func writeUserTargets(path string, targets []UserTarget) error {
	data, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
