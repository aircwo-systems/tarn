package eventsource

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Store is an in-memory store for event source mappings with optional JSON persistence.
type Store struct {
	mu       sync.RWMutex
	dirty    atomic.Bool
	cfg      *config.Config
	mappings map[string]*types.EventSourceMapping
}

// NewStore creates a new event source mapping store.
func NewStore(cfg *config.Config) *Store {
	return &Store{
		cfg:      cfg,
		mappings: make(map[string]*types.EventSourceMapping),
	}
}

// Init loads persisted state if persistence is enabled.
func (store *Store) Init() error {
	if !store.cfg.PersistenceEnabled {
		return nil
	}
	go store.startFlusher()

	dir := store.cfg.EventSourceDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create eventsource dir: %w", err)
	}

	statePath := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read eventsource state: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var mappings []*types.EventSourceMapping
	if err := json.Unmarshal(data, &mappings); err != nil {
		// Legacy: state may have been persisted as a map[uuid]->mapping object.
		var legacy map[string]*types.EventSourceMapping
		if err2 := json.Unmarshal(data, &legacy); err2 != nil {
			return fmt.Errorf("unmarshal eventsource state: %w", err)
		}
		for _, m := range legacy {
			mappings = append(mappings, m)
		}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	for _, m := range mappings {
		store.mappings[m.UUID] = m
	}
	log.Printf("[eventsource] loaded %d event source mapping(s) from disk", len(store.mappings))
	return nil
}

// Save stores or updates a mapping.
func (store *Store) Save(mapping *types.EventSourceMapping) error {
	store.mu.Lock()
	store.mappings[mapping.UUID] = mapping
	store.mu.Unlock()
	return store.persist()
}

// Get returns a mapping by UUID.
func (store *Store) Get(uuid string) (*types.EventSourceMapping, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	m, exists := store.mappings[uuid]
	if !exists {
		return nil, fmt.Errorf("event source mapping %s not found", uuid)
	}
	return m, nil
}

// List returns all mappings.
func (store *Store) List() []*types.EventSourceMapping {
	store.mu.RLock()
	defer store.mu.RUnlock()

	result := make([]*types.EventSourceMapping, 0, len(store.mappings))
	for _, m := range store.mappings {
		result = append(result, m)
	}
	return result
}

// Delete removes a mapping by UUID.
func (store *Store) Delete(uuid string) error {
	store.mu.Lock()
	if _, exists := store.mappings[uuid]; !exists {
		store.mu.Unlock()
		return fmt.Errorf("event source mapping %s not found", uuid)
	}
	delete(store.mappings, uuid)
	store.mu.Unlock()
	return store.persist()
}

func (store *Store) persist() error {
	store.dirty.Store(true)
	return nil
}

func (store *Store) Flush() { store.flushToDisk() }

func (store *Store) startFlusher() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		if store.dirty.Swap(false) {
			store.flushToDisk()
		}
	}
}

func (store *Store) flushToDisk() {
	if !store.cfg.PersistenceEnabled {
		return
	}

	store.mu.RLock()
	mappings := make([]*types.EventSourceMapping, 0, len(store.mappings))
	for _, m := range store.mappings {
		mappings = append(mappings, m)
	}
	store.mu.RUnlock()

	data, err := json.Marshal(mappings)
	if err != nil {
		return
	}

	dir := store.cfg.EventSourceDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	tmpFile, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return
	}

	statePath := filepath.Join(dir, "state.json")
	if err := os.Rename(tmpPath, statePath); err != nil {
		_ = os.Remove(tmpPath)
	}
}
