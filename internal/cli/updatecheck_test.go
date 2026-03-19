package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareSemanticVersions(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		wantCmp int
		wantOK  bool
	}{
		{name: "equal with prefix", current: "v1.2.3", latest: "1.2.3", wantCmp: 0, wantOK: true},
		{name: "latest newer patch", current: "1.2.3", latest: "1.2.4", wantCmp: -1, wantOK: true},
		{name: "current newer minor", current: "1.3.0", latest: "1.2.9", wantCmp: 1, wantOK: true},
		{name: "prerelease lower than release", current: "1.2.3-rc.1", latest: "1.2.3", wantCmp: -1, wantOK: true},
		{name: "invalid semver", current: "main", latest: "1.2.3", wantCmp: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmp, gotOK := compareSemanticVersions(tt.current, tt.latest)
			if gotCmp != tt.wantCmp || gotOK != tt.wantOK {
				t.Fatalf("compareSemanticVersions(%q, %q) = (%d, %v), want (%d, %v)", tt.current, tt.latest, gotCmp, gotOK, tt.wantCmp, tt.wantOK)
			}
		})
	}
}

func TestIsOutdatedVersionSkipsDevBuilds(t *testing.T) {
	if isOutdatedVersion("0.1.0-dev", "0.2.0") {
		t.Fatal("expected dev builds to skip outdated notifications")
	}
	if !isOutdatedVersion("0.1.0", "0.2.0") {
		t.Fatal("expected release build to be considered outdated")
	}
}

func TestCheckForUpdatesFetchesThenUsesFreshCache(t *testing.T) {
	origURL := updateCheckLatestReleaseURL
	origClient := updateCheckHTTPClient
	origNow := updateCheckNow
	t.Cleanup(func() {
		updateCheckLatestReleaseURL = origURL
		updateCheckHTTPClient = origClient
		updateCheckNow = origNow
	})

	var hits int
	updateCheckLatestReleaseURL = "https://example.com/latest"
	updateCheckHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			hits++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v9.9.9","html_url":"https://example.com/releases/v9.9.9"}`)),
			}, nil
		}),
	}

	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	updateCheckNow = func() time.Time { return now }

	dataDir := t.TempDir()
	first, err := checkForUpdates(context.Background(), updateCheckOptions{
		CurrentVersion: "v0.1.0",
		DataDir:        dataDir,
	})
	if err != nil {
		t.Fatalf("checkForUpdates (first): %v", err)
	}
	if !first.Outdated {
		t.Fatalf("expected first result to be outdated, got %+v", first)
	}
	if first.FromCache {
		t.Fatalf("expected first result from network, got %+v", first)
	}
	if hits != 1 {
		t.Fatalf("expected one HTTP hit, got %d", hits)
	}

	updateCheckLatestReleaseURL = "http://127.0.0.1:1"
	updateCheckHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network unavailable")
		}),
	}
	updateCheckNow = func() time.Time { return now.Add(2 * time.Hour) }

	second, err := checkForUpdates(context.Background(), updateCheckOptions{
		CurrentVersion: "v0.1.0",
		DataDir:        dataDir,
	})
	if err != nil {
		t.Fatalf("checkForUpdates (second): %v", err)
	}
	if !second.FromCache {
		t.Fatalf("expected second result from cache, got %+v", second)
	}
	if second.LatestVersion != "v9.9.9" {
		t.Fatalf("latest version from cache = %q, want v9.9.9", second.LatestVersion)
	}
}

func TestCheckForUpdatesFallsBackToStaleCacheOnFetchFailure(t *testing.T) {
	origURL := updateCheckLatestReleaseURL
	origClient := updateCheckHTTPClient
	origNow := updateCheckNow
	t.Cleanup(func() {
		updateCheckLatestReleaseURL = origURL
		updateCheckHTTPClient = origClient
		updateCheckNow = origNow
	})

	dataDir := t.TempDir()
	cachePath := filepath.Join(dataDir, updateCheckSubdirName, updateCheckCacheName)
	oldNow := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)
	if err := saveUpdateCheckCache(cachePath, updateCheckCache{
		CurrentVersion: "v0.1.0",
		LatestVersion:  "v0.2.0",
		ReleaseURL:     "https://example.com/releases/v0.2.0",
		CheckedAt:      oldNow,
	}); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	updateCheckLatestReleaseURL = "http://127.0.0.1:1"
	updateCheckHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network unavailable")
		}),
	}
	updateCheckNow = func() time.Time { return oldNow.Add(48 * time.Hour) }

	result, err := checkForUpdates(context.Background(), updateCheckOptions{
		CurrentVersion: "v0.1.0",
		DataDir:        dataDir,
	})
	if err != nil {
		t.Fatalf("checkForUpdates: %v", err)
	}
	if !result.FromCache {
		t.Fatalf("expected stale cache fallback, got %+v", result)
	}
	if !result.Outdated {
		t.Fatalf("expected outdated result from cache, got %+v", result)
	}
}

func TestCheckForUpdatesDisabledByEnv(t *testing.T) {
	t.Setenv("OPENSTACK_DISABLE_UPDATE_CHECK", "true")
	result, err := checkForUpdates(context.Background(), updateCheckOptions{
		CurrentVersion: "v0.1.0",
		DataDir:        t.TempDir(),
		Force:          true,
	})
	if err != nil {
		t.Fatalf("checkForUpdates: %v", err)
	}
	if strings.TrimSpace(result.LatestVersion) != "" {
		t.Fatalf("expected no latest version while disabled, got %+v", result)
	}
}
