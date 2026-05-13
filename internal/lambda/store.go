package lambda

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const functionZipName = "function.zip"
const configFileName = "config.json"

// Store persists Lambda function configurations and code to disk.
type Store struct {
	cfg *config.Config
	// fnMus holds a per-function RWMutex so concurrent creates/reads for
	// different functions don't block each other.
	fnMus   sync.Map    // function name → *sync.RWMutex
	layerMu sync.RWMutex // guards all layer operations
}

// NewStore creates a new function store.
func NewStore(cfg *config.Config) *Store {
	return &Store{cfg: cfg}
}

// fnMu returns the per-function mutex, creating it lazily.
func (s *Store) fnMu(name string) *sync.RWMutex {
	mu := &sync.RWMutex{}
	actual, _ := s.fnMus.LoadOrStore(name, mu)
	return actual.(*sync.RWMutex)
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
	mu := s.fnMu(fn.FunctionName)
	mu.Lock()
	defer mu.Unlock()

	fnDir := filepath.Join(s.cfg.FunctionsDir(), fn.FunctionName)
	if err := os.MkdirAll(fnDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(fn, "", "  ")
	if err != nil {
		return err
	}

	// Write to a temp file then rename atomically so concurrent readers in
	// ListFunctions never observe a partial write.
	configPath := filepath.Join(fnDir, configFileName)
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, configPath)
}

// GetFunction loads a function configuration from disk.
func (s *Store) GetFunction(name string) (*types.FunctionConfig, error) {
	mu := s.fnMu(name)
	mu.RLock()
	defer mu.RUnlock()

	configPath := filepath.Join(s.cfg.FunctionsDir(), name, configFileName)
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
		configPath := filepath.Join(fnDir, entry.Name(), configFileName)
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
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()

	fnDir := filepath.Join(s.cfg.FunctionsDir(), name)
	if _, err := os.Stat(fnDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("function %s not found", name)
		}
		return err
	}
	if err := os.RemoveAll(fnDir); err != nil {
		return err
	}
	s.fnMus.Delete(name)
	return nil
}

// SaveCode saves function code (zip bytes) and returns the SHA256 hash.
func (s *Store) SaveCode(name string, code []byte) (string, error) {
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()

	codeDir := filepath.Join(s.cfg.FunctionsDir(), name, "code")
	if err := os.MkdirAll(codeDir, 0755); err != nil {
		return "", err
	}

	codePath := filepath.Join(codeDir, functionZipName)
	if err := os.WriteFile(codePath, code, 0600); err != nil {
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
	return filepath.Join(s.cfg.FunctionsDir(), name, "code", functionZipName)
}

// FunctionExists checks if a function exists in the store.
func (s *Store) FunctionExists(name string) bool {
	mu := s.fnMu(name)
	mu.RLock()
	defer mu.RUnlock()

	configPath := filepath.Join(s.cfg.FunctionsDir(), name, "config.json")
	_, err := os.Stat(configPath)
	return err == nil
}

// ExtractCode extracts function.zip into an "extracted" subdirectory and returns
// the path to it. If already extracted and the zip hasn't changed, returns the
// cached extraction.
func (s *Store) ExtractCode(name string) (string, error) {
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()

	zipPath := filepath.Join(s.cfg.FunctionsDir(), name, "code", functionZipName)
	extractDir := filepath.Join(s.cfg.FunctionsDir(), name, "code", "extracted")

	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		return "", fmt.Errorf("function code not found for %s: %w", name, err)
	}

	// Check if extraction is already up-to-date via a marker file
	markerPath := filepath.Join(extractDir, ".extracted_at")
	if markerData, err := os.ReadFile(markerPath); err == nil {
		markerTime := string(markerData)
		if markerTime == zipInfo.ModTime().String() {
			return extractDir, nil
		}
	}

	// Remove stale extraction
	_ = os.RemoveAll(extractDir)
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", err
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target := filepath.Join(extractDir, f.Name)

		// Guard against zip slip
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(extractDir)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			// Always use 0755 for directories regardless of the permissions stored
			// in the ZIP/JAR entry. Maven fat JARs often have entries with mode 0000
			// which would make the directory inaccessible for subsequent writes.
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}

		// Ensure files are at least user-readable/writable (0644).
		// ZIP entries from some tools (e.g. maven-shade-plugin) may carry mode 0.
		mode := f.Mode()
		if mode&0600 == 0 {
			mode = 0644
		}
		outFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return "", err
		}

		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close()
			return "", err
		}

		_, err = io.Copy(outFile, rc)
		_ = rc.Close()
		_ = outFile.Close()
		if err != nil {
			return "", err
		}
	}

	// Write marker
	_ = os.WriteFile(markerPath, []byte(zipInfo.ModTime().String()), 0600)

	return extractDir, nil
}

// --- Layer operations ---

// SaveLayer saves a layer version's code and config to disk.
// Returns the SHA256 hash of the code.
func (s *Store) SaveLayer(name string, version int64, code []byte, cfg *types.LayerConfig) (string, error) {
	s.layerMu.Lock()
	defer s.layerMu.Unlock()

	versionDir := filepath.Join(s.cfg.LayersDir(), name, strconv.FormatInt(version, 10))
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return "", err
	}

	// Save zip
	if err := os.WriteFile(filepath.Join(versionDir, "layer.zip"), code, 0600); err != nil {
		return "", err
	}

	hash := sha256.Sum256(code)
	hashStr := hex.EncodeToString(hash[:])

	cfg.CodeSHA256 = hashStr
	cfg.CodeSize = int64(len(code))

	// Save config
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(versionDir, configFileName), data, 0600); err != nil {
		return "", err
	}

	return hashStr, nil
}

// GetLayer loads a layer version's config from disk.
func (s *Store) GetLayer(name string, version int64) (*types.LayerConfig, error) {
	s.layerMu.RLock()
	defer s.layerMu.RUnlock()

	configPath := filepath.Join(s.cfg.LayersDir(), name, strconv.FormatInt(version, 10), configFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("layer %s version %d not found", name, version)
		}
		return nil, err
	}

	var cfg types.LayerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// NextLayerVersion determines the next version number for a layer.
func (s *Store) NextLayerVersion(name string) int64 {
	s.layerMu.RLock()
	defer s.layerMu.RUnlock()

	layerDir := filepath.Join(s.cfg.LayersDir(), name)
	entries, err := os.ReadDir(layerDir)
	if err != nil {
		return 1
	}

	var maxVersion int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		v, err := strconv.ParseInt(entry.Name(), 10, 64)
		if err != nil {
			continue
		}
		if v > maxVersion {
			maxVersion = v
		}
	}
	return maxVersion + 1
}

// ListLayers returns the latest version of each layer.
func (s *Store) ListLayers() ([]*types.LayerConfig, error) {
	s.layerMu.RLock()
	defer s.layerMu.RUnlock()

	layersDir := s.cfg.LayersDir()
	entries, err := os.ReadDir(layersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var layers []*types.LayerConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Find the latest version
		versions, _ := s.listLayerVersionsUnsafe(entry.Name())
		if len(versions) > 0 {
			layers = append(layers, versions[len(versions)-1])
		}
	}
	return layers, nil
}

// ListLayerVersions returns all versions for a specific layer.
func (s *Store) ListLayerVersions(name string) ([]*types.LayerConfig, error) {
	s.layerMu.RLock()
	defer s.layerMu.RUnlock()
	return s.listLayerVersionsUnsafe(name)
}

// listLayerVersionsUnsafe returns layer versions without locking (caller must hold lock).
func (s *Store) listLayerVersionsUnsafe(name string) ([]*types.LayerConfig, error) {
	layerDir := filepath.Join(s.cfg.LayersDir(), name)
	entries, err := os.ReadDir(layerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var versions []*types.LayerConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		configPath := filepath.Join(layerDir, entry.Name(), configFileName)
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}
		var cfg types.LayerConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		versions = append(versions, &cfg)
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].VersionNumber < versions[j].VersionNumber
	})
	return versions, nil
}

// DeleteLayerVersion removes a specific layer version from disk.
func (s *Store) DeleteLayerVersion(name string, version int64) error {
	s.layerMu.Lock()
	defer s.layerMu.Unlock()

	versionDir := filepath.Join(s.cfg.LayersDir(), name, strconv.FormatInt(version, 10))
	if _, err := os.Stat(versionDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("layer %s version %d not found", name, version)
		}
		return err
	}
	return os.RemoveAll(versionDir)
}

// ExtractLayer extracts a layer's zip into an "extracted" subdirectory (same pattern as function code).
func (s *Store) ExtractLayer(name string, version int64) (string, error) {
	s.layerMu.Lock()
	defer s.layerMu.Unlock()

	versionDir := filepath.Join(s.cfg.LayersDir(), name, strconv.FormatInt(version, 10))
	zipPath := filepath.Join(versionDir, "layer.zip")
	extractDir := filepath.Join(versionDir, "extracted")

	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		return "", fmt.Errorf("layer code not found for %s v%d: %w", name, version, err)
	}

	// Check cache marker
	markerPath := filepath.Join(extractDir, ".extracted_at")
	if markerData, err := os.ReadFile(markerPath); err == nil {
		if string(markerData) == zipInfo.ModTime().String() {
			return extractDir, nil
		}
	}

	_ = os.RemoveAll(extractDir)
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", err
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to open layer zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target := filepath.Join(extractDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(extractDir)+string(os.PathSeparator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}
		mode := f.Mode()
		if mode&0600 == 0 {
			mode = 0644
		}
		outFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close()
			return "", err
		}
		_, err = io.Copy(outFile, rc)
		_ = rc.Close()
		_ = outFile.Close()
		if err != nil {
			return "", err
		}
	}

	_ = os.WriteFile(markerPath, []byte(zipInfo.ModTime().String()), 0600)
	return extractDir, nil
}
