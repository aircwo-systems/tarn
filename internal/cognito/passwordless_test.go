package cognito

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func otpFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixture(t, func(p *CreateUserPoolInput, c *CreateUserPoolClientInput) {
		p.Policies = &UserPoolPolicyType{SignInPolicy: &SignInPolicyType{AllowedFirstAuthFactors: []string{"PASSWORD", "EMAIL_OTP", "SMS_OTP"}}}
		c.ExplicitAuthFlows = []string{"ALLOW_USER_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"}
	})
}

func (f *fixture) otpSignUp(t *testing.T, name string) *SignUpOutput {
	t.Helper()
	return must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{ClientId: f.client.ClientId, Username: name, UserAttributes: []AttributeType{{Name: "email", Value: name + "@example.com"}, {Name: "phone_number", Value: "+447700900123"}}}))
}

func (f *fixture) choice(t *testing.T, name, preferred string) *AuthOutput {
	t.Helper()
	return must[*AuthOutput](t)(f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "USER_AUTH", AuthParameters: map[string]string{"USERNAME": name, "PREFERRED_CHALLENGE": preferred}}))
}

func (f *fixture) answerChoice(ch *AuthOutput, name, code string) (*AuthOutput, error) {
	return f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{ClientId: f.client.ClientId, Session: ch.Session, ChallengeName: ch.ChallengeName, ChallengeResponses: map[string]string{"USERNAME": name, ch.ChallengeName + "_CODE": code}})
}

func TestNativeOTPUserLifecycle(t *testing.T) {
	for _, factor := range []string{"EMAIL_OTP", "SMS_OTP"} {
		t.Run(factor, func(t *testing.T) {
			f := otpFixture(t)
			f.otpSignUp(t, "alice")
			ch := f.choice(t, "alice", factor)
			if ch.ChallengeName != factor {
				t.Fatalf("challenge %+v", ch)
			}
			code := f.code(t, "alice", authCodePurpose(ch.Session))
			_, err := f.answerChoice(ch, "alice", "incorrect")
			wantCode(t, err, "CodeMismatchException")
			done := must[*AuthOutput](t)(f.answerChoice(ch, "alice", code)).AuthenticationResult
			if done == nil {
				t.Fatal("missing tokens")
			}
			id := verifyWithJWKS(t, f.svc, f.poolID, done.IdToken)
			if id[otpAttribute(factor)+"_verified"] != true {
				t.Fatalf("unverified ID claims %v", id)
			}
			user := must[*AdminGetUserOutput](t)(f.svc.AdminGetUser(&AdminUserInput{UserPoolId: f.poolID, Username: "alice"}))
			if user.UserStatus != StatusConfirmed {
				t.Fatalf("status %s", user.UserStatus)
			}
			_, err = f.answerChoice(ch, "alice", code)
			wantCode(t, err, "NotAuthorizedException")
			refreshed := must[*AuthOutput](t)(f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "REFRESH_TOKEN_AUTH", AuthParameters: map[string]string{"REFRESH_TOKEN": done.RefreshToken}}))
			verifyWithJWKS(t, f.svc, f.poolID, refreshed.AuthenticationResult.AccessToken)
			must[struct{}](t)(f.svc.ChangePassword(&ChangePasswordInput{AccessToken: done.AccessToken, ProposedPassword: testPassword}))
			pw := must[*AuthOutput](t)(f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "USER_AUTH", AuthParameters: map[string]string{"USERNAME": "alice", "PREFERRED_CHALLENGE": "PASSWORD", "PASSWORD": testPassword}}))
			if pw.AuthenticationResult == nil {
				t.Fatal("first password did not work")
			}
			must[struct{}](t)(f.svc.GlobalSignOut(&AccessTokenInput{AccessToken: done.AccessToken}))
			_, err = f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "REFRESH_TOKEN_AUTH", AuthParameters: map[string]string{"REFRESH_TOKEN": done.RefreshToken}})
			wantCode(t, err, "NotAuthorizedException")
		})
	}
}

func TestNativeChoiceSelection(t *testing.T) {
	f := otpFixture(t)
	f.otpSignUp(t, "alice")
	for _, preferred := range []string{"", "WEB_AUTHN"} {
		ch := f.choice(t, "alice", preferred)
		if ch.ChallengeName != "SELECT_CHALLENGE" || !slices.Equal(ch.AvailableChallenges, []string{"EMAIL_OTP", "SMS_OTP"}) {
			t.Fatalf("choices %+v", ch)
		}
		_, err := f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{ClientId: f.client.ClientId, Session: ch.Session, ChallengeName: "SELECT_CHALLENGE", ChallengeResponses: map[string]string{"USERNAME": "alice", "ANSWER": "PASSWORD"}})
		wantCode(t, err, "InvalidParameterException")
		next := must[*AuthOutput](t)(f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{ClientId: f.client.ClientId, Session: ch.Session, ChallengeName: "SELECT_CHALLENGE", ChallengeResponses: map[string]string{"USERNAME": "alice", "ANSWER": "SMS_OTP"}}))
		if next.ChallengeName != "SMS_OTP" {
			t.Fatalf("selected %+v", next)
		}
		must[*AuthOutput](t)(f.answerChoice(next, "alice", f.code(t, "alice", authCodePurpose(next.Session))))
	}
}

func TestNativeOTPBinding(t *testing.T) {
	for _, change := range []string{"username", "client", "challenge", "contact", "disabled", "deleted", "recreated", "expired", "policy"} {
		t.Run(change, func(t *testing.T) {
			f := otpFixture(t)
			f.otpSignUp(t, "alice")
			f.otpSignUp(t, "bob")
			now := time.Now()
			f.svc.now = func() time.Time { return now }
			ch := f.choice(t, "alice", "EMAIL_OTP")
			code := f.code(t, "alice", authCodePurpose(ch.Session))
			in := &RespondToAuthChallengeInput{ClientId: f.client.ClientId, Session: ch.Session, ChallengeName: ch.ChallengeName, ChallengeResponses: map[string]string{"USERNAME": "alice", "EMAIL_OTP_CODE": code}}
			switch change {
			case "username":
				in.ChallengeResponses["USERNAME"] = "bob"
			case "client":
				cin := &CreateUserPoolClientInput{UserPoolId: f.poolID}
				cin.ClientName = "other"
				cin.ExplicitAuthFlows = []string{"ALLOW_USER_AUTH"}
				in.ClientId = must[*UserPoolClientOutput](t)(f.svc.CreateUserPoolClient(cin)).UserPoolClient.ClientId
			case "challenge":
				in.ChallengeName = "SMS_OTP"
			case "contact":
				must[struct{}](t)(f.svc.AdminUpdateUserAttributes(&AdminUpdateUserAttributesInput{UserPoolId: f.poolID, Username: "alice", UserAttributes: []AttributeType{{Name: "email", Value: "new@example.com"}}}))
			case "disabled":
				must[struct{}](t)(f.svc.AdminDisableUser(&AdminUserInput{UserPoolId: f.poolID, Username: "alice"}))
			case "deleted", "recreated":
				must[struct{}](t)(f.svc.AdminDeleteUser(&AdminUserInput{UserPoolId: f.poolID, Username: "alice"}))
				if change == "recreated" {
					f.otpSignUp(t, "alice")
				}
			case "expired":
				now = now.Add(3 * time.Minute)
			case "policy":
				must[struct{}](t)(f.svc.UpdateUserPool(&UpdateUserPoolInput{UserPoolId: f.poolID}))
			}
			_, err := f.svc.RespondToAuthChallenge(in)
			wantCode(t, err, "NotAuthorizedException")
		})
	}
}

func TestNativeOTPConcurrentProofs(t *testing.T) {
	f := otpFixture(t)
	f.otpSignUp(t, "alice")
	a, b := f.choice(t, "alice", "EMAIL_OTP"), f.choice(t, "alice", "EMAIL_OTP")
	ca, cb := f.code(t, "alice", authCodePurpose(a.Session)), f.code(t, "alice", authCodePurpose(b.Session))
	if ca != cb {
		_, err := f.answerChoice(a, "alice", cb)
		wantCode(t, err, "CodeMismatchException")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := f.answerChoice(a, "alice", ca); results <- err })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			wantCode(t, err, "NotAuthorizedException")
		}
	}
	if successes != 1 {
		t.Fatalf("proof succeeded %d times", successes)
	}
	must[*AuthOutput](t)(f.answerChoice(b, "alice", cb))
}

func TestNativeOTPConfirmationHandoff(t *testing.T) {
	f := otpFixture(t)
	signed := f.otpSignUp(t, "alice")
	out := must[*ConfirmSignUpOutput](t)(f.svc.ConfirmSignUp(&ConfirmSignUpInput{ClientId: f.client.ClientId, Username: "alice", Session: signed.Session, ConfirmationCode: f.code(t, "alice", purposeSignUp)}))
	if out.Session == "" {
		t.Fatal("missing confirmation handoff")
	}
	in := &InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "USER_AUTH", Session: out.Session}
	done := must[*AuthOutput](t)(f.svc.InitiateAuth(in))
	if done.AuthenticationResult == nil {
		t.Fatal("handoff did not issue tokens")
	}
	_, err := f.svc.InitiateAuth(in)
	wantCode(t, err, "NotAuthorizedException")
	if codes := must[[]PendingCode](t)(f.svc.PendingCodes(f.poolID, "alice")); len(codes) != 0 {
		t.Fatalf("handoff sent a new code: %+v", codes)
	}
}

func TestNativeChoicePasswordAndSRP(t *testing.T) {
	f := otpFixture(t)
	f.signUpConfirmed(t, "alice", "alice@example.com")
	for _, direct := range []bool{false, true} {
		ch := f.choice(t, "alice", "")
		name := "SELECT_CHALLENGE"
		if direct {
			name = "PASSWORD"
		}
		done := must[*AuthOutput](t)(f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{ClientId: f.client.ClientId, Session: ch.Session, ChallengeName: name, ChallengeResponses: map[string]string{"USERNAME": "alice", "ANSWER": "PASSWORD", "PASSWORD": testPassword}}))
		if done.AuthenticationResult == nil {
			t.Fatal("password choice failed")
		}
	}
	client := newSRPClient()
	ch := must[*AuthOutput](t)(f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "USER_AUTH", AuthParameters: map[string]string{"USERNAME": "alice", "PREFERRED_CHALLENGE": "PASSWORD_SRP", "SRP_A": client.A.Text(16)}}))
	if ch.ChallengeName != "PASSWORD_VERIFIER" {
		t.Fatalf("SRP challenge %+v", ch)
	}
	timestamp := "Wed Nov 11 12:00:00 UTC 2026"
	done := must[*AuthOutput](t)(f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{ClientId: f.client.ClientId, Session: ch.Session, ChallengeName: ch.ChallengeName, ChallengeResponses: map[string]string{"USERNAME": "alice", "TIMESTAMP": timestamp, "PASSWORD_CLAIM_SECRET_BLOCK": ch.ChallengeParameters["SECRET_BLOCK"], "PASSWORD_CLAIM_SIGNATURE": client.signature(f.poolID, "alice", testPassword, ch.ChallengeParameters, timestamp)}}))
	if done.AuthenticationResult == nil {
		t.Fatal("SRP choice failed")
	}
}

func TestNativeOTPExistenceSuppression(t *testing.T) {
	f := otpFixture(t)
	_, err := f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "USER_AUTH", AuthParameters: map[string]string{"USERNAME": "missing"}})
	wantCode(t, err, "UserNotFoundException")
	in := &UpdateUserPoolClientInput{UserPoolId: f.poolID, ClientId: f.client.ClientId, UserPoolClientSettings: f.client.UserPoolClientSettings}
	in.PreventUserExistenceErrors = "ENABLED"
	f.client = must[*UserPoolClientOutput](t)(f.svc.UpdateUserPoolClient(in)).UserPoolClient
	ch := f.choice(t, "missing", "EMAIL_OTP")
	if ch.ChallengeName != "EMAIL_OTP" {
		t.Fatalf("simulated %+v", ch)
	}
	_, err = f.answerChoice(ch, "missing", "123456")
	wantCode(t, err, "CodeMismatchException")
	if codes := must[[]PendingCode](t)(f.svc.PendingCodes(f.poolID, "")); len(codes) != 0 {
		t.Fatalf("simulated code exposed: %+v", codes)
	}
	// Creating the account later must not turn a simulated proof into a real one.
	fake := f.choice(t, "missing", "PASSWORD")
	f.signUpConfirmed(t, "missing", "missing@example.com")
	_, err = f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{ClientId: f.client.ClientId, Session: fake.Session, ChallengeName: fake.ChallengeName, ChallengeResponses: map[string]string{"USERNAME": "missing", "PASSWORD": testPassword}})
	wantCode(t, err, "NotAuthorizedException")
}

func TestNativeOTPOptionalMFA(t *testing.T) {
	f := otpFixture(t)
	f.signUpConfirmed(t, "alice", "alice@example.com")
	config := &SetUserPoolMfaConfigInput{UserPoolId: f.poolID}
	config.MfaConfiguration = "OPTIONAL"
	must[*MfaSettings](t)(f.svc.SetUserPoolMfaConfig(config))
	if !slices.Contains(f.choice(t, "alice", "").AvailableChallenges, "EMAIL_OTP") {
		t.Fatal("unenrolled user lost OTP")
	}
	must[struct{}](t)(f.svc.AdminSetUserMFAPreference(&SetUserMFAPreferenceInput{UserPoolId: f.poolID, Username: "alice", EmailMfaSettings: &SMSMfaSettingsType{Enabled: true}}))
	if slices.Contains(f.choice(t, "alice", "").AvailableChallenges, "EMAIL_OTP") {
		t.Fatal("MFA user offered OTP first factor")
	}
}

func TestNativeOTPConfiguration(t *testing.T) {
	for _, kind := range []string{"empty", "unknown", "duplicate", "tier", "requiredMFA"} {
		t.Run(kind, func(t *testing.T) {
			svc := newTestService(t)
			in := &CreateUserPoolInput{PoolName: "invalid"}
			in.Policies = &UserPoolPolicyType{SignInPolicy: &SignInPolicyType{AllowedFirstAuthFactors: []string{"EMAIL_OTP"}}}
			switch kind {
			case "empty":
				in.Policies.SignInPolicy.AllowedFirstAuthFactors = nil
			case "unknown":
				in.Policies.SignInPolicy.AllowedFirstAuthFactors = []string{"MAGIC_LINK"}
			case "duplicate":
				in.Policies.SignInPolicy.AllowedFirstAuthFactors = []string{"EMAIL_OTP", "EMAIL_OTP"}
			case "tier":
				in.UserPoolTier = "LITE"
			case "requiredMFA":
				in.MfaConfiguration = "ON"
			}
			_, err := svc.CreateUserPool(in)
			code := "InvalidParameterException"
			if kind == "tier" {
				code = "FeatureUnavailableInTierException"
			}
			wantCode(t, err, code)
		})
	}
	f := otpFixture(t)
	mfa := &SetUserPoolMfaConfigInput{UserPoolId: f.poolID}
	mfa.MfaConfiguration = "ON"
	_, err := f.svc.SetUserPoolMfaConfig(mfa)
	wantCode(t, err, "InvalidParameterException")
	update := &UpdateUserPoolInput{UserPoolId: f.poolID}
	update.Policies = &UserPoolPolicyType{SignInPolicy: &SignInPolicyType{AllowedFirstAuthFactors: []string{"EMAIL_OTP"}}}
	update.MfaConfiguration = "ON"
	_, err = f.svc.UpdateUserPool(update)
	wantCode(t, err, "InvalidParameterException")
	got := must[*UserPoolOutput](t)(f.svc.DescribeUserPool(&PoolIDInput{UserPoolId: f.poolID}))
	if got.UserPool.MfaConfiguration != "OFF" {
		t.Fatal("failed mutation changed pool")
	}
}

func TestNativeOTPAdminOnboarding(t *testing.T) {
	f := otpFixture(t)
	out := must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{UserPoolId: f.poolID, Username: "alice", MessageAction: "SUPPRESS", UserAttributes: []AttributeType{{Name: "email", Value: "alice@example.com"}}}))
	if out.User.UserStatus != StatusConfirmed {
		t.Fatalf("admin status %s", out.User.UserStatus)
	}
	ch := f.choice(t, "alice", "EMAIL_OTP")
	must[*AuthOutput](t)(f.svc.AdminRespondToAuthChallenge(&RespondToAuthChallengeInput{UserPoolId: f.poolID, ClientId: f.client.ClientId, Session: ch.Session, ChallengeName: ch.ChallengeName, ChallengeResponses: map[string]string{"USERNAME": "alice", "EMAIL_OTP_CODE": f.code(t, "alice", authCodePurpose(ch.Session))}}))
	_, err := f.svc.AdminCreateUser(&AdminCreateUserInput{UserPoolId: f.poolID, Username: "no-contact"})
	wantCode(t, err, "InvalidParameterException")
}

func TestNativeOTPTriggers(t *testing.T) {
	f := otpFixture(t)
	fl := &fakeLambdas{handlers: map[string]func(map[string]any) error{}}
	f.svc.SetTriggerInvoker(fl.invoke)
	f.svc.mu.Lock()
	f.svc.pools[f.poolID].Config.LambdaConfig = json.RawMessage(`{"PreAuthentication":"` + arnPreAuth + `","PostConfirmation":"` + arnPostConf + `","PreTokenGeneration":"` + arnPreToken + `"}`)
	f.svc.mu.Unlock()
	f.otpSignUp(t, "alice")
	ch := f.choice(t, "alice", "EMAIL_OTP")
	must[*AuthOutput](t)(f.answerChoice(ch, "alice", f.code(t, "alice", authCodePurpose(ch.Session))))
	if got := strings.Join(fl.sources(), ","); got != "PreAuthentication_Authentication,PostConfirmation_ConfirmSignUp,TokenGeneration_Authentication" {
		t.Fatalf("sources %s", got)
	}
	fl.handlers[arnPreAuth] = func(map[string]any) error { return errors.New("denied") }
	ch = f.choice(t, "alice", "EMAIL_OTP")
	_, err := f.answerChoice(ch, "alice", f.code(t, "alice", authCodePurpose(ch.Session)))
	wantCode(t, err, "UserLambdaValidationException")
}

func TestNativeOTPSecretHash(t *testing.T) {
	f := otpFixture(t)
	f.svc.mu.Lock()
	f.svc.pools[f.poolID].Clients[f.client.ClientId].ClientSecret = "client-secret"
	f.svc.mu.Unlock()
	// Use the same HMAC calculation exercised by the existing secret-hash tests.
	hash := secretHash("client-secret", "alice", f.client.ClientId)
	must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{ClientId: f.client.ClientId, Username: "alice", SecretHash: hash, UserAttributes: []AttributeType{{Name: "email", Value: "alice@example.com"}}}))
	_, err := f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "USER_AUTH", AuthParameters: map[string]string{"USERNAME": "alice"}})
	wantCode(t, err, "NotAuthorizedException")
	ch := must[*AuthOutput](t)(f.svc.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "USER_AUTH", AuthParameters: map[string]string{"USERNAME": "alice", "SECRET_HASH": hash, "PREFERRED_CHALLENGE": "EMAIL_OTP"}}))
	in := &RespondToAuthChallengeInput{ClientId: f.client.ClientId, Session: ch.Session, ChallengeName: ch.ChallengeName, ChallengeResponses: map[string]string{"USERNAME": "alice", "EMAIL_OTP_CODE": f.code(t, "alice", authCodePurpose(ch.Session))}}
	_, err = f.svc.RespondToAuthChallenge(in)
	wantCode(t, err, "NotAuthorizedException")
	in.ChallengeResponses["SECRET_HASH"] = hash
	must[*AuthOutput](t)(f.svc.RespondToAuthChallenge(in))
}

func TestNativeOTPCustomSender(t *testing.T) {
	for _, factor := range []string{"EMAIL_OTP", "SMS_OTP"} {
		t.Run(factor, func(t *testing.T) {
			f := otpFixture(t)
			f.svc.mu.Lock()
			f.svc.pools[f.poolID].Config.LambdaConfig = json.RawMessage(senderConfig)
			f.svc.mu.Unlock()
			fl := &fakeLambdas{handlers: map[string]func(map[string]any) error{}}
			enc := &recordingEncrypter{}
			f.svc.SetTriggerInvoker(fl.invoke)
			f.svc.SetMessageEncrypter(enc.encrypt)
			f.otpSignUp(t, "alice")
			f.svc.WaitForSenders()
			ch := must[*AuthOutput](t)(f.svc.AdminInitiateAuth(&InitiateAuthInput{UserPoolId: f.poolID, ClientId: f.client.ClientId, AuthFlow: "USER_AUTH", AuthParameters: map[string]string{"USERNAME": "alice", "PREFERRED_CHALLENGE": factor}, ClientMetadata: map[string]string{"journey": "passwordless"}}))
			event, code := lastSend(t, f, fl)
			expected := "CustomEmailSender_Authentication"
			if factor == "SMS_OTP" {
				expected = "CustomSMSSender_Authentication"
			}
			if event["triggerSource"] != expected || code != f.code(t, "alice", authCodePurpose(ch.Session)) {
				t.Fatalf("sender %v", event)
			}
			request := event["request"].(map[string]any)
			if request["clientMetadata"].(map[string]any)["journey"] != "passwordless" {
				t.Fatalf("metadata %v", request)
			}
		})
	}
}

func TestNativeOTPAliasCollision(t *testing.T) {
	f := newFixture(t, func(p *CreateUserPoolInput, c *CreateUserPoolClientInput) {
		p.AliasAttributes = []string{"email"}
		p.Policies = &UserPoolPolicyType{SignInPolicy: &SignInPolicyType{AllowedFirstAuthFactors: []string{"EMAIL_OTP"}}}
		c.ExplicitAuthFlows = []string{"ALLOW_USER_AUTH"}
	})
	f.otpSignUp(t, "alice")
	a := f.choice(t, "alice", "EMAIL_OTP")
	must[*AuthOutput](t)(f.answerChoice(a, "alice", f.code(t, "alice", authCodePurpose(a.Session))))
	must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{ClientId: f.client.ClientId, Username: "bob", UserAttributes: []AttributeType{{Name: "email", Value: "alice@example.com"}}}))
	b := f.choice(t, "bob", "EMAIL_OTP")
	_, err := f.answerChoice(b, "bob", f.code(t, "bob", authCodePurpose(b.Session)))
	wantCode(t, err, "AliasExistsException")
	user := must[*AdminGetUserOutput](t)(f.svc.AdminGetUser(&AdminUserInput{UserPoolId: f.poolID, Username: "bob"}))
	if user.UserStatus != StatusUnconfirmed {
		t.Fatal("failed alias claim confirmed user")
	}
	for _, attr := range user.UserAttributes {
		if attr.Name == "email_verified" && attr.Value == "true" {
			t.Fatal("failed alias claim verified email")
		}
	}
}

func TestNativeOTPRechecksAfterLambda(t *testing.T) {
	f := otpFixture(t)
	f.otpSignUp(t, "alice")
	f.svc.mu.Lock()
	f.svc.pools[f.poolID].Config.LambdaConfig = json.RawMessage(`{"PreAuthentication":"` + arnPreAuth + `"}`)
	f.svc.mu.Unlock()
	fl := &fakeLambdas{handlers: map[string]func(map[string]any) error{arnPreAuth: func(map[string]any) error {
		_, err := f.svc.AdminDisableUser(&AdminUserInput{UserPoolId: f.poolID, Username: "alice"})
		return err
	}}}
	f.svc.SetTriggerInvoker(fl.invoke)
	ch := f.choice(t, "alice", "EMAIL_OTP")
	_, err := f.answerChoice(ch, "alice", f.code(t, "alice", authCodePurpose(ch.Session)))
	wantCode(t, err, "NotAuthorizedException")
}

func TestNativeOTPPersistence(t *testing.T) {
	f := otpFixture(t)
	f.svc.cfg.PersistenceEnabled = true
	if err := f.svc.Init(); err != nil {
		t.Fatal(err)
	}
	f.otpSignUp(t, "alice")
	ch := f.choice(t, "alice", "EMAIL_OTP")
	done := must[*AuthOutput](t)(f.answerChoice(ch, "alice", f.code(t, "alice", authCodePurpose(ch.Session)))).AuthenticationResult
	pending := f.choice(t, "alice", "EMAIL_OTP")
	code := f.code(t, "alice", authCodePurpose(pending.Session))
	f.svc.Close()
	restarted := NewService(f.svc.cfg, NewIndex())
	if err := restarted.Init(); err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	f.svc = restarted
	verifyWithJWKS(t, restarted, f.poolID, done.IdToken)
	must[*AuthOutput](t)(restarted.InitiateAuth(&InitiateAuthInput{ClientId: f.client.ClientId, AuthFlow: "REFRESH_TOKEN_AUTH", AuthParameters: map[string]string{"REFRESH_TOKEN": done.RefreshToken}}))
	_, err := f.answerChoice(pending, "alice", code)
	wantCode(t, err, "NotAuthorizedException")
	codes := must[[]PendingCode](t)(restarted.PendingCodes(f.poolID, "alice"))
	for _, c := range codes {
		if strings.HasPrefix(c.Purpose, "auth:") {
			t.Fatal("persisted auth proof")
		}
	}
	next := f.choice(t, "alice", "EMAIL_OTP")
	must[*AuthOutput](t)(f.answerChoice(next, "alice", f.code(t, "alice", authCodePurpose(next.Session))))
}

func TestNativeOTPAdminRequiredAttributes(t *testing.T) {
	f := newFixture(t, func(p *CreateUserPoolInput, c *CreateUserPoolClientInput) {
		p.Policies = &UserPoolPolicyType{SignInPolicy: &SignInPolicyType{AllowedFirstAuthFactors: []string{"PASSWORD", "EMAIL_OTP"}}}
		p.Schema = []schemaAttributeInput{{Name: "name", AttributeDataType: "String", Required: ptr(true)}}
		c.ExplicitAuthFlows = []string{"ALLOW_USER_AUTH"}
	})
	in := &AdminCreateUserInput{UserPoolId: f.poolID, Username: "alice", MessageAction: "SUPPRESS", UserAttributes: []AttributeType{{Name: "email", Value: "alice@example.com"}}}
	_, err := f.svc.AdminCreateUser(in)
	wantCode(t, err, "InvalidParameterException")
	in.TemporaryPassword = testPassword
	out := must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(in))
	if out.User.UserStatus != StatusForceChangePassword {
		t.Fatalf("status %s", out.User.UserStatus)
	}
	ch := f.choice(t, "alice", "EMAIL_OTP")
	if ch.ChallengeName != "SELECT_CHALLENGE" || slices.Contains(ch.AvailableChallenges, "EMAIL_OTP") {
		t.Fatalf("temporary-password user offered OTP: %+v", ch)
	}
}

func TestNativeChoiceClientValidation(t *testing.T) {
	f := otpFixture(t)
	for _, validity := range []int32{2, 16} {
		in := &CreateUserPoolClientInput{UserPoolId: f.poolID}
		in.ClientName = "invalid"
		in.ExplicitAuthFlows = []string{"ALLOW_USER_AUTH"}
		in.AuthSessionValidity = validity
		_, err := f.svc.CreateUserPoolClient(in)
		wantCode(t, err, "InvalidParameterException")
	}
	pin := &CreateUserPoolInput{PoolName: "lite"}
	pin.UserPoolTier = "LITE"
	pool := must[*UserPoolOutput](t)(f.svc.CreateUserPool(pin)).UserPool
	cin := &CreateUserPoolClientInput{UserPoolId: pool.Id}
	cin.ClientName = "invalid"
	cin.ExplicitAuthFlows = []string{"ALLOW_USER_AUTH"}
	_, err := f.svc.CreateUserPoolClient(cin)
	wantCode(t, err, "FeatureUnavailableInTierException")
	// Failed updates preserve the current client configuration.
	update := &UpdateUserPoolClientInput{UserPoolId: f.poolID, ClientId: f.client.ClientId, UserPoolClientSettings: f.client.UserPoolClientSettings}
	update.AuthSessionValidity = 16
	_, err = f.svc.UpdateUserPoolClient(update)
	wantCode(t, err, "InvalidParameterException")
	current := must[*UserPoolClientOutput](t)(f.svc.DescribeUserPoolClient(&ClientIDInput{UserPoolId: f.poolID, ClientId: f.client.ClientId}))
	if current.UserPoolClient.AuthSessionValidity != 3 {
		t.Fatal("invalid update changed client")
	}
}
