package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRelease struct {
	tag      string
	assets   map[string][]byte
	sums     string // overrides generated checksums.txt when non-empty
	requests []string
}

func (f *fakeRelease) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		switch {
		case r.URL.Path == "/api/releases":
			fmt.Fprintf(w, `[{"tag_name":%q,"html_url":"https://example.test/%s"}]`, f.tag, f.tag)
		case r.URL.Path == "/download/"+f.tag+"/checksums.txt":
			if f.sums != "" {
				fmt.Fprint(w, f.sums)
				return
			}
			for name, body := range f.assets {
				sum := sha256.Sum256(body)
				fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
			}
		case strings.HasPrefix(r.URL.Path, "/download/"+f.tag+"/"):
			body, ok := f.assets[strings.TrimPrefix(r.URL.Path, "/download/"+f.tag+"/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	})
}

func tarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// setupUpdate points the updater at a fake release server and an installed
// binary inside a temp TARN_INSTALL_DIR. It returns the binary path.
func setupUpdate(t *testing.T, rel *fakeRelease) string {
	t.Helper()
	srv := httptest.NewServer(rel.handler())
	t.Cleanup(srv.Close)

	installDir := t.TempDir()
	exe := filepath.Join(installDir, "tarn")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TARN_INSTALL_DIR", installDir)
	t.Setenv("TARN_DISABLE_UPDATE_CHECK", "")

	origBase, origReleases, origLatest := updateDownloadBaseURL, updateCheckReleasesURL, updateCheckLatestReleaseURL
	origClient, origExe, origSmoke := updateHTTPClient, updateExecutablePath, updateSmokeTest
	origOS, origArch := updateGOOS, updateGOARCH
	t.Cleanup(func() {
		updateDownloadBaseURL, updateCheckReleasesURL, updateCheckLatestReleaseURL = origBase, origReleases, origLatest
		updateHTTPClient, updateExecutablePath, updateSmokeTest = origClient, origExe, origSmoke
		updateGOOS, updateGOARCH = origOS, origArch
	})
	updateDownloadBaseURL = srv.URL + "/download"
	updateCheckReleasesURL = srv.URL + "/api/releases"
	updateCheckLatestReleaseURL = srv.URL + "/api/latest"
	updateHTTPClient = srv.Client()
	updateExecutablePath = func() (string, error) { return exe, nil }
	updateSmokeTest = func(string) error { return nil }
	updateGOOS, updateGOARCH = "darwin", "arm64"
	return exe
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runTestUpdate(opts updateOptions) (string, error) {
	var out bytes.Buffer
	if opts.CurrentVersion == "" {
		opts.CurrentVersion = "v0.3.0"
	}
	if opts.DataDir == "" {
		opts.DataDir = os.TempDir()
	}
	err := runUpdate(context.Background(), &out, opts)
	return out.String(), err
}

func TestRunUpdateInstallsVerifiedRelease(t *testing.T) {
	rel := &fakeRelease{tag: "v0.4.0", assets: map[string][]byte{}}
	rel.assets["tarn-darwin-arm64.tar.gz"] = tarGz(t, "tarn", []byte("new-binary"))
	exe := setupUpdate(t, rel)

	out, err := runTestUpdate(updateOptions{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, out)
	}
	if got := readFile(t, exe); got != "new-binary" {
		t.Fatalf("binary = %q, want new-binary", got)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed, stat err = %v", err)
	}
	if !strings.Contains(out, "checksum verified") || !strings.Contains(out, "tarn is now v0.4.0") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestRunUpdateRejectsChecksumMismatch(t *testing.T) {
	rel := &fakeRelease{tag: "v0.4.0", assets: map[string][]byte{}}
	rel.assets["tarn-darwin-arm64.tar.gz"] = tarGz(t, "tarn", []byte("tampered"))
	rel.sums = strings.Repeat("0", 64) + "  tarn-darwin-arm64.tar.gz\n"
	exe := setupUpdate(t, rel)

	_, err := runTestUpdate(updateOptions{})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if got := readFile(t, exe); got != "old-binary" {
		t.Fatalf("binary changed to %q after checksum failure", got)
	}
}

func TestRunUpdateRollsBackWhenNewBinaryFails(t *testing.T) {
	rel := &fakeRelease{tag: "v0.4.0", assets: map[string][]byte{}}
	rel.assets["tarn-darwin-arm64.tar.gz"] = tarGz(t, "tarn", []byte("broken"))
	exe := setupUpdate(t, rel)
	updateSmokeTest = func(string) error { return errors.New("exec format error") }

	_, err := runTestUpdate(updateOptions{})
	if err == nil || !strings.Contains(err.Error(), "kept previous version") {
		t.Fatalf("err = %v, want rollback error", err)
	}
	if got := readFile(t, exe); got != "old-binary" {
		t.Fatalf("binary = %q, want rollback to old-binary", got)
	}
}

func TestRunUpdateNoopWhenCurrent(t *testing.T) {
	rel := &fakeRelease{tag: "v0.3.0", assets: map[string][]byte{}}
	exe := setupUpdate(t, rel)

	out, err := runTestUpdate(updateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "up to date") {
		t.Fatalf("output = %q, want up to date", out)
	}
	if got := readFile(t, exe); got != "old-binary" {
		t.Fatalf("binary changed to %q", got)
	}
}

func TestRunUpdateCheckOnlyDownloadsNothing(t *testing.T) {
	rel := &fakeRelease{tag: "v0.4.0", assets: map[string][]byte{}}
	exe := setupUpdate(t, rel)

	out, err := runTestUpdate(updateOptions{CheckOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Would update tarn v0.3.0 -> v0.4.0") {
		t.Fatalf("output = %q", out)
	}
	for _, p := range rel.requests {
		if strings.HasPrefix(p, "/download/") {
			t.Fatalf("--check downloaded %s", p)
		}
	}
	if got := readFile(t, exe); got != "old-binary" {
		t.Fatalf("binary changed to %q", got)
	}
}

func TestRunUpdatePinnedVersionAddsPrefix(t *testing.T) {
	rel := &fakeRelease{tag: "v0.2.0", assets: map[string][]byte{}}
	rel.assets["tarn-darwin-arm64.tar.gz"] = tarGz(t, "tarn", []byte("older-binary"))
	exe := setupUpdate(t, rel)

	if out, err := runTestUpdate(updateOptions{TargetVersion: "0.2.0"}); err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, out)
	}
	if got := readFile(t, exe); got != "older-binary" {
		t.Fatalf("binary = %q, want pinned release", got)
	}
}

func TestRunUpdateRefusesUnmanagedAndDevBuilds(t *testing.T) {
	rel := &fakeRelease{tag: "v0.4.0", assets: map[string][]byte{}}
	setupUpdate(t, rel)

	t.Run("dev build", func(t *testing.T) {
		_, err := runTestUpdate(updateOptions{CurrentVersion: "0.1.0-dev"})
		if err == nil || !strings.Contains(err.Error(), "development build") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("local build dir", func(t *testing.T) {
		buildDir := filepath.Join(t.TempDir(), "build")
		if err := os.MkdirAll(buildDir, 0o755); err != nil {
			t.Fatal(err)
		}
		exe := filepath.Join(buildDir, "tarn")
		if err := os.WriteFile(exe, []byte("local"), 0o755); err != nil {
			t.Fatal(err)
		}
		updateExecutablePath = func() (string, error) { return exe, nil }
		_, err := runTestUpdate(updateOptions{})
		if err == nil || !strings.Contains(err.Error(), "make build") {
			t.Fatalf("err = %v", err)
		}
		if got := readFile(t, exe); got != "local" {
			t.Fatalf("binary changed to %q", got)
		}
	})
}

func TestExtractBinaryZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("tarn.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("windows-binary"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractBinary(buf.Bytes(), "tarn-windows-amd64.zip", "tarn.exe")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "windows-binary" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractBinaryRejectsNestedPath(t *testing.T) {
	archive := tarGz(t, "raw-bins/tarn-darwin-arm64", []byte("x"))
	if _, err := extractBinary(archive, "tarn-darwin-arm64.tar.gz", "tarn"); err == nil {
		t.Fatal("expected error for archive without root tarn binary")
	}
}

func TestChecksumFor(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	sums := []byte(sum + "  tarn-linux-amd64.tar.gz\n" + strings.Repeat("cd", 32) + " *tarn-darwin-arm64.tar.gz\n")

	if got, err := checksumFor(sums, "tarn-linux-amd64.tar.gz"); err != nil || got != sum {
		t.Fatalf("got (%q, %v)", got, err)
	}
	if got, err := checksumFor(sums, "tarn-darwin-arm64.tar.gz"); err != nil || got != strings.Repeat("cd", 32) {
		t.Fatalf("binary-mode entry: got (%q, %v)", got, err)
	}
	if _, err := checksumFor(sums, "tarn-windows-amd64.zip"); err == nil {
		t.Fatal("expected missing checksum error")
	}
	if _, err := checksumFor([]byte("nothex  tarn-linux-amd64.tar.gz\n"), "tarn-linux-amd64.tar.gz"); err == nil {
		t.Fatal("expected malformed checksum error")
	}
}
