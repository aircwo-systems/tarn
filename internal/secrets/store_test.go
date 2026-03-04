package secrets

import (
	"testing"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

func newTestService() *Service {
	cfg := config.Default()
	return NewService(cfg)
}

func TestCreateAndGetSecret(t *testing.T) {
	svc := newTestService()

	secret, err := svc.CreateSecret("test-secret", "a test secret", "password123", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if secret.Name != "test-secret" {
		t.Fatalf("expected name 'test-secret', got %q", secret.Name)
	}
	if secret.SecretString != "password123" {
		t.Fatalf("expected secret value 'password123', got %q", secret.SecretString)
	}
	if secret.ARN == "" {
		t.Fatal("expected non-empty ARN")
	}
	if secret.VersionId == "" {
		t.Fatal("expected non-empty VersionId")
	}
	if len(secret.VersionStages) == 0 || secret.VersionStages[0] != "AWSCURRENT" {
		t.Fatalf("expected VersionStages [AWSCURRENT], got %v", secret.VersionStages)
	}

	// Get by name
	got, err := svc.GetSecretValue("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got.SecretString != "password123" {
		t.Fatalf("expected 'password123', got %q", got.SecretString)
	}
}

func TestCreateSecretBinary(t *testing.T) {
	svc := newTestService()

	binaryData := []byte{0x00, 0x01, 0x02, 0xFF}
	secret, err := svc.CreateSecret("binary-secret", "", "", binaryData, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetSecretValue("binary-secret")
	if err != nil {
		t.Fatal(err)
	}

	if string(got.SecretBinary) != string(binaryData) {
		t.Fatalf("binary data mismatch")
	}
	if got.SecretString != "" {
		t.Fatalf("expected empty SecretString for binary secret, got %q", got.SecretString)
	}
	_ = secret
}

func TestUpdateSecret(t *testing.T) {
	svc := newTestService()

	if _, err := svc.CreateSecret("update-me", "", "old-value", nil, nil); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	updated, err := svc.UpdateSecret("update-me", "new-value", nil)
	if err != nil {
		t.Fatal(err)
	}

	if updated.SecretString != "new-value" {
		t.Fatalf("expected 'new-value', got %q", updated.SecretString)
	}

	got, _ := svc.GetSecretValue("update-me")
	if got.SecretString != "new-value" {
		t.Fatalf("expected 'new-value' after update, got %q", got.SecretString)
	}
}

func TestDeleteSecret(t *testing.T) {
	svc := newTestService()

	if _, err := svc.CreateSecret("delete-me", "", "value", nil, nil); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	err := svc.DeleteSecret("delete-me")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.GetSecretValue("delete-me")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestListSecrets(t *testing.T) {
	svc := newTestService()

	if _, err := svc.CreateSecret("secret-1", "first", "val1", nil, nil); err != nil {
		t.Fatalf("create secret-1: %v", err)
	}
	if _, err := svc.CreateSecret("secret-2", "second", "val2", nil, nil); err != nil {
		t.Fatalf("create secret-2: %v", err)
	}

	list := svc.ListSecrets()
	if len(list) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(list))
	}

	names := map[string]bool{}
	for _, s := range list {
		names[s.Name] = true
	}
	if !names["secret-1"] || !names["secret-2"] {
		t.Fatalf("expected both secrets in list, got %v", names)
	}
}

func TestDescribeSecret(t *testing.T) {
	svc := newTestService()

	if _, err := svc.CreateSecret("describe-me", "test description", "value", nil, nil); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	secret, err := svc.DescribeSecret("describe-me")
	if err != nil {
		t.Fatal(err)
	}

	if secret.Description != "test description" {
		t.Fatalf("expected description 'test description', got %q", secret.Description)
	}
}

func TestSecretTags(t *testing.T) {
	svc := newTestService()

	tags := []types.SecretTag{
		{Key: "env", Value: "test"},
		{Key: "team", Value: "backend"},
	}
	if _, err := svc.CreateSecret("tagged-secret", "", "value", nil, tags); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	secret, _ := svc.DescribeSecret("tagged-secret")
	if len(secret.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(secret.Tags))
	}

	// Add tag
	if err := svc.TagResource("tagged-secret", []types.SecretTag{{Key: "version", Value: "1.0"}}); err != nil {
		t.Fatalf("tag resource: %v", err)
	}
	secret, _ = svc.DescribeSecret("tagged-secret")
	if len(secret.Tags) != 3 {
		t.Fatalf("expected 3 tags after add, got %d", len(secret.Tags))
	}

	// Update existing tag
	if err := svc.TagResource("tagged-secret", []types.SecretTag{{Key: "env", Value: "prod"}}); err != nil {
		t.Fatalf("tag resource update: %v", err)
	}
	secret, _ = svc.DescribeSecret("tagged-secret")
	for _, tag := range secret.Tags {
		if tag.Key == "env" && tag.Value != "prod" {
			t.Fatalf("expected env=prod, got env=%s", tag.Value)
		}
	}

	// Remove tag
	if err := svc.UntagResource("tagged-secret", []string{"team"}); err != nil {
		t.Fatalf("untag resource: %v", err)
	}
	secret, _ = svc.DescribeSecret("tagged-secret")
	if len(secret.Tags) != 2 {
		t.Fatalf("expected 2 tags after remove, got %d", len(secret.Tags))
	}
	for _, tag := range secret.Tags {
		if tag.Key == "team" {
			t.Fatal("tag 'team' should have been removed")
		}
	}
}

func TestGetByARN(t *testing.T) {
	svc := newTestService()

	secret, _ := svc.CreateSecret("arn-test", "", "secret-val", nil, nil)

	got, err := svc.GetSecretValue(secret.ARN)
	if err != nil {
		t.Fatal(err)
	}
	if got.SecretString != "secret-val" {
		t.Fatalf("expected 'secret-val', got %q", got.SecretString)
	}
}

func TestDuplicateNameError(t *testing.T) {
	svc := newTestService()

	_, err := svc.CreateSecret("dupe", "", "val1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateSecret("dupe", "", "val2", nil, nil)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestSecretsPersistAcrossServiceInit(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true

	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}

	_, err := svc.CreateSecret("persisted-secret", "", "super-secret", nil, []types.SecretTag{{Key: "feature", Value: "r10"}})
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}

	reloaded := NewService(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("reload service: %v", err)
	}

	secret, err := reloaded.GetSecretValue("persisted-secret")
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if secret.SecretString != "super-secret" {
		t.Fatalf("secret value = %q, want %q", secret.SecretString, "super-secret")
	}
	if len(secret.Tags) != 1 || secret.Tags[0].Value != "r10" {
		t.Fatalf("secret tags = %+v, want feature=r10", secret.Tags)
	}
}
