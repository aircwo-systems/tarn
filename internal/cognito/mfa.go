package cognito

// SMS and email MFA. Codes are never sent: like confirmation codes they are
// logged and exposed as pending codes (purpose "mfa"), and
// TARN_COGNITO_FIXED_CODE applies to them. TOTP is not emulated.

const purposeMFA = "mfa"

const (
	mfaSMS   = "SMS_MFA"
	mfaEmail = "EMAIL_OTP"
)

var errMFACodeMismatch = &Error{"CodeMismatchException", "Invalid code or auth state for the user."}

// mfaPrefs are a user's MFA settings, as set by SetUserMFAPreference.
type mfaPrefs struct {
	SMS       bool   `json:"sms,omitempty"`
	Email     bool   `json:"email,omitempty"`
	Preferred string `json:"preferred,omitempty"`
}

func (u *user) mfa() mfaPrefs {
	if u.MFA == nil {
		return mfaPrefs{}
	}
	return *u.MFA
}

// mfaSettingList is UserMFASettingList for GetUser and AdminGetUser.
func (u *user) mfaSettingList() []string {
	m := u.mfa()
	var out []string
	if m.SMS {
		out = append(out, mfaSMS)
	}
	if m.Email {
		out = append(out, mfaEmail)
	}
	return out
}

func (p *pool) mfaMode() string {
	if p.Mfa.MfaConfiguration != "" {
		return p.Mfa.MfaConfiguration
	}
	if p.Config.MfaConfiguration != "" {
		return p.Config.MfaConfiguration
	}
	return "OFF"
}

// mfaChallengeType decides which MFA challenge, if any, a sign-in gets. With
// the pool's MFA OFF there is never one, whatever the user's preference, as
// in AWS. Email OTP also needs the pool's EmailMfaConfiguration.
func (p *pool) mfaChallengeType(u *user) string {
	mode := p.mfaMode()
	if mode == "OFF" {
		return ""
	}
	m := u.mfa()
	var enabled []string
	if m.SMS && u.Attributes["phone_number"] != "" {
		enabled = append(enabled, mfaSMS)
	}
	if m.Email && u.Attributes["email"] != "" && len(p.Mfa.EmailMfaConfiguration) > 0 {
		enabled = append(enabled, mfaEmail)
	}
	for _, t := range enabled {
		if t == m.Preferred {
			return t
		}
	}
	if len(enabled) > 0 {
		return enabled[0]
	}
	if mode == "ON" && u.Attributes["phone_number"] != "" {
		return mfaSMS
	}
	return ""
}

// mfaChallenge issues an MFA code and returns the challenge, or nil when the
// sign-in needs no MFA. Callers hold s.mu.
func (s *Service) mfaChallenge(p *pool, c *UserPoolClientType, u *user) *AuthOutput {
	typ := p.mfaChallengeType(u)
	if typ == "" {
		return nil
	}
	attr := "phone_number"
	if typ == mfaEmail {
		attr = "email"
	}
	s.issueCode(p, u, purposeMFA, attr)
	d := deliveryDetails(u, attr)
	return s.challenge(p, c, u, typ, nil, map[string]string{
		"CODE_DELIVERY_DELIVERY_MEDIUM": d.DeliveryMedium,
		"CODE_DELIVERY_DESTINATION":     d.Destination,
		"USER_ID_FOR_SRP":               u.Username,
	})
}

type SMSMfaSettingsType struct {
	Enabled      bool `json:"Enabled"`
	PreferredMfa bool `json:"PreferredMfa"`
}

type SetUserMFAPreferenceInput struct {
	AccessToken              string              `json:"AccessToken"`
	UserPoolId               string              `json:"UserPoolId"` // AdminSetUserMFAPreference only
	Username                 string              `json:"Username"`   // AdminSetUserMFAPreference only
	SMSMfaSettings           *SMSMfaSettingsType `json:"SMSMfaSettings"`
	SoftwareTokenMfaSettings *SMSMfaSettingsType `json:"SoftwareTokenMfaSettings"`
	EmailMfaSettings         *SMSMfaSettingsType `json:"EmailMfaSettings"`
}

func applyMFAPreference(u *user, in *SetUserMFAPreferenceInput) error {
	if t := in.SoftwareTokenMfaSettings; t != nil && t.Enabled {
		return invalidParameter("User has not verified software token mfa")
	}
	m := u.mfa()
	set := func(settings *SMSMfaSettingsType, kind, attr string, enabled *bool) error {
		if settings == nil {
			return nil
		}
		if settings.Enabled && u.Attributes[attr] == "" {
			return invalidParameter("User does not have an %s to use for %s.", attr, kind)
		}
		*enabled = settings.Enabled
		switch {
		case settings.Enabled && settings.PreferredMfa:
			m.Preferred = kind
		case m.Preferred == kind && (!settings.Enabled || !settings.PreferredMfa):
			m.Preferred = ""
		}
		return nil
	}
	if err := set(in.SMSMfaSettings, mfaSMS, "phone_number", &m.SMS); err != nil {
		return err
	}
	if err := set(in.EmailMfaSettings, mfaEmail, "email", &m.Email); err != nil {
		return err
	}
	if m == (mfaPrefs{}) {
		u.MFA = nil
	} else {
		u.MFA = &m
	}
	return nil
}

func (s *Service) SetUserMFAPreference(in *SetUserMFAPreferenceInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, err := s.authenticateAccess(in.AccessToken)
	if err != nil {
		return struct{}{}, err
	}
	if err := applyMFAPreference(ac.user, in); err != nil {
		return struct{}{}, err
	}
	ac.user.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}

func (s *Service) AdminSetUserMFAPreference(in *SetUserMFAPreferenceInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	if err := applyMFAPreference(u, in); err != nil {
		return struct{}{}, err
	}
	u.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}
