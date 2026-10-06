package cognito

import (
	"encoding/json"
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
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return s.initiateAuth(p, c, in)
}

func (s *Service) AdminInitiateAuth(in *InitiateAuthInput) (*AuthOutput, error) {
	switch in.AuthFlow {
	case "ADMIN_USER_PASSWORD_AUTH", "ADMIN_NO_SRP_AUTH", "USER_SRP_AUTH", "REFRESH_TOKEN_AUTH", "REFRESH_TOKEN", "CUSTOM_AUTH":
	default:
		return nil, invalidParameter("Initiate Auth method not supported.")
	}
	s.mu.RLock()
	p, c, err := s.poolClient(in.UserPoolId, in.ClientId)
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
		return s.passwordAuth(p, c, params["USERNAME"], params["PASSWORD"], params["SECRET_HASH"])
	case "USER_SRP_AUTH":
		return s.srpAuth(p, c, params)
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

// passwordAuth checks a plaintext password. The bcrypt comparison runs
// outside the service lock so concurrent sign-ins don't queue behind it.
func (s *Service) passwordAuth(p *pool, c *UserPoolClientType, login, password, hash string) (*AuthOutput, error) {
	s.mu.RLock()
	u := p.findUser(login)
	var pwHash string
	var enabled bool
	var username, sub string
	if u != nil {
		pwHash, enabled, username, sub = u.PasswordHash, u.Enabled, u.Username, u.Sub
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
	if !passwordMatches(pwHash, password) {
		return nil, errIncorrectPassword
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Users[p.userKey(username)] != u {
		return nil, userMissing(c)
	}
	return s.completeAuth(p, c, u)
}

// completeAuth runs after the password is proven: it applies the user's
// status and either issues tokens or returns the next challenge. Callers
// hold s.mu.
func (s *Service) completeAuth(p *pool, c *UserPoolClientType, u *user) (*AuthOutput, error) {
	switch u.Status {
	case StatusUnconfirmed:
		return nil, errUserNotConfirmed
	case StatusResetRequired:
		return nil, errResetRequired
	case StatusForceChangePassword:
		if !u.TempExpires.IsZero() && s.now().After(u.TempExpires) {
			return nil, errTempPasswordExpired
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
		}), nil
	}
	sess, refresh := s.startSession(p, c, u)
	result, err := s.mintTokens(p, c, u, sess, refresh)
	if err != nil {
		return nil, err
	}
	return &AuthOutput{ChallengeParameters: map[string]string{}, AuthenticationResult: result}, nil
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

func (s *Service) srpAuth(p *pool, c *UserPoolClientType, params map[string]string) (*AuthOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	login := params["USERNAME"]
	u := p.findUser(login)
	username, sub := "", ""
	if u != nil {
		username, sub = u.Username, u.Sub
	}
	if err := checkSecretHash(c, params["SECRET_HASH"], login, username, sub); err != nil {
		return nil, err
	}
	if u == nil {
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
	defer s.mu.Unlock()
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
	result, err := s.mintTokens(p, c, u, sess, "")
	if err != nil {
		return nil, err
	}
	return &AuthOutput{ChallengeParameters: map[string]string{}, AuthenticationResult: result}, nil
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
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return s.respond(p, c, in)
}

func (s *Service) AdminRespondToAuthChallenge(in *RespondToAuthChallengeInput) (*AuthOutput, error) {
	s.mu.RLock()
	p, c, err := s.poolClient(in.UserPoolId, in.ClientId)
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
		s.mu.RUnlock()
		if username == "" {
			return nil, errInvalidSession
		}
		if err := checkPasswordPolicy(p.passwordPolicy(), newPassword); err != nil {
			return nil, err
		}
		var err error
		if creds, err = newCredentials(p.Config.Id, username, newPassword); err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.challenges[in.Session]
	if ch == nil || ch.name != in.ChallengeName || ch.clientID != c.ClientId || ch.poolID != p.Config.Id || s.now().After(ch.expires) {
		return nil, errInvalidSession
	}
	delete(s.challenges, in.Session)
	u := p.Users[ch.userKey]
	if u == nil {
		return nil, userMissing(c)
	}
	resp := in.ChallengeResponses
	if err := checkSecretHash(c, resp["SECRET_HASH"], resp["USERNAME"], u.Username, u.Sub); err != nil {
		return nil, err
	}

	switch in.ChallengeName {
	case "PASSWORD_VERIFIER":
		if !ch.srp.verify(p.Config.Id, u.Username, resp["PASSWORD_CLAIM_SECRET_BLOCK"], resp["TIMESTAMP"], resp["PASSWORD_CLAIM_SIGNATURE"]) {
			return nil, errIncorrectPassword
		}
		return s.completeAuth(p, c, u)

	case "NEW_PASSWORD_REQUIRED":
		var attrs []AttributeType
		for k, v := range resp {
			if name, ok := strings.CutPrefix(k, "userAttributes."); ok {
				attrs = append(attrs, AttributeType{Name: name, Value: v})
			}
		}
		sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
		if err := checkWritable(c, attrs); err != nil {
			return nil, err
		}
		if err := p.validateAttributes(attrs, false); err != nil {
			return nil, err
		}
		u.applyAttributes(attrs)
		for _, sa := range p.Config.SchemaAttributes {
			if sa.Required && sa.Name != "sub" && u.Attributes[sa.Name] == "" {
				return nil, invalidParameter("Invalid attributes given, %s is missing", sa.Name)
			}
		}
		p.applyCredentials(u, creds, false, s.now())
		u.Status = StatusConfirmed
		delete(u.Codes, purposeInvite)
		u.Modified = s.now()
		s.markDirty()
		return s.completeAuth(p, c, u)
	}
	return nil, invalidParameter("Unsupported challenge %s.", in.ChallengeName)
}
