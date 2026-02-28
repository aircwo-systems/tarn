package lambda

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

func TestExtractCode(t *testing.T) {
	// Create a temp data dir
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}

	fnName := "test-func"

	// Build a zip in memory with a handler file
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("index.js")
	if err != nil {
		t.Fatal(err)
	}
	handlerCode := `exports.handler = async (event) => { return { statusCode: 200, body: "hello" }; };`
	f.Write([]byte(handlerCode))
	zw.Close()

	// Save the zip
	hash, err := store.SaveCode(fnName, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// Extract
	extractDir, err := store.ExtractCode(fnName)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the extracted file exists and has correct content
	content, err := os.ReadFile(filepath.Join(extractDir, "index.js"))
	if err != nil {
		t.Fatalf("expected index.js to exist in %s: %v", extractDir, err)
	}
	if string(content) != handlerCode {
		t.Fatalf("content mismatch: got %q", string(content))
	}

	// Extract again — should use cache (marker file)
	extractDir2, err := store.ExtractCode(fnName)
	if err != nil {
		t.Fatal(err)
	}
	if extractDir2 != extractDir {
		t.Fatalf("expected cached dir %s, got %s", extractDir, extractDir2)
	}
}

func TestExtractCodeRefreshesWhenZipChanges(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}

	fnName := "refresh-func"
	createZip := func(contents string) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, _ := zw.Create("index.js")
		_, _ = f.Write([]byte(contents))
		_ = zw.Close()
		return buf.Bytes()
	}

	if _, err := store.SaveCode(fnName, createZip("exports.handler = async () => 'v1'")); err != nil {
		t.Fatal(err)
	}
	extractDir, err := store.ExtractCode(fnName)
	if err != nil {
		t.Fatal(err)
	}

	initial, err := os.ReadFile(filepath.Join(extractDir, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(initial), "v1") {
		t.Fatalf("expected extracted code to contain v1, got %q", string(initial))
	}

	time.Sleep(10 * time.Millisecond)

	if _, err := store.SaveCode(fnName, createZip("exports.handler = async () => 'v2'")); err != nil {
		t.Fatal(err)
	}
	extractDir2, err := store.ExtractCode(fnName)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := os.ReadFile(filepath.Join(extractDir2, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "v2") {
		t.Fatalf("expected extracted code to contain v2, got %q", string(updated))
	}
}

func TestExtractCodeZipSlip(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	store := NewStore(cfg)
	store.Init()

	fnName := "slip-func"

	// Build a zip with a path traversal entry
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// This should be skipped during extraction
	f, _ := zw.Create("../../etc/evil.txt")
	f.Write([]byte("malicious"))
	// This is legitimate
	f2, _ := zw.Create("handler.py")
	f2.Write([]byte("def handler(event, context): pass"))
	zw.Close()

	store.SaveCode(fnName, buf.Bytes())
	extractDir, err := store.ExtractCode(fnName)
	if err != nil {
		t.Fatal(err)
	}

	// The zip slip entry should NOT exist
	if _, err := os.Stat(filepath.Join(extractDir, "../../etc/evil.txt")); err == nil {
		t.Fatal("zip slip: evil.txt should not have been extracted")
	}

	// The legitimate file should exist
	if _, err := os.Stat(filepath.Join(extractDir, "handler.py")); err != nil {
		t.Fatal("handler.py should have been extracted")
	}
}

func TestExtractCodeNested(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	store := NewStore(cfg)
	store.Init()

	fnName := "nested-func"

	// Build a zip with nested directories
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f1, _ := zw.Create("src/utils/helper.js")
	f1.Write([]byte("module.exports = {}"))
	f2, _ := zw.Create("index.js")
	f2.Write([]byte("require('./src/utils/helper')"))
	zw.Close()

	store.SaveCode(fnName, buf.Bytes())
	extractDir, err := store.ExtractCode(fnName)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(extractDir, "src", "utils", "helper.js")); err != nil {
		t.Fatal("nested file should have been extracted")
	}
	if _, err := os.Stat(filepath.Join(extractDir, "index.js")); err != nil {
		t.Fatal("root file should have been extracted")
	}
}

func TestSaveAndGetLayer(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	store := NewStore(cfg)
	store.Init()

	layerCfg := &types.LayerConfig{
		LayerName:     "my-layer",
		LayerArn:      "arn:aws:lambda:us-east-1:000000000000:layer:my-layer",
		VersionNumber: 1,
		Description:   "test layer",
	}

	// Build layer zip
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("python/lib.py")
	f.Write([]byte("def helper(): return 42"))
	zw.Close()

	hash, err := store.SaveLayer("my-layer", 1, buf.Bytes(), layerCfg)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// Retrieve
	got, err := store.GetLayer("my-layer", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.LayerName != "my-layer" {
		t.Fatalf("expected layer name 'my-layer', got %q", got.LayerName)
	}
	if got.CodeSHA256 != hash {
		t.Fatalf("hash mismatch")
	}
}

func TestExtractLayer(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	store := NewStore(cfg)
	store.Init()

	layerCfg := &types.LayerConfig{
		LayerName:     "extract-layer",
		VersionNumber: 1,
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("python/utils.py")
	f.Write([]byte("VALUE = 'hello'"))
	zw.Close()

	store.SaveLayer("extract-layer", 1, buf.Bytes(), layerCfg)

	dir, err := store.ExtractLayer("extract-layer", 1)
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "python", "utils.py"))
	if err != nil {
		t.Fatalf("expected python/utils.py in extracted layer: %v", err)
	}
	if string(content) != "VALUE = 'hello'" {
		t.Fatalf("content mismatch: %q", string(content))
	}
}

func TestDeleteLayerVersion(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	store := NewStore(cfg)
	store.Init()

	layerCfg := &types.LayerConfig{
		LayerName:     "del-layer",
		VersionNumber: 1,
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("lib.js")
	f.Write([]byte("module.exports = {}"))
	zw.Close()

	store.SaveLayer("del-layer", 1, buf.Bytes(), layerCfg)

	// Delete
	if err := store.DeleteLayerVersion("del-layer", 1); err != nil {
		t.Fatal(err)
	}

	// Should be gone
	_, err := store.GetLayer("del-layer", 1)
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestNextLayerVersion(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	store := NewStore(cfg)
	store.Init()

	// First version should be 1
	v := store.NextLayerVersion("new-layer")
	if v != 1 {
		t.Fatalf("expected version 1, got %d", v)
	}

	// Save two versions
	layerCfg := &types.LayerConfig{LayerName: "new-layer", VersionNumber: 1}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("a.txt")
	f.Write([]byte("v1"))
	zw.Close()
	store.SaveLayer("new-layer", 1, buf.Bytes(), layerCfg)

	layerCfg2 := &types.LayerConfig{LayerName: "new-layer", VersionNumber: 2}
	buf.Reset()
	zw = zip.NewWriter(&buf)
	f, _ = zw.Create("a.txt")
	f.Write([]byte("v2"))
	zw.Close()
	store.SaveLayer("new-layer", 2, buf.Bytes(), layerCfg2)

	v = store.NextLayerVersion("new-layer")
	if v != 3 {
		t.Fatalf("expected version 3, got %d", v)
	}
}
