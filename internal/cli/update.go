package cli

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/spf13/cobra"
)

const maxUpdateArchiveBytes = 512 << 20

var (
	updateDownloadBaseURL = "https://github.com/aircwo-systems/tarn/releases/download"
	updateHTTPClient      = &http.Client{Timeout: 5 * time.Minute}
	updateExecutablePath  = os.Executable
	updateGOOS            = runtime.GOOS
	updateGOARCH          = runtime.GOARCH
	// updateSmokeTest runs the freshly installed binary; swapped out in tests.
	updateSmokeTest = func(path string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, "--help")
		cmd.Env = append(os.Environ(), "TARN_DISABLE_UPDATE_CHECK=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

type updateOptions struct {
	CurrentVersion string
	TargetVersion  string
	DataDir        string
	CheckOnly      bool
	Force          bool
}

func newUpdateCmd() *cobra.Command {
	var target string
	var checkOnly, force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update tarn to the latest release",
		Long: `Download the latest tarn release (or --version), verify it against the
release checksums.txt, and replace the running binary in place.

Only binaries installed by the tarn install script (~/.tarn/bin, or
TARN_INSTALL_DIR) are updated; use your package manager or rebuild otherwise.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), updateOptions{
				CurrentVersion: version,
				TargetVersion:  target,
				DataDir:        resolveCLIDataDir(cmd),
				CheckOnly:      checkOnly,
				Force:          force,
			})
		},
	}
	cmd.Flags().StringVar(&target, "version", "", "Install a specific release tag (e.g. v0.4.0) instead of the latest")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Report what would be installed without changing anything")
	cmd.Flags().BoolVar(&force, "force", false, "Update dev builds and binaries outside the install directory")
	return cmd
}

func runUpdate(ctx context.Context, out io.Writer, opts updateOptions) error {
	exe, err := updateExecutablePath()
	if err != nil {
		return fmt.Errorf("locate tarn binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	if !opts.Force {
		if err := checkManagedInstall(exe); err != nil {
			return err
		}
		if _, ok := parseSemanticVersion(opts.CurrentVersion); !ok || strings.HasSuffix(opts.CurrentVersion, "-dev") {
			return fmt.Errorf("tarn %s is a development build; rebuild from source or pass --force", opts.CurrentVersion)
		}
	}

	tag := strings.TrimSpace(opts.TargetVersion)
	if tag == "" {
		lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		release, err := fetchLatestRelease(lookupCtx)
		if err != nil {
			return fmt.Errorf("look up latest release: %w", err)
		}
		tag = strings.TrimSpace(release.TagName)
		if tag == "" {
			return errors.New("no release information available")
		}
		if !opts.Force && !isOutdatedVersion(opts.CurrentVersion, tag) {
			_, _ = fmt.Fprintf(out, "tarn %s is up to date\n", opts.CurrentVersion)
			return nil
		}
	} else if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}

	asset, err := updateAssetName(updateGOOS, updateGOARCH)
	if err != nil {
		return err
	}

	if opts.CheckOnly {
		_, _ = fmt.Fprintf(out, "Would update tarn %s -> %s (%s)\n  %s\n", opts.CurrentVersion, tag, asset, exe)
		return nil
	}

	_, _ = fmt.Fprintf(out, "Updating tarn %s -> %s\n", opts.CurrentVersion, tag)

	base := strings.TrimRight(updateDownloadBaseURL, "/") + "/" + tag
	sums, err := downloadBytes(ctx, base+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w (releases before checksums were published can't be verified; use the install script)", err)
	}
	expected, err := checksumFor(sums, asset)
	if err != nil {
		return err
	}

	archive, err := downloadBytes(ctx, base+"/"+asset)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expected {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s); not installing", asset, expected, hex.EncodeToString(actual[:]))
	}
	_, _ = fmt.Fprintln(out, "  checksum verified")

	binName := "tarn"
	if updateGOOS == "windows" {
		binName = "tarn.exe"
	}
	bin, err := extractBinary(archive, asset, binName)
	if err != nil {
		return err
	}

	if err := replaceExecutable(exe, bin); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "  installed %s\n", exe)

	if pid := strings.TrimSpace(readPIDFile(opts.DataDir)); pid != "" {
		_, _ = fmt.Fprintf(out, "A tarn server may be running (pid %s); restart it with `tarn stop && tarn start` to use %s.\n", pid, tag)
	}
	_, _ = fmt.Fprintf(out, "tarn is now %s\n", tag)
	return nil
}

// checkManagedInstall refuses to overwrite binaries owned by a package manager
// or a developer's local build tree.
func checkManagedInstall(exe string) error {
	dir := filepath.Clean(filepath.Dir(exe))
	for _, allowed := range managedInstallDirs() {
		// exe has symlinks resolved, so compare against the resolved dir too.
		if resolved, err := filepath.EvalSymlinks(allowed); err == nil {
			allowed = resolved
		}
		if dir == filepath.Clean(allowed) {
			return nil
		}
	}

	hint := "reinstall with: curl -fsSL https://aircwo-systems.github.io/tarn/install.sh | sh"
	switch {
	case strings.Contains(dir, "/Cellar/") || strings.Contains(dir, "/homebrew/") || strings.Contains(dir, "/linuxbrew/"):
		hint = "this binary is managed by Homebrew; run: brew upgrade tarn"
	case strings.HasSuffix(dir, string(filepath.Separator)+"build"):
		hint = "this looks like a local build; run: git pull && make build"
	case isGoBinDir(dir):
		hint = "this binary was installed with go install; rerun go install"
	}
	return fmt.Errorf("tarn at %s was not installed by the tarn installer; %s (or pass --force)", exe, hint)
}

func managedInstallDirs() []string {
	var dirs []string
	if v := strings.TrimSpace(os.Getenv("TARN_INSTALL_DIR")); v != "" {
		dirs = append(dirs, v)
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".tarn", "bin"),
			// Location used by the original darwin-only install script.
			filepath.Join(home, ".local", "bin"),
		)
	}
	return dirs
}

func isGoBinDir(dir string) bool {
	if v := os.Getenv("GOBIN"); v != "" && filepath.Clean(v) == dir {
		return true
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			gopath = filepath.Join(home, "go")
		}
	}
	return gopath != "" && filepath.Join(gopath, "bin") == dir
}

func updateAssetName(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64":
		return fmt.Sprintf("tarn-%s-%s.tar.gz", goos, goarch), nil
	case "windows/amd64":
		return "tarn-windows-amd64.zip", nil
	}
	return "", fmt.Errorf("no prebuilt tarn release for %s/%s", goos, goarch)
}

func downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tarn-cli-update")
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpdateArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxUpdateArchiveBytes {
		return nil, fmt.Errorf("GET %s: response exceeds %d bytes", url, maxUpdateArchiveBytes)
	}
	return body, nil
}

// checksumFor finds asset's SHA-256 in sha256sum-formatted output.
func checksumFor(sums []byte, asset string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") == asset {
			sum := strings.ToLower(fields[0])
			if _, err := hex.DecodeString(sum); err != nil || len(sum) != sha256.Size*2 {
				return "", fmt.Errorf("malformed checksum for %s", asset)
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in checksums.txt", asset)
}

// extractBinary returns the contents of binName from a .tar.gz or .zip
// archive. The binary must sit at the archive root.
func extractBinary(archive []byte, asset, binName string) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", asset, err)
		}
		for _, f := range zr.File {
			if f.Name != binName {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, maxUpdateArchiveBytes))
		}
		return nil, fmt.Errorf("%s not found in %s", binName, asset)
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", asset, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s not found in %s", binName, asset)
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", asset, err)
		}
		if hdr.Typeflag == tar.TypeReg && strings.TrimPrefix(hdr.Name, "./") == binName {
			return io.ReadAll(io.LimitReader(tr, maxUpdateArchiveBytes))
		}
	}
}

// replaceExecutable swaps exe for bin, smoke-tests the result and restores
// the previous binary if the new one doesn't run.
func replaceExecutable(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".tarn-update-*")
	if err != nil {
		return fmt.Errorf("stage update in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return fmt.Errorf("stage update: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("stage update: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("stage update: %w", err)
	}

	// Keep the old binary alongside so we can roll back. Renaming a running
	// executable is allowed on Unix and Windows; overwriting it is not on Windows.
	backup := exe + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("replace %s: %w", exe, err)
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		_ = os.Rename(backup, exe)
		return fmt.Errorf("replace %s: %w", exe, err)
	}

	if err := updateSmokeTest(exe); err != nil {
		_ = os.Remove(exe)
		if rbErr := os.Rename(backup, exe); rbErr != nil {
			return fmt.Errorf("new binary failed to run (%v) and rollback failed: %w; previous binary is at %s", err, rbErr, backup)
		}
		return fmt.Errorf("new binary failed to run, kept previous version: %w", err)
	}

	// Windows can't delete the running binary; it is cleaned up on the next update.
	_ = os.Remove(backup)
	return nil
}

func readPIDFile(dataDir string) string {
	cfg := config.Default()
	if strings.TrimSpace(dataDir) != "" {
		cfg.DataDir = dataDir
	}
	data, err := os.ReadFile(cfg.PIDFilePath())
	if err != nil {
		return ""
	}
	return string(data)
}
