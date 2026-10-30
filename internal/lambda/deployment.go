package lambda

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

type fileStamp struct {
	Size     int64 `json:"size"`
	Modified int64 `json:"modified"`
}
type preparedDeployment struct {
	ID            string                `json:"id,omitempty"`
	Config        *types.FunctionConfig `json:"configuration"`
	HasCode       bool                  `json:"hasCode,omitempty"`
	MergeHash     string                `json:"mergeHash,omitempty"`
	SkippedLayers []string              `json:"skippedLayers,omitempty"`
}

type deploymentLease struct {
	InstanceID    string
	Config        *types.FunctionConfig
	CodeDir       string
	LayerDirs     []string
	SkippedLayers []string
	Revision      string
	release       func()
}

func codeHash(code []byte) string { hash := sha256.Sum256(code); return hex.EncodeToString(hash[:]) }
func validDeploymentID(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func stamp(info os.FileInfo) fileStamp {
	if info == nil {
		return fileStamp{}
	}
	return fileStamp{Size: info.Size(), Modified: info.ModTime().UnixNano()}
}
func (s *Store) deploymentsRoot() string { return filepath.Join(s.cfg.DataDir, "lambda-deployments") }
func (s *Store) deploymentPath(name, id string) (string, error) {
	if err := validName(name, 64); err != nil {
		return "", err
	}
	if !validDeploymentID(id) {
		return "", invalid("invalid deployment ID")
	}
	return checkedPath(s.deploymentsRoot(), name, id)
}

// extractZip is shared by functions and layers. Extract only into a new staging
// directory, so a corrupt archive cannot destroy a working deployment.
func extractZip(code []byte, destination string) error {
	r, err := zip.NewReader(bytes.NewReader(code), int64(len(code)))
	if err != nil {
		return fmt.Errorf("%w: invalid deployment ZIP: %v", ErrInvalidParameter, err)
	}
	for _, entry := range r.File {
		rel := filepath.Clean(filepath.FromSlash(entry.Name))
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == "." {
			continue
		}
		target := filepath.Join(destination, rel)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		mode := entry.Mode().Perm()
		if mode&0600 == 0 {
			mode |= 0644
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		writer, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = reader.Close()
			return err
		}
		_, copyErr := io.Copy(writer, reader)
		readErr := reader.Close()
		writeErr := writer.Close()
		if copyErr != nil {
			return fmt.Errorf("%w: invalid ZIP entry: %v", ErrInvalidParameter, copyErr)
		}
		if readErr != nil {
			return readErr
		}
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// mergeTree copies in configured order. Later layers replace earlier files,
// including file/directory collisions, without modifying either source layer.
func mergeTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return invalid("symlink in extracted layer")
		}
		target := filepath.Join(destination, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if existing, err := os.Lstat(target); err == nil && existing.IsDir() != entry.IsDir() {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = input.Close() }()
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm()|0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Chmod(target, info.Mode().Perm())
	})
}

func executionConfig(fn *types.FunctionConfig) types.FunctionConfig {
	copy := *fn
	copy.Tags = nil
	copy.Description = ""
	copy.Role = ""
	copy.DeadLetterConfig = nil
	copy.KMSKeyArn = ""
	copy.State = ""
	copy.LastUpdateStatus = ""
	copy.LastModified = time.Time{}
	copy.FunctionArn = ""
	copy.Version = ""
	return copy
}

func (s *Store) sourceStampLocked(name string) (fileStamp, error) {
	path, err := s.functionPath(name, "code", functionZipName)
	if err != nil {
		return fileStamp{}, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fileStamp{}, nil
	}
	return stamp(info), err
}

func (s *Store) currentPreparedLocked(record *functionRecord) (*preparedDeployment, error) {
	prepared := record.Metadata.Deployment
	if prepared == nil || !reflect.DeepEqual(executionConfig(record.FunctionConfig), executionConfig(prepared.Config)) {
		return nil, nil
	}
	current, err := s.sourceStampLocked(record.FunctionName)
	if err != nil {
		return nil, err
	}
	if current != (fileStamp{}) && current != record.Metadata.CodeSource {
		return nil, nil
	}
	return prepared, nil
}

func (s *Store) prepareCurrentLocked(record *functionRecord, requireCode bool) (*preparedDeployment, error) {
	ready, err := s.currentPreparedLocked(record)
	if err != nil {
		return nil, err
	}
	if ready != nil && (!requireCode || ready.HasCode || record.PackageType == "Image") {
		return ready, nil
	}
	var code []byte
	currentSource, err := s.sourceStampLocked(record.FunctionName)
	if err != nil {
		return nil, err
	}
	if currentSource != (fileStamp{}) && (record.Metadata.Deployment == nil || !record.Metadata.Deployment.HasCode || currentSource != record.Metadata.CodeSource) {
		code, err = os.ReadFile(s.GetCodePath(record.FunctionName))
	} else if previous := record.Metadata.Deployment; previous != nil && previous.HasCode {
		dir, pathErr := s.deploymentPath(record.FunctionName, previous.ID)
		if pathErr != nil {
			return nil, pathErr
		}
		code, err = os.ReadFile(filepath.Join(dir, functionZipName))
	}
	if err != nil {
		return nil, err
	}
	if requireCode && record.PackageType != "Image" && len(code) == 0 {
		return nil, fmt.Errorf("function code not found for %s", record.FunctionName)
	}
	prepared, err := s.prepareDeploymentLocked(record.FunctionConfig, code, record.Metadata.Deployment)
	if err != nil {
		return nil, err
	}
	record.Metadata.Deployment = prepared
	record.Metadata.CodeSource = currentSource
	return prepared, nil
}

func (s *Store) prepareDeploymentLocked(fn *types.FunctionConfig, code []byte, previous *preparedDeployment) (*preparedDeployment, error) {
	prepared := &preparedDeployment{Config: cloneFunction(fn), HasCode: len(code) > 0}
	if len(code) == 0 && fn.PackageType != "Image" {
		return prepared, nil
	}
	if len(code) > 0 {
		fn.CodeSHA256 = codeHash(code)
		fn.CodeSize = int64(len(code))
		prepared.Config = cloneFunction(fn)
	}
	var layers []string
	var previousOpt string
	if previous != nil && previous.ID != "" && slices.Equal(fn.Layers, previous.Config.Layers) {
		previousDir, err := s.deploymentPath(fn.FunctionName, previous.ID)
		if err != nil {
			return nil, err
		}
		previousOpt = filepath.Join(previousDir, "opt")
		prepared.MergeHash = previous.MergeHash
		prepared.SkippedLayers = slices.Clone(previous.SkippedLayers)
	} else if len(fn.Layers) > 0 {
		var identities []string
		for _, arn := range fn.Layers {
			name, version, err := parseLayerArn(arn)
			if err != nil {
				return nil, err
			}
			dir, err := s.ExtractLayer(name, version)
			if err != nil {
				if shouldSkipUnavailableExternalLayer(arn, s.cfg.AccountID, err) {
					prepared.SkippedLayers = append(prepared.SkippedLayers, arn)
					identities = append(identities, arn+":skipped")
					continue
				}
				return nil, err
			}
			layers = append(layers, dir)
			identities = append(identities, arn+":"+filepath.Base(dir))
		}
		encoded, _ := json.Marshal(identities)
		prepared.MergeHash = codeHash(encoded)
	}
	identity, err := json.Marshal(struct {
		Config types.FunctionConfig
		Merge  string
	}{executionConfig(fn), prepared.MergeHash})
	if err != nil {
		return nil, err
	}
	prepared.ID = codeHash(identity)
	destination, err := s.deploymentPath(fn.FunctionName, prepared.ID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(destination, "manifest.json")); err == nil {
		return prepared, nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".pending-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if len(code) > 0 {
		if err := os.MkdirAll(filepath.Join(stage, "code"), 0755); err != nil {
			return nil, err
		}
		if err := extractZip(code, filepath.Join(stage, "code")); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(stage, functionZipName), code, 0600); err != nil {
			return nil, err
		}
	}
	if prepared.MergeHash != "" {
		opt := filepath.Join(stage, "opt")
		if err := os.MkdirAll(opt, 0755); err != nil {
			return nil, err
		}
		if previousOpt != "" {
			layers = []string{previousOpt}
		}
		for _, dir := range layers {
			if err := mergeTree(dir, opt); err != nil {
				return nil, err
			}
		}
	}
	if err := atomicJSON(filepath.Join(stage, "manifest.json"), prepared); err != nil {
		return nil, err
	}
	if err := os.Rename(stage, destination); err != nil {
		return nil, err
	}
	return prepared, nil
}

// Commit code/configuration together. The immutable prepared directory becomes
// visible through one config.json rename; failures retain the previous record.
func (s *Store) commitDeployment(fn *types.FunctionConfig, code []byte, create bool, onCommit func(*functionRecord)) (*types.FunctionConfig, error) {
	if err := validName(fn.FunctionName, 64); err != nil {
		return nil, err
	}
	mu := s.fnMu(fn.FunctionName)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.readFunctionLocked(fn.FunctionName)
	if err != nil && (!create || !isFunctionNotFound(err)) {
		return nil, err
	}
	if record == nil {
		record = &functionRecord{Metadata: functionMetadata{InstanceID: uuid.NewString()}}
	} else {
		record = cloneRecord(record)
	}
	if record.Metadata.InstanceID == "" {
		record.Metadata.InstanceID = uuid.NewString()
	}
	if len(code) == 0 && fn.PackageType != "Image" && record.Metadata.Deployment != nil && record.Metadata.Deployment.HasCode {
		dir, err := s.deploymentPath(fn.FunctionName, record.Metadata.Deployment.ID)
		if err != nil {
			return nil, err
		}
		code, err = os.ReadFile(filepath.Join(dir, functionZipName))
		if err != nil {
			return nil, err
		}
	}
	record.FunctionConfig = cloneFunction(fn)
	prepared, err := s.prepareDeploymentLocked(record.FunctionConfig, code, record.Metadata.Deployment)
	if err != nil {
		return nil, err
	}
	record.Metadata.Deployment = prepared
	source, err := s.sourceStampLocked(fn.FunctionName)
	if err != nil {
		return nil, err
	}
	record.Metadata.CodeSource = source
	if err := s.saveFunctionLocked(record); err != nil {
		s.pruneFromCurrentLocked(fn.FunctionName)
		return nil, err
	}
	s.mirrorCodeLocked(record, code)
	if onCommit != nil {
		onCommit(record)
	}
	s.pruneDeploymentsLocked(fn.FunctionName, record)
	return cloneFunction(record.FunctionConfig), nil
}

// Keep the legacy ZIP observable for local tooling. It is a source for manual
// edits, never the mount used by an acquired deployment. Mirror after commit so
// a failed publication cannot replace the previous executable deployment.
func (s *Store) mirrorCodeLocked(record *functionRecord, code []byte) {
	if len(code) == 0 {
		return
	}
	path, err := s.functionPath(record.FunctionName, "code", functionZipName)
	if err != nil || atomicFile(path, code, 0600) != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	record.Metadata.CodeSource = stamp(info)
	// A crash before this stamp is saved merely reimports the same ZIP.
	_ = s.saveFunctionLocked(record)
}

func (s *Store) acquireDeployment(reference, qualifier string, onAcquire ...func(*deploymentLease)) (*deploymentLease, error) {
	name, qualifier, err := ParseFunctionReference(reference, qualifier)
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
	if record.Metadata.InstanceID == "" {
		record = cloneRecord(record)
		record.Metadata.InstanceID = uuid.NewString()
		if err := s.saveFunctionLocked(record); err != nil {
			return nil, err
		}
	}
	fn, prepared, err := selectVersion(record, qualifier)
	if err != nil {
		return nil, err
	}
	if fn.Version == "$LATEST" || qualifier == "" || qualifier == "$LATEST" {
		ready, err := s.currentPreparedLocked(record)
		if err != nil {
			return nil, err
		}
		if ready == nil || (!ready.HasCode && record.PackageType != "Image") {
			record = cloneRecord(record)
			prepared, err = s.prepareCurrentLocked(record, true)
			if err != nil {
				s.pruneFromCurrentLocked(name)
				return nil, err
			}
			if err := s.saveFunctionLocked(record); err != nil {
				s.pruneFromCurrentLocked(name)
				return nil, err
			}
			fn = record.FunctionConfig
		} else {
			prepared = ready
		}
	}
	if prepared == nil || prepared.ID == "" {
		return nil, fmt.Errorf("function code not found for %s", name)
	}
	path, err := s.deploymentPath(name, prepared.ID)
	if err != nil {
		return nil, err
	}
	lease := &deploymentLease{InstanceID: record.Metadata.InstanceID, Config: cloneFunction(fn), Revision: prepared.ID + ":" + record.Metadata.InstanceID + ":" + fn.Version}
	lease.SkippedLayers = slices.Clone(prepared.SkippedLayers)
	if prepared.HasCode {
		lease.CodeDir = filepath.Join(path, "code")
	}
	if prepared.MergeHash != "" {
		lease.LayerDirs = []string{filepath.Join(path, "opt")}
	}
	leases := s.leasesLocked(name)
	leases[prepared.ID]++
	var once sync.Once
	lease.release = func() {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			leases[prepared.ID]--
			if leases[prepared.ID] == 0 {
				delete(leases, prepared.ID)
			}
			if pending, _ := s.fnGC.Load(name); pending == true {
				s.pruneFromCurrentLocked(name)
			}
		})
	}
	for _, callback := range onAcquire {
		callback(lease)
	}
	return lease, nil
}

func (s *Store) leasesLocked(name string) map[string]int {
	if v, ok := s.fnLeases.Load(name); ok {
		return v.(map[string]int)
	}
	leases := make(map[string]int)
	s.fnLeases.Store(name, leases)
	return leases
}

func (s *Store) pruneFromCurrentLocked(name string) {
	record, err := s.readFunctionLocked(name)
	if err == nil || isFunctionNotFound(err) {
		s.pruneDeploymentsLocked(name, record)
	}
}

func (s *Store) pruneDeploymentsLocked(name string, record *functionRecord) {
	root, err := checkedPath(s.deploymentsRoot(), name)
	if err != nil {
		return
	}
	kept := make(map[string]bool)
	if record != nil {
		if record.Metadata.Deployment != nil {
			kept[record.Metadata.Deployment.ID] = true
		}
		for _, version := range record.Metadata.Versions {
			kept[version.ID] = true
		}
	}
	deferred := false
	for id, count := range s.leasesLocked(name) {
		if count > 0 {
			if !kept[id] {
				deferred = true
			}
			kept[id] = true
		}
	}
	s.fnGC.Store(name, deferred)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || kept[entry.Name()] {
			continue
		}
		if validDeploymentID(entry.Name()) || strings.HasPrefix(entry.Name(), ".pending-") {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

// ExtractCode remains available to local tooling; immutable paths preserve
// code already acquired by another caller when the source ZIP changes.
func (s *Store) ExtractCode(name string) (string, error) {
	if err := validName(name, 64); err != nil {
		return "", err
	}
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	if record, err := s.readFunctionLocked(name); err == nil {
		record = cloneRecord(record)
		previous := record.Metadata.Deployment
		prepared, err := s.prepareCurrentLocked(record, true)
		if err != nil {
			return "", err
		}
		if prepared != previous {
			if err := s.saveFunctionLocked(record); err != nil {
				return "", err
			}
		}
		if !prepared.HasCode {
			return "", fmt.Errorf("function %s uses an image", name)
		}
		path, err := s.deploymentPath(name, prepared.ID)
		return filepath.Join(path, "code"), err
	} else if !isFunctionNotFound(err) {
		return "", err
	}
	code, err := os.ReadFile(s.GetCodePath(name))
	if err != nil {
		return "", fmt.Errorf("function code not found for %s: %w", name, err)
	}
	root, err := checkedPath(s.deploymentsRoot(), name, "code-"+codeHash(code))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, ".complete")); err == nil {
		return root, nil
	}
	if err := os.MkdirAll(filepath.Dir(root), 0755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(root), ".pending-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := extractZip(code, stage); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stage, ".complete"), nil, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(stage, root); err != nil {
		return "", err
	}
	return root, nil
}
