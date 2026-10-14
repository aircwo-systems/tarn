package lambda

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func (s *Store) layerPath(name string, parts ...string) (string, error) {
	if err := validName(name, 140); err != nil {
		return "", err
	}
	return checkedPath(s.cfg.LayersDir(), append([]string{name}, parts...)...)
}

func layerVersion(version int64) (string, error) {
	if version < 1 {
		return "", invalid("layer version must be positive")
	}
	return strconv.FormatInt(version, 10), nil
}

func (s *Store) saveLayerLocked(name string, version int64, code []byte, cfg *types.LayerConfig) (string, error) {
	number, err := layerVersion(version)
	if err != nil {
		return "", err
	}
	destination, err := s.layerPath(name, number)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(destination); err == nil {
		return "", fmt.Errorf("%w: layer version already exists", ErrResourceConflict)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".pending-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	hash := codeHash(code)
	copy := *cfg
	copy.CodeSHA256 = hash
	copy.CodeSize = int64(len(code))
	if err := os.WriteFile(filepath.Join(stage, "layer.zip"), code, 0600); err != nil {
		return "", err
	}
	// Validate ZIPs before exposing a version, retaining executable file modes.
	extracted := filepath.Join(stage, "extracted-"+hash)
	if err := os.MkdirAll(extracted, 0755); err != nil {
		return "", err
	}
	if err := extractZip(code, extracted); err != nil {
		return "", err
	}
	if err := atomicJSON(filepath.Join(stage, configFileName), &copy); err != nil {
		return "", err
	}
	if err := os.Rename(stage, destination); err != nil {
		return "", err
	}
	sequence, err := s.layerPath(name, ".last-version")
	if err != nil {
		_ = os.RemoveAll(destination)
		return "", err
	}
	previous, err := s.nextLayerVersionLocked(name)
	if err != nil {
		_ = os.RemoveAll(destination)
		return "", err
	}
	last := version
	if previous-1 > last {
		last = previous - 1
	}
	if err := atomicFile(sequence, []byte(strconv.FormatInt(last, 10)), 0600); err != nil {
		_ = os.RemoveAll(destination)
		return "", err
	}
	cfg.CodeSHA256 = hash
	cfg.CodeSize = int64(len(code))
	return hash, nil
}

func (s *Store) SaveLayer(name string, version int64, code []byte, cfg *types.LayerConfig) (string, error) {
	if cfg == nil {
		return "", invalid("layer configuration is required")
	}
	if _, err := s.layerPath(name); err != nil {
		return "", err
	}
	mu := stableMutex(&s.layerMus, name)
	mu.Lock()
	defer mu.Unlock()
	return s.saveLayerLocked(name, version, code, cfg)
}

func (s *Store) publishLayer(name, description string, runtimes []string, code []byte) (*types.LayerConfig, error) {
	if _, err := s.layerPath(name); err != nil {
		return nil, err
	}
	mu := stableMutex(&s.layerMus, name)
	mu.Lock()
	defer mu.Unlock()
	version, err := s.nextLayerVersionLocked(name)
	if err != nil {
		return nil, err
	}
	if version < 1 {
		return nil, invalid("layer version counter exhausted")
	}
	cfg := &types.LayerConfig{LayerName: name, VersionNumber: version, Description: description, CompatibleRuntimes: append([]string(nil), runtimes...), CreatedDate: time.Now().UTC().Format(time.RFC3339)}
	cfg.LayerArn = fmt.Sprintf("arn:aws:lambda:%s:%s:layer:%s", s.cfg.Region, s.cfg.AccountID, name)
	cfg.LayerVersionArn = fmt.Sprintf("%s:%d", cfg.LayerArn, version)
	_, err = s.saveLayerLocked(name, version, code, cfg)
	return cfg, err
}

func (s *Store) nextLayerVersionLocked(name string) (int64, error) {
	path, err := s.layerPath(name)
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	var last int64
	if data, err := os.ReadFile(filepath.Join(path, ".last-version")); err == nil {
		value, err := strconv.ParseInt(string(data), 10, 64)
		if err != nil || value < 0 {
			return 0, invalid("invalid layer version sequence")
		}
		last = value
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			value, err := strconv.ParseInt(entry.Name(), 10, 64)
			if err == nil && value > last {
				last = value
			}
		}
	}
	if last == math.MaxInt64 {
		return 0, invalid("layer version counter exhausted")
	}
	return last + 1, nil
}

func (s *Store) NextLayerVersion(name string) int64 {
	if validName(name, 140) != nil {
		return 0
	}
	mu := stableMutex(&s.layerMus, name)
	mu.RLock()
	defer mu.RUnlock()
	version, _ := s.nextLayerVersionLocked(name)
	return version
}

func (s *Store) GetLayer(name string, version int64) (*types.LayerConfig, error) {
	number, err := layerVersion(version)
	if err != nil {
		return nil, err
	}
	path, err := s.layerPath(name, number, configFileName)
	if err != nil {
		return nil, err
	}
	mu := stableMutex(&s.layerMus, name)
	mu.RLock()
	defer mu.RUnlock()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: layer %s version %d", ErrFunctionNotFound, name, version)
		}
		return nil, err
	}
	var result types.LayerConfig
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Store) ListLayerVersions(name string) ([]*types.LayerConfig, error) {
	path, err := s.layerPath(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var versions []*types.LayerConfig
	for _, entry := range entries {
		version, err := strconv.ParseInt(entry.Name(), 10, 64)
		if !entry.IsDir() || err != nil || version < 1 {
			continue
		}
		cfg, err := s.GetLayer(name, version)
		if err == nil {
			versions = append(versions, cfg)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber < versions[j].VersionNumber })
	return versions, nil
}

func (s *Store) ListLayers() ([]*types.LayerConfig, error) {
	if _, err := checkedPath(s.cfg.LayersDir()); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.cfg.LayersDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var layers []*types.LayerConfig
	for _, entry := range entries {
		if !entry.IsDir() || validName(entry.Name(), 140) != nil {
			continue
		}
		versions, err := s.ListLayerVersions(entry.Name())
		if err == nil && len(versions) > 0 {
			layers = append(layers, versions[len(versions)-1])
		}
	}
	return layers, nil
}

func (s *Store) DeleteLayerVersion(name string, version int64) error {
	number, err := layerVersion(version)
	if err != nil {
		return err
	}
	path, err := s.layerPath(name, number)
	if err != nil {
		return err
	}
	mu := stableMutex(&s.layerMus, name)
	mu.Lock()
	defer mu.Unlock()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: layer %s version %d", ErrFunctionNotFound, name, version)
		}
		return err
	}
	next, err := s.nextLayerVersionLocked(name)
	if err != nil {
		return err
	}
	sequence, err := s.layerPath(name, ".last-version")
	if err != nil {
		return err
	}
	if err := atomicFile(sequence, []byte(strconv.FormatInt(next-1, 10)), 0600); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func (s *Store) ExtractLayer(name string, version int64) (string, error) {
	number, err := layerVersion(version)
	if err != nil {
		return "", err
	}
	path, err := s.layerPath(name, number, "layer.zip")
	if err != nil {
		return "", err
	}
	mu := stableMutex(&s.layerMus, name)
	mu.Lock()
	defer mu.Unlock()
	code, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("layer code not found for %s v%d: %w", name, version, err)
	}
	destination, err := s.layerPath(name, number, "extracted-"+codeHash(code))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(destination); err == nil {
		return destination, nil
	}
	stage, err := os.MkdirTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := extractZip(code, stage); err != nil {
		return "", err
	}
	if err := os.Rename(stage, destination); err != nil {
		return "", err
	}
	return destination, nil
}
