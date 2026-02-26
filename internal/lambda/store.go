package lambda

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// Store persists Lambda function configurations and code to disk.
type Store struct {
	cfg *config.Config
	mu  sync.RWMutex
}

// NewStore creates a new function store.
func NewStore(cfg *config.Config) *Store {
	return &Store{cfg: cfg}
}

// Init ensures the storage directories exist.
func (s *Store) Init() error {
	dirs := []string{
		s.cfg.FunctionsDir(),
		s.cfg.LayersDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

// SaveFunction persists a function configuration to disk.
func (s *Store) SaveFunction(fn *types.FunctionConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fnDir := filepath.Join(s.cfg.FunctionsDir(), fn.FunctionName)
	if err := os.MkdirAll(fnDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(fn, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(fnDir, "config.json"), data, 0644)
}

// GetFunction loads a function configuration from disk.
func (s *Store) GetFunction(name string) (*types.FunctionConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configPath := filepath.Join(s.cfg.FunctionsDir(), name, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("function %s not found", name)
		}
		return nil, err
	}

	var fn types.FunctionConfig
	if err := json.Unmarshal(data, &fn); err != nil {
		return nil, err
	}
	return &fn, nil
}

// ListFunctions returns all stored function configurations.
func (s *Store) ListFunctions() ([]*types.FunctionConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fnDir := s.cfg.FunctionsDir()
	entries, err := os.ReadDir(fnDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var functions []*types.FunctionConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		configPath := filepath.Join(fnDir, entry.Name(), "config.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}
		var fn types.FunctionConfig
		if err := json.Unmarshal(data, &fn); err != nil {
			continue
		}
		functions = append(functions, &fn)
	}
	return functions, nil
}

// DeleteFunction removes a function and its code from disk.
func (s *Store) DeleteFunction(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fnDir := filepath.Join(s.cfg.FunctionsDir(), name)
	if _, err := os.Stat(fnDir); os.IsNotExist(err) {
		return fmt.Errorf("function %s not found", name)
	}
	return os.RemoveAll(fnDir)
}

// SaveCode saves function code (zip bytes) and returns the SHA256 hash.
func (s *Store) SaveCode(name string, code []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	codeDir := filepath.Join(s.cfg.FunctionsDir(), name, "code")
	if err := os.MkdirAll(codeDir, 0755); err != nil {
		return "", err
	}

	codePath := filepath.Join(codeDir, "function.zip")
	if err := os.WriteFile(codePath, code, 0644); err != nil {
		return "", err
	}

	hash := sha256.Sum256(code)
	return hex.EncodeToString(hash[:]), nil
}

// GetCodeDir returns the path to the extracted code directory for a function.
func (s *Store) GetCodeDir(name string) string {
	return filepath.Join(s.cfg.FunctionsDir(), name, "code")
}

// GetCodePath returns the path to the function's zip file.
func (s *Store) GetCodePath(name string) string {
	return filepath.Join(s.cfg.FunctionsDir(), name, "code", "function.zip")
}

// FunctionExists checks if a function exists in the store.
func (s *Store) FunctionExists(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configPath := filepath.Join(s.cfg.FunctionsDir(), name, "config.json")
	_, err := os.Stat(configPath)
	return err == nil
}
