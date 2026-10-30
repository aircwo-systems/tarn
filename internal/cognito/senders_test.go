package cognito

import (
	"encoding/base64"
	"errors"
	"sync"
	"testing"
)

const (
	arnEmailSender = "arn:aws:lambda:us-east-1:000000000000:function:email-sender"
	arnSMSSender   = "arn:aws:lambda:us-east-1:000000000000:function:sms-sender"
	senderKeyARN   = "arn:aws:kms:us-east-1:000000000000:key/1234abcd-12ab-34cd-56ef-1234567890ab"

	senderConfig = `{"KMSKeyID":"` + senderKeyARN + `",` +
		`"CustomEmailSender":{"LambdaArn":"` + arnEmailSender + `","LambdaVersion":"V1_0"},` +
		`"CustomSMSSender":{"LambdaArn":"` + arnSMSSender + `","LambdaVersion":"V1_0"}}`
)

// recordingEncrypter stands in for KMS: the message it returns is the
// plaintext with a prefix, so a test can read back what Cognito encrypted.
// The KMS package's golden test covers the real message format.
type recordingEncrypter struct {
	mu    sync.Mutex
	keyID string
	ctx   map[string]string
}

func (e *recordingEncrypter) encrypt(keyID string, plaintext []byte, ctx map[string]string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.keyID, e.ctx = keyID, ctx
	return append([]byte("esdk:"), plaintext...), nil
}

func senderFixture(t *testing.T, lambdaConfig string) (*fixture, *fakeLambdas, *recordingEncrypter) {
	t.Helper()
	f, fl := triggerFixture(t, lambdaConfig)
	enc := &recordingEncrypter{}
	f.svc.SetMessageEncrypter(enc.encrypt)
	return f, fl, enc
}

// lastSend waits for custom senders to finish and returns the last event and
// the code it carried.
func lastSend(t *testing.T, f *fixture, fl *fakeLambdas) (map[string]any, string) {
	t.Helper()
	f.svc.WaitForSenders()
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if len(fl.events) == 0 {
		t.Fatal("no custom sender event")
	}
	e := fl.events[len(fl.events)-1]
	raw, err := base64.StdEncoding.DecodeString(e["request"].(map[string]any)["code"].(string))
	if err != nil {
		t.Fatalf("code is not base64: %v", err)
	}
	return e, string(raw[len("esdk:"):])
}

func TestCustomEmailSenderGetsEncryptedSignUpCodes(t *testing.T) {
	f, fl, enc := senderFixture(t, senderConfig)
	must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{
		ClientId: f.client.ClientId, Username: "ann", Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: "ann@example.com"}},
		ClientMetadata: map[string]string{"journey": "apply"},
	}))
	e, code := lastSend(t, f, fl)

	if e["triggerSource"] != "CustomEmailSender_SignUp" || e["version"] != "1" || e["userPoolId"] != f.poolID || e["userName"] != "ann" {
		t.Fatalf("event %v", e)
	}
	req := e["request"].(map[string]any)
	if req["type"] != "customEmailSenderRequestV1" || req["userAttributes"].(map[string]any)["email"] != "ann@example.com" {
		t.Fatalf("request %v", req)
	}
	if req["clientMetadata"].(map[string]any)["journey"] != "apply" {
		t.Fatalf("clientMetadata %v", req["clientMetadata"])
	}
	if code != f.code(t, "ann", purposeSignUp) {
		t.Fatalf("encrypted code %q is not the pending sign-up code", code)
	}
	if enc.keyID != senderKeyARN || enc.ctx["userpool-id"] != f.poolID {
		t.Fatalf("encrypted with key %q context %v", enc.keyID, enc.ctx)
	}

	must[*CodeDeliveryOutput](t)(f.svc.ResendConfirmationCode(&ClientUserInput{ClientId: f.client.ClientId, Username: "ann"}))
	e, code = lastSend(t, f, fl)
	if e["triggerSource"] != "CustomEmailSender_ResendCode" || code != f.code(t, "ann", purposeSignUp) {
		t.Fatalf("resend event %v code %q", e["triggerSource"], code)
	}
	if _, ok := e["request"].(map[string]any)["clientMetadata"]; ok {
		t.Fatal("ResendCode events carry no clientMetadata")
	}
}

func TestCustomSendersCoverResetAndAdminCreateUser(t *testing.T) {
	f, fl, _ := senderFixture(t, senderConfig)
	f.signUpConfirmed(t, "bo", "bo@example.com")
	f.svc.WaitForSenders() // senders are async; keep the sign-up event out of the way

	must[*CodeDeliveryOutput](t)(f.svc.ForgotPassword(&ClientUserInput{ClientId: f.client.ClientId, Username: "bo"}))
	e, code := lastSend(t, f, fl)
	if e["triggerSource"] != "CustomEmailSender_ForgotPassword" || code != f.code(t, "bo", purposeReset) {
		t.Fatalf("forgot password event %v code %q", e["triggerSource"], code)
	}

	// Temporary passwords are HTML-escaped, as Cognito does.
	must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{
		UserPoolId: f.poolID, Username: "cy", TemporaryPassword: "Temp<Pass1!",
		UserAttributes: []AttributeType{{Name: "email", Value: "cy@example.com"}},
	}))
	e, code = lastSend(t, f, fl)
	if e["triggerSource"] != "CustomEmailSender_AdminCreateUser" || code != "Temp&lt;Pass1!" {
		t.Fatalf("admin create event %v code %q", e["triggerSource"], code)
	}

	// A user with only a phone number goes to the SMS sender.
	must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{
		UserPoolId: f.poolID, Username: "di", TemporaryPassword: "TempPass1!",
		UserAttributes: []AttributeType{{Name: "phone_number", Value: "+447700900123"}},
	}))
	e, _ = lastSend(t, f, fl)
	if e["triggerSource"] != "CustomSMSSender_AdminCreateUser" || e["request"].(map[string]any)["type"] != "customSMSSenderRequestV1" {
		t.Fatalf("sms event %v", e)
	}

	before := len(fl.sources())
	must[*AdminCreateUserOutput](t)(f.svc.AdminCreateUser(&AdminCreateUserInput{
		UserPoolId: f.poolID, Username: "ed", TemporaryPassword: "TempPass1!", MessageAction: "SUPPRESS",
		UserAttributes: []AttributeType{{Name: "email", Value: "ed@example.com"}},
	}))
	f.svc.WaitForSenders()
	if len(fl.sources()) != before {
		t.Fatal("MessageAction SUPPRESS still invoked the sender")
	}
}

func TestCustomSenderFailureDoesNotFailTheCall(t *testing.T) {
	f, fl, _ := senderFixture(t, senderConfig)
	fl.handlers[arnEmailSender] = func(map[string]any) error { return errors.New("smtp down") }
	must[*SignUpOutput](t)(f.svc.SignUp(&SignUpInput{
		ClientId: f.client.ClientId, Username: "fay", Password: testPassword,
		UserAttributes: []AttributeType{{Name: "email", Value: "fay@example.com"}},
	}))
	f.svc.WaitForSenders()
	if got := fl.sources(); len(got) != 1 || got[0] != "CustomEmailSender_SignUp" {
		t.Fatalf("sources %v", got)
	}
	f.code(t, "fay", purposeSignUp) // still issued
}

func TestCustomSenderNeedsKMSKeyID(t *testing.T) {
	f, fl, _ := senderFixture(t, `{"CustomEmailSender":{"LambdaArn":"`+arnEmailSender+`","LambdaVersion":"V1_0"}}`)
	f.signUpConfirmed(t, "gus", "gus@example.com")
	f.svc.WaitForSenders()
	if got := fl.sources(); len(got) != 0 {
		t.Fatalf("sender ran without a KMSKeyID: %v", got)
	}
}
