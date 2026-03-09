package infrastructure

import (
	"context"
	"testing"
)

func TestParseTargetsDefault(t *testing.T) {
	targets := parseTargets("")
	if len(targets) != 3 {
		t.Fatalf("expected 3 default targets, got %d", len(targets))
	}
	if targets[0].Kind != "postgresql" || targets[0].Port != 5432 {
		t.Fatalf("first target = %+v, want postgresql:5432", targets[0])
	}
	if targets[1].Kind != "redis" || targets[1].Port != 6379 {
		t.Fatalf("second target = %+v, want redis:6379", targets[1])
	}
	if targets[2].Kind != "mysql" || targets[2].Port != 3306 {
		t.Fatalf("third target = %+v, want mysql:3306", targets[2])
	}
}

func TestParseTargetsCustom(t *testing.T) {
	targets := parseTargets("postgresql:db.local:5433,redis:cache:6380")
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].Host != "db.local" || targets[0].Port != 5433 {
		t.Fatalf("target[0] = %+v", targets[0])
	}
	if targets[1].Host != "cache" || targets[1].Port != 6380 {
		t.Fatalf("target[1] = %+v", targets[1])
	}
}

func TestParseTargetsInvalid(t *testing.T) {
	targets := parseTargets("bad_entry,also_bad,:,postgresql:host:0")
	if len(targets) != 0 {
		t.Fatalf("expected 0 valid targets from invalid input, got %d", len(targets))
	}
}

func TestParseTargetsHTTP(t *testing.T) {
	// 4-part format with display name
	targets := parseTargets("http:My Frontend:localhost:5173")
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Kind != "http" {
		t.Fatalf("kind = %q, want http", targets[0].Kind)
	}
	if targets[0].Name != "My Frontend" {
		t.Fatalf("name = %q, want My Frontend", targets[0].Name)
	}
	if targets[0].Host != "localhost" {
		t.Fatalf("host = %q, want localhost", targets[0].Host)
	}
	if targets[0].Port != 5173 {
		t.Fatalf("port = %d, want 5173", targets[0].Port)
	}

	// 3-part format — name auto-derived from kind
	targets2 := parseTargets("http:localhost:3000")
	if len(targets2) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets2))
	}
	if targets2[0].Name != "HTTP" {
		t.Fatalf("name = %q, want HTTP", targets2[0].Name)
	}
	if targets2[0].Port != 3000 {
		t.Fatalf("port = %d, want 3000", targets2[0].Port)
	}

	// Mixed with DB targets
	targets3 := parseTargets("postgresql:localhost:5432,http:Admin UI:localhost:4000")
	if len(targets3) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets3))
	}
	if targets3[1].Kind != "http" || targets3[1].Name != "Admin UI" || targets3[1].Port != 4000 {
		t.Fatalf("http target = %+v", targets3[1])
	}
}

func TestProbeHTTPUnreachable(t *testing.T) {
	target := ProbeTarget{
		Name: "My App",
		Host: "localhost",
		Port: 59998,
		Kind: "http",
	}
	result := probe(context.Background(), target)
	if result.Status == "connected" {
		t.Skip("port 59998 unexpectedly open")
	}
	if result.Status != "refused" && result.Status != "unreachable" {
		t.Fatalf("status = %q, want refused or unreachable", result.Status)
	}
}

func TestProbeUnreachableHost(t *testing.T) {
	target := ProbeTarget{
		Name: "PostgreSQL",
		Host: "localhost",
		Port: 59999, // unlikely to be open
		Kind: "postgresql",
	}
	result := probe(context.Background(), target)
	if result.Status == "connected" {
		t.Skip("port 59999 unexpectedly open")
	}
	if result.Status != "refused" && result.Status != "unreachable" {
		t.Fatalf("status = %q, want refused or unreachable", result.Status)
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error")
	}
}

func TestServiceStartStop(t *testing.T) {
	svc := NewService("postgresql:localhost:59999", true)
	if len(svc.Targets()) != 1 {
		t.Fatalf("targets = %d, want 1", len(svc.Targets()))
	}

	ctx := context.Background()
	svc.Start(ctx)

	results := svc.Results()
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Status == "connected" {
		t.Skip("port 59999 unexpectedly open")
	}

	svc.Stop()
}

func TestServiceDisabled(t *testing.T) {
	svc := NewService(DefaultTargets, false)
	if len(svc.Targets()) != 0 {
		t.Fatalf("disabled service should have no targets, got %d", len(svc.Targets()))
	}
	results := svc.Results()
	if len(results) != 0 {
		t.Fatalf("disabled service should return empty results, got %d", len(results))
	}
}

func TestSetResult(t *testing.T) {
	svc := NewService("", true)
	ctx := context.Background()
	svc.Start(ctx)
	defer svc.Stop()

	svc.SetResult(ProbeResult{
		Name:   "Docker",
		Kind:   "docker",
		Host:   "localhost",
		Port:   0,
		Status: "connected",
	})

	results := svc.Results()
	found := false
	for _, r := range results {
		if r.Kind == "docker" && r.Status == "connected" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected docker result to be set")
	}
}
