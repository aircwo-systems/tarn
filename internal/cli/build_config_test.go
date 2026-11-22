package cli

import "testing"

// TestBuildConfigEnvAndFlags checks that flag defaults do not override
// TARN_HOST, TARN_PORT and TARN_REGION, while flags the user passes do.
func TestBuildConfigEnvAndFlags(t *testing.T) {
	t.Setenv("TARN_HOST", "127.0.0.1")
	t.Setenv("TARN_PORT", "4599")
	t.Setenv("TARN_REGION", "eu-west-1")

	build := func(args ...string) (host string, port int, region string) {
		t.Helper()
		start, _, err := NewRootCmd().Find([]string{"start"})
		if err != nil {
			t.Fatalf("find start: %v", err)
		}
		if err := start.ParseFlags(args); err != nil {
			t.Fatalf("parse flags: %v", err)
		}
		cfg, err := buildConfig(start)
		if err != nil {
			t.Fatalf("buildConfig: %v", err)
		}
		return cfg.Host, cfg.Port, cfg.Region
	}

	if host, port, region := build(); host != "127.0.0.1" || port != 4599 || region != "eu-west-1" {
		t.Errorf("env only: got %s:%d %s, want 127.0.0.1:4599 eu-west-1", host, port, region)
	}
	if host, port, region := build("--host", "0.0.0.0", "--port", "4600", "--region", "us-west-2"); host != "0.0.0.0" || port != 4600 || region != "us-west-2" {
		t.Errorf("flags: got %s:%d %s, want 0.0.0.0:4600 us-west-2", host, port, region)
	}
}

func TestBuildConfigRedisViewerFlags(t *testing.T) {
	run := func(args ...string) (bool, bool) {
		t.Helper()
		start, _, err := NewRootCmd().Find([]string{"start"})
		if err != nil {
			t.Fatalf("find start: %v", err)
		}
		if err := start.ParseFlags(args); err != nil {
			t.Fatalf("parse flags %v: %v", args, err)
		}
		cfg, err := buildConfig(start)
		if err != nil {
			t.Fatalf("buildConfig: %v", err)
		}
		return cfg.RedisViewerEnabled, cfg.RedisViewerExplicit
	}

	if enabled, explicit := run(); enabled || explicit {
		t.Errorf("default: enabled=%v explicit=%v, want false/false", enabled, explicit)
	}
	if enabled, explicit := run("--redis-viewer"); !enabled || !explicit {
		t.Errorf("--redis-viewer: enabled=%v explicit=%v, want true/true", enabled, explicit)
	}
	if enabled, explicit := run("--rv"); !enabled || !explicit {
		t.Errorf("--rv: enabled=%v explicit=%v, want true/true", enabled, explicit)
	}

	t.Setenv("TARN_REDIS_VIEWER", "true")
	if enabled, explicit := run(); !enabled || !explicit {
		t.Errorf("env TARN_REDIS_VIEWER=true: enabled=%v explicit=%v, want true/true", enabled, explicit)
	}
	if enabled, explicit := run("--redis-viewer=false"); enabled || !explicit {
		t.Errorf("--redis-viewer=false override: enabled=%v explicit=%v, want false/true", enabled, explicit)
	}
}
