package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestNormalizeUserTarget(t *testing.T) {
	cases := []struct {
		in      UserTarget
		wantURL string
		wantID  string
		wantErr bool
	}{
		{in: UserTarget{URL: "192.168.1.20:8080"}, wantURL: "http://192.168.1.20:8080", wantID: "http-192.168.1.20-8080"},
		{in: UserTarget{Name: "API", URL: "https://api.lan/health"}, wantURL: "https://api.lan/health", wantID: "https-api.lan-443"},
		{in: UserTarget{URL: "tcp://10.0.0.7:5432"}, wantURL: "tcp://10.0.0.7:5432", wantID: "tcp-10.0.0.7-5432"},
		{in: UserTarget{URL: "tcp://10.0.0.7"}, wantErr: true},
		{in: UserTarget{URL: "ftp://host"}, wantErr: true},
		{in: UserTarget{URL: "http://user:pw@host"}, wantErr: true},
		{in: UserTarget{URL: "http://host:70000"}, wantErr: true},
		{in: UserTarget{URL: " "}, wantErr: true},
	}
	for _, tc := range cases {
		got, err := NormalizeUserTarget(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: expected error", tc.in.URL)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.in.URL, err)
		}
		if got.URL != tc.wantURL || got.ID() != tc.wantID {
			t.Errorf("%q: got url=%s id=%s", tc.in.URL, got.URL, got.ID())
		}
		if got.Name == "" {
			t.Errorf("%q: expected a default name", tc.in.URL)
		}
	}
}

func TestUserTargetsProbeAndPersist(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "infra-services.json")
	svc := NewService("", true)
	svc.targets = nil // only the registered service
	if err := svc.LoadUserTargets(path, "000000000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetUserTargets(context.Background(), []UserTarget{{Name: "API", URL: srv.URL + "/health"}}); err != nil {
		t.Fatal(err)
	}

	results := svc.Results()
	if len(results) != 1 || results[0].Status != "connected" || results[0].Source != SourceUser {
		t.Fatalf("unexpected results: %+v", results)
	}
	if gotPath != "/health" {
		t.Errorf("probe hit %q, want /health", gotPath)
	}

	reloaded := NewService("", false)
	if err := reloaded.LoadUserTargets(path, "000000000000"); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.UserTargets(); len(got) != 1 || got[0].Name != "API" {
		t.Fatalf("targets not persisted: %+v", got)
	}
}

func TestDisabledServiceDoesNotProbeUserTargets(t *testing.T) {
	svc := NewService("", false)
	if _, err := svc.SetUserTargets(context.Background(), []UserTarget{{URL: "tcp://192.0.2.1:9"}}); err != nil {
		t.Fatal(err)
	}
	svc.Start(context.Background())
	defer svc.Stop()
	if got := svc.Results(); len(got) != 0 {
		t.Fatalf("probing disabled, got results %+v", got)
	}
}

func TestLegacyUserTargetsMigrateToDefaultAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra-services.json")
	if err := os.WriteFile(path, []byte(`[{"name":"Legacy API","url":"localhost:8080"},{"name":"Shared API","url":"localhost:8081","scope":"global"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewService("", false)
	if err := svc.LoadUserTargets(path, "123456789012"); err != nil {
		t.Fatal(err)
	}
	if got := svc.UserTargetsForAccount("123456789012"); len(got) != 2 || got[0].Scope != ScopeAccount || got[0].AccountID != "123456789012" {
		t.Fatalf("bad migration: %+v", got)
	}
	if got := svc.UserTargetsForAccount("222222222222"); len(got) != 1 || got[0].Name != "Shared API" {
		t.Fatalf("legacy service visible in another account: %+v", got)
	}
	reloaded := NewService("", false)
	if err := reloaded.LoadUserTargets(path, "999999999999"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(svc.UserTargets(), reloaded.UserTargets()) {
		t.Fatal("ownership was not persisted or migration ran again")
	}
}

func TestAccountUserTargetsConcurrentSavesAndSameEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra-services.json")
	svc := NewService("", false)
	if err := svc.LoadUserTargets(path, "000000000000"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, id := range []string{"111111111111", "222222222222"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.SetUserTargetsForAccount(context.Background(), id, []UserTarget{{Name: id, URL: "localhost:8080"}}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, id := range []string{"111111111111", "222222222222"} {
		if got := svc.UserTargetsForAccount(id); len(got) != 1 || got[0].Name != id {
			t.Fatalf("account's registration lost: %+v", got)
		}
	}
	reloaded := NewService("", false)
	if err := reloaded.LoadUserTargets(path, "000000000000"); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.UserTargets()) != 2 {
		t.Fatal("concurrent saves lost persisted services")
	}
}

func TestAccountProbeResultsPreferLocalOverGlobal(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer endpoint.Close()
	svc := NewService("", false)
	if _, err := svc.SetUserTargetsForAccount(context.Background(), "111111111111", []UserTarget{
		{Name: "Shared", URL: endpoint.URL, Scope: ScopeGlobal},
		{Name: "Local", URL: endpoint.URL, Scope: ScopeAccount},
	}); err != nil {
		t.Fatal(err)
	}
	svc.ProbeAll(context.Background())
	if got := svc.ResultsForAccount("111111111111"); len(got) != 1 || got[0].Name != "Local" {
		t.Fatalf("local did not override duplicate global endpoint: %+v", got)
	}
	if got := svc.ResultsForAccount("222222222222"); len(got) != 1 || got[0].Name != "Shared" {
		t.Fatalf("global not visible: %+v", got)
	}
}
