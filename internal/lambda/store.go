package lambda

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

const functionZipName = "function.zip"
const configFileName = "config.json"

// Store keeps configuration and durable metadata in one atomic JSON record.
// Deployment directories are immutable and outlive the containers using them.
type Store struct {
	cfg      *config.Config
	fnMus    sync.Map // function name -> stable *sync.RWMutex
	fnCache  sync.Map // function name -> *functionCache
	fnLeases sync.Map // function name -> map[deploymentID]int, guarded by fnMu
	fnGC     sync.Map // function names with obsolete revisions still leased
	layerMus sync.Map // layer name -> stable *sync.RWMutex
}

type functionRecord struct {
	*types.FunctionConfig
	Metadata     functionMetadata `json:"_tarn,omitempty"`
	codeToMirror []byte
	afterCommit  func(*functionRecord)
}

type functionCache struct {
	info   os.FileInfo
	record *functionRecord
}

func NewStore(cfg *config.Config) *Store { return &Store{cfg: cfg} }

func stableMutex(registry *sync.Map, name string) *sync.RWMutex {
	if v, ok := registry.Load(name); ok {
		return v.(*sync.RWMutex)
	}
	v, _ := registry.LoadOrStore(name, &sync.RWMutex{})
	return v.(*sync.RWMutex)
}

func (s *Store) fnMu(name string) *sync.RWMutex { return stableMutex(&s.fnMus, name) }

func (s *Store) Init() error {
	for _, dir := range []string{s.cfg.FunctionsDir(), s.cfg.LayersDir(), s.deploymentsRoot()} {
		if _, err := checkedPath(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	// No containers from this store have been acquired yet. Retire revisions
	// left by an interrupted update, retaining current and published versions.
	entries, err := os.ReadDir(s.deploymentsRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || validName(entry.Name(), 64) != nil {
			continue
		}
		mu := s.fnMu(entry.Name())
		mu.Lock()
		record, loadErr := s.readFunctionLocked(entry.Name())
		if loadErr == nil || os.IsNotExist(loadErr) || isFunctionNotFound(loadErr) {
			s.pruneDeploymentsLocked(entry.Name(), record)
		}
		mu.Unlock()
	}
	return nil
}

func cloneFunction(fn *types.FunctionConfig) *types.FunctionConfig {
	if fn == nil {
		return nil
	}
	copy := *fn
	copy.Environment = maps.Clone(fn.Environment)
	copy.Tags = maps.Clone(fn.Tags)
	copy.Layers = slices.Clone(fn.Layers)
	if fn.DeadLetterConfig != nil {
		value := *fn.DeadLetterConfig
		copy.DeadLetterConfig = &value
	}
	if fn.ImageConfig != nil {
		value := *fn.ImageConfig
		value.EntryPoint = slices.Clone(value.EntryPoint)
		value.Command = slices.Clone(value.Command)
		copy.ImageConfig = &value
	}
	return &copy
}

func cloneRecord(record *functionRecord) *functionRecord {
	copy := *record
	copy.codeToMirror = nil
	copy.afterCommit = nil
	copy.FunctionConfig = cloneFunction(record.FunctionConfig)
	copy.Metadata = cloneMetadata(record.Metadata)
	return &copy
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicFile(path, data, 0600)
}

func atomicFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (s *Store) functionPath(name string, parts ...string) (string, error) {
	if err := validName(name, 64); err != nil {
		return "", err
	}
	return checkedPath(s.cfg.FunctionsDir(), append([]string{name}, parts...)...)
}

func (s *Store) readFunctionLocked(name string) (*functionRecord, error) {
	path, err := s.functionPath(name, configFileName)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.fnCache.Delete(name)
			return nil, fmt.Errorf("%w: %s", ErrFunctionNotFound, name)
		}
		return nil, err
	}
	if v, ok := s.fnCache.Load(name); ok {
		cached := v.(*functionCache)
		if os.SameFile(info, cached.info) && info.Size() == cached.info.Size() && info.ModTime().Equal(cached.info.ModTime()) {
			return cached.record, nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	record := &functionRecord{FunctionConfig: &types.FunctionConfig{}}
	if err := json.Unmarshal(data, record); err != nil {
		return nil, err
	}
	if record.FunctionName != name {
		return nil, invalid("stored function name does not match its directory")
	}
	if err := validateMetadata(record.Metadata, name); err != nil {
		return nil, err
	}
	s.fnCache.Store(name, &functionCache{info: info, record: record})
	s.fnGC.Store(name, true)
	return record, nil
}

func (s *Store) saveFunctionLocked(record *functionRecord) error {
	path, err := s.functionPath(record.FunctionName, configFileName)
	if err != nil {
		return err
	}
	if err := atomicJSON(path, record); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		s.fnCache.Delete(record.FunctionName)
		return err
	}
	s.fnCache.Store(record.FunctionName, &functionCache{info: info, record: cloneRecord(record)})
	s.fnGC.Store(record.FunctionName, true)
	return nil
}

func (s *Store) mutateFunction(name string, mutation func(*functionRecord) error) (*types.FunctionConfig, error) {
	if err := validName(name, 64); err != nil {
		return nil, err
	}
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.readFunctionLocked(name)
	if err != nil {
		return nil, err
	}
	record = cloneRecord(record)
	if err := mutation(record); err != nil {
		s.pruneFromCurrentLocked(name)
		return nil, err
	}
	if err := s.saveFunctionLocked(record); err != nil {
		s.pruneFromCurrentLocked(name)
		return nil, err
	}
	s.mirrorCodeLocked(record, record.codeToMirror)
	if record.afterCommit != nil {
		record.afterCommit(record)
	}
	s.pruneDeploymentsLocked(name, record)
	return cloneFunction(record.FunctionConfig), nil
}

// SaveFunction preserves durable metadata while replacing configuration.
func (s *Store) SaveFunction(fn *types.FunctionConfig) error {
	if fn == nil {
		return invalid("function configuration is required")
	}
	if err := validName(fn.FunctionName, 64); err != nil {
		return err
	}
	mu := s.fnMu(fn.FunctionName)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.readFunctionLocked(fn.FunctionName)
	if err != nil && !isFunctionNotFound(err) {
		return err
	}
	if record == nil {
		record = &functionRecord{Metadata: functionMetadata{InstanceID: uuid.NewString()}}
	} else {
		record = cloneRecord(record)
	}
	record.FunctionConfig = cloneFunction(fn)
	return s.saveFunctionLocked(record)
}

func (s *Store) GetFunction(reference string) (*types.FunctionConfig, error) {
	name, qualifier, err := ParseFunctionReference(reference, "")
	if err != nil {
		return nil, err
	}
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.readFunctionLocked(name)
	if err != nil {
		return nil, err
	}
	fn, _, err := selectVersion(record, qualifier)
	return cloneFunction(fn), err
}

func (s *Store) ListFunctions() ([]*types.FunctionConfig, error) {
	if _, err := checkedPath(s.cfg.FunctionsDir()); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.cfg.FunctionsDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var functions []*types.FunctionConfig
	for _, entry := range entries {
		if !entry.IsDir() || validName(entry.Name(), 64) != nil {
			continue
		}
		fn, err := s.GetFunction(entry.Name())
		if err == nil {
			functions = append(functions, fn)
		}
	}
	return functions, nil
}

func (s *Store) DeleteFunction(name string) error {
	return s.deleteFunction(name, nil)
}

func (s *Store) deleteFunction(name string, onDelete func()) error {
	path, err := s.functionPath(name)
	if err != nil {
		return err
	}
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	if _, err := s.readFunctionLocked(name); err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	s.fnCache.Delete(name)
	if onDelete != nil {
		onDelete()
	}
	// Keep the lock: a queued operation must share the same lock as a recreate.
	s.pruneDeploymentsLocked(name, nil)
	return nil
}

func (s *Store) SaveCode(name string, code []byte) (string, error) {
	path, err := s.functionPath(name, "code", functionZipName)
	if err != nil {
		return "", err
	}
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	if err := atomicFile(path, code, 0600); err != nil {
		return "", err
	}
	return codeHash(code), nil
}

func (s *Store) GetCodeDir(name string) string { path, _ := s.functionPath(name, "code"); return path }
func (s *Store) GetCodePath(name string) string {
	path, _ := s.functionPath(name, "code", functionZipName)
	return path
}
func (s *Store) FunctionExists(name string) bool { _, err := s.GetFunction(name); return err == nil }
