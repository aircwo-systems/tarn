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
		{name: "rc.9 before rc.10", current: "1.2.3-rc.9", latest: "1.2.3-rc.10", wantCmp: -1, wantOK: true},
		{name: "beta.2 before beta.10", current: "1.0.0-beta.2", latest: "1.0.0-beta.10", wantCmp: -1, wantOK: true},
		{name: "alpha before beta", current: "1.0.0-alpha", latest: "1.0.0-beta", wantCmp: -1, wantOK: true},
		{name: "beta equal", current: "v0.0.8-beta", latest: "v0.0.8-beta", wantCmp: 0, wantOK: true},
		{name: "beta versions different patch", current: "v0.0.7-beta", latest: "v0.0.8-beta", wantCmp: -1, wantOK: true},
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

func TestIsOutdatedVersionDevBuilds(t *testing.T) {
	// Dev builds report outdated so local builds still surface available releases.
	if !isOutdatedVersion("0.1.0-dev", "0.2.0") {
		t.Fatal("expected dev build to be considered outdated")
	}
	if !isOutdatedVersion("0.1.0", "0.2.0") {
		t.Fatal("expected release build to be considered outdated")
	}
}

func TestCheckForUpdatesFetchesThenUsesFreshCache(t *testing.T) {
	origURL := updateCheckLatestReleaseURL
	origReleasesURL := updateCheckReleasesURL
	origClient := updateCheckHTTPClient
	origNow := updateCheckNow
	t.Cleanup(func() {
		updateCheckLatestReleaseURL = origURL
		updateCheckReleasesURL = origReleasesURL
		updateCheckHTTPClient = origClient
		updateCheckNow = origNow
	})

	var hits int
	updateCheckLatestReleaseURL = "https://example.com/latest"
	updateCheckReleasesURL = "https://example.com/releases"
	updateCheckHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			hits++
			if r.URL.String() == updateCheckReleasesURL {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`[{"tag_name":"v9.9.9","html_url":"https://example.com/releases/v9.9.9"}]`)),
				}, nil
			}
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
	origReleasesURL := updateCheckReleasesURL
	origClient := updateCheckHTTPClient
	origNow := updateCheckNow
	t.Cleanup(func() {
		updateCheckLatestReleaseURL = origURL
		updateCheckReleasesURL = origReleasesURL
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
	updateCheckReleasesURL = "http://127.0.0.1:1"
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
	t.Setenv("TARN_DISABLE_UPDATE_CHECK", "true")
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

func TestCheckForUpdatesReturnsNewestOverall(t *testing.T) {
	origURL := updateCheckLatestReleaseURL
	origReleasesURL := updateCheckReleasesURL
	origClient := updateCheckHTTPClient
	origNow := updateCheckNow
	t.Cleanup(func() {
		updateCheckLatestReleaseURL = origURL
		updateCheckReleasesURL = origReleasesURL
		updateCheckHTTPClient = origClient
		updateCheckNow = origNow
	})

	updateCheckLatestReleaseURL = "https://example.com/latest"
	updateCheckReleasesURL = "https://example.com/releases"
	updateCheckHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.String() {
			case updateCheckReleasesURL:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`[
						{"tag_name":"v1.0.0","html_url":"https://example.com/releases/v1.0.0"},
						{"tag_name":"v1.1.0-beta","html_url":"https://example.com/releases/v1.1.0-beta"}
					]`)),
				}, nil
			default:
				return nil, fmt.Errorf("unexpected URL: %s", r.URL.String())
			}
		}),
	}
	updateCheckNow = func() time.Time {
		return time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)
	}

	result, err := checkForUpdates(context.Background(), updateCheckOptions{
		CurrentVersion: "v0.9.0",
		DataDir:        t.TempDir(),
		Force:          true,
	})
	if err != nil {
		t.Fatalf("checkForUpdates: %v", err)
	}
	if !result.Outdated {
		t.Fatalf("expected outdated=true, got %+v", result)
	}
	// Newest overall is v1.1.0-beta; older stable v1.0.0 must not suppress it.
	if result.LatestVersion != "v1.1.0-beta" {
		t.Fatalf("latest version = %q, want v1.1.0-beta", result.LatestVersion)
	}
}

func TestCheckForUpdatesFallsBackToPrerelease(t *testing.T) {
	origURL := updateCheckLatestReleaseURL
	origReleasesURL := updateCheckReleasesURL
	origClient := updateCheckHTTPClient
	origNow := updateCheckNow
	t.Cleanup(func() {
		updateCheckLatestReleaseURL = origURL
		updateCheckReleasesURL = origReleasesURL
		updateCheckHTTPClient = origClient
		updateCheckNow = origNow
	})

	updateCheckLatestReleaseURL = "https://example.com/latest"
	updateCheckReleasesURL = "https://example.com/releases"
	updateCheckHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.String() {
			case updateCheckReleasesURL:
				// Only prereleases exist (current state of the project).
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`[
						{"tag_name":"v0.0.7-beta","html_url":"https://example.com/releases/v0.0.7-beta"},
						{"tag_name":"v0.0.8-beta","html_url":"https://example.com/releases/v0.0.8-beta"}
					]`)),
				}, nil
			default:
				return nil, fmt.Errorf("unexpected URL: %s", r.URL.String())
			}
		}),
	}
	updateCheckNow = func() time.Time {
		return time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)
	}

	result, err := checkForUpdates(context.Background(), updateCheckOptions{
		CurrentVersion: "v0.0.7-beta",
		DataDir:        t.TempDir(),
		Force:          true,
	})
	if err != nil {
		t.Fatalf("checkForUpdates: %v", err)
	}
	if !result.Outdated {
		t.Fatalf("expected outdated=true, got %+v", result)
	}
	// No stable releases exist, so the newest prerelease should be used.
	if result.LatestVersion != "v0.0.8-beta" {
		t.Fatalf("latest version = %q, want v0.0.8-beta", result.LatestVersion)
	}
}

func TestComparePrereleaseIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{name: "equal", a: "beta", b: "beta", want: 0},
		{name: "alpha before beta", a: "alpha", b: "beta", want: -1},
		{name: "numeric segments", a: "rc.2", b: "rc.10", want: -1},
		{name: "numeric less than string", a: "1", b: "alpha", want: -1},
		{name: "shorter prefix lower", a: "beta", b: "beta.1", want: -1},
		{name: "beta.2 before beta.10", a: "beta.2", b: "beta.10", want: -1},
		{name: "rc before rc.1", a: "rc", b: "rc.1", want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comparePrereleaseIdentifiers(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("comparePrereleaseIdentifiers(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestNewestComparableReleaseReturnsNewest(t *testing.T) {
	releases := []latestReleaseResponse{
		{TagName: "v1.1.0-beta", HTMLURL: "https://example.com/v1.1.0-beta"},
		{TagName: "v1.0.0", HTMLURL: "https://example.com/v1.0.0"},
		{TagName: "v0.9.0", HTMLURL: "https://example.com/v0.9.0"},
	}
	best, ok := newestComparableRelease(releases)
	if !ok {
		t.Fatal("expected a result")
	}
	// v1.1.0-beta is newer than v1.0.0; older stable must not suppress it.
	if best.TagName != "v1.1.0-beta" {
		t.Fatalf("got %q, want v1.1.0-beta (newest overall)", best.TagName)
	}
}

func TestNewestComparableReleasePrefersStableWhenNewer(t *testing.T) {
	releases := []latestReleaseResponse{
		{TagName: "v1.0.0", HTMLURL: "https://example.com/v1.0.0"},
		{TagName: "v0.9.0-beta", HTMLURL: "https://example.com/v0.9.0-beta"},
		{TagName: "v0.8.0", HTMLURL: "https://example.com/v0.8.0"},
	}
	best, ok := newestComparableRelease(releases)
	if !ok {
		t.Fatal("expected a result")
	}
	// v1.0.0 stable is newer than v0.9.0-beta; stable wins.
	if best.TagName != "v1.0.0" {
		t.Fatalf("got %q, want v1.0.0 (stable is newest)", best.TagName)
	}
}

func TestNewestComparableReleaseFallsBackToPrerelease(t *testing.T) {
	releases := []latestReleaseResponse{
		{TagName: "v0.0.7-beta", HTMLURL: "https://example.com/v0.0.7-beta"},
		{TagName: "v0.0.8-beta", HTMLURL: "https://example.com/v0.0.8-beta"},
	}
	best, ok := newestComparableRelease(releases)
	if !ok {
		t.Fatal("expected a result")
	}
	if best.TagName != "v0.0.8-beta" {
		t.Fatalf("got %q, want v0.0.8-beta (should use newest prerelease when no stable exists)", best.TagName)
	}
}
