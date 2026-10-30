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

	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
)

func testProfileFiles(t *testing.T, config, credentials string) string {
	t.Helper()
	dir := t.TempDir()
	configPath, credentialPath := filepath.Join(dir, "config"), filepath.Join(dir, "credentials")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialPath, []byte(credentials), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialPath)
	return credentialPath
}

func TestProfileOnlyPersistenceAndSourceChanges(t *testing.T) {
	dir := t.TempDir()
	validate := func(_ context.Context, _ string, c Credentials) (Identity, error) {
		if c.Kind != "profile" || c.Profile != "company-dev" || c.SecretAccessKey != "" {
			t.Errorf("wrong profile reference: %s", c.Kind)
		}
		return testIdentity, nil
	}
	s, err := Open(dir, nil, validate)
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Name: "Company", Region: "eu-west-2", Credentials: &Credentials{Kind: "profile", Profile: "company-dev"}}
	m, err := s.Create(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if m.Profile != "company-dev" || m.Kind != "profile" || m.AccessKeySuffix != "" {
		t.Fatalf("profile metadata: %+v", m)
	}
	if s.records[m.ID].Credentials != "" {
		t.Fatal("profile reference has a credential payload")
	}
	if needed, err := RequiresVault(dir); err != nil || needed {
		t.Fatalf("profile requires vault: %v", err)
	}
	reopened, err := Open(dir, nil, validate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Test(t.Context(), m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Update(t.Context(), m.ID, testInput("Company")); err == nil {
		t.Fatal("manual source accepted without encryption")
	}
	if reopened.List()[0].Kind != "profile" {
		t.Fatal("failed replacement changed source")
	}
	manual, _, _ := openTestService(t, nil)
	old, err := manual.Create(t.Context(), testInput("Manual"))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manual.Update(t.Context(), old.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Kind != "profile" || manual.records[old.ID].Credentials != "" {
		t.Fatal("old manual credential payload retained after source change")
	}
	updated, err = manual.Update(t.Context(), old.ID, testInput("Manual again"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profile != "" || updated.Kind != "temporary" {
		t.Fatal("profile retained after manual source change")
	}
}

func TestProfileKeysReloadAfterRotationAndIgnoreAmbientCredentials(t *testing.T) {
	file := testProfileFiles(t, "[profile company-dev]\nregion=us-west-1\nendpoint_url=http://localhost:4566\n", "[company-dev]\naws_access_key_id=AKIAFIRST1234567890\naws_secret_access_key=first-secret\n")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_ENDPOINT_URL", "http://localhost:4566")
	t.Setenv("AWS_PROFILE", "wrong-profile")
	expectedKey := "AKIAFIRST1234567890"
	client := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sts.eu-west-2.amazonaws.com" || r.URL.Scheme != "https" {
			t.Fatal("profile endpoint override used")
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Credential="+expectedKey+"/") {
			t.Fatal("wrong profile or stale credentials used")
		}
		body := `<GetCallerIdentityResponse><GetCallerIdentityResult><Account>123456789012</Account><Arn>arn:aws:iam::123456789012:user/developer</Arn></GetCallerIdentityResult></GetCallerIdentityResponse>`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	for _, key := range []string{"AKIAFIRST1234567890", "AKIASECOND1234567890"} {
		expectedKey = key
		if err := os.WriteFile(file, []byte("[company-dev]\naws_access_key_id="+key+"\naws_secret_access_key=rotated-secret\n"), 0600); err != nil {
			t.Fatal(err)
		}
		identity, err := validateSTS(t.Context(), "eu-west-2", Credentials{Kind: "profile", Profile: "company-dev"}, client)
		if err != nil || identity != testIdentity {
			t.Fatalf("profile validation: %+v %v", identity, err)
		}
	}
}

func TestProfileNamesExposeOnlyProfileNames(t *testing.T) {
	testProfileFiles(t, "[default]\n[profile company-dev]\n[sso-session company]\n[services custom]\n", "[company-dev]\naws_secret_access_key=must-not-leak\n[legacy]\n")
	names, err := ProfileNames()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(names)
	if string(data) != `["company-dev","default","legacy"]` {
		t.Fatalf("profile names: %s", data)
	}
}

func TestEmptyOrMissingProfilesNeverUseAmbientCredentials(t *testing.T) {
	testProfileFiles(t, "[profile empty]\nregion=eu-west-2\n", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	client := roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network call"); return nil, nil })
	for _, profile := range []string{"empty", "missing"} {
		if _, err := validateSTS(t.Context(), "eu-west-2", Credentials{Kind: "profile", Profile: profile}, client); err == nil {
			t.Fatal("invalid profile accepted")
		}
	}
}

func TestSSOLoginGuidanceIsSafeAndDistinctFromNetworkErrors(t *testing.T) {
	for _, err := range []error{&ssocreds.InvalidTokenError{}, os.ErrNotExist, &smithy.GenericAPIError{Code: "InvalidGrantException", Message: "private token"}} {
		result := profileValidationError(err, "company'dev", true)
		if result.Status != "needs-login" || !strings.Contains(result.Message, `aws sso login --profile 'company'\''dev'`) {
			t.Fatalf("login guidance: %+v", result)
		}
		if strings.Contains(result.Message, "private token") {
			t.Fatal("upstream token leaked")
		}
	}
	if result := profileValidationError(errors.New("network failure containing secrets"), "company-dev", true); result.Status != "unavailable" || strings.Contains(result.Message, "secrets") {
		t.Fatalf("network error: %+v", result)
	}
}

func TestSSOProfileWithMissingSessionRequestsLoginWithoutNetwork(t *testing.T) {
	session := "tarn-test-" + uuid.NewString()
	testProfileFiles(t, "[profile company-dev]\nsso_session="+session+"\nsso_account_id=123456789012\nsso_role_name=Developer\n[sso-session "+session+"]\nsso_region=eu-west-2\nsso_start_url=https://tarn-tests.awsapps.com/start\n", "")
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing SSO session triggered a network call")
		return nil, nil
	})
	_, err := validateSTS(t.Context(), "eu-west-2", Credentials{Kind: "profile", Profile: "company-dev"}, client)
	var login *ValidationError
	if !errors.As(err, &login) || login.Status != "needs-login" || !strings.Contains(login.Message, "aws sso login --profile 'company-dev'") {
		t.Fatalf("missing SSO session: %v", err)
	}
}

func TestProfileRenewalAcceptsNewSessionForSameRoleOnly(t *testing.T) {
	identity := Identity{AccountID: "123456789012", ARN: "arn:aws:sts::123456789012:assumed-role/Developer/session-one"}
	s, err := Open(t.TempDir(), nil, func(context.Context, string, Credentials) (Identity, error) { return identity, nil })
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Create(t.Context(), Input{Name: "Company", Region: "eu-west-2", Credentials: &Credentials{Kind: "profile", Profile: "company-dev"}})
	if err != nil {
		t.Fatal(err)
	}
	identity.ARN = "arn:aws:sts::123456789012:assumed-role/Developer/session-two"
	updated, err := s.Test(t.Context(), m.ID)
	if err != nil || updated.Status != "verified" || updated.Identity != identity {
		t.Fatalf("session renewal: %+v %v", updated, err)
	}
	identity.ARN = "arn:aws:sts::123456789012:assumed-role/Administrator/session-three"
	updated, err = s.Test(t.Context(), m.ID)
	if err != nil || updated.Status != "rejected" || updated.Identity.ARN == identity.ARN {
		t.Fatalf("changed role accepted: %+v %v", updated, err)
	}
	identity = Identity{AccountID: "999999999999", ARN: "arn:aws:sts::999999999999:assumed-role/Developer/session-four"}
	updated, err = s.Test(t.Context(), m.ID)
	if err != nil || updated.Status != "rejected" || updated.Identity.AccountID == identity.AccountID {
		t.Fatalf("changed account accepted: %+v %v", updated, err)
	}
}
