package cognito

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeLambdas records trigger events and answers them with per-function
// handlers that may edit the event's response, as a real trigger does.
type fakeLambdas struct {
	mu       sync.Mutex
	events   []map[string]any
	handlers map[string]func(event map[string]any) error
}

func (f *fakeLambdas) invoke(_ context.Context, arn string, payload []byte) ([]byte, error) {
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.events = append(f.events, event)
	h := f.handlers[arn]
	f.mu.Unlock()
	if h != nil {
		if err := h(event); err != nil {
			return nil, err
		}
	}
	return json.Marshal(event)
}

func (f *fakeLambdas) sources() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, e := range f.events {
		out = append(out, e["triggerSource"].(string))
	}
	return out
}

const (
	arnPreSignUp = "arn:aws:lambda:us-east-1:000000000000:function:pre-sign-up"
	arnPostConf  = "arn:aws:lambda:us-east-1:000000000000:function:post-confirmation"
	arnPreToken  = "arn:aws:lambda:us-east-1:000000000000:function:pre-token"
	arnPreAuth   = "arn:aws:lambda:us-east-1:000000000000:function:pre-auth"
)

func triggerFixture(t *testing.T, lambdaConfig string) (*fixture, *fakeLambdas) {
	t.Helper()
	f := newFixture(t, func(p *CreateUserPoolInput, _ *CreateUserPoolClientInput) {
		p.LambdaConfig = json.RawMessage(lambdaConfig)
	})
	fl := &fakeLambdas{handlers: map[string]func(map[string]any) error{}}
	f.svc.SetTriggerInvoker(fl.invoke)
	return f, fl
}

func TestPostConfirmationAndPreTokenGenerationV1(t *testing.T) {
	f, fl := triggerFixture(t, `{"PostConfirmation":"`+arnPostConf+`","PreTokenGeneration":"`+arnPreToken+`"}`)
	fl.handlers[arnPreToken] = func(e map[string]any) error {
		e["response"] = map[string]any{"claimsOverrideDetails": map[string]any{
			"claimsToAddOrOverride": map[string]any{"candidateId": "123456", "sub": "hijack"},
			"claimsToSuppress":      []any{"email"},
		}}
		return nil
	}
	sub := f.signUpConfirmed(t, "vic", "vic@example.com")

	fl.mu.Lock()
	post := fl.events[0]
	fl.mu.Unlock()
	if post["triggerSource"] != "PostConfirmation_ConfirmSignUp" || post["userName"] != "vic" {
		t.Fatalf("post confirmation event %v", post)
	}
	attrs := post["request"].(map[string]any)["userAttributes"].(map[string]any)
	if attrs["sub"] != sub || attrs["email_verified"] != "true" || attrs["cognito:user_status"] != "CONFIRMED" {
		t.Fatalf("post confirmation attributes %v", attrs)
	}

	res := must[*AuthOutput](t)(f.passwordAuth(t, "vic", testPassword)).AuthenticationResult
	id := verifyWithJWKS(t, f.svc, f.poolID, res.IdToken)
	if id["candidateId"] != "123456" || id["sub"] != sub || id["email"] != nil {
		t.Fatalf("id claims %v", id)
	}
	// Version 1 events only customise the ID token, as in AWS.
	if access := verifyWithJWKS(t, f.svc, f.poolID, res.AccessToken); access["candidateId"] != nil {
		t.Fatalf("v1 trigger changed the access token: %v", access)
	}

	must[*AuthOutput](t)(f.svc.InitiateAuth(&InitiateAuthInput{
		ClientId: f.client.ClientId, AuthFlow: "REFRESH_TOKEN_AUTH",
		AuthParameters: map[string]string{"REFRESH_TOKEN": res.RefreshToken},
	}))
	got := strings.Join(fl.sources(), ",")
	if got != "PostConfirmation_ConfirmSignUp,TokenGeneration_Authentication,TokenGeneration_RefreshTokens" {
		t.Fatalf("trigger sources %s", got)
	}
}

func TestPreTokenGenerationV2CustomisesAccessToken(t *testing.T) {
	f, fl := triggerFixture(t, `{"PreTokenGenerationConfig":{"LambdaArn":"`+arnPreToken+`","LambdaVersion":"V2_0"}}`)
	fl.handlers[arnPreToken] = func(e map[string]any) error {
		if e["version"] != "2" {
			return errors.New("want version 2 event")
		}
		e["response"] = map[string]any{"claimsAndScopeOverrideDetails": map[string]any{
			"idTokenGeneration":     map[string]any{"claimsToAddOrOverride": map[string]any{"candidateId": "42"}},
			"accessTokenGeneration": map[string]any{"claimsToAddOrOverride": map[string]any{"candidateId": "42"}, "scopesToAdd": []any{"jobs/read"}},
			"groupOverrideDetails":  map[string]any{"groupsToOverride": []any{"candidates"}},
		}}
		return nil
	}
	f.signUpConfirmed(t, "wes", "wes@example.com")
	res := must[*AuthOutput](t)(f.passwordAuth(t, "wes", testPassword)).AuthenticationResult
	access := verifyWithJWKS(t, f.svc, f.poolID, res.AccessToken)
	if access["candidateId"] != "42" || access["scope"] != "aws.cognito.signin.user.admin jobs/read" {
		t.Fatalf("access claims %v", access)
	}
	if g, _ := access["cognito:groups"].([]any); len(g) != 1 || g[0] != "candidates" {
		t.Fatalf("groups %v", access["cognito:groups"])
	}
}

func TestPreSignUpAutoConfirm(t *testing.T) {
	f, fl := triggerFixture(t, `{"PreSignUp":"`+arnPreSignUp+`","PostConfirmation":"`+arnPostConf+`"}`)
	fl.handlers[arnPreSignUp] = func(e map[string]any) error {
		e["response"] = map[string]any{"autoConfirmUser": true, "autoVerifyEmail": true}
		return nil
	}
	out := must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{
		ClientId: f.client.ClientId, Username: "xan", Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: "xan@example.com"}},
		ClientMetadata: map[string]string{"source": "test"},
	}))
	if !out.UserConfirmed || out.CodeDeliveryDetails != nil {
		t.Fatalf("sign up %+v", out)
	}
	fl.mu.Lock()
	req := fl.events[0]["request"].(map[string]any)
	fl.mu.Unlock()
	if req["clientMetadata"].(map[string]any)["source"] != "test" {
		t.Fatalf("client metadata not passed: %v", req)
	}
	must[*AuthOutput](t)(f.passwordAuth(t, "xan", testPassword))
}

func TestTriggerErrorsAreUserLambdaValidation(t *testing.T) {
	f, fl := triggerFixture(t, `{"PreSignUp":"`+arnPreSignUp+`","PreAuthentication":"`+arnPreAuth+`"}`)
	fl.handlers[arnPreSignUp] = func(map[string]any) error { return errors.New("domain not allowed") }
	_, err := f.svc.SignUp(&SignUpInput{
		ClientId: f.client.ClientId, Username: "yas", Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: "yas@example.com"}},
	})
	if err == nil || err.Error() != "UserLambdaValidationException: PreSignUp failed with error domain not allowed." {
		t.Fatalf("got %v", err)
	}
	if _, err := f.svc.AdminGetUser(&AdminUserInput{UserPoolId: f.poolID, Username: "yas"}); err == nil {
		t.Fatal("user created despite PreSignUp failure")
	}

	delete(fl.handlers, arnPreSignUp)
	f.signUpConfirmed(t, "yas", "yas@example.com")
	fl.handlers[arnPreAuth] = func(map[string]any) error { return errors.New("locked out") }
	_, err = f.passwordAuth(t, "yas", testPassword)
	wantCode(t, err, "UserLambdaValidationException")
}

func TestTriggersSkippedWithoutInvoker(t *testing.T) {
	f := newFixture(t, func(p *CreateUserPoolInput, _ *CreateUserPoolClientInput) {
		p.LambdaConfig = json.RawMessage(`{"PostConfirmation":"` + arnPostConf + `"}`)
	})
	f.signUpConfirmed(t, "zed", "zed@example.com")
}

// --- MFA ---

func mfaFixture(t *testing.T, mode string, email bool) *fixture {
	t.Helper()
	f := newFixture(t, nil)
	in := &SetUserPoolMfaConfigInput{UserPoolId: f.poolID}
	in.MfaConfiguration = mode
	in.SmsMfaConfiguration = json.RawMessage(`{"SmsAuthenticationMessage":"{####}"}`)
	if email {
		in.EmailMfaConfiguration = json.RawMessage(`{"Message":"{####}","Subject":"Code"}`)
	}
	must[*MfaSettings](t)(f.svc.SetUserPoolMfaConfig(in))
	return f
}

func (f *fixture) signedInWithPhone(t *testing.T, name string) *AuthenticationResultType {
	t.Helper()
	f.signUpConfirmed(t, name, name+"@example.com")
	res := must[*AuthOutput](t)(f.passwordAuth(t, name, testPassword)).AuthenticationResult
	must[*UpdateUserAttributesOutput](t)(f.svc.UpdateUserAttributes(&UpdateUserAttributesInput{
		AccessToken: res.AccessToken, UserAttributes: []AttributeType{{Name: "phone_number", Value: "+447700900123"}},
	}))
	return res
}

func TestSMSMFAChallenge(t *testing.T) {
	f := mfaFixture(t, "OPTIONAL", false)
	res := f.signedInWithPhone(t, "amy")
	must[struct{}](t)(f.svc.SetUserMFAPreference(&SetUserMFAPreferenceInput{
		AccessToken: res.AccessToken, SMSMfaSettings: &SMSMfaSettingsType{Enabled: true, PreferredMfa: true},
	}))
	user := must[*GetUserOutput](t)(f.svc.GetUser(&AccessTokenInput{AccessToken: res.AccessToken}))
	if user.PreferredMfaSetting != "SMS_MFA" || len(user.UserMFASettingList) != 1 {
		t.Fatalf("mfa settings %+v", user)
	}

	ch := must[*AuthOutput](t)(f.passwordAuth(t, "amy", testPassword))
	if ch.ChallengeName != "SMS_MFA" || ch.ChallengeParameters["CODE_DELIVERY_DELIVERY_MEDIUM"] != "SMS" ||
		ch.ChallengeParameters["CODE_DELIVERY_DESTINATION"] != "+********0123" {
		t.Fatalf("challenge %+v", ch)
	}
	respond := func(code string) (*AuthOutput, error) {
		return f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{
			ClientId: f.client.ClientId, ChallengeName: "SMS_MFA", Session: ch.Session,
			ChallengeResponses: map[string]string{"USERNAME": "amy", "SMS_MFA_CODE": code},
		})
	}
	_, err := respond("000000")
	wantCode(t, err, "CodeMismatchException")
	// The session survives a wrong code.
	done := must[*AuthOutput](t)(respond(f.code(t, "amy", purposeMFA)))
	if done.AuthenticationResult == nil {
		t.Fatalf("no tokens %+v", done)
	}
	_, err = respond("123456")
	wantCode(t, err, "NotAuthorizedException")
}

func TestEmailOTPNeedsPoolEmailMFA(t *testing.T) {
	f := mfaFixture(t, "OPTIONAL", true)
	f.signUpConfirmed(t, "ben", "ben@example.com")
	must[struct{}](t)(f.svc.AdminSetUserMFAPreference(&SetUserMFAPreferenceInput{
		UserPoolId: f.poolID, Username: "ben", EmailMfaSettings: &SMSMfaSettingsType{Enabled: true, PreferredMfa: true},
	}))
	ch := must[*AuthOutput](t)(f.passwordAuth(t, "ben", testPassword))
	if ch.ChallengeName != "EMAIL_OTP" || ch.ChallengeParameters["CODE_DELIVERY_DELIVERY_MEDIUM"] != "EMAIL" {
		t.Fatalf("challenge %+v", ch)
	}
	must[*AuthOutput](t)(f.svc.RespondToAuthChallenge(&RespondToAuthChallengeInput{
		ClientId: f.client.ClientId, ChallengeName: "EMAIL_OTP", Session: ch.Session,
		ChallengeResponses: map[string]string{"USERNAME": "ben", "EMAIL_OTP_CODE": f.code(t, "ben", purposeMFA)},
	}))

	// Without EmailMfaConfiguration on the pool, the preference is never used.
	g := mfaFixture(t, "OPTIONAL", false)
	g.signUpConfirmed(t, "cal", "cal@example.com")
	must[struct{}](t)(g.svc.AdminSetUserMFAPreference(&SetUserMFAPreferenceInput{
		UserPoolId: g.poolID, Username: "cal", EmailMfaSettings: &SMSMfaSettingsType{Enabled: true, PreferredMfa: true},
	}))
	if out := must[*AuthOutput](t)(g.passwordAuth(t, "cal", testPassword)); out.AuthenticationResult == nil {
		t.Fatalf("want tokens, got %+v", out)
	}
}

func TestMFAOffNeverChallenges(t *testing.T) {
	f := newFixture(t, nil)
	res := f.signedInWithPhone(t, "dee")
	must[struct{}](t)(f.svc.SetUserMFAPreference(&SetUserMFAPreferenceInput{
		AccessToken: res.AccessToken, SMSMfaSettings: &SMSMfaSettingsType{Enabled: true, PreferredMfa: true},
	}))
	if out := must[*AuthOutput](t)(f.passwordAuth(t, "dee", testPassword)); out.ChallengeName != "" {
		t.Fatalf("pool MFA is OFF but got %s", out.ChallengeName)
	}
}

func TestMFAPreferenceValidation(t *testing.T) {
	f := mfaFixture(t, "OPTIONAL", false)
	f.signUpConfirmed(t, "eve", "eve@example.com")
	_, err := f.svc.AdminSetUserMFAPreference(&SetUserMFAPreferenceInput{
		UserPoolId: f.poolID, Username: "eve", SMSMfaSettings: &SMSMfaSettingsType{Enabled: true},
	})
	wantCode(t, err, "InvalidParameterException")
	_, err = f.svc.AdminSetUserMFAPreference(&SetUserMFAPreferenceInput{
		UserPoolId: f.poolID, Username: "eve", SoftwareTokenMfaSettings: &SMSMfaSettingsType{Enabled: true},
	})
	wantCode(t, err, "InvalidParameterException")
}

func TestMFAAfterSRPAndRequiredByPool(t *testing.T) {
	f := mfaFixture(t, "ON", false)
	f.signedInWithPhone(t, "fay")
	ch := must[*AuthOutput](t)(f.srpSignIn(t, "fay", testPassword))
	if ch.ChallengeName != "SMS_MFA" {
		t.Fatalf("pool MFA ON with a phone number should challenge, got %+v", ch)
	}
}
