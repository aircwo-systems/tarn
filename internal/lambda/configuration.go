package lambda

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func mutableFunction(reference string) (string, error) {
	name, qualifier, err := ParseFunctionReference(reference, "")
	if err != nil {
		return "", err
	}
	if qualifier != "" && qualifier != "$LATEST" {
		return "", invalid("published versions are immutable")
	}
	return name, nil
}

func validateConfiguration(fn *types.FunctionConfig) error {
	if err := validName(fn.FunctionName, 64); err != nil {
		return err
	}
	if fn.Timeout < 1 || fn.Timeout > 900 {
		return invalid("timeout must be between 1 and 900 seconds")
	}
	if fn.MemorySize < 128 || fn.MemorySize > 10240 {
		return invalid("memory must be between 128 and 10240 MB")
	}
	if len(fn.Architectures) > 1 || (len(fn.Architectures) == 1 && fn.Architectures[0] != "arm64" && fn.Architectures[0] != "x86_64") {
		return invalid("Architectures must contain one of arm64 or x86_64")
	}
	if len(fn.Layers) > 5 {
		return invalid("a function may have at most five layers")
	}
	for _, arn := range fn.Layers {
		if _, _, err := parseLayerArn(arn); err != nil {
			return err
		}
	}
	switch fn.PackageType {
	case "Image":
		if fn.ImageURI == "" {
			return invalid("ImageUri is required for image functions")
		}
	case "Zip", "":
		if !types.ValidRuntime(string(fn.Runtime)) {
			return invalid("unsupported runtime: " + string(fn.Runtime))
		}
	default:
		return invalid("PackageType must be Zip or Image")
	}
	return nil
}

func (s *Service) ensureFunctionImage(ctx context.Context, fn *types.FunctionConfig) error {
	return s.engine.EnsureLambdaImage(ctx, fn)
}

func (s *Service) UpdateFunctionImage(ctx context.Context, reference, imageURI string, architectures ...[]string) (*types.FunctionConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if imageURI == "" {
		return nil, invalid("ImageUri is required")
	}
	name, err := mutableFunction(reference)
	if err != nil {
		return nil, err
	}
	result, err := s.store.mutateFunction(name, func(record *functionRecord) error {
		if record.PackageType != "Image" {
			return invalid("function uses a ZIP deployment")
		}
		if len(architectures) > 0 && architectures[0] != nil {
			if len(architectures[0]) != 1 {
				return invalid("Architectures must contain one architecture")
			}
			record.Architectures = append([]string{}, architectures[0]...)
		}
		if err := validateConfiguration(record.FunctionConfig); err != nil {
			return err
		}
		record.ImageURI = imageURI
		record.ResolvedImageURI = ""
		if s.engine != nil {
			resolved, err := s.engine.ResolveImageReference(ctx, imageURI, record.Architectures...)
			if err != nil {
				return err
			}
			record.ResolvedImageURI = resolved
		}
		record.LastModified = time.Now()
		record.LastUpdateStatus = types.LastUpdateStatusSuccessful
		_, err := s.store.prepareCurrentLocked(record, false)
		return err
	})
	if err == nil {
		s.retireLatestContainers(name)
	}
	return result, err
}

func (s *Service) freezeLegacyImage(ctx context.Context, reference string) error {
	name, qualifier, err := ParseFunctionReference(reference, "")
	if err != nil {
		return err
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error {
		fn, prepared, err := selectVersion(record, qualifier)
		if err != nil {
			return err
		}
		if fn.PackageType != "Image" || fn.ResolvedImageURI != "" {
			return nil
		}
		resolved, err := s.engine.ResolveImageReference(ctx, fn.ImageURI, fn.Architectures...)
		if err != nil {
			return err
		}
		fn.ResolvedImageURI = resolved
		ready, err := s.store.prepareDeploymentLocked(fn, nil, prepared)
		if err != nil {
			return err
		}
		if qualifier == "" || qualifier == "$LATEST" {
			record.Metadata.Deployment = ready
		} else {
			version := qualifier
			if alias, ok := record.Metadata.Aliases[qualifier]; ok {
				version = alias.FunctionVersion
			}
			record.Metadata.Versions[version] = ready
		}
		return nil
	})
	return err
}

func (s *Service) retireLatestContainers(name string) {
	if s.engine == nil {
		return
	}
	mu := s.store.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	record, err := s.store.readFunctionLocked(name)
	if isFunctionNotFound(err) {
		s.engine.RetireFunction(s.cfg.AccountID, name)
		return
	}
	if err != nil || record.Metadata.Deployment == nil {
		return
	}
	s.engine.RetireLatestRevision(s.cfg.AccountID, name, record.Metadata.Deployment.ID+":"+record.Metadata.InstanceID+":"+record.Version)
}

func (s *Store) retainDeployment(name, id string) (func(), error) {
	path, err := s.deploymentPath(name, id)
	if err != nil {
		return nil, err
	}
	mu := s.fnMu(name)
	mu.Lock()
	defer mu.Unlock()
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("deployment is unavailable: %w", err)
	}
	leases := s.leasesLocked(name)
	leases[id]++
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			leases[id]--
			if leases[id] == 0 {
				delete(leases, id)
			}
			if pending, _ := s.fnGC.Load(name); pending == true {
				s.pruneFromCurrentLocked(name)
			}
		})
	}, nil
}
