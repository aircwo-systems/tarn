package cognito

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

const testPassword = "Passw0rd!"

func newTestService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	return NewService(cfg, NewIndex())
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("want %s, got %v", code, err)
	}
	if ce.Code != code {
		t.Fatalf("want %s, got %s: %s", code, ce.Code, ce.Message)
	}
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

type fixture struct {
	svc    *Service
	poolID string
	client UserPoolClientType
}

// newFixture creates a pool that auto-verifies email and a client allowing
// the password, SRP and refresh flows.
func newFixture(t *testing.T, mutate func(*CreateUserPoolInput, *CreateUserPoolClientInput)) *fixture {
	t.Helper()
	svc := newTestService(t)
	pin := &CreateUserPoolInput{PoolName: "app"}
	pin.AutoVerifiedAttributes = []string{"email"}
	cin := &CreateUserPoolClientInput{}
	cin.ClientName = "web"
	cin.ExplicitAuthFlows = []string{"ALLOW_USER_PASSWORD_AUTH", "ALLOW_ADMIN_USER_PASSWORD_AUTH", "ALLOW_USER_SRP_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"}
	if mutate != nil {
		mutate(pin, cin)
	}
	pool := must[*UserPoolOutput](t)(svc.CreateUserPool(pin)).UserPool
	cin.UserPoolId = pool.Id
	client := must[*UserPoolClientOutput](t)(svc.CreateUserPoolClient(cin)).UserPoolClient
	return &fixture{svc: svc, poolID: pool.Id, client: client}
}

func (f *fixture) code(t *testing.T, username, purpose string) string {
	t.Helper()
	codes := must[[]PendingCode](t)(f.svc.PendingCodes(f.poolID, username))
	for _, c := range codes {
		if c.Purpose == purpose {
			return c.Code
		}
	}
	t.Fatalf("no %s code for %s in %+v", purpose, username, codes)
	return ""
}

// signUpConfirmed signs a user up and confirms them with the logged code.
func (f *fixture) signUpConfirmed(t *testing.T, username, email string) string {
	t.Helper()
	out := must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{
		ClientId: f.client.ClientId, Username: username, Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: email}},
	}))
	must[struct{}](t)(f.svc.ConfirmSignUp(&ConfirmSignUpInput{
		ClientId: f.client.ClientId, Username: username, ConfirmationCode: f.code(t, username, purposeSignUp),
	}))
	return out.UserSub
}

func (f *fixture) passwordAuth(t *testing.T, username, password string) (*AuthOutput, error) {
	t.Helper()
	return f.svc.InitiateAuth(&InitiateAuthInput{
		ClientId: f.client.ClientId, AuthFlow: "USER_PASSWORD_AUTH",
		AuthParameters: map[string]string{"USERNAME": username, "PASSWORD": password},
	})
}

// verifyWithJWKS checks a token the way an app would: against the published
// JWKS, not the pool's private key.
func verifyWithJWKS(t *testing.T, svc *Service, poolID, token string) map[string]any {
	t.Helper()
	jwks := must[map[string][]JWK](t)(svc.JWKS(poolID))
	key := jwks["keys"][0]
	n, _ := base64.RawURLEncoding.DecodeString(key.N)
	e, _ := base64.RawURLEncoding.DecodeString(key.E)
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	parts := strings.Split(token, ".")
	var header map[string]string
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	_ = json.Unmarshal(hb, &header)
	if header["kid"] != key.Kid || header["alg"] != "RS256" {
		t.Fatalf("header %v does not match JWKS key %s", header, key.Kid)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("token does not verify against JWKS: %v", err)
	}
	claims, err := decodeClaims(token)
	if err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestCreateUserPoolDefaults(t *testing.T) {
	svc := newTestService(t)
	in := &CreateUserPoolInput{PoolName: "app", Schema: []schemaAttributeInput{
		{Name: "tenant", AttributeDataType: "String"},
		{Name: "email", AttributeDataType: "String", Required: ptr(true)},
	}}
	in.EmailVerificationMessage = ptr("Code {####}")
	pool := must[*UserPoolOutput](t)(svc.CreateUserPool(in)).UserPool

	if !IsPoolID(pool.Id) || !strings.HasPrefix(pool.Id, "us-east-1_") {
		t.Fatalf("pool ID %q", pool.Id)
	}
	if pool.Arn != "arn:aws:cognito-idp:us-east-1:000000000000:userpool/"+pool.Id {
		t.Fatalf("arn %q", pool.Arn)
	}
	pp := pool.Policies.PasswordPolicy
	if *pp.MinimumLength != 8 || !pp.RequireSymbols || pp.TemporaryPasswordValidityDays != 7 {
		t.Fatalf("password policy defaults %+v", pp)
	}
	if pool.MfaConfiguration != "OFF" || pool.DeletionProtection != "INACTIVE" || pool.UserPoolTier != "ESSENTIALS" {
		t.Fatalf("defaults: mfa=%s deletion=%s tier=%s", pool.MfaConfiguration, pool.DeletionProtection, pool.UserPoolTier)
	}
	if got := *pool.VerificationMessageTemplate.EmailMessage; got != "Code {####}" {
		t.Fatalf("template email message %q not mirrored", got)
	}
	if pool.VerificationMessageTemplate.DefaultEmailOption != "CONFIRM_WITH_CODE" {
		t.Fatal("default email option")
	}
	var names []string
	for _, a := range pool.SchemaAttributes {
		names = append(names, a.Name)
		if a.Name == "email" && !a.Required {
			t.Fatal("email should be required")
		}
		if a.Name == "email" && *a.StringAttributeConstraints.MaxLength != "2048" {
			t.Fatal("standard constraints should be kept")
		}
	}
	if names[0] != "sub" || names[len(names)-1] != "custom:tenant" {
		t.Fatalf("schema order %v", names)
	}
}

func TestUpdateUserPoolResetsOmittedSettings(t *testing.T) {
	svc := newTestService(t)
	in := &CreateUserPoolInput{PoolName: "app"}
	in.AutoVerifiedAttributes = []string{"email"}
	in.UserPoolTags = map[string]string{"env": "dev"}
	pool := must[*UserPoolOutput](t)(svc.CreateUserPool(in)).UserPool

	must[struct{}](t)(svc.UpdateUserPool(&UpdateUserPoolInput{UserPoolId: pool.Id}))
	got := must[*UserPoolOutput](t)(svc.DescribeUserPool(&PoolIDInput{UserPoolId: pool.Id})).UserPool
	if len(got.AutoVerifiedAttributes) != 0 {
		t.Fatalf("auto verified attributes kept: %v", got.AutoVerifiedAttributes)
	}
	if got.UserPoolTags["env"] != "dev" {
		t.Fatal("tags should survive an update that omits them")
	}
}

func TestDeletionProtection(t *testing.T) {
	svc := newTestService(t)
	in := &CreateUserPoolInput{PoolName: "app"}
	in.DeletionProtection = "ACTIVE"
	pool := must[*UserPoolOutput](t)(svc.CreateUserPool(in)).UserPool
	_, err := svc.DeleteUserPool(&PoolIDInput{UserPoolId: pool.Id})
	wantCode(t, err, "InvalidParameterException")
}

func TestDeleteUserPoolUnregistersIndex(t *testing.T) {
	f := newFixture(t, nil)
	if acct, ok := f.svc.index.AccountForClient(f.client.ClientId); !ok || acct != "000000000000" {
		t.Fatalf("client not indexed: %q %v", acct, ok)
	}
	must[struct{}](t)(f.svc.DeleteUserPool(&PoolIDInput{UserPoolId: f.poolID}))
	if _, ok := f.svc.index.AccountForClient(f.client.ClientId); ok {
		t.Fatal("client still indexed after pool delete")
	}
	_, err := f.svc.DescribeUserPool(&PoolIDInput{UserPoolId: f.poolID})
	wantCode(t, err, "ResourceNotFoundException")
}

func TestClientDefaults(t *testing.T) {
	svc := newTestService(t)
	pool := must[*UserPoolOutput](t)(svc.CreateUserPool(&CreateUserPoolInput{PoolName: "app"})).UserPool
	in := &CreateUserPoolClientInput{UserPoolId: pool.Id, GenerateSecret: true}
	in.ClientName = "server"
	c := must[*UserPoolClientOutput](t)(svc.CreateUserPoolClient(in)).UserPoolClient
	if len(c.ClientId) != 26 || len(c.ClientSecret) != 51 {
		t.Fatalf("id %q secret %q", c.ClientId, c.ClientSecret)
	}
	if c.RefreshTokenValidity != 30 || c.TokenValidityUnits != nil || !*c.EnableTokenRevocation {
		t.Fatalf("client defaults %+v", c)
	}
	if strings.Join(c.ExplicitAuthFlows, ",") != "ALLOW_REFRESH_TOKEN_AUTH,ALLOW_USER_SRP_AUTH,ALLOW_CUSTOM_AUTH" {
		t.Fatalf("default flows %v", c.ExplicitAuthFlows)
	}
}

func TestSignUpConfirmSignInRefreshSignOut(t *testing.T) {
	f := newFixture(t, nil)
	out := must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{
		ClientId: f.client.ClientId, Username: "alice", Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: "alice@example.com"}},
	}))
	if out.UserConfirmed || out.CodeDeliveryDetails == nil || out.CodeDeliveryDetails.Destination != "a***@e***.com" {
		t.Fatalf("sign up output %+v %+v", out, out.CodeDeliveryDetails)
	}

	_, err := f.passwordAuth(t, "alice", testPassword)
	wantCode(t, err, "UserNotConfirmedException")

	_, err = f.svc.ConfirmSignUp(&ConfirmSignUpInput{ClientId: f.client.ClientId, Username: "alice", ConfirmationCode: "000000"})
	wantCode(t, err, "CodeMismatchException")
	must[struct{}](t)(f.svc.ConfirmSignUp(&ConfirmSignUpInput{
		ClientId: f.client.ClientId, Username: "alice", ConfirmationCode: f.code(t, "alice", purposeSignUp),
	}))

	// Usernames are case-insensitive by default.
	auth := must[*AuthOutput](t)(f.passwordAuth(t, "ALICE", testPassword))
	res := auth.AuthenticationResult
	if res == nil || res.TokenType != "Bearer" || res.ExpiresIn != 3600 || res.RefreshToken == "" {
		t.Fatalf("auth result %+v", res)
	}

	id := verifyWithJWKS(t, f.svc, f.poolID, res.IdToken)
	if id["token_use"] != "id" || id["aud"] != f.client.ClientId || id["sub"] != out.UserSub ||
		id["email"] != "alice@example.com" || id["email_verified"] != true || id["cognito:username"] != "alice" {
		t.Fatalf("id claims %v", id)
	}
	if id["iss"] != "http://localhost:4566/"+f.poolID {
		t.Fatalf("issuer %v", id["iss"])
	}
	access := verifyWithJWKS(t, f.svc, f.poolID, res.AccessToken)
	if access["token_use"] != "access" || access["client_id"] != f.client.ClientId || access["scope"] != "aws.cognito.signin.user.admin" || access["username"] != "alice" {
		t.Fatalf("access claims %v", access)
	}

	user := must[*GetUserOutput](t)(f.svc.GetUser(&AccessTokenInput{AccessToken: res.AccessToken}))
	if user.Username != "alice" || user.UserAttributes[0].Name != "sub" {
		t.Fatalf("get user %+v", user)
	}

	refreshed := must[*AuthOutput](t)(f.svc.InitiateAuth(&InitiateAuthInput{
		ClientId: f.client.ClientId, AuthFlow: "REFRESH_TOKEN_AUTH",
		AuthParameters: map[string]string{"REFRESH_TOKEN": res.RefreshToken},
	})).AuthenticationResult
	if refreshed.RefreshToken != "" {
		t.Fatal("refresh should not rotate the refresh token")
	}
	newAccess := verifyWithJWKS(t, f.svc, f.poolID, refreshed.AccessToken)
	if newAccess["origin_jti"] != access["origin_jti"] {
		t.Fatal("refreshed token should keep origin_jti")
	}

	must[struct{}](t)(f.svc.GlobalSignOut(&AccessTokenInput{AccessToken: res.AccessToken}))
	_, err = f.svc.GetUser(&AccessTokenInput{AccessToken: refreshed.AccessToken})
	wantCode(t, err, "NotAuthorizedException")
	if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("want revoked, got %v", err)
	}
	_, err = f.svc.InitiateAuth(&InitiateAuthInput{
		ClientId: f.client.ClientId, AuthFlow: "REFRESH_TOKEN_AUTH",
		AuthParameters: map[string]string{"REFRESH_TOKEN": res.RefreshToken},
	})
	if err == nil || !strings.Contains(err.Error(), "Refresh Token has been revoked") {
		t.Fatalf("refresh after sign-out: %v", err)
	}

	// A new sign-in after sign-out works.
	again := must[*AuthOutput](t)(f.passwordAuth(t, "alice", testPassword))
	must[*GetUserOutput](t)(f.svc.GetUser(&AccessTokenInput{AccessToken: again.AuthenticationResult.AccessToken}))
}

func TestSignInErrors(t *testing.T) {
	f := newFixture(t, nil)
	f.signUpConfirmed(t, "bob", "bob@example.com")

	_, err := f.passwordAuth(t, "bob", "Wrong123!")
	wantCode(t, err, "NotAuthorizedException")
	_, err = f.passwordAuth(t, "nobody", testPassword)
	wantCode(t, err, "UserNotFoundException")

	_, err = f.svc.AdminDisableUser(&AdminUserInput{UserPoolId: f.poolID, Username: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.passwordAuth(t, "bob", testPassword)
	if err == nil || !strings.Contains(err.Error(), "User is disabled.") {
		t.Fatalf("disabled: %v", err)
	}

	_, err = f.svc.InitiateAuth(&InitiateAuthInput{ClientId: "missing", AuthFlow: "USER_PASSWORD_AUTH"})
	wantCode(t, err, "ResourceNotFoundException")
	_, err = f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "ADMIN_USER_PASSWORD_AUTH"})
	wantCode(t, err, "InvalidParameterException")
}

func TestPreventUserExistenceErrors(t *testing.T) {
	f := newFixture(t, func(_ *CreateUserPoolInput, c *CreateUserPoolClientInput) {
		c.PreventUserExistenceErrors = "ENABLED"
	})
	_, err := f.passwordAuth(t, "nobody", testPassword)
	if err == nil || err.Error() != "NotAuthorizedException: Incorrect username or password." {
		t.Fatalf("got %v", err)
	}
}

func TestFlowMustBeEnabled(t *testing.T) {
	f := newFixture(t, func(_ *CreateUserPoolInput, c *CreateUserPoolClientInput) {
		c.ExplicitAuthFlows = []string{"ALLOW_USER_SRP_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"}
	})
	_, err := f.passwordAuth(t, "alice", testPassword)
	if err == nil || err.Error() != "InvalidParameterException: USER_PASSWORD_AUTH flow not enabled for this client" {
		t.Fatalf("got %v", err)
	}
}

func TestSecretHash(t *testing.T) {
	f := newFixture(t, func(_ *CreateUserPoolInput, c *CreateUserPoolClientInput) { c.GenerateSecret = true })
	in := &SignUpInput{
		ClientId: f.client.ClientId, Username: "carol", Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: "carol@example.com"}},
	}
	_, err := f.svc.SignUp(in)
	if err == nil || !strings.Contains(err.Error(), "Unable to verify secret hash") {
		t.Fatalf("missing secret hash: %v", err)
	}
	in.SecretHash = secretHash(f.client.ClientSecret, "carol", f.client.ClientId)
	must[*SignUpOutput](t)(f.svc.SignUp(in))
	must[struct{}](t)(f.svc.AdminConfirmSignUp(&AdminConfirmSignUpInput{UserPoolId: f.poolID, Username: "carol"}))

	_, err = f.passwordAuth(t, "carol", testPassword)
	wantCode(t, err, "NotAuthorizedException")
	must[*AuthOutput](t)(f.svc.InitiateAuth(&InitiateAuthInput{
		ClientId: f.client.ClientId, AuthFlow: "USER_PASSWORD_AUTH",
		AuthParameters: map[string]string{"USERNAME": "carol", "PASSWORD": testPassword, "SECRET_HASH": in.SecretHash},
	}))
}

func TestSignUpValidation(t *testing.T) {
	f := newFixture(t, func(p *CreateUserPoolInput, _ *CreateUserPoolClientInput) {
		p.Schema = []schemaAttributeInput{{Name: "email", AttributeDataType: "String", Required: ptr(true)}}
	})
	base := func() *SignUpInput {
		return &SignUpInput{
			ClientId: f.client.ClientId, Username: "dave", Password: testPassword,
			UserAttributes: []AttributeType{{Name: "email", Value: "dave@example.com"}},
		}
	}
	in := base()
	in.Password = "short"
	_, err := f.svc.SignUp(in)
	if err == nil || err.Error() != "InvalidPasswordException: Password did not conform with policy: Password not long enough" {
		t.Fatalf("got %v", err)
	}
	in = base()
	in.Password = "nouppercase1!"
	_, err = f.svc.SignUp(in)
	wantCode(t, err, "InvalidPasswordException")

	in = base()
	in.UserAttributes = nil
	_, err = f.svc.SignUp(in)
	if err == nil || !strings.Contains(err.Error(), "email: The attribute email is required") {
		t.Fatalf("got %v", err)
	}
	in = base()
	in.UserAttributes = append(in.UserAttributes, AttributeType{Name: "custom:nope", Value: "x"})
	_, err = f.svc.SignUp(in)
	wantCode(t, err, "InvalidParameterException")

	must[*SignUpOutput](t)(f.svc.SignUp(base()))
	_, err = f.svc.SignUp(base())
	wantCode(t, err, "UsernameExistsException")
}

func TestCodeAttemptLimit(t *testing.T) {
	f := newFixture(t, nil)
	must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{
		ClientId: f.client.ClientId, Username: "erin", Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: "erin@example.com"}},
	}))
	for range maxCodeAttempts {
		_, err := f.svc.ConfirmSignUp(&ConfirmSignUpInput{ClientId: f.client.ClientId, Username: "erin", ConfirmationCode: "badbad"})
		wantCode(t, err, "CodeMismatchException")
	}
	_, err := f.svc.ConfirmSignUp(&ConfirmSignUpInput{ClientId: f.client.ClientId, Username: "erin", ConfirmationCode: "badbad"})
	wantCode(t, err, "LimitExceededException")
}

func TestFixedCode(t *testing.T) {
	f := newFixture(t, nil)
	f.svc.cfg.CognitoFixedCode = "123456"
	must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{
		ClientId: f.client.ClientId, Username: "fred", Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: "fred@example.com"}},
	}))
	must[struct{}](t)(f.svc.ConfirmSignUp(&ConfirmSignUpInput{ClientId: f.client.ClientId, Username: "fred", ConfirmationCode: "123456"}))
}

func TestAdminCreateUserNewPasswordRequired(t *testing.T) {
	f := newFixture(t, nil)
	created := must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{
		UserPoolId: f.poolID, Username: "gina",
		UserAttributes: []AttributeType{{Name: "email", Value: "gina@example.com"}, {Name: "email_verified", Value: "true"}},
	})).User
	if created.UserStatus != StatusForceChangePassword {
		t.Fatalf("status %s", created.UserStatus)
	}
	temp := f.code(t, "gina", purposeInvite)

	auth := must[*AuthOutput](t)(f.passwordAuth(t, "gina", temp))
	if auth.ChallengeName != "NEW_PASSWORD_REQUIRED" || auth.Session == "" || auth.ChallengeParameters["USER_ID_FOR_SRP"] != "gina" {
		t.Fatalf("challenge %+v", auth)
	}
	var attrs map[string]string
	if err := json.Unmarshal([]byte(auth.ChallengeParameters["userAttributes"]), &attrs); err != nil || attrs["email"] != "gina@example.com" {
		t.Fatalf("userAttributes %q", auth.ChallengeParameters["userAttributes"])
	}

	_, err := f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{
		ClientId: f.client.ClientId, ChallengeName: "NEW_PASSWORD_REQUIRED", Session: "bogus",
		ChallengeResponses: map[string]string{"USERNAME": "gina", "NEW_PASSWORD": "N3wPassword!"},
	})
	wantCode(t, err, "NotAuthorizedException")

	done := must[*AuthOutput](t)(f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{
		ClientId: f.client.ClientId, ChallengeName: "NEW_PASSWORD_REQUIRED", Session: auth.Session,
		ChallengeResponses: map[string]string{"USERNAME": "gina", "NEW_PASSWORD": "N3wPassword!", "userAttributes.name": "Gina"},
	}))
	if done.AuthenticationResult == nil {
		t.Fatalf("no tokens: %+v", done)
	}
	got := must[*AdminGetUserOutput](t)(f.svc.AdminGetUser(&AdminUserInput{UserPoolId: f.poolID, Username: "gina"}))
	if got.UserStatus != StatusConfirmed {
		t.Fatalf("status %s", got.UserStatus)
	}
	must[*AuthOutput](t)(f.passwordAuth(t, "gina", "N3wPassword!"))
	if _, err := f.passwordAuth(t, "gina", temp); err == nil {
		t.Fatal("temporary password still works")
	}
}

func TestTemporaryPasswordExpires(t *testing.T) {
	f := newFixture(t, nil)
	must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{
		UserPoolId: f.poolID, Username: "hal", TemporaryPassword: "Temp0rary!", MessageAction: "SUPPRESS",
	}))
	if codes := must[[]PendingCode](t)(f.svc.PendingCodes(f.poolID, "hal")); len(codes) != 0 {
		t.Fatalf("SUPPRESS should not record the invite: %+v", codes)
	}
	f.svc.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	_, err := f.passwordAuth(t, "hal", "Temp0rary!")
	if err == nil || !strings.Contains(err.Error(), "Temporary password has expired") {
		t.Fatalf("got %v", err)
	}
}

func TestForgotPassword(t *testing.T) {
	f := newFixture(t, nil)
	f.signUpConfirmed(t, "ivy", "ivy@example.com")
	out := must[*CodeDeliveryOutput](t)(f.svc.ForgotPassword(&ClientUserInput{ClientId: f.client.ClientId, Username: "ivy"}))
	if out.CodeDeliveryDetails.AttributeName != "email" {
		t.Fatalf("delivery %+v", out.CodeDeliveryDetails)
	}
	must[struct{}](t)(f.svc.ConfirmForgotPassword(&ConfirmForgotPasswordInput{
		ClientId: f.client.ClientId, Username: "ivy", ConfirmationCode: f.code(t, "ivy", purposeReset), Password: "Rec0vered!",
	}))
	must[*AuthOutput](t)(f.passwordAuth(t, "ivy", "Rec0vered!"))

	// An unverified user has nowhere to receive the code.
	must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{UserPoolId: f.poolID, Username: "jo", MessageAction: "SUPPRESS"}))
	_, err := f.svc.ForgotPassword(&ClientUserInput{ClientId: f.client.ClientId, Username: "jo"})
	wantCode(t, err, "InvalidParameterException")
}

func TestAdminResetUserPassword(t *testing.T) {
	f := newFixture(t, nil)
	f.signUpConfirmed(t, "kim", "kim@example.com")
	must[struct{}](t)(f.svc.AdminResetUserPassword(&AdminResetUserPasswordInput{UserPoolId: f.poolID, Username: "kim"}))
	_, err := f.passwordAuth(t, "kim", testPassword)
	wantCode(t, err, "PasswordResetRequiredException")
	must[struct{}](t)(f.svc.ConfirmForgotPassword(&ConfirmForgotPasswordInput{
		ClientId: f.client.ClientId, Username: "kim", ConfirmationCode: f.code(t, "kim", purposeReset), Password: "Rec0vered!",
	}))
	must[*AuthOutput](t)(f.passwordAuth(t, "kim", "Rec0vered!"))
}

func TestEmailAsUsername(t *testing.T) {
	f := newFixture(t, func(p *CreateUserPoolInput, _ *CreateUserPoolClientInput) {
		p.UsernameAttributes = []string{"email"}
	})
	_, err := f.svc.SignUp(&SignUpInput{ClientId: f.client.ClientId, Username: "notanemail", Password: testPassword})
	wantCode(t, err, "InvalidParameterException")

	out := must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{ClientId: f.client.ClientId, Username: "Lee@Example.com", Password: testPassword}))
	got := must[*AdminGetUserOutput](t)(f.svc.AdminGetUser(&AdminUserInput{UserPoolId: f.poolID, Username: "lee@example.com"}))
	if got.Username != out.UserSub {
		t.Fatalf("username should be the sub, got %s", got.Username)
	}
	_, err = f.svc.SignUp(&SignUpInput{ClientId: f.client.ClientId, Username: "lee@example.com", Password: testPassword})
	wantCode(t, err, "UsernameExistsException")

	must[struct{}](t)(f.svc.ConfirmSignUp(&ConfirmSignUpInput{
		ClientId: f.client.ClientId, Username: "lee@example.com", ConfirmationCode: f.code(t, out.UserSub, purposeSignUp),
	}))
	res := must[*AuthOutput](t)(f.passwordAuth(t, "lee@example.com", testPassword)).AuthenticationResult
	claims := verifyWithJWKS(t, f.svc, f.poolID, res.IdToken)
	if claims["cognito:username"] != out.UserSub {
		t.Fatalf("cognito:username %v", claims["cognito:username"])
	}
}

func TestGroupsInTokens(t *testing.T) {
	f := newFixture(t, nil)
	f.signUpConfirmed(t, "mo", "mo@example.com")
	for name, prec := range map[string]int32{"admins": 1, "readers": 5} {
		must[*GroupOutput](t)(f.svc.CreateGroup(&GroupInput{UserPoolId: f.poolID, GroupName: name, Precedence: ptr(prec)}))
		must[struct{}](t)(f.svc.AdminAddUserToGroup(&UserGroupInput{UserPoolId: f.poolID, Username: "mo", GroupName: name}))
	}
	_, err := f.svc.CreateGroup(&GroupInput{UserPoolId: f.poolID, GroupName: "admins"})
	wantCode(t, err, "GroupExistsException")

	res := must[*AuthOutput](t)(f.passwordAuth(t, "mo", testPassword)).AuthenticationResult
	claims := verifyWithJWKS(t, f.svc, f.poolID, res.AccessToken)
	groups, _ := claims["cognito:groups"].([]any)
	if len(groups) != 2 || groups[0] != "admins" || groups[1] != "readers" {
		t.Fatalf("groups %v", claims["cognito:groups"])
	}
	inGroup := must[*ListUsersInGroupOutput](t)(f.svc.ListUsersInGroup(&ListUsersInGroupInput{UserPoolId: f.poolID, GroupName: "readers"}))
	if len(inGroup.Users) != 1 {
		t.Fatalf("users in group %+v", inGroup.Users)
	}
	must[struct{}](t)(f.svc.DeleteGroup(&GroupInput{UserPoolId: f.poolID, GroupName: "readers"}))
	forUser := must[*ListGroupsOutput](t)(f.svc.AdminListGroupsForUser(&ListGroupsInput{UserPoolId: f.poolID, Username: "mo"}))
	if len(forUser.Groups) != 1 || forUser.Groups[0].GroupName != "admins" {
		t.Fatalf("groups after delete %+v", forUser.Groups)
	}
}

func TestListUsersFilterAndPagination(t *testing.T) {
	f := newFixture(t, nil)
	for _, name := range []string{"ann", "andy", "bea"} {
		must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{
			UserPoolId: f.poolID, Username: name, MessageAction: "SUPPRESS",
			UserAttributes: []AttributeType{{Name: "email", Value: name + "@example.com"}},
		}))
	}
	got := must[*ListUsersOutput](t)(f.svc.ListUsers(&ListUsersInput{UserPoolId: f.poolID, Filter: `username ^= "an"`}))
	if len(got.Users) != 2 {
		t.Fatalf("prefix filter %+v", got.Users)
	}
	got = must[*ListUsersOutput](t)(f.svc.ListUsers(&ListUsersInput{UserPoolId: f.poolID, Filter: `email = "bea@example.com"`}))
	if len(got.Users) != 1 || got.Users[0].Username != "bea" {
		t.Fatalf("equals filter %+v", got.Users)
	}
	got = must[*ListUsersOutput](t)(f.svc.ListUsers(&ListUsersInput{UserPoolId: f.poolID, Filter: `cognito:user_status = "FORCE_CHANGE_PASSWORD"`}))
	if len(got.Users) != 3 {
		t.Fatalf("status filter %d", len(got.Users))
	}
	_, err := f.svc.ListUsers(&ListUsersInput{UserPoolId: f.poolID, Filter: `nickname = "x"`})
	wantCode(t, err, "InvalidParameterException")

	page := must[*ListUsersOutput](t)(f.svc.ListUsers(&ListUsersInput{UserPoolId: f.poolID, Limit: 2}))
	if len(page.Users) != 2 || page.PaginationToken == "" {
		t.Fatalf("page 1 %+v", page)
	}
	page = must[*ListUsersOutput](t)(f.svc.ListUsers(&ListUsersInput{UserPoolId: f.poolID, Limit: 2, PaginationToken: page.PaginationToken}))
	if len(page.Users) != 1 || page.PaginationToken != "" {
		t.Fatalf("page 2 %+v", page)
	}
}

func TestRevokeToken(t *testing.T) {
	f := newFixture(t, nil)
	f.signUpConfirmed(t, "nia", "nia@example.com")
	first := must[*AuthOutput](t)(f.passwordAuth(t, "nia", testPassword)).AuthenticationResult
	second := must[*AuthOutput](t)(f.passwordAuth(t, "nia", testPassword)).AuthenticationResult

	must[struct{}](t)(f.svc.RevokeToken(&RevokeTokenInput{ClientId: f.client.ClientId, Token: first.RefreshToken}))
	_, err := f.svc.GetUser(&AccessTokenInput{AccessToken: first.AccessToken})
	wantCode(t, err, "NotAuthorizedException")
	// Only the revoked session is affected.
	must[*GetUserOutput](t)(f.svc.GetUser(&AccessTokenInput{AccessToken: second.AccessToken}))
}

func TestTokenTTLOverrideAndExpiry(t *testing.T) {
	f := newFixture(t, nil)
	f.svc.cfg.CognitoTokenTTL = time.Minute
	f.signUpConfirmed(t, "oli", "oli@example.com")
	res := must[*AuthOutput](t)(f.passwordAuth(t, "oli", testPassword)).AuthenticationResult
	if res.ExpiresIn != 60 {
		t.Fatalf("ExpiresIn %d", res.ExpiresIn)
	}
	f.svc.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	_, err := f.svc.GetUser(&AccessTokenInput{AccessToken: res.AccessToken})
	if err == nil || !strings.Contains(err.Error(), "Access Token has expired") {
		t.Fatalf("got %v", err)
	}
	if _, err := f.svc.VerifyToken(res.IdToken); err == nil {
		t.Fatal("VerifyToken accepted an expired token")
	}
}

func TestUpdateUserAttributesReverifiesEmail(t *testing.T) {
	f := newFixture(t, nil)
	f.signUpConfirmed(t, "pat", "pat@example.com")
	res := must[*AuthOutput](t)(f.passwordAuth(t, "pat", testPassword)).AuthenticationResult
	out := must[*UpdateUserAttributesOutput](t)(f.svc.UpdateUserAttributes(&UpdateUserAttributesInput{
		AccessToken: res.AccessToken, UserAttributes: []AttributeType{{Name: "email", Value: "pat@new.example.com"}},
	}))
	if len(out.CodeDeliveryDetailsList) != 1 {
		t.Fatalf("delivery %+v", out)
	}
	user := must[*AdminGetUserOutput](t)(f.svc.AdminGetUser(&AdminUserInput{UserPoolId: f.poolID, Username: "pat"}))
	for _, a := range user.UserAttributes {
		if a.Name == "email_verified" && a.Value != "false" {
			t.Fatal("email should be unverified after change")
		}
	}
	must[struct{}](t)(f.svc.VerifyUserAttribute(&AttributeVerificationInput{
		AccessToken: res.AccessToken, AttributeName: "email", Code: f.code(t, "pat", "verify:email"),
	}))
}

func TestChangePasswordAndDeleteUser(t *testing.T) {
	f := newFixture(t, nil)
	f.signUpConfirmed(t, "quin", "quin@example.com")
	res := must[*AuthOutput](t)(f.passwordAuth(t, "quin", testPassword)).AuthenticationResult
	_, err := f.svc.ChangePassword(&ChangePasswordInput{AccessToken: res.AccessToken, PreviousPassword: "nope", ProposedPassword: "Chang3d!!"})
	wantCode(t, err, "NotAuthorizedException")
	must[struct{}](t)(f.svc.ChangePassword(&ChangePasswordInput{AccessToken: res.AccessToken, PreviousPassword: testPassword, ProposedPassword: "Chang3d!!"}))
	must[*AuthOutput](t)(f.passwordAuth(t, "quin", "Chang3d!!"))

	must[struct{}](t)(f.svc.DeleteUser(&AccessTokenInput{AccessToken: res.AccessToken}))
	_, err = f.svc.AdminGetUser(&AdminUserInput{UserPoolId: f.poolID, Username: "quin"})
	wantCode(t, err, "UserNotFoundException")
}

func TestIssuerModes(t *testing.T) {
	svc := newTestService(t)
	if got := svc.Issuer("us-east-1_abc"); got != "http://localhost:4566/us-east-1_abc" {
		t.Fatalf("local %q", got)
	}
	svc.cfg.CognitoIssuer = "aws"
	if got := svc.Issuer("us-east-1_abc"); got != "https://cognito-idp.us-east-1.amazonaws.com/us-east-1_abc" {
		t.Fatalf("aws %q", got)
	}
	svc.cfg.CognitoIssuer = "http://tarn.test:4566/"
	if got := svc.Issuer("us-east-1_abc"); got != "http://tarn.test:4566/us-east-1_abc" {
		t.Fatalf("explicit %q", got)
	}
	if PoolIDFromIssuer("https://cognito-idp.us-east-1.amazonaws.com/us-east-1_abc") != "us-east-1_abc" {
		t.Fatal("PoolIDFromIssuer")
	}
}

func TestPersistenceKeepsTokensValid(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	idx := NewIndex()
	svc := NewService(cfg, idx)
	if err := svc.Init(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{svc: svc}
	pin := &CreateUserPoolInput{PoolName: "app"}
	pin.AutoVerifiedAttributes = []string{"email"}
	f.poolID = must[*UserPoolOutput](t)(svc.CreateUserPool(pin)).UserPool.Id
	cin := &CreateUserPoolClientInput{UserPoolId: f.poolID}
	cin.ClientName = "web"
	cin.ExplicitAuthFlows = []string{"ALLOW_USER_PASSWORD_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"}
	f.client = must[*UserPoolClientOutput](t)(svc.CreateUserPoolClient(cin)).UserPoolClient
	f.signUpConfirmed(t, "rae", "rae@example.com")
	res := must[*AuthOutput](t)(f.passwordAuth(t, "rae", testPassword)).AuthenticationResult
	svc.Close()
	if _, ok := idx.AccountForPool(f.poolID); ok {
		t.Fatal("Close should drop the account from the index")
	}

	restarted := NewService(cfg, idx)
	if err := restarted.Init(); err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if acct, ok := idx.AccountForClient(f.client.ClientId); !ok || acct != cfg.AccountID {
		t.Fatal("restored client not indexed")
	}
	verifyWithJWKS(t, restarted, f.poolID, res.IdToken)
	must[*GetUserOutput](t)(restarted.GetUser(&AccessTokenInput{AccessToken: res.AccessToken}))
	must[*AuthOutput](t)(restarted.InitiateAuth(&InitiateAuthInput{
		ClientId: f.client.ClientId, AuthFlow: "REFRESH_TOKEN_AUTH",
		AuthParameters: map[string]string{"REFRESH_TOKEN": res.RefreshToken},
	}))
	must[*AuthOutput](t)((&fixture{svc: restarted, poolID: f.poolID, client: f.client}).passwordAuth(t, "rae", testPassword))
}

type fakeVault struct{}

func (fakeVault) Seal(s string) (string, error) {
	return "sealed:" + base64.StdEncoding.EncodeToString([]byte(s)), nil
}

func (fakeVault) Unseal(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "sealed:"))
	return string(b), err
}

func TestSigningKeySealedAtRest(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	svc := NewService(cfg, nil)
	svc.SetVault(fakeVault{})
	if err := svc.Init(); err != nil {
		t.Fatal(err)
	}
	id := must[*UserPoolOutput](t)(svc.CreateUserPool(&CreateUserPoolInput{PoolName: "app"})).UserPool.Id
	svc.Close()
	raw, err := os.ReadFile(cfg.CognitoStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE KEY") {
		t.Fatal("signing key written in plaintext")
	}
	restarted := NewService(cfg, nil)
	restarted.SetVault(fakeVault{})
	if err := restarted.Init(); err != nil {
		t.Fatal(err)
	}
	must[map[string][]JWK](t)(restarted.JWKS(id))
}

// --- SRP ---

// srpClient is the client half of Cognito SRP, transcribed from
// amazon-cognito-identity-js's AuthenticationHelper and CognitoUser.
type srpClient struct {
	a, A *big.Int
}

func newSRPClient() *srpClient {
	ab := make([]byte, 128)
	_, _ = rand.Read(ab)
	a := new(big.Int).Mod(new(big.Int).SetBytes(ab), srpN)
	return &srpClient{a: a, A: new(big.Int).Exp(srpG, a, srpN)}
}

func (c *srpClient) signature(poolID, userID, password string, params map[string]string, timestamp string) string {
	B, _ := new(big.Int).SetString(params["SRP_B"], 16)
	salt, _ := new(big.Int).SetString(params["SALT"], 16)
	u := new(big.Int).SetBytes(hexHash(padHex(c.A) + padHex(B)))
	x := srpX(poolID, userID, password, salt)
	// S = (B - k * g^x) ^ (a + u * x) mod N
	gx := new(big.Int).Exp(srpG, x, srpN)
	base := new(big.Int).Sub(B, new(big.Int).Mul(srpK, gx))
	base.Mod(base, srpN)
	exp := new(big.Int).Add(c.a, new(big.Int).Mul(u, x))
	S := new(big.Int).Exp(base, exp, srpN)
	key, _ := srpKey(S, u)
	block, _ := base64.StdEncoding.DecodeString(params["SECRET_BLOCK"])
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(poolName(poolID)))
	mac.Write([]byte(userID))
	mac.Write(block)
	mac.Write([]byte(timestamp))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (f *fixture) srpSignIn(t *testing.T, username, password string) (*AuthOutput, error) {
	t.Helper()
	c := newSRPClient()
	ch, err := f.svc.InitiateAuth(&InitiateAuthInput{
		ClientId: f.client.ClientId, AuthFlow: "USER_SRP_AUTH",
		AuthParameters: map[string]string{"USERNAME": username, "SRP_A": c.A.Text(16)},
	})
	if err != nil {
		return nil, err
	}
	if ch.ChallengeName != "PASSWORD_VERIFIER" {
		t.Fatalf("challenge %+v", ch)
	}
	userID := ch.ChallengeParameters["USER_ID_FOR_SRP"]
	ts := "Tue Oct 6 09:00:00 UTC 2026"
	return f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{
		ClientId: f.client.ClientId, ChallengeName: "PASSWORD_VERIFIER", Session: ch.Session,
		ChallengeResponses: map[string]string{
			"USERNAME":                    userID,
			"TIMESTAMP":                   ts,
			"PASSWORD_CLAIM_SECRET_BLOCK": ch.ChallengeParameters["SECRET_BLOCK"],
			"PASSWORD_CLAIM_SIGNATURE":    c.signature(f.poolID, userID, password, ch.ChallengeParameters, ts),
		},
	})
}

func TestSRPSignIn(t *testing.T) {
	f := newFixture(t, nil)
	f.signUpConfirmed(t, "sam", "sam@example.com")

	out := must[*AuthOutput](t)(f.srpSignIn(t, "Sam", testPassword))
	if out.AuthenticationResult == nil {
		t.Fatalf("no tokens: %+v", out)
	}
	verifyWithJWKS(t, f.svc, f.poolID, out.AuthenticationResult.AccessToken)

	_, err := f.srpSignIn(t, "sam", "Wrong123!")
	if err == nil || err.Error() != "NotAuthorizedException: Incorrect username or password." {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestSRPNewPasswordRequired(t *testing.T) {
	f := newFixture(t, nil)
	must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{
		UserPoolId: f.poolID, Username: "tia", TemporaryPassword: "Temp0rary!", MessageAction: "SUPPRESS",
	}))
	out := must[*AuthOutput](t)(f.srpSignIn(t, "tia", "Temp0rary!"))
	if out.ChallengeName != "NEW_PASSWORD_REQUIRED" {
		t.Fatalf("want NEW_PASSWORD_REQUIRED, got %+v", out)
	}
}

// TestSRPKnownVector pins the shared constants against values computed
// independently from the amazon-cognito-identity-js definitions.
func TestSRPKnownVector(t *testing.T) {
	if got := hex.EncodeToString(srpK.Bytes()); got != "538282c4354742d7cbbde2359fcf67f9f5b3a6b08791e5011b43b8a5b66d9ee6" {
		t.Fatalf("k = %s", got)
	}
	if padHex(big.NewInt(200)) != "00c8" || padHex(big.NewInt(20)) != "14" {
		t.Fatal("padHex")
	}
}
