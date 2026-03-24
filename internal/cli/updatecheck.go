package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/spf13/cobra"
)

const (
	updateCheckCacheTTL   = 24 * time.Hour
	updateCheckCacheName  = "update-check.json"
	updateCheckSubdirName = "cli"
)

var (
	updateCheckNow        = time.Now
	updateCheckHTTPClient = &http.Client{
		Timeout: 2 * time.Second,
	}
	updateCheckLatestReleaseURL = "https://api.github.com/repos/openstack-project/openstack/releases/latest"
	updateCheckReleasesURL      = "https://api.github.com/repos/openstack-project/openstack/releases?per_page=20"
)

type updateCheckOptions struct {
	CurrentVersion string
	DataDir        string
	Force          bool
}

type updateCheckResult struct {
	CurrentVersion string
	LatestVersion  string
	ReleaseURL     string
	Outdated       bool
	FromCache      bool
	CheckedAt      time.Time
}

type updateCheckCache struct {
	CurrentVersion string    `json:"currentVersion"`
	LatestVersion  string    `json:"latestVersion"`
	ReleaseURL     string    `json:"releaseUrl"`
	CheckedAt      time.Time `json:"checkedAt"`
}

type latestReleaseResponse struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

func maybePrintUpdateNotice(out io.Writer, dataDir, currentVersion string) {
	if shouldDisableUpdateCheck() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()

		result, err := checkForUpdates(ctx, updateCheckOptions{
			CurrentVersion: currentVersion,
			DataDir:        dataDir,
			Force:          false,
		})
		if err != nil || !result.Outdated {
			return
		}
		_, _ = fmt.Fprintf(out, "Update available: openstack %s -> %s (%s)\n", result.CurrentVersion, result.LatestVersion, result.ReleaseURL)
	}()
}

func runVersionUpdateCheck(cmd *cobra.Command, out io.Writer, currentVersion string) error {
	dataDir := resolveCLIDataDir(cmd)
	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer cancel()

	result, err := checkForUpdates(ctx, updateCheckOptions{
		CurrentVersion: currentVersion,
		DataDir:        dataDir,
		Force:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	if result.Outdated {
		_, _ = fmt.Fprintf(out, "Update available: openstack %s -> %s\n", result.CurrentVersion, result.LatestVersion)
		if strings.TrimSpace(result.ReleaseURL) != "" {
			_, _ = fmt.Fprintf(out, "Release notes: %s\n", result.ReleaseURL)
		}
		return nil
	}

	if strings.TrimSpace(result.LatestVersion) == "" {
		_, _ = fmt.Fprintln(out, "No release information available.")
		return nil
	}
	_, _ = fmt.Fprintf(out, "openstack %s is up to date\n", result.CurrentVersion)
	return nil
}

func checkForUpdates(ctx context.Context, opts updateCheckOptions) (updateCheckResult, error) {
	result := updateCheckResult{CurrentVersion: strings.TrimSpace(opts.CurrentVersion)}
	if shouldDisableUpdateCheck() {
		return result, nil
	}

	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir == "" {
		dataDir = config.Default().DataDir
	}

	cachePath := filepath.Join(dataDir, updateCheckSubdirName, updateCheckCacheName)
	cached, hasCached, cacheErr := loadUpdateCheckCache(cachePath)
	if cacheErr != nil {
		return result, cacheErr
	}

	if hasCached && strings.EqualFold(cached.CurrentVersion, result.CurrentVersion) {
		result.LatestVersion = cached.LatestVersion
		result.ReleaseURL = cached.ReleaseURL
		result.Outdated = isOutdatedVersion(result.CurrentVersion, cached.LatestVersion)
		result.CheckedAt = cached.CheckedAt
		result.FromCache = true

		if !opts.Force && updateCheckNow().Sub(cached.CheckedAt) < updateCheckCacheTTL {
			return result, nil
		}
	}

	release, err := fetchLatestRelease(ctx)
	if err != nil {
		if hasCached {
			return result, nil
		}
		return result, err
	}

	result.LatestVersion = strings.TrimSpace(release.TagName)
	result.ReleaseURL = strings.TrimSpace(release.HTMLURL)
	result.Outdated = isOutdatedVersion(result.CurrentVersion, result.LatestVersion)
	result.CheckedAt = updateCheckNow().UTC()
	result.FromCache = false

	_ = saveUpdateCheckCache(cachePath, updateCheckCache{
		CurrentVersion: result.CurrentVersion,
		LatestVersion:  result.LatestVersion,
		ReleaseURL:     result.ReleaseURL,
		CheckedAt:      result.CheckedAt,
	})

	return result, nil
}

func fetchLatestRelease(ctx context.Context) (*latestReleaseResponse, error) {
	releases, err := fetchReleaseList(ctx, updateCheckReleasesURL)
	if err != nil {
		// Fallback to GitHub's /latest endpoint for compatibility.
		return fetchSingleRelease(ctx, updateCheckLatestReleaseURL)
	}
	best, ok := newestComparableRelease(releases)
	if ok {
		return &best, nil
	}
	// If list endpoint had no comparable tags, still try /latest.
	return fetchSingleRelease(ctx, updateCheckLatestReleaseURL)
}

func fetchSingleRelease(ctx context.Context, url string) (*latestReleaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "openstack-cli-version-check")

	resp, err := updateCheckHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release lookup failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release latestReleaseResponse
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse release response: %w", err)
	}
	return &release, nil
}

func fetchReleaseList(ctx context.Context, url string) ([]latestReleaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "openstack-cli-version-check")

	resp, err := updateCheckHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("releases lookup failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var releases []latestReleaseResponse
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases response: %w", err)
	}
	return releases, nil
}

func newestComparableRelease(releases []latestReleaseResponse) (latestReleaseResponse, bool) {
	var best latestReleaseResponse
	bestSet := false

	for _, release := range releases {
		if release.Draft || strings.TrimSpace(release.TagName) == "" {
			continue
		}
		if _, ok := parseSemanticVersion(release.TagName); !ok {
			continue
		}
		if !bestSet {
			best = release
			bestSet = true
			continue
		}
		if cmp, ok := compareSemanticVersions(best.TagName, release.TagName); ok && cmp < 0 {
			best = release
		}
	}

	return best, bestSet
}

func loadUpdateCheckCache(path string) (updateCheckCache, bool, error) {
	var cached updateCheckCache

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cached, false, nil
		}
		return cached, false, fmt.Errorf("read update-check cache: %w", err)
	}

	if err := json.Unmarshal(raw, &cached); err != nil {
		return cached, false, fmt.Errorf("decode update-check cache: %w", err)
	}
	return cached, true, nil
}

func saveUpdateCheckCache(path string, cached updateCheckCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create update-check cache dir: %w", err)
	}
	payload, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("encode update-check cache: %w", err)
	}
	return os.WriteFile(path, payload, 0o644)
}

func resolveCLIDataDir(cmd *cobra.Command) string {
	cfg := config.Default()
	cfg.LoadFromEnv()
	if cmd != nil {
		if v, err := cmd.Flags().GetString("data-dir"); err == nil && strings.TrimSpace(v) != "" {
			cfg.DataDir = strings.TrimSpace(v)
		}
	}
	return cfg.DataDir
}

func shouldDisableUpdateCheck() bool {
	v := strings.TrimSpace(os.Getenv("OPENSTACK_DISABLE_UPDATE_CHECK"))
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

func isOutdatedVersion(current, latest string) bool {
	if isDevelopmentVersion(current) {
		return false
	}
	cmp, ok := compareSemanticVersions(current, latest)
	if !ok {
		return false
	}
	return cmp < 0
}

func isDevelopmentVersion(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return strings.Contains(v, "-dev")
}

type semanticVersion struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
}

func compareSemanticVersions(current, latest string) (int, bool) {
	a, ok := parseSemanticVersion(current)
	if !ok {
		return 0, false
	}
	b, ok := parseSemanticVersion(latest)
	if !ok {
		return 0, false
	}

	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1, true
		}
		return 1, true
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1, true
		}
		return 1, true
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1, true
		}
		return 1, true
	}

	if a.Prerelease == b.Prerelease {
		return 0, true
	}
	if a.Prerelease == "" {
		return 1, true
	}
	if b.Prerelease == "" {
		return -1, true
	}
	if a.Prerelease < b.Prerelease {
		return -1, true
	}
	return 1, true
}

func parseSemanticVersion(v string) (semanticVersion, bool) {
	normalized := strings.TrimSpace(v)
	normalized = strings.TrimPrefix(normalized, "v")
	if normalized == "" {
		return semanticVersion{}, false
	}

	if idx := strings.Index(normalized, "+"); idx >= 0 {
		normalized = normalized[:idx]
	}

	parsed := semanticVersion{}
	if idx := strings.Index(normalized, "-"); idx >= 0 {
		parsed.Prerelease = normalized[idx+1:]
		normalized = normalized[:idx]
	}

	parts := strings.Split(normalized, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semanticVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semanticVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semanticVersion{}, false
	}

	parsed.Major = major
	parsed.Minor = minor
	parsed.Patch = patch
	return parsed, true
}
