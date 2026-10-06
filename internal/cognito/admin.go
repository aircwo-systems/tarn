package cognito

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dashboard views. These are Tarn's own shapes (lowerCamel JSON, like the rest
// of the admin API), not AWS ones, and never carry signing keys, password
// hashes, SRP verifiers or client secrets.

// PoolSummary is one pool in the dashboard overview.
type PoolSummary struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Arn          string            `json:"arn"`
	Issuer       string            `json:"issuer"`
	JwksURL      string            `json:"jwksUrl"`
	Users        int               `json:"users"`
	Clients      int               `json:"clients"`
	Groups       int               `json:"groups"`
	PendingCodes int               `json:"pendingCodes"`
	MfaMode      string            `json:"mfaMode"`
	Triggers     []string          `json:"triggers,omitempty"`
	Domain       string            `json:"domain,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	Created      time.Time         `json:"created"`
}

// lambdaTriggers lists the configured triggers by LambdaConfig member name.
func (p *pool) lambdaTriggers() map[string]string {
	lc := p.lambdaConfig()
	out := map[string]string{}
	add := func(name, arn string) {
		if arn != "" {
			out[name] = arn
		}
	}
	add("PreSignUp", lc.PreSignUp)
	add("CustomMessage", lc.CustomMessage)
	add("PostConfirmation", lc.PostConfirmation)
	add("PreAuthentication", lc.PreAuthentication)
	add("PostAuthentication", lc.PostAuthentication)
	add("DefineAuthChallenge", lc.DefineAuthChallenge)
	add("CreateAuthChallenge", lc.CreateAuthChallenge)
	add("VerifyAuthChallengeResponse", lc.VerifyAuthChallengeResponse)
	add("PreTokenGeneration", lc.PreTokenGeneration)
	add("UserMigration", lc.UserMigration)
	if cfg := lc.PreTokenGenerationConfig; cfg != nil && cfg.LambdaArn != "" {
		out["PreTokenGeneration"] = cfg.LambdaArn
	}
	return out
}

func (s *Service) pendingCodeCount(p *pool) int {
	now := s.now()
	n := 0
	for _, u := range p.Users {
		for _, c := range u.Codes {
			if !now.After(c.Expires) && c.Attempts < maxCodeAttempts {
				n++
			}
		}
	}
	return n
}

func (s *Service) summary(p *pool) PoolSummary {
	triggers := sortedKeys(p.lambdaTriggers())
	domain := ""
	if p.Domain != nil {
		domain = p.Domain.Domain
	}
	return PoolSummary{
		ID:           p.Config.Id,
		Name:         p.Config.Name,
		Arn:          p.Config.Arn,
		Issuer:       s.Issuer(p.Config.Id),
		JwksURL:      s.LocalBaseURL() + "/" + p.Config.Id + "/.well-known/jwks.json",
		Users:        len(p.Users),
		Clients:      len(p.Clients),
		Groups:       len(p.Groups),
		PendingCodes: s.pendingCodeCount(p),
		MfaMode:      p.mfaMode(),
		Triggers:     triggers,
		Domain:       domain,
		Tags:         cloneJSON(p.Config.UserPoolTags),
		Created:      p.Config.CreationDate.Time(),
	}
}

// PoolSummaries lists every pool for the dashboard overview.
func (s *Service) PoolSummaries() []PoolSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PoolSummary, 0, len(s.pools))
	for _, id := range sortedKeys(s.pools) {
		out = append(out, s.summary(s.pools[id]))
	}
	return out
}

// PoolCount returns the number of pools, for account resource counts.
func (s *Service) PoolCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pools)
}

// ClientView is an app client as shown in the dashboard.
type ClientView struct {
	ClientID                   string    `json:"clientId"`
	Name                       string    `json:"name"`
	HasSecret                  bool      `json:"hasSecret"`
	AuthFlows                  []string  `json:"authFlows"`
	AccessTokenValidity        string    `json:"accessTokenValidity"`
	IDTokenValidity            string    `json:"idTokenValidity"`
	RefreshTokenValidity       string    `json:"refreshTokenValidity"`
	ReadAttributes             []string  `json:"readAttributes,omitempty"`
	WriteAttributes            []string  `json:"writeAttributes,omitempty"`
	OAuthFlows                 []string  `json:"oauthFlows,omitempty"`
	OAuthScopes                []string  `json:"oauthScopes,omitempty"`
	PreventUserExistenceErrors string    `json:"preventUserExistenceErrors"`
	TokenRevocation            bool      `json:"tokenRevocation"`
	Created                    time.Time `json:"created"`
}

// GroupView is a group as shown in the dashboard.
type GroupView struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Precedence  *int32    `json:"precedence,omitempty"`
	RoleArn     string    `json:"roleArn,omitempty"`
	Members     int       `json:"members"`
	Created     time.Time `json:"created"`
}

// UserView is a user as shown in the dashboard.
type UserView struct {
	Username      string            `json:"username"`
	Sub           string            `json:"sub"`
	Status        string            `json:"status"`
	Enabled       bool              `json:"enabled"`
	Email         string            `json:"email,omitempty"`
	EmailVerified bool              `json:"emailVerified"`
	Phone         string            `json:"phone,omitempty"`
	PhoneVerified bool              `json:"phoneVerified"`
	Groups        []string          `json:"groups"`
	MFA           []string          `json:"mfa,omitempty"`
	PreferredMFA  string            `json:"preferredMfa,omitempty"`
	Sessions      int               `json:"sessions"`
	Attributes    map[string]string `json:"attributes"`
	Created       time.Time         `json:"created"`
	Modified      time.Time         `json:"modified"`
}

// PoolSettingsView is the subset of pool settings the dashboard shows.
type PoolSettingsView struct {
	UsernameAttributes     []string            `json:"usernameAttributes,omitempty"`
	AliasAttributes        []string            `json:"aliasAttributes,omitempty"`
	AutoVerifiedAttributes []string            `json:"autoVerifiedAttributes,omitempty"`
	CaseSensitive          bool                `json:"caseSensitive"`
	PasswordPolicy         *PasswordPolicyType `json:"passwordPolicy,omitempty"`
	MfaMode                string              `json:"mfaMode"`
	SmsMfa                 bool                `json:"smsMfa"`
	EmailMfa               bool                `json:"emailMfa"`
	AdminCreateOnly        bool                `json:"adminCreateOnly"`
	DeletionProtection     string              `json:"deletionProtection"`
	CustomAttributes       []string            `json:"customAttributes,omitempty"`
	RequiredAttributes     []string            `json:"requiredAttributes,omitempty"`
	Tags                   map[string]string   `json:"tags,omitempty"`
}

// TarnSettingsView reports the Tarn settings that change Cognito behaviour.
type TarnSettingsView struct {
	IssuerMode string `json:"issuerMode"`
	FixedCode  bool   `json:"fixedCode"`
	TokenTTL   string `json:"tokenTtl,omitempty"`
	Triggers   bool   `json:"triggers"`
}

// PoolDetail is everything the dashboard's pool view needs in one call.
type PoolDetail struct {
	PoolSummary
	Settings     PoolSettingsView  `json:"settings"`
	Tarn         TarnSettingsView  `json:"tarn"`
	LambdaConfig map[string]string `json:"lambdaConfig"`
	// PreTokenVersion is the pre token generation event version: V1_0 events
	// can only change the ID token.
	PreTokenVersion string        `json:"preTokenGenerationVersion,omitempty"`
	Clients         []ClientView  `json:"clientList"`
	GroupList       []GroupView   `json:"groupList"`
	UserList        []UserView    `json:"userList"`
	UsersTotal      int           `json:"usersTotal"`
	UsersOmitted    int           `json:"usersOmitted"`
	Codes           []PendingCode `json:"codes"`
	ResourceScope   []string      `json:"resourceServerScopes,omitempty"`
}

// maxDetailUsers caps the users returned in one pool detail.
const maxDetailUsers = 500

func validity(n *int32, unit string, fallback string) string {
	if n == nil {
		return fallback
	}
	return strconv.Itoa(int(*n)) + " " + unit
}

// PoolDetail returns the dashboard view of one pool.
func (s *Service) PoolDetail(poolID string) (*PoolDetail, error) {
	s.mu.RLock()
	p, err := s.lookupPool(poolID)
	if err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	d := &PoolDetail{PoolSummary: s.summary(p), LambdaConfig: p.lambdaTriggers()}
	if _, ok := d.LambdaConfig["PreTokenGeneration"]; ok {
		d.PreTokenVersion = "V1_0"
		if cfg := p.lambdaConfig().PreTokenGenerationConfig; cfg != nil && cfg.LambdaVersion != "" {
			d.PreTokenVersion = cfg.LambdaVersion
		}
	}

	st := PoolSettingsView{
		UsernameAttributes:     p.Config.UsernameAttributes,
		AliasAttributes:        p.Config.AliasAttributes,
		AutoVerifiedAttributes: p.Config.AutoVerifiedAttributes,
		CaseSensitive:          p.caseSensitive(),
		PasswordPolicy:         p.passwordPolicy(),
		MfaMode:                p.mfaMode(),
		SmsMfa:                 len(p.Mfa.SmsMfaConfiguration) > 0,
		EmailMfa:               len(p.Mfa.EmailMfaConfiguration) > 0,
		DeletionProtection:     p.Config.DeletionProtection,
		Tags:                   p.Config.UserPoolTags,
	}
	if p.Config.AdminCreateUserConfig != nil {
		st.AdminCreateOnly = p.Config.AdminCreateUserConfig.AllowAdminCreateUserOnly
	}
	for _, a := range p.Config.SchemaAttributes {
		if strings.HasPrefix(a.Name, "custom:") || strings.HasPrefix(a.Name, "dev:") {
			st.CustomAttributes = append(st.CustomAttributes, a.Name)
		}
		if a.Required && a.Name != "sub" {
			st.RequiredAttributes = append(st.RequiredAttributes, a.Name)
		}
	}
	d.Settings = cloneJSON(st)

	ttl := ""
	if s.cfg.CognitoTokenTTL > 0 {
		ttl = s.cfg.CognitoTokenTTL.String()
	}
	mode := s.cfg.CognitoIssuer
	if mode == "" {
		mode = "local"
	}
	d.Tarn = TarnSettingsView{IssuerMode: mode, FixedCode: s.cfg.CognitoFixedCode != "", TokenTTL: ttl, Triggers: s.invoker != nil}

	for _, id := range sortedKeys(p.Clients) {
		c := p.Clients[id]
		u := tokenUnits(c)
		access, idv := validity(c.AccessTokenValidity, u.AccessToken, "60 minutes"), validity(c.IdTokenValidity, u.IdToken, "60 minutes")
		if s.cfg.CognitoTokenTTL > 0 {
			access, idv = s.cfg.CognitoTokenTTL.String()+" (TARN_COGNITO_TOKEN_TTL)", s.cfg.CognitoTokenTTL.String()+" (TARN_COGNITO_TOKEN_TTL)"
		}
		d.Clients = append(d.Clients, ClientView{
			ClientID:                   id,
			Name:                       c.ClientName,
			HasSecret:                  c.ClientSecret != "",
			AuthFlows:                  slices.Clone(c.ExplicitAuthFlows),
			AccessTokenValidity:        access,
			IDTokenValidity:            idv,
			RefreshTokenValidity:       validity(&c.RefreshTokenValidity, u.RefreshToken, "30 days"),
			ReadAttributes:             slices.Clone(c.ReadAttributes),
			WriteAttributes:            slices.Clone(c.WriteAttributes),
			OAuthFlows:                 slices.Clone(c.AllowedOAuthFlows),
			OAuthScopes:                slices.Clone(c.AllowedOAuthScopes),
			PreventUserExistenceErrors: c.PreventUserExistenceErrors,
			TokenRevocation:            c.EnableTokenRevocation == nil || *c.EnableTokenRevocation,
			Created:                    c.CreationDate.Time(),
		})
	}

	members := map[string]int{}
	for _, u := range p.Users {
		for _, g := range u.Groups {
			members[g]++
		}
	}
	for _, name := range sortedKeys(p.Groups) {
		g := p.Groups[name]
		v := GroupView{Name: name, Precedence: g.Precedence, Members: members[name], Created: g.CreationDate.Time()}
		if g.Description != nil {
			v.Description = *g.Description
		}
		if g.RoleArn != nil {
			v.RoleArn = *g.RoleArn
		}
		d.GroupList = append(d.GroupList, v)
	}
	for _, rs := range p.ResourceServers {
		for _, sc := range rs.Scopes {
			d.ResourceScope = append(d.ResourceScope, rs.Identifier+"/"+sc.ScopeName)
		}
	}
	sort.Strings(d.ResourceScope)

	users := make([]*user, 0, len(p.Users))
	for _, u := range p.Users {
		users = append(users, u)
	}
	// Newest first: the user you just signed up is the one you want to see.
	sort.Slice(users, func(i, j int) bool { return users[i].Created.After(users[j].Created) })
	d.UsersTotal = len(users)
	if len(users) > maxDetailUsers {
		d.UsersOmitted = len(users) - maxDetailUsers
		users = users[:maxDetailUsers]
	}
	now := s.now()
	for _, u := range users {
		attrs := make(map[string]string, len(u.Attributes))
		for k, v := range u.Attributes {
			attrs[k] = v
		}
		live := 0
		for _, sess := range u.Sessions {
			if !sess.Revoked && now.Before(sess.Expires) {
				live++
			}
		}
		d.UserList = append(d.UserList, UserView{
			Username:      u.Username,
			Sub:           u.Sub,
			Status:        u.Status,
			Enabled:       u.Enabled,
			Email:         u.Attributes["email"],
			EmailVerified: u.Attributes["email_verified"] == "true",
			Phone:         u.Attributes["phone_number"],
			PhoneVerified: u.Attributes["phone_number_verified"] == "true",
			Groups:        p.groupNames(u),
			MFA:           u.mfaSettingList(),
			PreferredMFA:  u.mfa().Preferred,
			Sessions:      live,
			Attributes:    attrs,
			Created:       u.Created,
			Modified:      u.Modified,
		})
	}
	s.mu.RUnlock()

	codes, err := s.PendingCodes(poolID, "")
	if err != nil {
		return nil, err
	}
	d.Codes = codes
	return d, nil
}

// ClientSecret returns an app client's secret, for the dashboard's reveal.
func (s *Service) ClientSecret(poolID, clientID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, c, err := s.poolClient(poolID, clientID)
	if err != nil {
		return "", err
	}
	return c.ClientSecret, nil
}

// MintTokens issues tokens for a user without their password, a Tarn-only
// shortcut for testing APIs with curl. The tokens are real: they run the pre
// token generation trigger and Cognito's own APIs accept them.
func (s *Service) MintTokens(poolID, clientID, username string) (*AuthenticationResultType, error) {
	s.mu.Lock()
	p, c, err := s.poolClient(poolID, clientID)
	var job *tokenJob
	if err == nil {
		var u *user
		u, err = p.mustFindUser(username)
		if err == nil && !u.Enabled {
			err = errUserDisabled
		}
		if err == nil {
			sess, refresh := s.startSession(p, c, u)
			job = s.prepareTokens(p, c, u, sess, refresh, "Authentication")
		}
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.issueTokens(job)
}

// DecodedToken is a JWT taken apart for the dashboard's decoder.
type DecodedToken struct {
	Header         map[string]any `json:"header,omitempty"`
	Claims         map[string]any `json:"claims,omitempty"`
	PoolID         string         `json:"poolId,omitempty"`
	KnownPool      bool           `json:"knownPool"`
	SignatureValid bool           `json:"signatureValid"`
	Expired        bool           `json:"expired"`
	ExpiresAt      *time.Time     `json:"expiresAt,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// DecodeToken decodes a JWT and reports separately whether this account's
// pool key verifies it and whether it has expired, so an expired token from
// Tarn does not look forged.
func (s *Service) DecodeToken(token string) *DecodedToken {
	out := &DecodedToken{}
	token = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bearer "))
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		out.Error = "Not a JWT: expected three dot-separated parts."
		return out
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err == nil {
		err = json.Unmarshal(hb, &out.Header)
	}
	if err != nil {
		out.Error = "The header is not valid base64url JSON."
		return out
	}
	claims, err := decodeClaims(token)
	if err != nil {
		out.Error = "The payload is not valid base64url JSON."
		return out
	}
	out.Claims = claims
	out.PoolID = PoolIDFromToken(token)
	if exp := claimInt(claims, "exp"); exp > 0 {
		t := time.Unix(exp, 0).UTC()
		out.ExpiresAt = &t
		out.Expired = !s.now().Before(t)
	}
	s.mu.RLock()
	p, ok := s.pools[out.PoolID]
	if ok {
		_, err := verifyJWT(p, token)
		out.SignatureValid = err == nil
	}
	s.mu.RUnlock()
	out.KnownPool = ok
	return out
}
