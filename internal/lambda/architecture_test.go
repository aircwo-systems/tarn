package lambda

import (
	"context"
	"testing"
)

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
