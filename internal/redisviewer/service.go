package redisviewer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

type persistedState struct {
	Enabled bool `json:"enabled"`
}

// Service manages runtime enablement of the Redis viewer across accounts.
type Service struct {
	enabled   atomic.Bool
	mu        sync.Mutex
	statePath string
}

// NewService creates a Redis viewer controller with the given initial state.
func NewService(initialEnabled bool) *Service {
	s := &Service{}
	s.enabled.Store(initialEnabled)
	return s
}

// LoadState remembers path for persisting runtime toggles. When explicitOverride
// is false and path exists, the persisted preference is loaded.
func (s *Service) LoadState(path string, explicitOverride bool) error {
	s.mu.Lock()
	s.statePath = path
	s.mu.Unlock()

	if explicitOverride || path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var st persistedState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	s.enabled.Store(st.Enabled)
	return nil
}

// Enabled reports whether the Redis viewer endpoints and UI section are active.
func (s *Service) Enabled() bool {
	if s == nil {
		return false
	}
	return s.enabled.Load()
}

// SetEnabled updates runtime enablement and persists the preference when a state path is configured.
func (s *Service) SetEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.statePath != "" {
		if err := writeStateFile(s.statePath, persistedState{Enabled: enabled}); err != nil {
			return err
		}
	}
	s.enabled.Store(enabled)
	return nil
}

func writeStateFile(path string, st persistedState) error {
	data, err := json.MarshalIndent(st, "", "  ")
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
