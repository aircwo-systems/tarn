package cognito

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"
)

// authChallenge is the server side of a pending challenge, keyed by the
// Session string returned to the client.
type authChallenge struct {
	name     string // NEW_PASSWORD_REQUIRED or PASSWORD_VERIFIER
	poolID   string
	clientID string
	userKey  string
	srp      *srpServerState
	expires  time.Time
}

type InitiateAuthInput struct {
	UserPoolId     string            `json:"UserPoolId"` // AdminInitiateAuth only
	ClientId       string            `json:"ClientId"`
	AuthFlow       string            `json:"AuthFlow"`
	AuthParameters map[string]string `json:"AuthParameters"`
	ClientMetadata map[string]string `json:"ClientMetadata"`
}

type AuthOutput struct {
	ChallengeName        string                    `json:"ChallengeName,omitempty"`
	Session              string                    `json:"Session,omitempty"`
	ChallengeParameters  map[string]string         `json:"ChallengeParameters"`
	AuthenticationResult *AuthenticationResultType `json:"AuthenticationResult,omitempty"`
}

func (s *Service) InitiateAuth(in *InitiateAuthInput) (*AuthOutput, error) {
	switch in.AuthFlow {
	case "USER_PASSWORD_AUTH", "USER_SRP_AUTH", "REFRESH_TOKEN_AUTH", "REFRESH_TOKEN", "CUSTOM_AUTH":
	default:
		return nil, invalidParameter("Initiate Auth method not supported.")
	}
	s.mu.RLock()
	p, c, err := s.lookupClient(in.ClientId)
	c = snapshot(c)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return s.initiateAuth(p, c, in)
}

// snapshot copies a client while the caller holds s.mu, so an auth flow can
// keep reading it after unlocking. UpdateUserPoolClient replaces fields rather
// than mutating them, so a shallow copy is enough.
func snapshot(c *UserPoolClientType) *UserPoolClientType {
	if c == nil {
		return nil
	}
	cc := *c
	return &cc
}

func (s *Service) AdminInitiateAuth(in *InitiateAuthInput) (*AuthOutput, error) {
	switch in.AuthFlow {
	case "ADMIN_USER_PASSWORD_AUTH", "ADMIN_NO_SRP_AUTH", "USER_SRP_AUTH", "REFRESH_TOKEN_AUTH", "REFRESH_TOKEN", "CUSTOM_AUTH":
	default:
		return nil, invalidParameter("Initiate Auth method not supported.")
	}
	s.mu.RLock()
	p, c, err := s.poolClient(in.UserPoolId, in.ClientId)
	c = snapshot(c)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return s.initiateAuth(p, c, in)
}

func (s *Service) initiateAuth(p *pool, c *UserPoolClientType, in *InitiateAuthInput) (*AuthOutput, error) {
	if !allowsFlow(c, in.AuthFlow) {
		return nil, flowNotEnabled(in.AuthFlow)
	}
	params := in.AuthParameters
	switch in.AuthFlow {
	case "USER_PASSWORD_AUTH", "ADMIN_USER_PASSWORD_AUTH", "ADMIN_NO_SRP_AUTH":
		return s.passwordAuth(p, c, params["USERNAME"], params["PASSWORD"], params["SECRET_HASH"], in.ClientMetadata)
	case "USER_SRP_AUTH":
		return s.srpAuth(p, c, params, in.ClientMetadata)
	case "REFRESH_TOKEN_AUTH", "REFRESH_TOKEN":
		return s.refreshAuth(p, c, params)
	default:
		return nil, invalidParameter("Custom auth lambda trigger is not configured for the user pool.")
	}
}

// userMissing is the error for an unknown user, which clients with
// PreventUserExistenceErrors hide behind the wrong-password error.
func userMissing(c *UserPoolClientType) error {
	if c.PreventUserExistenceErrors == "ENABLED" {
		return errIncorrectPassword
	}
	return errUserNotFound
}

// passwordAuth checks a plaintext password. The bcrypt comparison and the
// pre authentication trigger run outside the service lock.
func (s *Service) passwordAuth(p *pool, c *UserPoolClientType, login, password, hash string, meta map[string]string) (*AuthOutput, error) {
	s.mu.RLock()
	u := p.findUser(login)
	var pwHash string
	var enabled bool
	var username, sub string
	var preAuth *triggerCall
	if u != nil {
		pwHash, enabled, username, sub = u.PasswordHash, u.Enabled, u.Username, u.Sub
		preAuth = preAuthenticationCall(p, u, c.ClientId, meta)
	}
	s.mu.RUnlock()

	if err := checkSecretHash(c, hash, login, username, sub); err != nil {
		return nil, err
	}
	if u == nil {
		return nil, userMissing(c)
	}
	if !enabled {
		return nil, errUserDisabled
	}
	if _, err := s.invoke(preAuth); err != nil {
		return nil, err
	}
	if !passwordMatches(pwHash, password) {
		return nil, errIncorrectPassword
	}

	s.mu.Lock()
	var (
		out *AuthOutput
		job *tokenJob
		err error
	)
	if p.Users[p.userKey(username)] != u {
		err = userMissing(c)
	} else {
		out, job, err = s.completeAuth(p, c, u, false, "Authentication")
	}
	s.mu.Unlock()
	return s.finishAuth(out, job, err, meta)
}

// completeAuth runs after the password is proven: it applies the user's
// status and MFA, then returns either the next challenge or a token job for
// finishAuth. source is the TokenGeneration_ trigger source suffix. Callers
// hold s.mu.
func (s *Service) completeAuth(p *pool, c *UserPoolClientType, u *user, mfaDone bool, source string) (*AuthOutput, *tokenJob, error) {
	switch u.Status {
	case StatusUnconfirmed:
		return nil, nil, errUserNotConfirmed
	case StatusResetRequired:
		return nil, nil, errResetRequired
	case StatusForceChangePassword:
		if !u.TempExpires.IsZero() && s.now().After(u.TempExpires) {
			return nil, nil, errTempPasswordExpired
		}
		attrs := make(map[string]string, len(u.Attributes))
		for k, v := range u.Attributes {
			if k != "sub" {
				attrs[k] = v
			}
		}
		attrJSON, _ := json.Marshal(attrs)
		required := []string{}
		for _, sa := range p.Config.SchemaAttributes {
			if sa.Required && sa.Name != "sub" && u.Attributes[sa.Name] == "" {
				required = append(required, "userAttributes."+sa.Name)
			}
		}
		reqJSON, _ := json.Marshal(required)
		return s.challenge(p, c, u, "NEW_PASSWORD_REQUIRED", nil, map[string]string{
			"USER_ID_FOR_SRP":    u.Username,
			"requiredAttributes": string(reqJSON),
			"userAttributes":     string(attrJSON),
		}), nil, nil
	}
	if !mfaDone {
		if ch := s.mfaChallenge(p, c, u); ch != nil {
			return ch, nil, nil
		}
	}
	sess, refresh := s.startSession(p, c, u)
	return &AuthOutput{ChallengeParameters: map[string]string{}}, s.prepareTokens(p, c, u, sess, refresh, source), nil
}

// finishAuth issues the tokens for job, outside the lock, and runs the post
// authentication trigger for sign-ins. Its errors are logged rather than
// failing the sign-in.
func (s *Service) finishAuth(out *AuthOutput, job *tokenJob, err error, meta map[string]string) (*AuthOutput, error) {
	if err != nil {
		return nil, err
	}
	if job == nil {
		return out, nil
	}
	result, err := s.issueTokens(job)
	if err != nil {
		return nil, err
	}
	out.AuthenticationResult = result
	if job.source != "RefreshTokens" && job.lambda.PostAuthentication != "" {
		_, err := s.invoke(&triggerCall{
			name: "PostAuthentication", arn: job.lambda.PostAuthentication, source: "PostAuthentication_Authentication",
			poolID: job.poolID, username: job.username, clientID: job.clientID,
			request: map[string]any{"userAttributes": job.userAttrs, "newDeviceUsed": false, "clientMetadata": clientMetadata(meta)},
		})
		if err != nil {
			log.Printf("[cognito] %v", err)
		}
	}
	return out, nil
}

// challenge records a pending challenge and returns it. Callers hold s.mu.
func (s *Service) challenge(p *pool, c *UserPoolClientType, u *user, name string, srp *srpServerState, params map[string]string) *AuthOutput {
	now := s.now()
	for id, ch := range s.challenges {
		if now.After(ch.expires) {
			delete(s.challenges, id)
		}
	}
	validity := time.Duration(c.AuthSessionValidity) * time.Minute
	if validity <= 0 {
		validity = 3 * time.Minute
	}
	session := newOpaque(160)
	s.challenges[session] = &authChallenge{
		name: name, poolID: p.Config.Id, clientID: c.ClientId,
		userKey: p.userKey(u.Username), srp: srp, expires: now.Add(validity),
	}
	return &AuthOutput{ChallengeName: name, Session: session, ChallengeParameters: params}
}

func (s *Service) srpAuth(p *pool, c *UserPoolClientType, params, meta map[string]string) (*AuthOutput, error) {
	login := params["USERNAME"]
	s.mu.RLock()
	u := p.findUser(login)
	username, sub := "", ""
	var preAuth *triggerCall
	if u != nil {
		username, sub = u.Username, u.Sub
		preAuth = preAuthenticationCall(p, u, c.ClientId, meta)
	}
	s.mu.RUnlock()
	if err := checkSecretHash(c, params["SECRET_HASH"], login, username, sub); err != nil {
		return nil, err
	}
	if u == nil {
		return nil, userMissing(c)
	}
	if _, err := s.invoke(preAuth); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Users[p.userKey(username)] != u {
		return nil, userMissing(c)
	}
	if !u.Enabled {
		return nil, errUserDisabled
	}
	srp, ok := startSRP(params["SRP_A"], u.SRPSalt, u.SRPVerifier)
	if !ok {
		return nil, invalidParameter("Invalid SRP_A value.")
	}
	salt, b, block := srp.challengeParameters()
	return s.challenge(p, c, u, "PASSWORD_VERIFIER", srp, map[string]string{
		"SALT":            salt,
		"SRP_B":           b,
		"SECRET_BLOCK":    block,
		"USERNAME":        u.Username,
		"USER_ID_FOR_SRP": u.Username,
	}), nil
}

func (s *Service) refreshAuth(p *pool, c *UserPoolClientType, params map[string]string) (*AuthOutput, error) {
	s.mu.Lock()
	job, err := s.refreshLocked(p, c, params)
	s.mu.Unlock()
	return s.finishAuth(&AuthOutput{ChallengeParameters: map[string]string{}}, job, err, nil)
}

func (s *Service) refreshLocked(p *pool, c *UserPoolClientType, params map[string]string) (*tokenJob, error) {
	ref, ok := p.refresh[hashToken(params["REFRESH_TOKEN"])]
	if !ok {
		return nil, errInvalidRefreshToken
	}
	u := p.Users[ref.userKey]
	if u == nil {
		return nil, errInvalidRefreshToken
	}
	sess := u.Sessions[ref.originJTI]
	if sess == nil || sess.ClientID != c.ClientId {
		return nil, errInvalidRefreshToken
	}
	if err := checkSecretHash(c, params["SECRET_HASH"], params["USERNAME"], u.Username, u.Sub); err != nil {
		return nil, err
	}
	if sess.Revoked {
		return nil, errRefreshTokenRevoked
	}
	if s.now().After(sess.Expires) {
		return nil, notAuthorized("Refresh Token has expired")
	}
	if !u.Enabled {
		return nil, errUserDisabled
	}
	return s.prepareTokens(p, c, u, sess, "", "RefreshTokens"), nil
}

type RespondToAuthChallengeInput struct {
	UserPoolId         string            `json:"UserPoolId"` // AdminRespondToAuthChallenge only
	ClientId           string            `json:"ClientId"`
	ChallengeName      string            `json:"ChallengeName"`
	Session            string            `json:"Session"`
	ChallengeResponses map[string]string `json:"ChallengeResponses"`
	ClientMetadata     map[string]string `json:"ClientMetadata"`
}

func (s *Service) RespondToAuthChallenge(in *RespondToAuthChallengeInput) (*AuthOutput, error) {
	s.mu.RLock()
	p, c, err := s.lookupClient(in.ClientId)
	c = snapshot(c)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return s.respond(p, c, in)
}

func (s *Service) AdminRespondToAuthChallenge(in *RespondToAuthChallengeInput) (*AuthOutput, error) {
	s.mu.RLock()
	p, c, err := s.poolClient(in.UserPoolId, in.ClientId)
	c = snapshot(c)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return s.respond(p, c, in)
}

func (s *Service) respond(p *pool, c *UserPoolClientType, in *RespondToAuthChallengeInput) (*AuthOutput, error) {
	// NEW_PASSWORD_REQUIRED hashes a password; do it before locking.
	var creds credentials
	newPassword := in.ChallengeResponses["NEW_PASSWORD"]
	if in.ChallengeName == "NEW_PASSWORD_REQUIRED" {
		s.mu.RLock()
		ch := s.challenges[in.Session]
		var username string
		if ch != nil {
			if u := p.Users[ch.userKey]; u != nil {
				username = u.Username
			}
		}
		policy := *p.passwordPolicy()
		s.mu.RUnlock()
		if username == "" {
			return nil, errInvalidSession
		}
		if err := checkPasswordPolicy(&policy, newPassword); err != nil {
			return nil, err
		}
		var err error
		if creds, err = newCredentials(p.Config.Id, username, newPassword); err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	out, job, err := s.respondLocked(p, c, in, creds)
	s.mu.Unlock()
	return s.finishAuth(out, job, err, in.ClientMetadata)
}

func (s *Service) respondLocked(p *pool, c *UserPoolClientType, in *RespondToAuthChallengeInput, creds credentials) (*AuthOutput, *tokenJob, error) {
	ch := s.challenges[in.Session]
	if ch == nil || ch.name != in.ChallengeName || ch.clientID != c.ClientId || ch.poolID != p.Config.Id || s.now().After(ch.expires) {
		return nil, nil, errInvalidSession
	}
	delete(s.challenges, in.Session)
	u := p.Users[ch.userKey]
	if u == nil {
		return nil, nil, userMissing(c)
	}
	resp := in.ChallengeResponses
	if err := checkSecretHash(c, resp["SECRET_HASH"], resp["USERNAME"], u.Username, u.Sub); err != nil {
		return nil, nil, err
	}

	switch in.ChallengeName {
	case "PASSWORD_VERIFIER":
		if !ch.srp.verify(p.Config.Id, u.Username, resp["PASSWORD_CLAIM_SECRET_BLOCK"], resp["TIMESTAMP"], resp["PASSWORD_CLAIM_SIGNATURE"]) {
			return nil, nil, errIncorrectPassword
		}
		return s.completeAuth(p, c, u, false, "Authentication")

	case mfaSMS, mfaEmail:
		code := resp["SMS_MFA_CODE"]
		if in.ChallengeName == mfaEmail {
			code = resp["EMAIL_OTP_CODE"]
		}
		if err := s.consumeCode(u, purposeMFA, code); err != nil {
			if err == errCodeMismatch {
				// A wrong code leaves the session usable for another try.
				s.challenges[in.Session] = ch
				return nil, nil, errMFACodeMismatch
			}
			return nil, nil, err
		}
		return s.completeAuth(p, c, u, true, "Authentication")

	case "NEW_PASSWORD_REQUIRED":
		var attrs []AttributeType
		for k, v := range resp {
			if name, ok := strings.CutPrefix(k, "userAttributes."); ok {
				attrs = append(attrs, AttributeType{Name: name, Value: v})
			}
		}
		sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
		if err := checkWritable(c, attrs); err != nil {
			return nil, nil, err
		}
		if err := p.validateAttributes(attrs, false); err != nil {
			return nil, nil, err
		}
		u.applyAttributes(attrs)
		for _, sa := range p.Config.SchemaAttributes {
			if sa.Required && sa.Name != "sub" && u.Attributes[sa.Name] == "" {
				return nil, nil, invalidParameter("Invalid attributes given, %s is missing", sa.Name)
			}
		}
		p.applyCredentials(u, creds, false, s.now())
		u.Status = StatusConfirmed
		delete(u.Codes, purposeInvite)
		u.Modified = s.now()
		s.markDirty()
		return s.completeAuth(p, c, u, false, "NewPasswordChallenge")
	}
	return nil, nil, invalidParameter("Unsupported challenge %s.", in.ChallengeName)
}
