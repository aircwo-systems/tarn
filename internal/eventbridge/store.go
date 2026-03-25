package eventbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Store is an in-memory rule store with optional JSON persistence.
type Store struct {
	mu        sync.RWMutex
	persistMu sync.Mutex
	cfg       *config.Config
	rules     map[string]*types.EventBridgeRule // key: rule name
}

func NewStore(cfg *config.Config) *Store {
	return &Store{
		cfg:   cfg,
		rules: make(map[string]*types.EventBridgeRule),
	}
}

func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}

	if err := os.MkdirAll(s.cfg.EventBridgeDir(), 0o755); err != nil {
		return fmt.Errorf("create eventbridge dir: %w", err)
	}

	data, err := os.ReadFile(s.cfg.EventBridgeStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read eventbridge state: %w", err)
	}
	if len(data) == 0 {
		return nil
	}

	var snapshot struct {
		Rules []*types.EventBridgeRule `json:"rules"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&snapshot); err != nil {
		return fmt.Errorf("decode eventbridge state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = make(map[string]*types.EventBridgeRule, len(snapshot.Rules))
	for _, rule := range snapshot.Rules {
		if rule == nil || rule.Name == "" {
			continue
		}
		s.rules[rule.Name] = cloneRule(rule)
	}
	return nil
}

func (s *Store) SaveRule(rule *types.EventBridgeRule) error {
	if rule == nil || rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	s.mu.Lock()
	s.rules[rule.Name] = cloneRule(rule)
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) GetRule(name string) (*types.EventBridgeRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.rules[name]
	if !ok {
		return nil, fmt.Errorf("rule %s not found", name)
	}
	return cloneRule(rule), nil
}

func (s *Store) DeleteRule(name string) error {
	s.mu.Lock()
	if _, ok := s.rules[name]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("rule %s not found", name)
	}
	delete(s.rules, name)
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) ListRules() []*types.EventBridgeRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.EventBridgeRule, 0, len(s.rules))
	for _, rule := range s.rules {
		out = append(out, cloneRule(rule))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) persist() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}

	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	s.mu.RLock()
	rules := make([]*types.EventBridgeRule, 0, len(s.rules))
	for _, rule := range s.rules {
		rules = append(rules, cloneRule(rule))
	}
	s.mu.RUnlock()

	sort.Slice(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })

	payload := struct {
		Rules []*types.EventBridgeRule `json:"rules"`
	}{Rules: rules}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal eventbridge state: %w", err)
	}

	if err := os.MkdirAll(s.cfg.EventBridgeDir(), 0o755); err != nil {
		return fmt.Errorf("create eventbridge dir: %w", err)
	}

	tmp, err := os.CreateTemp(s.cfg.EventBridgeDir(), "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create eventbridge temp state: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write eventbridge temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close eventbridge temp state: %w", err)
	}
	if err := os.Rename(tmpPath, s.cfg.EventBridgeStatePath()); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename eventbridge temp state: %w", err)
	}

	return nil
}

func cloneRule(src *types.EventBridgeRule) *types.EventBridgeRule {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Tags != nil {
		dst.Tags = cloneStringMap(src.Tags)
	}
	if src.LastRunAt != nil {
		t := *src.LastRunAt
		dst.LastRunAt = &t
	}
	if src.NextRunAt != nil {
		t := *src.NextRunAt
		dst.NextRunAt = &t
	}
	if len(src.Targets) > 0 {
		dst.Targets = make([]types.EventBridgeTarget, len(src.Targets))
		for i := range src.Targets {
			target := src.Targets[i]
			if target.LastInvokedAt != nil {
				t := *target.LastInvokedAt
				target.LastInvokedAt = &t
			}
			if target.InputTransformer != nil {
				cloned := *target.InputTransformer
				if target.InputTransformer.InputPathsMap != nil {
					cloned.InputPathsMap = cloneStringMap(target.InputTransformer.InputPathsMap)
				}
				target.InputTransformer = &cloned
			}
			dst.Targets[i] = target
		}
	}
	return &dst
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
