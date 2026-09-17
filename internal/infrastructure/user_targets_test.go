package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	if err := svc.LoadUserTargets(path); err != nil {
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
	if err := reloaded.LoadUserTargets(path); err != nil {
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
