package cognito

import (
	"slices"

	"github.com/google/uuid"
)

// Public operations: called by apps with a ClientId or an access token and no
// AWS credentials.

type SignUpInput struct {
	ClientId       string            `json:"ClientId"`
	SecretHash     string            `json:"SecretHash"`
	Username       string            `json:"Username"`
	Password       string            `json:"Password"`
	UserAttributes []AttributeType   `json:"UserAttributes"`
	ValidationData []AttributeType   `json:"ValidationData"`
	ClientMetadata map[string]string `json:"ClientMetadata"`
}

type SignUpOutput struct {
	UserConfirmed       bool                     `json:"UserConfirmed"`
	CodeDeliveryDetails *CodeDeliveryDetailsType `json:"CodeDeliveryDetails,omitempty"`
	UserSub             string                   `json:"UserSub"`
}

func (s *Service) SignUp(in *SignUpInput) (*SignUpOutput, error) {
	s.mu.RLock()
	p, c, err := s.lookupClient(in.ClientId)
	if err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	sub := uuid.NewString()
	attrs := attrMap(in.UserAttributes)
	username, err := p.resolveNewUsername(in.Username, attrs, sub)
	if err == nil {
		err = s.checkSignUp(p, c, in, attrs)
	}
	preSignUp := preSignUpCall(p, "PreSignUp_SignUp", username, c.ClientId, attrs, in.ValidationData, in.ClientMetadata)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	resp, err := s.invoke(preSignUp)
	if err != nil {
		return nil, err
	}
	creds, err := newCredentials(p.Config.Id, username, in.Password)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pools[p.Config.Id] != p {
		return nil, poolNotFound(p.Config.Id)
	}
	if err := p.checkUsernameFree(username, attrs, false); err != nil {
		return nil, err
	}
	u := s.newUser(username, sub, attrs, StatusUnconfirmed)
	p.applyCredentials(u, creds, false, s.now())
	// The PreSignUp trigger can confirm the user and verify their email or
	// phone number, which skips the confirmation code.
	if resp["autoVerifyEmail"] == true && u.Attributes["email"] != "" {
		u.Attributes["email_verified"] = "true"
	}
	if resp["autoVerifyPhone"] == true && u.Attributes["phone_number"] != "" {
		u.Attributes["phone_number_verified"] = "true"
	}
	if resp["autoConfirmUser"] == true {
		u.Status = StatusConfirmed
	}
	p.Users[p.userKey(username)] = u
	s.markDirty()

	out := &SignUpOutput{UserSub: sub, UserConfirmed: u.Status == StatusConfirmed}
	if attr := autoVerifyAttribute(p, u); attr != "" && !out.UserConfirmed {
		s.issueCode(p, u, purposeSignUp, attr)
		out.CodeDeliveryDetails = deliveryDetails(u, attr)
	}
	return out, nil
}

func (s *Service) checkSignUp(p *pool, c *UserPoolClientType, in *SignUpInput, attrs map[string]string) error {
	if p.Config.AdminCreateUserConfig != nil && p.Config.AdminCreateUserConfig.AllowAdminCreateUserOnly {
		return notAuthorized("SignUp is not permitted for this user pool")
	}
	if err := checkSecretHash(c, in.SecretHash, in.Username); err != nil {
		return err
	}
	if err := checkWritable(c, in.UserAttributes); err != nil {
		return err
	}
	list := make([]AttributeType, 0, len(attrs))
	for _, k := range sortedKeys(attrs) {
		list = append(list, AttributeType{Name: k, Value: attrs[k]})
	}
	if err := p.validateAttributes(list, true); err != nil {
		return err
	}
	return checkPasswordPolicy(p.passwordPolicy(), in.Password)
}

// autoVerifyAttribute picks the attribute a sign-up code goes to: the first
// of email and phone_number that the pool auto-verifies and the user has.
func autoVerifyAttribute(p *pool, u *user) string {
	for _, attr := range []string{"email", "phone_number"} {
		if slices.Contains(p.Config.AutoVerifiedAttributes, attr) && u.Attributes[attr] != "" {
			return attr
		}
	}
	return ""
}

type ConfirmSignUpInput struct {
	ClientId           string            `json:"ClientId"`
	SecretHash         string            `json:"SecretHash"`
	Username           string            `json:"Username"`
	ConfirmationCode   string            `json:"ConfirmationCode"`
	ForceAliasCreation bool              `json:"ForceAliasCreation"`
	ClientMetadata     map[string]string `json:"ClientMetadata"`
}

// clientUser resolves the client and user for a ClientId-scoped operation,
// checking the secret hash. Callers hold s.mu.
func (s *Service) clientUser(clientID, login, hash string) (*pool, *UserPoolClientType, *user, error) {
	p, c, err := s.lookupClient(clientID)
	if err != nil {
		return nil, nil, nil, err
	}
	u := p.findUser(login)
	var username, sub string
	if u != nil {
		username, sub = u.Username, u.Sub
	}
	if err := checkSecretHash(c, hash, login, username, sub); err != nil {
		return nil, nil, nil, err
	}
	if u == nil {
		return nil, nil, nil, userMissing(c)
	}
	return p, c, u, nil
}

func (s *Service) ConfirmSignUp(in *ConfirmSignUpInput) (struct{}, error) {
	s.mu.Lock()
	call, err := s.confirmSignUpLocked(in)
	s.mu.Unlock()
	if err != nil {
		return struct{}{}, err
	}
	// AWS runs PostConfirmation after the user is confirmed; a failure is
	// reported to the caller but the user stays confirmed.
	_, err = s.invoke(call)
	return struct{}{}, err
}

func (s *Service) confirmSignUpLocked(in *ConfirmSignUpInput) (*triggerCall, error) {
	p, c, u, err := s.clientUser(in.ClientId, in.Username, in.SecretHash)
	if err != nil {
		return nil, err
	}
	if u.Status != StatusUnconfirmed {
		return nil, notAuthorized("User cannot be confirmed. Current status is %s", u.Status)
	}
	attr := ""
	if pc := u.Codes[purposeSignUp]; pc != nil {
		attr = pc.Attribute
	}
	if err := s.consumeCode(u, purposeSignUp, in.ConfirmationCode); err != nil {
		return nil, err
	}
	if attr != "" {
		if err := p.claimAlias(u, attr, in.ForceAliasCreation); err != nil {
			return nil, err
		}
		u.Attributes[attr+"_verified"] = "true"
	}
	u.Status = StatusConfirmed
	u.Modified = s.now()
	s.markDirty()
	return postConfirmationCall(p, u, "PostConfirmation_ConfirmSignUp", c.ClientId, in.ClientMetadata), nil
}

// claimAlias enforces alias uniqueness when u verifies attr: another user
// holding the same verified alias makes this fail, unless force moves it.
func (p *pool) claimAlias(u *user, attr string, force bool) error {
	if !slices.Contains(p.Config.AliasAttributes, attr) {
		return nil
	}
	for _, other := range p.Users {
		if other == u || other.Attributes[attr+"_verified"] != "true" || other.Attributes[attr] != u.Attributes[attr] {
			continue
		}
		if !force {
			return newError("AliasExistsException", "An account with the %s already exists.", attr)
		}
		other.Attributes[attr+"_verified"] = "false"
	}
	return nil
}

type ClientUserInput struct {
	ClientId       string            `json:"ClientId"`
	SecretHash     string            `json:"SecretHash"`
	Username       string            `json:"Username"`
	ClientMetadata map[string]string `json:"ClientMetadata"`
}

type CodeDeliveryOutput struct {
	CodeDeliveryDetails *CodeDeliveryDetailsType `json:"CodeDeliveryDetails,omitempty"`
}

func (s *Service) ResendConfirmationCode(in *ClientUserInput) (*CodeDeliveryOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, _, u, err := s.clientUser(in.ClientId, in.Username, in.SecretHash)
	if err != nil {
		return nil, err
	}
	if u.Status != StatusUnconfirmed {
		return nil, invalidParameter("User is already confirmed.")
	}
	attr := autoVerifyAttribute(p, u)
	if attr == "" {
		return nil, invalidParameter("Auto verification not turned on.")
	}
	s.issueCode(p, u, purposeSignUp, attr)
	return &CodeDeliveryOutput{CodeDeliveryDetails: deliveryDetails(u, attr)}, nil
}

func (s *Service) ForgotPassword(in *ClientUserInput) (*CodeDeliveryOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, _, u, err := s.clientUser(in.ClientId, in.Username, in.SecretHash)
	if err != nil {
		return nil, err
	}
	if !u.Enabled {
		return nil, errUserDisabled
	}
	attr := recoveryAttribute(u)
	if attr == "" {
		return nil, invalidParameter("Cannot reset password for the user as there is no registered/verified email or phone_number")
	}
	s.issueCode(p, u, purposeReset, attr)
	return &CodeDeliveryOutput{CodeDeliveryDetails: deliveryDetails(u, attr)}, nil
}

type ConfirmForgotPasswordInput struct {
	ClientId         string            `json:"ClientId"`
	SecretHash       string            `json:"SecretHash"`
	Username         string            `json:"Username"`
	ConfirmationCode string            `json:"ConfirmationCode"`
	Password         string            `json:"Password"`
	ClientMetadata   map[string]string `json:"ClientMetadata"`
}

func (s *Service) ConfirmForgotPassword(in *ConfirmForgotPasswordInput) (struct{}, error) {
	s.mu.Lock()
	call, err := s.confirmForgotPasswordLocked(in)
	s.mu.Unlock()
	if err != nil {
		return struct{}{}, err
	}
	_, err = s.invoke(call)
	return struct{}{}, err
}

func (s *Service) confirmForgotPasswordLocked(in *ConfirmForgotPasswordInput) (*triggerCall, error) {
	p, c, u, err := s.clientUser(in.ClientId, in.Username, in.SecretHash)
	if err != nil {
		return nil, err
	}
	if err := checkPasswordPolicy(p.passwordPolicy(), in.Password); err != nil {
		return nil, err
	}
	if err := s.consumeCode(u, purposeReset, in.ConfirmationCode); err != nil {
		return nil, err
	}
	if err := p.setPassword(u, in.Password, false, s.now()); err != nil {
		return nil, err
	}
	if u.Status == StatusResetRequired || u.Status == StatusForceChangePassword {
		u.Status = StatusConfirmed
	}
	u.Modified = s.now()
	s.markDirty()
	return postConfirmationCall(p, u, "PostConfirmation_ConfirmForgotPassword", c.ClientId, in.ClientMetadata), nil
}

// --- Access token operations ---

type AccessTokenInput struct {
	AccessToken string `json:"AccessToken"`
}

type GetUserOutput struct {
	Username            string          `json:"Username"`
	UserAttributes      []AttributeType `json:"UserAttributes"`
	UserMFASettingList  []string        `json:"UserMFASettingList,omitempty"`
	PreferredMfaSetting string          `json:"PreferredMfaSetting,omitempty"`
}

func (s *Service) GetUser(in *AccessTokenInput) (*GetUserOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ac, err := s.authenticateAccess(in.AccessToken)
	if err != nil {
		return nil, err
	}
	return &GetUserOutput{
		Username:            ac.user.Username,
		UserAttributes:      ac.user.attributeList(nil),
		UserMFASettingList:  ac.user.mfaSettingList(),
		PreferredMfaSetting: ac.user.mfa().Preferred,
	}, nil
}

type UpdateUserAttributesInput struct {
	AccessToken    string            `json:"AccessToken"`
	UserAttributes []AttributeType   `json:"UserAttributes"`
	ClientMetadata map[string]string `json:"ClientMetadata"`
}

type UpdateUserAttributesOutput struct {
	CodeDeliveryDetailsList []CodeDeliveryDetailsType `json:"CodeDeliveryDetailsList"`
}

func (s *Service) UpdateUserAttributes(in *UpdateUserAttributesInput) (*UpdateUserAttributesOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, err := s.authenticateAccess(in.AccessToken)
	if err != nil {
		return nil, err
	}
	if err := checkWritable(ac.client, in.UserAttributes); err != nil {
		return nil, err
	}
	if err := ac.pool.validateAttributes(in.UserAttributes, false); err != nil {
		return nil, err
	}
	for _, a := range in.UserAttributes {
		if sa := ac.pool.schemaAttribute(a.Name); sa != nil && !sa.Mutable && ac.user.Attributes[a.Name] != "" {
			return nil, invalidParameter("Cannot modify the non-mutable attribute %s", a.Name)
		}
	}
	out := &UpdateUserAttributesOutput{CodeDeliveryDetailsList: []CodeDeliveryDetailsType{}}
	for _, attr := range ac.user.applyAttributes(in.UserAttributes) {
		if slices.Contains(ac.pool.Config.AutoVerifiedAttributes, attr) {
			ac.user.Attributes[attr+"_verified"] = "false"
			s.issueCode(ac.pool, ac.user, "verify:"+attr, attr)
			if d := deliveryDetails(ac.user, attr); d != nil {
				out.CodeDeliveryDetailsList = append(out.CodeDeliveryDetailsList, *d)
			}
		}
	}
	ac.user.Modified = s.now()
	s.markDirty()
	return out, nil
}

func (s *Service) DeleteUser(in *AccessTokenInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, err := s.authenticateAccess(in.AccessToken)
	if err != nil {
		return struct{}{}, err
	}
	ac.pool.deleteUser(ac.user)
	s.markDirty()
	return struct{}{}, nil
}

type ChangePasswordInput struct {
	PreviousPassword string `json:"PreviousPassword"`
	ProposedPassword string `json:"ProposedPassword"`
	AccessToken      string `json:"AccessToken"`
}

func (s *Service) ChangePassword(in *ChangePasswordInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, err := s.authenticateAccess(in.AccessToken)
	if err != nil {
		return struct{}{}, err
	}
	if !passwordMatches(ac.user.PasswordHash, in.PreviousPassword) {
		return struct{}{}, errIncorrectPassword
	}
	if err := checkPasswordPolicy(ac.pool.passwordPolicy(), in.ProposedPassword); err != nil {
		return struct{}{}, err
	}
	if err := ac.pool.setPassword(ac.user, in.ProposedPassword, false, s.now()); err != nil {
		return struct{}{}, err
	}
	ac.user.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}

func (s *Service) GlobalSignOut(in *AccessTokenInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, err := s.authenticateAccess(in.AccessToken)
	if err != nil {
		return struct{}{}, err
	}
	ac.pool.revokeSessions(ac.user, "")
	s.markDirty()
	return struct{}{}, nil
}

type RevokeTokenInput struct {
	Token        string `json:"Token"`
	ClientId     string `json:"ClientId"`
	ClientSecret string `json:"ClientSecret"`
}

// RevokeToken revokes a refresh token and the access tokens minted from it.
func (s *Service) RevokeToken(in *RevokeTokenInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, c, err := s.lookupClient(in.ClientId)
	if err != nil {
		return struct{}{}, err
	}
	if c.ClientSecret != "" && in.ClientSecret != c.ClientSecret {
		return struct{}{}, notAuthorized("Client secret mismatch")
	}
	if c.EnableTokenRevocation != nil && !*c.EnableTokenRevocation {
		return struct{}{}, newError("UnsupportedOperationException", "Token revocation is not enabled for this client")
	}
	ref, ok := p.refresh[hashToken(in.Token)]
	if !ok {
		return struct{}{}, nil
	}
	if u := p.Users[ref.userKey]; u != nil {
		if sess := u.Sessions[ref.originJTI]; sess != nil && sess.ClientID != c.ClientId {
			return struct{}{}, newError("UnauthorizedException", "Token does not belong to the client")
		}
		p.revokeSessions(u, ref.originJTI)
		s.markDirty()
	}
	return struct{}{}, nil
}

type AttributeVerificationInput struct {
	AccessToken    string            `json:"AccessToken"`
	AttributeName  string            `json:"AttributeName"`
	Code           string            `json:"Code"`
	ClientMetadata map[string]string `json:"ClientMetadata"`
}

func (s *Service) GetUserAttributeVerificationCode(in *AttributeVerificationInput) (*CodeDeliveryOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, err := s.authenticateAccess(in.AccessToken)
	if err != nil {
		return nil, err
	}
	if (in.AttributeName != "email" && in.AttributeName != "phone_number") || ac.user.Attributes[in.AttributeName] == "" {
		return nil, invalidParameter("Invalid attribute name or the attribute has no value: %s", in.AttributeName)
	}
	s.issueCode(ac.pool, ac.user, "verify:"+in.AttributeName, in.AttributeName)
	return &CodeDeliveryOutput{CodeDeliveryDetails: deliveryDetails(ac.user, in.AttributeName)}, nil
}

func (s *Service) VerifyUserAttribute(in *AttributeVerificationInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, err := s.authenticateAccess(in.AccessToken)
	if err != nil {
		return struct{}{}, err
	}
	if err := s.consumeCode(ac.user, "verify:"+in.AttributeName, in.Code); err != nil {
		return struct{}{}, err
	}
	if err := ac.pool.claimAlias(ac.user, in.AttributeName, false); err != nil {
		return struct{}{}, err
	}
	ac.user.Attributes[in.AttributeName+"_verified"] = "true"
	ac.user.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}
