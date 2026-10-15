package engine

import (
	"context"
	"github.com/aircwo-systems/tarn/pkg/types"
	"os"
	"path/filepath"
	"testing"
)

func TestLambdaArchitecturesSelectSeparateImagesAndDockerPlatforms(t *testing.T) {
	for _, architecture := range []string{"arm64", "x86_64"} {
		t.Run(architecture, func(t *testing.T) {
			e, fake := newFakeEngine(t, 4566)
			fn := &types.FunctionConfig{FunctionName: "architecture", Runtime: types.RuntimeNodeJS20, Handler: "index.handler", MemorySize: 128, Architectures: []string{architecture}}
			if _, err := e.CreateContainer(context.Background(), fn, t.TempDir(), nil, "123456789012"); err != nil {
				t.Fatal(err)
			}
			wantPlatform := "linux/arm64"
			if architecture == "x86_64" {
				wantPlatform = "linux/amd64"
			}
			if fake.created.Image != "public.ecr.aws/lambda/nodejs:20-"+architecture || fake.platform != wantPlatform {
				t.Fatalf("image=%s platform=%s", fake.created.Image, fake.platform)
			}
		})
	}
}

func TestRuntimeProxySelectionFollowsFunctionArchitecture(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "build"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	legacy := filepath.Join(root, "build", "secrets-proxy-linux")
	if err := os.WriteFile(legacy, []byte("amd64"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := findRuntimeProxy("secrets-proxy", []string{"arm64"}); got != "" {
		t.Fatalf("ARM function selected amd64 binary: %s", got)
	}
	arm := filepath.Join(root, "build", "secrets-proxy-linux-arm64")
	if err := os.WriteFile(arm, []byte("arm64"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := findRuntimeProxy("secrets-proxy", []string{"arm64"}); got != arm {
		t.Fatalf("ARM binary=%s", got)
	}
	if got := findRuntimeProxy("secrets-proxy", []string{"x86_64"}); got != legacy {
		t.Fatalf("x86 binary=%s", got)
	}
}
