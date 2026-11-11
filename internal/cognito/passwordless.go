package cognito

import (
	"crypto/subtle"
	"log"
	"slices"
)

// choiceState is ephemeral proof for USER_AUTH. OTPs never enter persisted
// user code slots or the password-MFA state machine.
type choiceState struct {
	username, sub          string
	offered                []string
	simulated              bool
	attribute, destination string
	code                   *pendingCode
	confirmation           bool
}

func firstFactors(st *UserPoolSettings) []string {
	if st.Policies == nil || st.Policies.SignInPolicy == nil {
		return []string{"PASSWORD"}
	}
	return st.Policies.SignInPolicy.AllowedFirstAuthFactors
}

func validateSignInSettings(st *UserPoolSettings) error {
	if st.UserPoolTier != "" && !slices.Contains([]string{"LITE", "ESSENTIALS", "PLUS"}, st.UserPoolTier) {
		return invalidParameter("Invalid UserPoolTier.")
	}
	if st.MfaConfiguration != "" && !slices.Contains([]string{"OFF", "ON", "OPTIONAL"}, st.MfaConfiguration) {
		return invalidParameter("Invalid MfaConfiguration.")
	}
	factors := firstFactors(st)
	if len(factors) == 0 || len(factors) > 4 {
		return invalidParameter("AllowedFirstAuthFactors must contain between 1 and 4 factors.")
	}
	seen := map[string]bool{}
	for _, factor := range factors {
		if !slices.Contains([]string{"PASSWORD", "EMAIL_OTP", "SMS_OTP", "WEB_AUTHN"}, factor) || seen[factor] {
			return invalidParameter("Invalid AllowedFirstAuthFactors.")
		}
		seen[factor] = true
		if factor != "PASSWORD" && st.UserPoolTier == "LITE" {
			return newError("FeatureUnavailableInTierException", "Passwordless authentication requires Essentials or Plus.")
		}
		if (factor == "EMAIL_OTP" || factor == "SMS_OTP") && st.MfaConfiguration == "ON" {
			return invalidParameter("Required MFA is incompatible with OTP first factors.")
		}
	}
	return nil
}

func validateChoiceClient(p *pool, st *UserPoolClientSettings) error {
	if st.AuthSessionValidity < 3 || st.AuthSessionValidity > 15 {
		return invalidParameter("AuthSessionValidity must be between 3 and 15 minutes.")
	}
	if slices.Contains(st.ExplicitAuthFlows, "ALLOW_USER_AUTH") && p.Config.UserPoolTier == "LITE" {
		return newError("FeatureUnavailableInTierException", "USER_AUTH requires Essentials or Plus.")
	}
	return nil
}

func (p *pool) choiceFactors(u *user) []string {
	out := []string{}
	for _, factor := range firstFactors(&p.Config.UserPoolSettings) {
		switch factor {
		case "PASSWORD":
			if u == nil || u.PasswordHash != "" {
				out = append(out, "PASSWORD", "PASSWORD_SRP")
			}
		case "EMAIL_OTP", "SMS_OTP":
			if p.mfaMode() == "ON" || u != nil && p.mfaMode() == "OPTIONAL" && (u.mfa().SMS || u.mfa().Email) {
				continue
			}
			if u != nil && (u.Status == StatusForceChangePassword || u.Status == StatusResetRequired) {
				continue
			}
			attr := otpAttribute(factor)
			if u == nil || u.Attributes[attr] != "" {
				out = append(out, factor)
			}
		}
	}
	return out
}

func otpAttribute(factor string) string {
	if factor == "SMS_OTP" {
		return "phone_number"
	}
	return "email"
}

// choiceChallenge snapshots identity and contact information so deleting a user
// or changing their destination cannot transfer an outstanding proof.
func (s *Service) choiceChallenge(p *pool, c *UserPoolClientType, u *user, factor string, state *choiceState, meta map[string]string) *AuthOutput {
	out := s.challenge(p, c, u, factor, nil, map[string]string{"USERNAME": u.Username})
	ch := s.challenges[out.Session]
	copyState := *state
	copyState.code = nil
	ch.choice = &copyState
	if factor == "SELECT_CHALLENGE" {
		out.AvailableChallenges = slices.Clone(state.offered)
	}
	if factor == "EMAIL_OTP" || factor == "SMS_OTP" {
		attr := otpAttribute(factor)
		ch.choice.attribute, ch.choice.destination = attr, u.Attributes[attr]
		code := newCode()
		if s.cfg.CognitoFixedCode != "" {
			code = s.cfg.CognitoFixedCode
		}
		ch.choice.code = &pendingCode{Code: code, Attribute: attr, Created: s.now(), Expires: ch.expires}
		if !state.simulated {
			log.Printf("[cognito] code pool=%s user=%s purpose=auth:%s code=%s", p.Config.Id, u.Username, out.Session, code)
			s.sendCustom(p, u, attr, code, codeEvent{"Authentication", c.ClientId, meta})
		}
		if d := deliveryDetails(u, attr); d != nil {
			out.ChallengeParameters["CODE_DELIVERY_DELIVERY_MEDIUM"] = d.DeliveryMedium
			out.ChallengeParameters["CODE_DELIVERY_DESTINATION"] = d.Destination
		}
	}
	return out
}

func (s *Service) userAuth(p *pool, c *UserPoolClientType, in *InitiateAuthInput) (*AuthOutput, error) {
	if in.Session != "" {
		return s.confirmationAuth(p, c, in)
	}
	params := in.AuthParameters
	if params["USERNAME"] == "" {
		return nil, invalidParameter("Missing required parameter USERNAME")
	}
	s.mu.Lock()
	if p.Config.UserPoolTier == "LITE" {
		s.mu.Unlock()
		return nil, newError("FeatureUnavailableInTierException", "USER_AUTH requires Essentials or Plus.")
	}
	u := p.findUser(params["USERNAME"])
	username, sub := "", ""
	if u != nil {
		username, sub = u.Username, u.Sub
	}
	if err := checkSecretHash(c, params["SECRET_HASH"], params["USERNAME"], username, sub); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if u == nil && c.PreventUserExistenceErrors != "ENABLED" {
		s.mu.Unlock()
		return nil, errUserNotFound
	}
	if u != nil && !u.Enabled {
		s.mu.Unlock()
		return nil, errUserDisabled
	}
	state := &choiceState{username: username, sub: sub, simulated: u == nil, offered: p.choiceFactors(u)}
	if u == nil {
		u = &user{Username: params["USERNAME"], Attributes: map[string]string{"email": params["USERNAME"], "phone_number": "+00000000000"}}
		state.username = u.Username
	}
	if len(state.offered) == 0 {
		s.mu.Unlock()
		return nil, notAuthorized("No available authentication methods.")
	}
	preferred := params["PREFERRED_CHALLENGE"]
	// An unavailable preference falls back to selection.
	factor := "SELECT_CHALLENGE"
	if slices.Contains(state.offered, preferred) {
		factor = preferred
	}
	out := s.choiceChallenge(p, c, u, factor, state, in.ClientMetadata)
	s.mu.Unlock()
	if factor == "PASSWORD" && params["PASSWORD"] != "" || factor == "PASSWORD_SRP" && params["SRP_A"] != "" {
		return s.respondChoice(p, c, &RespondToAuthChallengeInput{ClientId: c.ClientId, Session: out.Session, ChallengeName: factor, ChallengeResponses: params, ClientMetadata: in.ClientMetadata})
	}
	return out, nil
}

func (s *Service) choiceUser(p *pool, c *UserPoolClientType, id string) (*authChallenge, *user, error) {
	ch := s.challenges[id]
	if ch == nil || ch.choice == nil || ch.clientID != c.ClientId || ch.poolID != p.Config.Id || !s.now().Before(ch.expires) || s.pools[p.Config.Id] != p || p.Clients[c.ClientId] == nil {
		return nil, nil, errInvalidSession
	}
	if !allowsFlow(p.Clients[c.ClientId], "USER_AUTH") || p.Config.UserPoolTier == "LITE" {
		return nil, nil, errInvalidSession
	}
	state := ch.choice
	u := p.Users[ch.userKey]
	if state.simulated {
		return ch, nil, nil
	}
	if u == nil || u.Sub != state.sub {
		return nil, nil, errInvalidSession
	}
	if !u.Enabled {
		return nil, nil, errUserDisabled
	}
	return ch, u, nil
}

func (s *Service) respondChoice(p *pool, c *UserPoolClientType, in *RespondToAuthChallengeInput) (*AuthOutput, error) {
	s.mu.Lock()
	ch, u, err := s.choiceUser(p, c, in.Session)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	state := ch.choice
	resp := in.ChallengeResponses
	login := resp["USERNAME"]
	if login == "" || state.simulated && login != state.username || !state.simulated && p.findUser(login) != u {
		s.mu.Unlock()
		return nil, errInvalidSession
	}
	if err := checkSecretHash(c, resp["SECRET_HASH"], login, state.username, state.sub); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	factor := in.ChallengeName
	if ch.name == "SELECT_CHALLENGE" {
		if factor == "SELECT_CHALLENGE" {
			factor = resp["ANSWER"]
		}
		if !slices.Contains(state.offered, factor) {
			s.mu.Unlock()
			return nil, invalidParameter("Selected challenge is not available.")
		}
	} else if ch.name != factor {
		s.mu.Unlock()
		return nil, errInvalidSession
	}
	eligible := factor
	if eligible == "PASSWORD_VERIFIER" {
		eligible = "PASSWORD_SRP"
	}
	if !slices.Contains(p.choiceFactors(u), eligible) {
		s.mu.Unlock()
		return nil, errInvalidSession
	}
	if state.simulated && u == nil {
		u = &user{Username: state.username, Attributes: map[string]string{"email": state.username, "phone_number": "+00000000000"}}
	}
	if ch.name == "SELECT_CHALLENGE" || factor == "PASSWORD" || factor == "PASSWORD_SRP" {
		delete(s.challenges, in.Session)
		switch factor {
		case "PASSWORD":
			if resp["PASSWORD"] != "" {
				if state.simulated {
					s.mu.Unlock()
					return nil, errIncorrectPassword
				}
				s.mu.Unlock()
				return s.passwordAuth(p, c, login, resp["PASSWORD"], resp["SECRET_HASH"], in.ClientMetadata)
			}
		case "PASSWORD_SRP":
			if resp["SRP_A"] != "" {
				salt, verifier := u.SRPSalt, u.SRPVerifier
				if state.simulated {
					salt, verifier = newSRPVerifier(p.Config.Id, u.Username, newOpaque(32))
				}
				srp, ok := startSRP(resp["SRP_A"], salt, verifier)
				if !ok {
					s.mu.Unlock()
					return nil, invalidParameter("Invalid SRP_A value.")
				}
				out := s.choiceChallenge(p, c, u, "PASSWORD_VERIFIER", state, in.ClientMetadata)
				s.challenges[out.Session].srp = srp
				salt, b, block := srp.challengeParameters()
				out.ChallengeParameters = map[string]string{"SALT": salt, "SRP_B": b, "SECRET_BLOCK": block, "USERNAME": u.Username, "USER_ID_FOR_SRP": u.Username}
				pre := preAuthenticationCall(p, u, c.ClientId, in.ClientMetadata)
				s.mu.Unlock()
				if _, err := s.invoke(pre); err != nil {
					s.mu.Lock()
					delete(s.challenges, out.Session)
					s.mu.Unlock()
					return nil, err
				}
				return out, nil
			}
		}
		out := s.choiceChallenge(p, c, u, factor, state, in.ClientMetadata)
		s.mu.Unlock()
		return out, nil
	}
	if factor == "PASSWORD_VERIFIER" {
		delete(s.challenges, in.Session)
		if state.simulated || ch.srp == nil || !ch.srp.verify(p.Config.Id, u.Username, resp["PASSWORD_CLAIM_SECRET_BLOCK"], resp["TIMESTAMP"], resp["PASSWORD_CLAIM_SIGNATURE"]) {
			s.mu.Unlock()
			return nil, errIncorrectPassword
		}
		out, job, err := s.completeAuth(p, c, u, false, "Authentication")
		s.mu.Unlock()
		return s.finishAuth(out, job, err, in.ClientMetadata)
	}
	if state.code == nil || state.confirmation {
		s.mu.Unlock()
		return nil, errInvalidSession
	}
	if state.simulated || subtle.ConstantTimeCompare([]byte(state.code.Code), []byte(resp[factor+"_CODE"])) != 1 {
		s.mu.Unlock()
		return nil, errCodeMismatch
	}
	if u.Attributes[state.attribute] != state.destination {
		s.mu.Unlock()
		return nil, errInvalidSession
	}
	pre := preAuthenticationCall(p, u, c.ClientId, in.ClientMetadata)
	// Reserve this proof before invoking Lambda. Concurrent responses cannot win twice.
	delete(s.challenges, in.Session)
	s.mu.Unlock()
	if _, err := s.invoke(pre); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if p.Users[ch.userKey] != u || !u.Enabled || u.Attributes[state.attribute] != state.destination || !slices.Contains(p.choiceFactors(u), factor) || !s.now().Before(ch.expires) || s.pools[p.Config.Id] != p || p.Clients[c.ClientId] == nil || !allowsFlow(p.Clients[c.ClientId], "USER_AUTH") {
		s.mu.Unlock()
		return nil, errInvalidSession
	}
	if err := p.claimAlias(u, state.attribute, false); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	wasUnconfirmed := u.Status == StatusUnconfirmed
	u.Attributes[state.attribute+"_verified"] = "true"
	if wasUnconfirmed {
		u.Status = StatusConfirmed
		delete(u.Codes, purposeSignUp)
	}
	u.Modified = s.now()
	s.markDirty()
	var post *triggerCall
	if wasUnconfirmed {
		post = postConfirmationCall(p, u, "PostConfirmation_ConfirmSignUp", c.ClientId, in.ClientMetadata)
	}
	out, job, err := s.completeAuth(p, c, u, true, "Authentication")
	s.mu.Unlock()
	if _, postErr := s.invoke(post); postErr != nil {
		return nil, postErr
	}
	return s.finishAuth(out, job, err, in.ClientMetadata)
}

func (s *Service) confirmationAuth(p *pool, c *UserPoolClientType, in *InitiateAuthInput) (*AuthOutput, error) {
	s.mu.Lock()
	ch, u, err := s.choiceUser(p, c, in.Session)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	state := ch.choice
	if !state.confirmation || u == nil || u.Status != StatusConfirmed || u.Attributes[state.attribute] != state.destination || !slices.Contains(p.choiceFactors(u), ch.name) || s.pools[p.Config.Id] != p || p.Clients[c.ClientId] == nil || !allowsFlow(p.Clients[c.ClientId], "USER_AUTH") {
		s.mu.Unlock()
		return nil, errInvalidSession
	}
	delete(s.challenges, in.Session)
	pre := preAuthenticationCall(p, u, c.ClientId, in.ClientMetadata)
	s.mu.Unlock()
	if _, err := s.invoke(pre); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if p.Users[ch.userKey] != u || !u.Enabled || !s.now().Before(ch.expires) || u.Attributes[state.attribute] != state.destination || !slices.Contains(p.choiceFactors(u), ch.name) || s.pools[p.Config.Id] != p || p.Clients[c.ClientId] == nil || !allowsFlow(p.Clients[c.ClientId], "USER_AUTH") {
		s.mu.Unlock()
		return nil, errInvalidSession
	}
	out, job, err := s.completeAuth(p, c, u, true, "Authentication")
	s.mu.Unlock()
	return s.finishAuth(out, job, err, in.ClientMetadata)
}

func authCodePurpose(session string) string { return "auth:" + session }
