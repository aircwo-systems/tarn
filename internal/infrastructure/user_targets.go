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

const (
	ScopeAccount = "account"
	ScopeGlobal  = "global"
)

// MaxUserTargets caps how many services can be registered.
const MaxUserTargets = 100

// UserTarget is a service registered at runtime: a local app, an API on
// another machine on the LAN, or any host reachable from the Tarn server.
type UserTarget struct {
	Name string `json:"name"`
	// URL is http(s)://host[:port][/path] or tcp://host:port.
	URL       string `json:"url"`
	Scope     string `json:"scope"`
	AccountID string `json:"accountId,omitempty"`
}

// ID matches the id the admin overview and canvas use for infrastructure nodes.
func (t UserTarget) ID() string {
	pt := t.probeTarget()
	return pt.Kind + "-" + pt.Host + "-" + strconv.Itoa(pt.Port)
}

func (t UserTarget) probeTarget() ProbeTarget {
	u, _ := url.Parse(t.URL) // validated on the way in
	kind := u.Scheme
	pt := ProbeTarget{Name: t.Name, Kind: kind, Host: u.Hostname(), Port: portForURL(u), Source: SourceUser, AccountID: t.AccountID}
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
	switch t.Scope {
	case "", ScopeGlobal:
		t.Scope, t.AccountID = ScopeGlobal, ""
	case ScopeAccount:
		if len(t.AccountID) != 12 || strings.IndexFunc(t.AccountID, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return t, errors.New("account ID must be exactly 12 digits")
		}
	default:
		return t, errors.New("service scope must be account or global")
	}
	return UserTarget{Name: name, URL: u.String(), Scope: t.Scope, AccountID: t.AccountID}, nil
}

// LoadUserTargets reads registered services from path and remembers the path
// for later saves. A missing file is not an error.
func (s *Service) LoadUserTargets(path, defaultAccountID string) error {
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
	migrated := false
	for _, t := range stored {
		if t.Scope == "" {
			t.Scope, t.AccountID = ScopeAccount, defaultAccountID
			migrated = true
		}
		if nt, err := NormalizeUserTarget(t); err == nil {
			targets = append(targets, nt)
		}
	}
	if migrated {
		if err := writeUserTargets(path, targets); err != nil {
			return err
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

// UserTargetsForAccount includes only this account's services and shared services.
func (s *Service) UserTargetsForAccount(accountID string) []UserTarget {
	out := []UserTarget{}
	for _, target := range s.UserTargets() {
		if target.Scope == ScopeGlobal || target.AccountID == accountID {
			out = append(out, target)
		}
	}
	return out
}

// SetUserTargets replaces all registrations. Account requests use SetUserTargetsForAccount.
func (s *Service) SetUserTargets(ctx context.Context, targets []UserTarget) ([]UserTarget, error) {
	normalized, err := normalizeUserTargets(targets)
	if err != nil {
		return nil, err
	}
	s.userTargetsMu.Lock()
	err = s.replaceUserTargets(normalized)
	s.userTargetsMu.Unlock()
	if err != nil {
		return nil, err
	}
	s.probeUserTargets(ctx)
	return normalized, nil
}

// SetUserTargetsForAccount replaces the caller's visible registrations, preserving
// registrations owned by other accounts. Scope changes take effect on save.
func (s *Service) SetUserTargetsForAccount(ctx context.Context, accountID string, targets []UserTarget) ([]UserTarget, error) {
	scoped := make([]UserTarget, 0, len(targets))
	for _, target := range targets {
		if target.Scope == "" {
			target.Scope = ScopeAccount
		}
		if target.Scope == ScopeAccount {
			if target.AccountID != "" && target.AccountID != accountID {
				return nil, errors.New("cannot register a service for another account")
			}
			target.AccountID = accountID
		}
		scoped = append(scoped, target)
	}
	normalized, err := normalizeUserTargets(scoped)
	if err != nil {
		return nil, err
	}
	s.userTargetsMu.Lock()
	merged := []UserTarget{}
	for _, target := range s.UserTargets() {
		if target.Scope == ScopeAccount && target.AccountID != accountID {
			merged = append(merged, target)
		}
	}
	merged = append(merged, normalized...)
	if len(merged) > MaxUserTargets {
		s.userTargetsMu.Unlock()
		return nil, fmt.Errorf("at most %d services can be registered", MaxUserTargets)
	}
	err = s.replaceUserTargets(merged)
	s.userTargetsMu.Unlock()
	if err != nil {
		return nil, err
	}
	s.probeUserTargets(ctx)
	return normalized, nil
}

func normalizeUserTargets(targets []UserTarget) ([]UserTarget, error) {
	if len(targets) > MaxUserTargets {
		return nil, fmt.Errorf("at most %d services can be registered", MaxUserTargets)
	}
	normalized := make([]UserTarget, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		target, err := NormalizeUserTarget(target)
		if err != nil {
			return nil, err
		}
		key := target.Scope + "/" + target.AccountID + "/" + target.ID()
		if !seen[key] {
			normalized = append(normalized, target)
			seen[key] = true
		}
	}
	return normalized, nil
}

// Caller holds userTargetsMu, keeping read/merge/save atomic across accounts.
func (s *Service) replaceUserTargets(targets []UserTarget) error {
	s.mu.RLock()
	path := s.userTargetsPath
	s.mu.RUnlock()
	if path != "" {
		if err := writeUserTargets(path, targets); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.userTargets = targets
	s.targetsGen++
	s.mu.Unlock()
	return nil
}

func (s *Service) probeUserTargets(ctx context.Context) {
	if s.enabled {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		s.ProbeAll(probeCtx)
	}
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
