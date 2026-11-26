package eventbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/persist"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Store is an in-memory rule and event bus store with optional JSON
// persistence.
type Store struct {
	mu      sync.RWMutex
	dirty   persist.Dirty
	flusher *persist.Flusher
	cfg     *config.Config
	rules   map[string]*types.EventBridgeRule     // key: ruleStoreKey(bus, name)
	buses   map[string]*types.EventBridgeEventBus // custom buses only; default is implicit
}

func NewStore(cfg *config.Config) *Store {
	return &Store{
		cfg:   cfg,
		rules: make(map[string]*types.EventBridgeRule),
		buses: make(map[string]*types.EventBridgeEventBus),
	}
}

// ruleStoreKey identifies a rule within the account. Rule names are unique
// per event bus; default-bus rules keep their bare name as the key so state
// written before custom buses existed loads unchanged.
func ruleStoreKey(bus, name string) string {
	if bus == "" || bus == defaultEventBusName {
		return name
	}
	return bus + "|" + name
}

func ruleBus(rule *types.EventBridgeRule) string {
	if rule == nil || rule.EventBusName == "" {
		return defaultEventBusName
	}
	return rule.EventBusName
}

func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}
	s.flusher = persist.StartFlusher(&s.dirty, s.flushToDisk)

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
		Rules []*types.EventBridgeRule     `json:"rules"`
		Buses []*types.EventBridgeEventBus `json:"buses"`
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
		cloned := cloneRule(rule)
		cloned.EventBusName = ruleBus(cloned)
		s.rules[ruleStoreKey(cloned.EventBusName, cloned.Name)] = cloned
	}
	s.buses = make(map[string]*types.EventBridgeEventBus, len(snapshot.Buses))
	for _, bus := range snapshot.Buses {
		if bus == nil || bus.Name == "" || bus.Name == defaultEventBusName {
			continue
		}
		s.buses[bus.Name] = cloneEventBus(bus)
	}
	return nil
}

func (s *Store) SaveRule(rule *types.EventBridgeRule) error {
	if rule == nil || rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	cloned := cloneRule(rule)
	cloned.EventBusName = ruleBus(cloned)
	s.mu.Lock()
	s.rules[ruleStoreKey(cloned.EventBusName, cloned.Name)] = cloned
	s.mu.Unlock()
	return s.persist()
}

// GetRule returns the named rule on bus ("" means the default bus).
func (s *Store) GetRule(bus, name string) (*types.EventBridgeRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.rules[ruleStoreKey(bus, name)]
	if !ok {
		return nil, fmt.Errorf("rule %s not found", name)
	}
	return cloneRule(rule), nil
}

func (s *Store) DeleteRule(bus, name string) error {
	key := ruleStoreKey(bus, name)
	s.mu.Lock()
	if _, ok := s.rules[key]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("rule %s not found", name)
	}
	delete(s.rules, key)
	s.mu.Unlock()
	return s.persist()
}

// ListRules returns every rule on every bus, sorted by name then bus.
func (s *Store) ListRules() []*types.EventBridgeRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.EventBridgeRule, 0, len(s.rules))
	for _, rule := range s.rules {
		out = append(out, cloneRule(rule))
	}
	sortRules(out)
	return out
}

func sortRules(rules []*types.EventBridgeRule) {
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Name != rules[j].Name {
			return rules[i].Name < rules[j].Name
		}
		return ruleBus(rules[i]) < ruleBus(rules[j])
	})
}

func (s *Store) SaveBus(bus *types.EventBridgeEventBus) error {
	if bus == nil || bus.Name == "" || bus.Name == defaultEventBusName {
		return fmt.Errorf("custom event bus name is required")
	}
	s.mu.Lock()
	s.buses[bus.Name] = cloneEventBus(bus)
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) GetBus(name string) (*types.EventBridgeEventBus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bus, ok := s.buses[name]
	if !ok {
		return nil, fmt.Errorf("event bus %s not found", name)
	}
	return cloneEventBus(bus), nil
}

func (s *Store) DeleteBus(name string) error {
	s.mu.Lock()
	if _, ok := s.buses[name]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("event bus %s not found", name)
	}
	delete(s.buses, name)
	s.mu.Unlock()
	return s.persist()
}

// ListBuses returns the custom event buses sorted by name.
func (s *Store) ListBuses() []*types.EventBridgeEventBus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.EventBridgeEventBus, 0, len(s.buses))
	for _, bus := range s.buses {
		out = append(out, cloneEventBus(bus))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) persist() error {
	s.dirty.Store(true)
	return nil
}

// Flush writes pending state to disk now.
func (s *Store) Flush() {
	if s.flusher != nil {
		s.flusher.Flush()
		return
	}
	s.flushToDisk()
}

// Close stops the background flusher after writing any pending state.
func (s *Store) Close() { s.flusher.Close() }

func (s *Store) flushToDisk() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	s.mu.RLock()
	rules := make([]*types.EventBridgeRule, 0, len(s.rules))
	for _, rule := range s.rules {
		rules = append(rules, cloneRule(rule))
	}
	buses := make([]*types.EventBridgeEventBus, 0, len(s.buses))
	for _, bus := range s.buses {
		buses = append(buses, cloneEventBus(bus))
	}
	s.mu.RUnlock()

	sortRules(rules)
	sort.Slice(buses, func(i, j int) bool { return buses[i].Name < buses[j].Name })

	payload := struct {
		Rules []*types.EventBridgeRule     `json:"rules"`
		Buses []*types.EventBridgeEventBus `json:"buses,omitempty"`
	}{Rules: rules, Buses: buses}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	if err := os.MkdirAll(s.cfg.EventBridgeDir(), 0o755); err != nil {
		return
	}

	tmp, err := os.CreateTemp(s.cfg.EventBridgeDir(), "state-*.json.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, s.cfg.EventBridgeStatePath()); err != nil {
		_ = os.Remove(tmpPath)
	}
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
			dst.Targets[i] = cloneTarget(src.Targets[i])
		}
	}
	return &dst
}

func cloneEventBus(src *types.EventBridgeEventBus) *types.EventBridgeEventBus {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Tags = cloneStringMap(src.Tags)
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
