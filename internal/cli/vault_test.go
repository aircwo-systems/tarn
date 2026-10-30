package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/api"
	"github.com/aircwo-systems/tarn/internal/awsconnection"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/secrets"
)

func TestVaultUnlockUsesPrivateLocalCapability(t *testing.T) {
	var owner *api.AWSConnections
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { owner.ServeHTTP(w, r) }))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	cfg := config.Default()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.DataDir = t.TempDir()
	vault, err := secrets.LoadOrCreateVault(filepath.Join(t.TempDir(), "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	owner = api.NewAWSConnections(cfg, vault, func(context.Context, string, awsconnection.Credentials) (awsconnection.Identity, error) {
		t.Fatal("unlock must not call AWS")
		return awsconnection.Identity{}, nil
	})
	defer owner.Close()
	link, err := ownerUnlockLink(t.Context(), cfg.DataDir)
	if err != nil || !strings.HasPrefix(link, server.URL+"/#settings?aws-unlock=") {
		t.Fatalf("unlock: %q %v", link, err)
	}
	info, err := os.Stat(api.OwnerControlPath(cfg.DataDir))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("owner capability is not private")
	}
	owner.Close()
	if _, err := ownerUnlockLink(t.Context(), cfg.DataDir); err == nil {
		t.Fatal("unlock succeeded after capability removal")
	}
}

func TestServerVaultPreservesConnectionsWhenKeyIsMissing(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.VaultKeyPath = filepath.Join(t.TempDir(), "missing.key")
	store := awsconnection.StorePath(cfg.DataDir)
	if err := os.MkdirAll(filepath.Dir(store), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, []byte("existing encrypted records"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadServerVault(cfg); err == nil {
		t.Fatal("startup accepted a missing original key")
	}
	if _, err := os.Stat(cfg.VaultKeyPath); !os.IsNotExist(err) {
		t.Fatal("a replacement Vault key was created")
	}
	data, err := os.ReadFile(store)
	if err != nil || string(data) != "existing encrypted records" {
		t.Fatal("connection store changed during failed recovery")
	}
}
