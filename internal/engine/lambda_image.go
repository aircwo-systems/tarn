package engine

import (
	"context"
	"fmt"
	"github.com/aircwo-systems/tarn/pkg/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"os"
	"path/filepath"
)

func lambdaPlatform(architectures []string) *ocispec.Platform {
	if len(architectures) != 1 {
		return nil
	}
	arch := architectures[0]
	if arch == "x86_64" {
		arch = "amd64"
	}
	return &ocispec.Platform{OS: "linux", Architecture: arch}
}
func lambdaRuntimeImage(fn *types.FunctionConfig) (string, error) {
	ref, ok := types.RuntimeImageMap[fn.Runtime]
	if !ok {
		return "", fmt.Errorf("unsupported runtime: %s", fn.Runtime)
	}
	// Separate tags keep a pull for one architecture from replacing the other
	// architecture's tag in Docker's classic image store.
	if len(fn.Architectures) == 1 {
		ref += "-" + fn.Architectures[0]
	}
	return ref, nil
}

// EnsureLambdaImage prepares the platform requested by this deployment.
func (e *Engine) EnsureLambdaImage(ctx context.Context, fn *types.FunctionConfig) error {
	platform := ""
	if p := lambdaPlatform(fn.Architectures); p != nil {
		platform = p.OS + "/" + p.Architecture
	}
	if fn.PackageType == "Image" {
		ref := fn.ImageURI
		if fn.ResolvedImageURI != "" {
			ref = fn.ResolvedImageURI
		}
		return e.EnsureImagePlatform(ctx, ref, platform)
	}
	if platform == "" {
		return e.EnsureImage(ctx, fn.Runtime)
	}
	ref, err := lambdaRuntimeImage(fn)
	if err != nil {
		return err
	}
	return e.EnsureImagePlatform(ctx, ref, platform)
}

// findRuntimeProxy selects helpers built for the function's platform. The
// unsuffixed binary is the legacy amd64 build, never an ARM fallback.
func findRuntimeProxy(name string, architectures []string) string {
	names := []string{}
	if len(architectures) == 1 {
		arch := architectures[0]
		if arch == "x86_64" {
			arch = "amd64"
		}
		names = append(names, name+"-linux-"+arch)
		if arch == "amd64" {
			names = append(names, name+"-linux")
		}
	} else {
		names = append(names, name+"-linux")
	}
	for _, filename := range names {
		paths := []string{filepath.Join("build", filename)}
		if exe, err := os.Executable(); err == nil {
			paths = append(paths, filepath.Join(filepath.Dir(exe), filename))
		}
		for _, path := range paths {
			abs, err := filepath.Abs(path)
			if err != nil {
				continue
			}
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	return ""
}
