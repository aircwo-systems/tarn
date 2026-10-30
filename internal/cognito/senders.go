package cognito

import (
	"encoding/base64"
	"html"
	"log"
)

// MessageEncrypter encrypts plaintext as an AWS Encryption SDK message under
// a KMS key, as Cognito does for custom sender triggers.
type MessageEncrypter func(keyID string, plaintext []byte, ctx map[string]string) ([]byte, error)

// SetMessageEncrypter lets the pool's CustomEmailSender and CustomSMSSender
// triggers run. Without one, codes are only logged and kept for the dashboard.
func (s *Service) SetMessageEncrypter(fn MessageEncrypter) { s.encrypter = fn }

// CustomSenderConfigType is the CustomEmailSender or CustomSMSSender member of
// LambdaConfig.
type CustomSenderConfigType struct {
	LambdaArn     string `json:"LambdaArn"`
	LambdaVersion string `json:"LambdaVersion"`
}

// codeEvent says why a code was issued, for custom sender triggers.
type codeEvent struct {
	source   string // trigger source suffix, e.g. "SignUp"
	clientID string
	metadata map[string]string
}

// Custom sender trigger sources that carry ClientMetadata, per the AWS docs.
var metadataSources = map[string]bool{"SignUp": true, "ForgotPassword": true, "Authentication": true}

// sendCustom hands a code to the pool's custom sender trigger for attribute,
// if one is configured. AWS invokes custom senders asynchronously, so the
// call is built under s.mu and run in the background; a failure is logged
// and does not fail the API call. Callers hold s.mu.
func (s *Service) sendCustom(p *pool, u *user, attribute, code string, ev codeEvent) {
	lc := p.lambdaConfig()
	name, kind, sender := "CustomEmailSender", "customEmailSenderRequestV1", lc.CustomEmailSender
	if attribute == "phone_number" {
		name, kind, sender = "CustomSMSSender", "customSMSSenderRequestV1", lc.CustomSMSSender
	}
	if sender == nil || sender.LambdaArn == "" {
		return
	}
	if lc.KMSKeyID == "" {
		log.Printf("[cognito] %s trigger %s skipped: pool %s has no KMSKeyID", name, sender.LambdaArn, p.Config.Id)
		return
	}
	if s.encrypter == nil {
		log.Printf("[cognito] %s trigger %s skipped: no KMS encrypter configured", name, sender.LambdaArn)
		return
	}
	if ev.source == "AdminCreateUser" {
		// Cognito HTML-escapes reserved characters in temporary passwords.
		code = html.EscapeString(code)
	}
	request := map[string]any{
		"type":           kind,
		"userAttributes": u.triggerAttributes(),
	}
	if metadataSources[ev.source] {
		request["clientMetadata"] = clientMetadata(ev.metadata)
	}
	call := &triggerCall{
		name: name, arn: sender.LambdaArn, version: "1", source: name + "_" + ev.source,
		poolID: p.Config.Id, username: u.Username, clientID: ev.clientID,
		request: request,
	}
	keyID, encryptionContext := lc.KMSKeyID, map[string]string{"userpool-id": p.Config.Id}

	s.senders.Add(1)
	go func() {
		defer s.senders.Done()
		msg, err := s.encrypter(keyID, []byte(code), encryptionContext)
		if err != nil {
			log.Printf("[cognito] %s trigger %s skipped: encrypt code with %s: %v", name, call.arn, keyID, err)
			return
		}
		call.request["code"] = base64.StdEncoding.EncodeToString(msg)
		if _, err := s.invoke(call); err != nil {
			log.Printf("[cognito] %s trigger %s: %v", name, call.arn, err)
		}
	}()
}

// WaitForSenders blocks until every custom sender trigger in flight has
// returned.
func (s *Service) WaitForSenders() { s.senders.Wait() }
