package kms

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/secrets"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	s := NewService(cfg)
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func createKey(t *testing.T, s *Service) KeyMetadata {
	t.Helper()
	out, err := s.CreateKey(&CreateKeyInput{Description: "test"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	return out.KeyMetadata
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ke *Error
	if !errors.As(err, &ke) || ke.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestEncryptDecryptRoundTripsUnderEveryKeyIDForm(t *testing.T) {
	s := newTestService(t)
	m := createKey(t, s)
	if _, err := s.CreateAlias(&AliasInput{AliasName: "alias/app", TargetKeyId: m.KeyId}); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	ctx := map[string]string{"userpool-id": "eu-west-2_abc", "a": "b"}

	for _, id := range []string{m.KeyId, m.Arn, "alias/app", s.aliasARN("alias/app")} {
		enc, err := s.Encrypt(&EncryptInput{KeyId: id, Plaintext: []byte("123456"), EncryptionContext: ctx})
		if err != nil {
			t.Fatalf("Encrypt(%s): %v", id, err)
		}
		if enc.KeyId != m.Arn {
			t.Fatalf("Encrypt KeyId = %q, want %q", enc.KeyId, m.Arn)
		}
		// The blob names its key, so Decrypt needs no KeyId.
		dec, err := s.Decrypt(&DecryptInput{CiphertextBlob: enc.CiphertextBlob, EncryptionContext: ctx})
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if string(dec.Plaintext) != "123456" || dec.KeyId != m.Arn {
			t.Fatalf("Decrypt = %q %q", dec.Plaintext, dec.KeyId)
		}
	}
}

func TestDecryptRejectsWrongContextKeyAndGarbage(t *testing.T) {
	s := newTestService(t)
	m := createKey(t, s)
	other := createKey(t, s)
	ctx := map[string]string{"k": "v"}
	enc, err := s.Encrypt(&EncryptInput{KeyId: m.KeyId, Plaintext: []byte("secret"), EncryptionContext: ctx})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = s.Decrypt(&DecryptInput{CiphertextBlob: enc.CiphertextBlob, EncryptionContext: map[string]string{"k": "other"}})
	wantCode(t, err, "InvalidCiphertextException")
	_, err = s.Decrypt(&DecryptInput{CiphertextBlob: enc.CiphertextBlob})
	wantCode(t, err, "InvalidCiphertextException")
	_, err = s.Decrypt(&DecryptInput{CiphertextBlob: enc.CiphertextBlob, KeyId: other.KeyId, EncryptionContext: ctx})
	wantCode(t, err, "IncorrectKeyException")
	for _, blob := range [][]byte{nil, []byte("AAAA"), bytes.Repeat([]byte{1}, 100)} {
		_, err = s.Decrypt(&DecryptInput{CiphertextBlob: blob})
		wantCode(t, err, "InvalidCiphertextException")
	}
	tampered := bytes.Clone(enc.CiphertextBlob)
	tampered[len(tampered)-1] ^= 1
	_, err = s.Decrypt(&DecryptInput{CiphertextBlob: tampered, EncryptionContext: ctx})
	wantCode(t, err, "InvalidCiphertextException")
}

func TestKeyStatesGateCryptography(t *testing.T) {
	s := newTestService(t)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	m := createKey(t, s)
	enc, err := s.Encrypt(&EncryptInput{KeyId: m.KeyId, Plaintext: []byte("x")})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := s.DisableKey(&KeyIDInput{KeyId: m.KeyId}); err != nil {
		t.Fatalf("DisableKey: %v", err)
	}
	_, err = s.Encrypt(&EncryptInput{KeyId: m.KeyId, Plaintext: []byte("x")})
	wantCode(t, err, "DisabledException")
	_, err = s.Decrypt(&DecryptInput{CiphertextBlob: enc.CiphertextBlob})
	wantCode(t, err, "DisabledException")
	if _, err := s.EnableKey(&KeyIDInput{KeyId: m.KeyId}); err != nil {
		t.Fatalf("EnableKey: %v", err)
	}

	del, err := s.ScheduleKeyDeletion(&ScheduleKeyDeletionInput{KeyId: m.KeyId, PendingWindowInDays: 7})
	if err != nil {
		t.Fatalf("ScheduleKeyDeletion: %v", err)
	}
	if del.KeyState != statePendingDeletion || del.KeyId != m.Arn {
		t.Fatalf("ScheduleKeyDeletion = %+v", del)
	}
	_, err = s.Decrypt(&DecryptInput{CiphertextBlob: enc.CiphertextBlob})
	wantCode(t, err, "KMSInvalidStateException")
	if _, err := s.CancelKeyDeletion(&KeyIDInput{KeyId: m.KeyId}); err != nil {
		t.Fatalf("CancelKeyDeletion: %v", err)
	}
	d, _ := s.DescribeKey(&KeyIDInput{KeyId: m.KeyId})
	if d.KeyMetadata.KeyState != stateDisabled || d.KeyMetadata.DeletionDate != nil {
		t.Fatalf("after cancel: %+v", d.KeyMetadata)
	}

	if _, err := s.ScheduleKeyDeletion(&ScheduleKeyDeletionInput{KeyId: m.KeyId, PendingWindowInDays: 7}); err != nil {
		t.Fatalf("ScheduleKeyDeletion: %v", err)
	}
	now = now.Add(8 * 24 * time.Hour)
	_, err = s.DescribeKey(&KeyIDInput{KeyId: m.KeyId})
	wantCode(t, err, "NotFoundException")
}

func TestAliases(t *testing.T) {
	s := newTestService(t)
	a, b := createKey(t, s), createKey(t, s)

	if _, err := s.CreateAlias(&AliasInput{AliasName: "alias/app", TargetKeyId: a.Arn}); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	_, err := s.CreateAlias(&AliasInput{AliasName: "alias/app", TargetKeyId: b.KeyId})
	wantCode(t, err, "AlreadyExistsException")
	_, err = s.CreateAlias(&AliasInput{AliasName: "alias/aws/mine", TargetKeyId: b.KeyId})
	wantCode(t, err, "NotAuthorizedException")
	_, err = s.CreateAlias(&AliasInput{AliasName: "app", TargetKeyId: b.KeyId})
	wantCode(t, err, "InvalidAliasNameException")

	if _, err := s.UpdateAlias(&AliasInput{AliasName: "alias/app", TargetKeyId: b.KeyId}); err != nil {
		t.Fatalf("UpdateAlias: %v", err)
	}
	d, err := s.DescribeKey(&KeyIDInput{KeyId: "alias/app"})
	if err != nil || d.KeyMetadata.KeyId != b.KeyId {
		t.Fatalf("DescribeKey(alias) = %+v, %v", d, err)
	}
	list, _ := s.ListAliases(&ListAliasesInput{KeyId: b.KeyId})
	if len(list.Aliases) != 1 || list.Aliases[0].AliasArn != s.aliasARN("alias/app") {
		t.Fatalf("ListAliases = %+v", list.Aliases)
	}

	// AWS managed aliases resolve to a key created on first use.
	managed, err := s.DescribeKey(&KeyIDInput{KeyId: "alias/aws/secretsmanager"})
	if err != nil || managed.KeyMetadata.KeyManager != managerAWS {
		t.Fatalf("DescribeKey(alias/aws/secretsmanager) = %+v, %v", managed, err)
	}
	_, err = s.DisableKey(&KeyIDInput{KeyId: "alias/aws/secretsmanager"})
	wantCode(t, err, "AccessDeniedException")

	if _, err := s.DeleteAlias(&AliasInput{AliasName: "alias/app"}); err != nil {
		t.Fatalf("DeleteAlias: %v", err)
	}
	_, err = s.DescribeKey(&KeyIDInput{KeyId: "alias/app"})
	wantCode(t, err, "NotFoundException")
}

func TestKeyIDResolutionIsScopedToAccountAndRegion(t *testing.T) {
	s := newTestService(t)
	m := createKey(t, s)
	for _, id := range []string{
		strings.Replace(m.Arn, s.cfg.AccountID, "111111111111", 1),
		strings.Replace(m.Arn, s.cfg.Region, "ap-south-1", 1),
		"alias/missing",
		"7f1c0000-0000-4000-8000-000000000000",
	} {
		_, err := s.DescribeKey(&KeyIDInput{KeyId: id})
		wantCode(t, err, "NotFoundException")
	}
	_, err := s.DescribeKey(&KeyIDInput{})
	wantCode(t, err, "ValidationException")
}

func TestGenerateDataKey(t *testing.T) {
	s := newTestService(t)
	m := createKey(t, s)
	for _, tc := range []struct {
		spec string
		n    int
		want int
	}{{"AES_256", 0, 32}, {"AES_128", 0, 16}, {"", 64, 64}} {
		out, err := s.GenerateDataKey(&GenerateDataKeyInput{KeyId: m.KeyId, KeySpec: tc.spec, NumberOfBytes: tc.n})
		if err != nil {
			t.Fatalf("GenerateDataKey(%s,%d): %v", tc.spec, tc.n, err)
		}
		if len(out.Plaintext) != tc.want {
			t.Fatalf("data key length = %d, want %d", len(out.Plaintext), tc.want)
		}
		dec, err := s.Decrypt(&DecryptInput{CiphertextBlob: out.CiphertextBlob})
		if err != nil || !bytes.Equal(dec.Plaintext, out.Plaintext) {
			t.Fatalf("Decrypt data key: %v", err)
		}
	}
	_, err := s.GenerateDataKey(&GenerateDataKeyInput{KeyId: m.KeyId, KeySpec: "AES_256", NumberOfBytes: 32})
	wantCode(t, err, "ValidationException")
	_, err = s.GenerateDataKey(&GenerateDataKeyInput{KeyId: m.KeyId})
	wantCode(t, err, "ValidationException")

	without, err := s.GenerateDataKeyWithoutPlaintext(&GenerateDataKeyInput{KeyId: m.KeyId, KeySpec: "AES_256"})
	if err != nil || without.Plaintext != nil || len(without.CiphertextBlob) == 0 {
		t.Fatalf("GenerateDataKeyWithoutPlaintext = %+v, %v", without, err)
	}
}

func TestReEncryptMovesCiphertextToAnotherKey(t *testing.T) {
	s := newTestService(t)
	a, b := createKey(t, s), createKey(t, s)
	enc, _ := s.Encrypt(&EncryptInput{KeyId: a.KeyId, Plaintext: []byte("x"), EncryptionContext: map[string]string{"k": "1"}})
	re, err := s.ReEncrypt(&ReEncryptInput{
		CiphertextBlob:               enc.CiphertextBlob,
		SourceEncryptionContext:      map[string]string{"k": "1"},
		DestinationKeyId:             b.KeyId,
		DestinationEncryptionContext: map[string]string{"k": "2"},
	})
	if err != nil || re.SourceKeyId != a.Arn || re.KeyId != b.Arn {
		t.Fatalf("ReEncrypt = %+v, %v", re, err)
	}
	dec, err := s.Decrypt(&DecryptInput{CiphertextBlob: re.CiphertextBlob, EncryptionContext: map[string]string{"k": "2"}})
	if err != nil || dec.KeyId != b.Arn || string(dec.Plaintext) != "x" {
		t.Fatalf("Decrypt re-encrypted = %+v, %v", dec, err)
	}
}

func TestPersistsSealedKeyMaterial(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	vault, err := secrets.LoadOrCreateVault(filepath.Join(t.TempDir(), "vault.key"))
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	s := NewService(cfg)
	s.SetVault(vault)
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	m := createKey(t, s)
	if _, err := s.CreateAlias(&AliasInput{AliasName: "alias/app", TargetKeyId: m.KeyId}); err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	enc, _ := s.Encrypt(&EncryptInput{KeyId: "alias/app", Plaintext: []byte("persisted")})
	material := s.keys[m.KeyId].Material
	s.Close()

	raw, err := os.ReadFile(cfg.KMSStatePath())
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if strings.Contains(string(raw), material) || !strings.Contains(string(raw), "vault:") {
		t.Fatalf("state file holds unsealed key material")
	}

	reloaded := NewService(cfg)
	reloaded.SetVault(vault)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	t.Cleanup(reloaded.Close)
	dec, err := reloaded.Decrypt(&DecryptInput{CiphertextBlob: enc.CiphertextBlob})
	if err != nil || string(dec.Plaintext) != "persisted" {
		t.Fatalf("Decrypt after reload = %+v, %v", dec, err)
	}

	noVault := NewService(cfg)
	if err := noVault.Init(); err == nil {
		t.Fatal("Init without a vault accepted sealed key material")
	}
}

func TestSerializeEncryptionContextMatchesESDKExample(t *testing.T) {
	// From the AWS Encryption SDK message header spec's example bytes.
	got := SerializeEncryptionContext(map[string]string{"1an": "encryption", "0This": "is", "2context": "example"})
	want := []byte("\x00\x03" +
		"\x00\x050This\x00\x02is" +
		"\x00\x031an\x00\x0aencryption" +
		"\x00\x082context\x00\x07example")
	if !bytes.Equal(got, want) {
		t.Fatalf("serialized = %x, want %x", got, want)
	}
	if SerializeEncryptionContext(nil) != nil {
		t.Fatal("empty context must serialize to no bytes")
	}
}
