package awsconnection

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/secrets"
	"github.com/aws/smithy-go"
)

var testIdentity = Identity{AccountID: "123456789012", ARN: "arn:aws:iam::123456789012:user/developer"}

func testInput(name string) Input {
	return Input{Name: name, Region: "eu-west-2", Credentials: &Credentials{
		Kind: "temporary", AccessKeyID: "ASIAEXAMPLE1234567890", SecretAccessKey: "example-secret-key", SessionToken: "example-session-token",
	}}
}

func openTestService(t *testing.T, validate Validator) (*Service, string, *secrets.Vault) {
	t.Helper()
	dir := t.TempDir()
	vault, err := secrets.LoadOrCreateVault(filepath.Join(t.TempDir(), "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	if validate == nil {
		validate = func(context.Context, string, Credentials) (Identity, error) { return testIdentity, nil }
	}
	s, err := Open(dir, vault, validate)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, vault
}

func TestEncryptedLifecycle(t *testing.T) {
	s, dir, vault := openTestService(t, nil)
	m, err := s.Create(t.Context(), testInput("Development"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), testInput("development")); !errors.Is(err, ErrName) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := s.Create(t.Context(), testInput("Staging")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(StorePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"ASIAEXAMPLE1234567890", "example-secret-key", "example-session-token"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("credential leaked to disk")
		}
		metadata, _ := json.Marshal(s.List())
		if strings.Contains(string(metadata), secret) {
			t.Fatal("credential leaked into metadata")
		}
	}
	for path, mode := range map[string]os.FileMode{StorePath(dir): 0600, filepath.Dir(StorePath(dir)): 0700} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("permissions for %s: %v %v", path, info, err)
		}
	}
	reopened, err := Open(dir, vault, s.validate)
	if err != nil {
		t.Fatal(err)
	}
	if list := reopened.List(); len(list) != 2 || list[0].Status != "untested" || list[0].Identity != testIdentity {
		t.Fatalf("reloaded: %+v", list)
	}
	if _, err := reopened.Test(t.Context(), m.ID); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	reopened, err = Open(dir, vault, s.validate)
	if err != nil || len(reopened.List()) != 1 {
		t.Fatalf("delete persistence: %v", err)
	}
}

func TestValidationAndReplacementFailuresPreserveCredentials(t *testing.T) {
	s, dir, vault := openTestService(t, nil)
	m, err := s.Create(t.Context(), testInput("Development"))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(StorePath(dir))
	s.validate = func(context.Context, string, Credentials) (Identity, error) {
		return Identity{}, &ValidationError{"expired", "The AWS session has expired."}
	}
	if _, err = s.Update(t.Context(), m.ID, testInput("Replacement")); err == nil {
		t.Fatal("expected replacement failure")
	}
	after, _ := os.ReadFile(StorePath(dir))
	if string(before) != string(after) {
		t.Fatal("failed replacement changed disk")
	}
	tested, err := s.Test(t.Context(), m.ID)
	if err != nil || tested.Status != "expired" || tested.LastVerifiedAt != m.LastVerifiedAt || tested.Identity != m.Identity {
		t.Fatalf("test failure: %+v %v", tested, err)
	}
	s.validate = func(_ context.Context, _ string, c Credentials) (Identity, error) {
		if c.SessionToken != "example-session-token" {
			t.Error("credentials changed")
		}
		return testIdentity, nil
	}
	reopened, err := Open(dir, vault, s.validate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Test(t.Context(), m.ID); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityChangeRequiresMatchingAcknowledgement(t *testing.T) {
	s, _, _ := openTestService(t, nil)
	m, err := s.Create(t.Context(), testInput("Development"))
	if err != nil {
		t.Fatal(err)
	}
	other := Identity{"999999999999", "arn:aws:iam::999999999999:user/other"}
	s.validate = func(context.Context, string, Credentials) (Identity, error) { return other, nil }
	in := testInput("Development")
	_, err = s.Update(t.Context(), m.ID, in)
	var changed *IdentityChangedError
	if !errors.As(err, &changed) || changed.Identity != other || s.List()[0].Identity != testIdentity {
		t.Fatalf("identity conflict: %v", err)
	}
	in.AcceptedIdentity = &other
	updated, err := s.Update(t.Context(), m.ID, in)
	if err != nil || updated.Identity != other {
		t.Fatalf("acknowledged update: %+v %v", updated, err)
	}
}

func TestFailedWritesDoNotChangeLiveState(t *testing.T) {
	s, _, _ := openTestService(t, nil)
	m, err := s.Create(t.Context(), testInput("Development"))
	if err != nil {
		t.Fatal(err)
	}
	s.write = func([]byte) error { return errors.New("disk full") }
	if _, err = s.Create(t.Context(), testInput("Other")); err == nil {
		t.Fatal("create should fail")
	}
	if _, err = s.Update(t.Context(), m.ID, Input{Name: "Renamed", Region: m.Region}); err == nil {
		t.Fatal("update should fail")
	}
	if err = s.Delete(m.ID); err == nil {
		t.Fatal("delete should fail")
	}
	if list := s.List(); len(list) != 1 || list[0] != m {
		t.Fatalf("live state changed: %+v", list)
	}
}

func TestWrongKeyAndDisabledEncryptionPreserveStore(t *testing.T) {
	s, dir, _ := openTestService(t, nil)
	if _, err := s.Create(t.Context(), testInput("Development")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(StorePath(dir))
	other, err := secrets.LoadOrCreateVault(filepath.Join(t.TempDir(), "other.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(dir, other, nil); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = Open(dir, nil, nil); err == nil {
		t.Fatal("disabled encryption accepted")
	}
	after, _ := os.ReadFile(StorePath(dir))
	if string(before) != string(after) {
		t.Fatal("failed load changed disk")
	}
}

func TestConcurrentOperationsAreExcluded(t *testing.T) {
	s, _, _ := openTestService(t, nil)
	m, err := s.Create(t.Context(), testInput("Development"))
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	s.validate = func(context.Context, string, Credentials) (Identity, error) {
		close(started)
		<-release
		return testIdentity, nil
	}
	done := make(chan error, 1)
	go func() { _, err := s.Test(t.Context(), m.ID); done <- err }()
	<-started
	if err = s.Delete(m.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("delete during test: %v", err)
	}
	if _, err = s.Update(t.Context(), m.ID, Input{Name: "Rename", Region: m.Region}); !errors.Is(err, ErrBusy) {
		t.Fatalf("update during test: %v", err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Test(t.Context(), m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted record recreated")
	}
}

func TestInputRejectsUnsupportedRegionsAndCredentialVariants(t *testing.T) {
	for _, region := range []string{"cn-north-1", "us-gov-west-1", "us-iso-east-1", "http://localhost:4566", ""} {
		in := testInput("Test")
		in.Region = region
		if validateInput(in, true) == nil {
			t.Errorf("accepted region %q", region)
		}
	}
	in := testInput("Test")
	in.Credentials.Kind = "long-lived"
	if validateInput(in, true) == nil {
		t.Fatal("accepted long-lived credentials with a token")
	}
	in.Credentials.Kind = "temporary"
	in.Credentials.SessionToken = ""
	if validateInput(in, true) == nil {
		t.Fatal("accepted temporary credentials without a token")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestSTSIgnoresEnvironmentOverridesAndSignsExplicitCredentials(t *testing.T) {
	t.Setenv("AWS_ENDPOINT_URL", "http://localhost:4566")
	t.Setenv("AWS_ENDPOINT_URL_STS", "http://localhost:4566")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_PROFILE", "missing-profile")
	c := *testInput("Test").Credentials
	client := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "sts.eu-west-2.amazonaws.com" {
			t.Fatalf("wrong STS endpoint: %s", r.URL)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Credential="+c.AccessKeyID+"/") || r.Header.Get("X-Amz-Security-Token") != c.SessionToken {
			t.Fatal("wrong signing credentials")
		}
		body := `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><GetCallerIdentityResult><Account>123456789012</Account><Arn>arn:aws:iam::123456789012:user/developer</Arn><UserId>test</UserId></GetCallerIdentityResult></GetCallerIdentityResponse>`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	identity, err := validateSTS(t.Context(), "eu-west-2", c, client)
	if err != nil || identity != testIdentity {
		t.Fatalf("STS validation: %+v %v", identity, err)
	}
}

func TestValidationErrorsAreSanitised(t *testing.T) {
	for _, code := range []string{"ExpiredToken", "InvalidClientTokenId", "SignatureDoesNotMatch", "ThrottlingException"} {
		err := classifyValidationError(&smithy.GenericAPIError{Code: code, Message: "secret-key-and-signed-request"})
		if strings.Contains(err.Error(), "secret-key") {
			t.Fatal("upstream details leaked")
		}
	}
}
