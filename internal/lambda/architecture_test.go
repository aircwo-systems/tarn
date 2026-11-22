package lambda

import (
	"context"
	"runtime"
	"strings"
	"testing"

	logssvc "github.com/aircwo-systems/tarn/internal/logs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestCreateFunctionWarnsAboutEmulatedArchitecture(t *testing.T) {
	for _, tc := range []struct{ name, arch, want string }{
		{"default", "", "x86_64"},
		{"explicit-x86", "x86_64", "x86_64"},
		{"native-arm", "arm64", "arm64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, cfg := regressionService(t)
			svc.logsSvc = logssvc.NewService(cfg)
			fn := &types.FunctionConfig{FunctionName: tc.name, Runtime: types.RuntimeNodeJS20, Handler: "index.handler"}
			if tc.arch != "" {
				fn.Architectures = []string{tc.arch}
			}
			created, err := svc.CreateFunction(t.Context(), fn, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(created.Architectures) != 1 || created.Architectures[0] != tc.want {
				t.Fatalf("architecture = %v, want [%s]", created.Architectures, tc.want)
			}
			warnings, _, _ := svc.logsSvc.GetLogEvents("/tarn/system", &logssvc.LogFilter{Level: logssvc.LevelWARN})
			if runtime.GOARCH == "arm64" && tc.want == "x86_64" {
				if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "x86_64 emulation") ||
					!strings.Contains(warnings[0].Message, tc.name) || !strings.Contains(warnings[0].Message, "arm64") {
					t.Fatalf("want a warning about x86_64 emulation, got %v", warnings)
				}
			} else if len(warnings) != 0 {
				t.Fatalf("unexpected architecture warning: %v", warnings)
			}
		})
	}
}

func TestArchitectureCodeUpdateIsPersistedAndPublishedVersionIsImmutable(t *testing.T) {
	svc, cfg := regressionService(t)
	code := functionsZIP(t, map[string]string{"index.js": "exports.handler=async()=>process.arch"})
	fn := createRegressionFunction(t, svc, "arch", code)
	if len(fn.Architectures) != 1 || fn.Architectures[0] != "x86_64" {
		t.Fatalf("default=%v", fn.Architectures)
	}
	version, err := svc.PublishVersion(context.Background(), "arch", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateFunctionCode(context.Background(), "arch", code, []string{"arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Architectures[0] != "arm64" {
		t.Fatalf("architecture ignored: %v", updated.Architectures)
	}
	updated.Architectures[0] = "caller mutation"
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	restored := NewService(cfg, store, nil, nil, nil)
	defer restored.Close()
	latest, err := restored.GetFunction("arch")
	if err != nil {
		t.Fatal(err)
	}
	published, err := restored.GetFunction("arch:" + version.Version)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Architectures[0] != "arm64" || published.Architectures[0] != "x86_64" {
		t.Fatalf("latest=%v published=%v", latest.Architectures, published.Architectures)
	}
	if _, err := svc.UpdateFunctionCode(context.Background(), "arch", code, []string{"invalid"}); err == nil {
		t.Fatal("invalid architecture accepted")
	}
	latest, _ = svc.GetFunction("arch")
	if latest.Architectures[0] != "arm64" {
		t.Fatal("invalid update changed stored architecture")
	}
}
