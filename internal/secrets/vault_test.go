package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSealUnsealRoundTrip(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v, err := LoadOrCreateVault(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateVault: %v", err)
	}

	plain := "super-secret-api-key-12345"
	sealed, err := v.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if !IsSealed(sealed) {
		t.Fatalf("expected sealed prefix, got: %s", sealed)
	}
	if sealed == plain {
		t.Fatal("sealed value should differ from plaintext")
	}

	got, err := v.Unseal(sealed)
	if err != nil {
		t.Fatalf("Unseal: %v", err)
	}
	if got != plain {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, plain)
	}
}

func TestSealUnsealBytesRoundTrip(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v, err := LoadOrCreateVault(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateVault: %v", err)
	}

	data := []byte{0x00, 0x01, 0xFF, 0xFE, 0x42}
	sealed, err := v.SealBytes(data)
	if err != nil {
		t.Fatalf("SealBytes: %v", err)
	}

	got, err := v.UnsealBytes(sealed)
	if err != nil {
		t.Fatalf("UnsealBytes: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(data))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("byte %d mismatch: got %02x, want %02x", i, got[i], data[i])
		}
	}
}

func TestSealEmptyIsNoop(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v, err := LoadOrCreateVault(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateVault: %v", err)
	}

	sealed, err := v.Seal("")
	if err != nil {
		t.Fatalf("Seal empty: %v", err)
	}
	if sealed != "" {
		t.Fatalf("expected empty, got %q", sealed)
	}

	unsealed, err := v.Unseal("")
	if err != nil {
		t.Fatalf("Unseal empty: %v", err)
	}
	if unsealed != "" {
		t.Fatalf("expected empty, got %q", unsealed)
	}
}

func TestAutoGeneratesKeyFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")

	// File should not exist yet
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("key file should not exist before LoadOrCreateVault")
	}

	_, err := LoadOrCreateVault(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateVault: %v", err)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file should exist after LoadOrCreateVault: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("key file permissions should be 0600, got %o", info.Mode().Perm())
	}
	if info.Size() != vaultKeyLen {
		t.Fatalf("key file should be %d bytes, got %d", vaultKeyLen, info.Size())
	}
}

func TestReloadsExistingKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")

	v1, err := LoadOrCreateVault(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateVault (first): %v", err)
	}

	sealed, err := v1.Seal("test-secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Load the same key again — should decrypt successfully
	v2, err := LoadOrCreateVault(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateVault (second): %v", err)
	}

	got, err := v2.Unseal(sealed)
	if err != nil {
		t.Fatalf("Unseal with reloaded key: %v", err)
	}
	if got != "test-secret" {
		t.Fatalf("expected 'test-secret', got %q", got)
	}
}

func TestWrongKeyFailsDecrypt(t *testing.T) {
	dir := t.TempDir()

	v1, err := LoadOrCreateVault(filepath.Join(dir, "key1"))
	if err != nil {
		t.Fatalf("LoadOrCreateVault key1: %v", err)
	}

	sealed, err := v1.Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	v2, err := LoadOrCreateVault(filepath.Join(dir, "key2"))
	if err != nil {
		t.Fatalf("LoadOrCreateVault key2: %v", err)
	}

	_, err = v2.Unseal(sealed)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestBadKeySizeRejected(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := os.WriteFile(keyPath, []byte("too-short"), 0600); err != nil {
		t.Fatalf("write bad key: %v", err)
	}

	_, err := LoadOrCreateVault(keyPath)
	if err == nil {
		t.Fatal("expected error for wrong key size")
	}
}

func TestUnsealNonPrefixedFails(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v, err := LoadOrCreateVault(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateVault: %v", err)
	}

	_, err = v.Unseal("not-a-sealed-value")
	if err == nil {
		t.Fatal("expected error for non-prefixed value")
	}
}

func TestIsSealedHelper(t *testing.T) {
	if IsSealed("plaintext") {
		t.Fatal("plaintext should not be sealed")
	}
	if !IsSealed("vault:v1:abc123") {
		t.Fatal("vault:v1: prefix should be sealed")
	}
	if IsSealed("") {
		t.Fatal("empty string should not be sealed")
	}
}

func TestSealProducesDifferentCiphertexts(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v, err := LoadOrCreateVault(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateVault: %v", err)
	}

	s1, _ := v.Seal("same-input")
	s2, _ := v.Seal("same-input")

	if s1 == s2 {
		t.Fatal("two seals of the same input should produce different ciphertexts (random nonce)")
	}

	// Both should unseal to the same value
	p1, _ := v.Unseal(s1)
	p2, _ := v.Unseal(s2)
	if p1 != p2 || p1 != "same-input" {
		t.Fatalf("both should unseal to 'same-input', got %q and %q", p1, p2)
	}
}
