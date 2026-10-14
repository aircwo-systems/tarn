package lambda

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

// Alias is a detached, durable mapping to a published function version.
type Alias struct {
	Name            string `json:"Name"`
	FunctionName    string `json:"FunctionName"`
	FunctionVersion string `json:"FunctionVersion"`
	Description     string `json:"Description,omitempty"`
	AliasArn        string `json:"AliasArn"`
	RevisionId      string `json:"RevisionId"`
}

// Permission is persisted with the function, independent of its deployments.
type Permission struct {
	StatementID   string
	Action        string
	Principal     string
	SourceARN     string
	SourceAccount string
}

type functionMetadata struct {
	InstanceID          string                         `json:"instanceId,omitempty"`
	Deployment          *preparedDeployment            `json:"deployment,omitempty"`
	CodeSource          fileStamp                      `json:"codeSource,omitempty"`
	Versions            map[string]*preparedDeployment `json:"versions,omitempty"`
	NextVersion         uint64                         `json:"nextVersion,omitempty"`
	Aliases             map[string]Alias               `json:"aliases,omitempty"`
	Policies            map[string]Permission          `json:"policies,omitempty"`
	ReservedConcurrency *int                           `json:"reservedConcurrency,omitempty"`
}

func isFunctionNotFound(err error) bool { return errors.Is(err, ErrFunctionNotFound) }

func cloneDeployment(value *preparedDeployment) *preparedDeployment {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Config = cloneFunction(value.Config)
	copy.SkippedLayers = append([]string(nil), value.SkippedLayers...)
	return &copy
}

func cloneMetadata(value functionMetadata) functionMetadata {
	copy := value
	copy.Deployment = cloneDeployment(value.Deployment)
	copy.Versions = make(map[string]*preparedDeployment, len(value.Versions))
	for key, version := range value.Versions {
		copy.Versions[key] = cloneDeployment(version)
	}
	copy.Aliases = maps.Clone(value.Aliases)
	copy.Policies = maps.Clone(value.Policies)
	if value.ReservedConcurrency != nil {
		n := *value.ReservedConcurrency
		copy.ReservedConcurrency = &n
	}
	return copy
}

func validateMetadata(value functionMetadata, name string) error {
	check := func(value *preparedDeployment) error {
		if value == nil {
			return nil
		}
		if value.Config == nil {
			return invalid("missing deployment configuration")
		}
		if value.Config.FunctionName != name {
			return invalid("deployment configuration belongs to another function")
		}
		if value.ID != "" && !validDeploymentID(value.ID) {
			return invalid("invalid deployment ID")
		}
		return validName(value.Config.FunctionName, 64)
	}
	if err := check(value.Deployment); err != nil {
		return err
	}
	for version, deployment := range value.Versions {
		if !numberedVersion(version) || deployment == nil {
			return invalid("invalid persisted function version")
		}
		if err := check(deployment); err != nil {
			return err
		}
	}
	if value.ReservedConcurrency != nil && *value.ReservedConcurrency < 0 {
		return invalid("reserved concurrency cannot be negative")
	}
	return nil
}

func numberedVersion(version string) bool {
	n, err := strconv.ParseUint(version, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == version
}

func selectVersion(record *functionRecord, qualifier string) (*types.FunctionConfig, *preparedDeployment, error) {
	if qualifier == "" || qualifier == "$LATEST" {
		return record.FunctionConfig, record.Metadata.Deployment, nil
	}
	if alias, ok := record.Metadata.Aliases[qualifier]; ok {
		qualifier = alias.FunctionVersion
	}
	deployment := record.Metadata.Versions[qualifier]
	if deployment == nil {
		return nil, nil, fmt.Errorf("%w: %s:%s", ErrFunctionNotFound, record.FunctionName, qualifier)
	}
	return deployment.Config, deployment, nil
}

func bareFunction(reference string) (string, error) {
	name, _, err := ParseFunctionReference(reference, "")
	return name, err
}

func (s *Service) PublishVersion(ctx context.Context, reference, description string) (*types.FunctionConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := bareFunction(reference)
	if err != nil {
		return nil, err
	}
	var result *types.FunctionConfig
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error {
		if record.PackageType == "Image" && record.ResolvedImageURI == "" && s.engine != nil {
			resolved, err := s.engine.ResolveImageReference(ctx, record.ImageURI)
			if err != nil {
				return err
			}
			record.ResolvedImageURI = resolved
		}
		deployment, err := s.store.prepareCurrentLocked(record, false)
		if err != nil {
			return err
		}
		next := record.Metadata.NextVersion
		for version := range record.Metadata.Versions {
			n, _ := strconv.ParseUint(version, 10, 64)
			if n > next {
				next = n
			}
		}
		if next == ^uint64(0) {
			return invalid("version counter exhausted")
		}
		next++
		version := strconv.FormatUint(next, 10)
		published := cloneDeployment(deployment)
		published.Config = cloneFunction(record.FunctionConfig)
		published.Config.Version = version
		published.Config.FunctionArn = strings.TrimSuffix(record.FunctionArn, ":$LATEST") + ":" + version
		if description != "" {
			published.Config.Description = description
		}
		if record.Metadata.Versions == nil {
			record.Metadata.Versions = make(map[string]*preparedDeployment)
		}
		record.Metadata.Versions[version] = published
		record.Metadata.NextVersion = next
		result = cloneFunction(published.Config)
		return nil
	})
	return result, err
}

func (s *Service) ListVersions(reference string) ([]*types.FunctionConfig, error) {
	name, err := bareFunction(reference)
	if err != nil {
		return nil, err
	}
	mu := s.store.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.store.readFunctionLocked(name)
	if err != nil {
		return nil, err
	}
	result := []*types.FunctionConfig{cloneFunction(record.FunctionConfig)}
	keys := make([]string, 0, len(record.Metadata.Versions))
	for key := range record.Metadata.Versions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.ParseUint(keys[i], 10, 64)
		b, _ := strconv.ParseUint(keys[j], 10, 64)
		return a < b
	})
	for _, key := range keys {
		result = append(result, cloneFunction(record.Metadata.Versions[key].Config))
	}
	return result, nil
}

func (s *Service) CreateAlias(reference string, alias Alias) (*Alias, error) {
	name, err := bareFunction(reference)
	if err != nil {
		return nil, err
	}
	if err := validName(alias.Name, 128); err != nil {
		return nil, err
	}
	if numberedVersion(alias.Name) {
		return nil, invalid("alias name cannot be a version number")
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error {
		if _, ok := record.Metadata.Aliases[alias.Name]; ok {
			return fmt.Errorf("%w: alias already exists", ErrResourceConflict)
		}
		if !numberedVersion(alias.FunctionVersion) || record.Metadata.Versions[alias.FunctionVersion] == nil {
			return invalid("alias target must be a published version")
		}
		alias.FunctionName = name
		alias.AliasArn = record.FunctionArn + ":" + alias.Name
		alias.RevisionId = uuid.NewString()
		if record.Metadata.Aliases == nil {
			record.Metadata.Aliases = make(map[string]Alias)
		}
		record.Metadata.Aliases[alias.Name] = alias
		return nil
	})
	return &alias, err
}

func (s *Service) GetAlias(reference, aliasName string) (*Alias, error) {
	name, err := bareFunction(reference)
	if err != nil {
		return nil, err
	}
	if err := validName(aliasName, 128); err != nil {
		return nil, err
	}
	mu := s.store.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.store.readFunctionLocked(name)
	if err != nil {
		return nil, err
	}
	alias, ok := record.Metadata.Aliases[aliasName]
	if !ok {
		return nil, fmt.Errorf("%w: alias %s", ErrFunctionNotFound, aliasName)
	}
	return &alias, nil
}

func (s *Service) ListAliases(reference string) ([]Alias, error) {
	name, err := bareFunction(reference)
	if err != nil {
		return nil, err
	}
	mu := s.store.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.store.readFunctionLocked(name)
	if err != nil {
		return nil, err
	}
	aliases := make([]Alias, 0, len(record.Metadata.Aliases))
	for _, alias := range record.Metadata.Aliases {
		aliases = append(aliases, alias)
	}
	sort.Slice(aliases, func(i, j int) bool { return aliases[i].Name < aliases[j].Name })
	return aliases, nil
}

func (s *Service) UpdateAlias(reference, aliasName string, version, description *string) (*Alias, error) {
	name, err := bareFunction(reference)
	if err != nil {
		return nil, err
	}
	if err := validName(aliasName, 128); err != nil {
		return nil, err
	}
	var result Alias
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error {
		alias, ok := record.Metadata.Aliases[aliasName]
		if !ok {
			return fmt.Errorf("%w: alias %s", ErrFunctionNotFound, aliasName)
		}
		if version != nil {
			if !numberedVersion(*version) || record.Metadata.Versions[*version] == nil {
				return invalid("alias target must be a published version")
			}
			alias.FunctionVersion = *version
		}
		if description != nil {
			alias.Description = *description
		}
		alias.RevisionId = uuid.NewString()
		record.Metadata.Aliases[aliasName] = alias
		result = alias
		return nil
	})
	return &result, err
}

func (s *Service) DeleteAlias(reference, aliasName string) error {
	name, err := bareFunction(reference)
	if err != nil {
		return err
	}
	if err := validName(aliasName, 128); err != nil {
		return err
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error { delete(record.Metadata.Aliases, aliasName); return nil })
	return err
}

func (s *Service) AddPermission(reference string, permission Permission) error {
	name, err := bareFunction(reference)
	if err != nil {
		return err
	}
	if strings.TrimSpace(permission.StatementID) == "" {
		return invalid("StatementId is required")
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error {
		if record.Metadata.Policies == nil {
			record.Metadata.Policies = make(map[string]Permission)
		}
		record.Metadata.Policies[permission.StatementID] = permission
		return nil
	})
	return err
}

func (s *Service) GetPolicy(reference string) ([]Permission, error) {
	name, err := bareFunction(reference)
	if err != nil {
		return nil, err
	}
	mu := s.store.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.store.readFunctionLocked(name)
	if err != nil {
		return nil, err
	}
	result := make([]Permission, 0, len(record.Metadata.Policies))
	for _, permission := range record.Metadata.Policies {
		result = append(result, permission)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StatementID < result[j].StatementID })
	return result, nil
}

func (s *Service) RemovePermission(reference, statementID string) error {
	name, err := bareFunction(reference)
	if err != nil {
		return err
	}
	if strings.TrimSpace(statementID) == "" {
		return invalid("StatementId is required")
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error { delete(record.Metadata.Policies, statementID); return nil })
	return err
}

func (s *Service) PutFunctionConcurrency(reference string, value int) error {
	if value < 0 {
		return invalid("reserved concurrency cannot be negative")
	}
	name, err := bareFunction(reference)
	if err != nil {
		return err
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error { record.Metadata.ReservedConcurrency = &value; return nil })
	if err == nil && s.engine != nil {
		s.engine.SignalAvailability(s.cfg.AccountID, name)
	}
	return err
}

func (s *Service) GetFunctionConcurrency(reference string) (*int, error) {
	name, err := bareFunction(reference)
	if err != nil {
		return nil, err
	}
	mu := s.store.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.store.readFunctionLocked(name)
	if err != nil {
		return nil, err
	}
	if record.Metadata.ReservedConcurrency == nil {
		return nil, nil
	}
	value := *record.Metadata.ReservedConcurrency
	return &value, nil
}

func (s *Service) DeleteFunctionConcurrency(reference string) error {
	name, err := bareFunction(reference)
	if err != nil {
		return err
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error { record.Metadata.ReservedConcurrency = nil; return nil })
	if err == nil && s.engine != nil {
		s.engine.SignalAvailability(s.cfg.AccountID, name)
	}
	return err
}
