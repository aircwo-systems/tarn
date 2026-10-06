package cognito

import (
	"encoding/json"
	"slices"
	"time"
)

// defaultAuthFlows are the flows AWS enables for a client created without
// ExplicitAuthFlows.
var defaultAuthFlows = []string{"ALLOW_REFRESH_TOKEN_AUTH", "ALLOW_USER_SRP_AUTH", "ALLOW_CUSTOM_AUTH"}

func applyClientDefaults(st *UserPoolClientSettings) {
	if st.RefreshTokenValidity == 0 {
		st.RefreshTokenValidity = 30
	}
	// AWS returns TokenValidityUnits only when the client sets them; the
	// defaults (hours, hours, days) apply otherwise. See tokenUnits.
	if u := st.TokenValidityUnits; u != nil {
		if u.AccessToken == "" {
			u.AccessToken = "hours"
		}
		if u.IdToken == "" {
			u.IdToken = "hours"
		}
		if u.RefreshToken == "" {
			u.RefreshToken = "days"
		}
	}
	if len(st.ExplicitAuthFlows) == 0 {
		st.ExplicitAuthFlows = slices.Clone(defaultAuthFlows)
	}
	if st.PreventUserExistenceErrors == "" {
		st.PreventUserExistenceErrors = "LEGACY"
	}
	if st.EnableTokenRevocation == nil {
		st.EnableTokenRevocation = ptr(true)
	}
	if st.AuthSessionValidity == 0 {
		st.AuthSessionValidity = 3
	}
}

// tokenUnits returns the client's validity units with AWS defaults applied.
func tokenUnits(c *UserPoolClientType) TokenValidityUnitsType {
	if c.TokenValidityUnits != nil {
		return *c.TokenValidityUnits
	}
	return TokenValidityUnitsType{AccessToken: "hours", IdToken: "hours", RefreshToken: "days"}
}

func unitDuration(unit string) time.Duration {
	switch unit {
	case "seconds":
		return time.Second
	case "minutes":
		return time.Minute
	case "days":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}

func (s *Service) accessTTL(c *UserPoolClientType) time.Duration {
	if s.cfg.CognitoTokenTTL > 0 {
		return s.cfg.CognitoTokenTTL
	}
	if c.AccessTokenValidity == nil {
		return time.Hour
	}
	return time.Duration(*c.AccessTokenValidity) * unitDuration(tokenUnits(c).AccessToken)
}

func (s *Service) idTTL(c *UserPoolClientType) time.Duration {
	if s.cfg.CognitoTokenTTL > 0 {
		return s.cfg.CognitoTokenTTL
	}
	if c.IdTokenValidity == nil {
		return time.Hour
	}
	return time.Duration(*c.IdTokenValidity) * unitDuration(tokenUnits(c).IdToken)
}

func refreshTTL(c *UserPoolClientType) time.Duration {
	return time.Duration(c.RefreshTokenValidity) * unitDuration(tokenUnits(c).RefreshToken)
}

// allowsFlow reports whether the client permits an auth flow. The legacy
// names (USER_PASSWORD_AUTH, ADMIN_NO_SRP_AUTH) still count, and a client
// configured only with legacy names keeps refresh, as in AWS.
func allowsFlow(c *UserPoolClientType, flow string) bool {
	has := func(names ...string) bool {
		for _, n := range names {
			if slices.Contains(c.ExplicitAuthFlows, n) {
				return true
			}
		}
		return false
	}
	switch flow {
	case "USER_PASSWORD_AUTH":
		return has("ALLOW_USER_PASSWORD_AUTH", "USER_PASSWORD_AUTH")
	case "ADMIN_USER_PASSWORD_AUTH", "ADMIN_NO_SRP_AUTH":
		return has("ALLOW_ADMIN_USER_PASSWORD_AUTH", "ADMIN_NO_SRP_AUTH")
	case "USER_SRP_AUTH":
		return has("ALLOW_USER_SRP_AUTH") || !hasAllowFlows(c)
	case "REFRESH_TOKEN_AUTH", "REFRESH_TOKEN":
		return has("ALLOW_REFRESH_TOKEN_AUTH") || !hasAllowFlows(c)
	case "CUSTOM_AUTH":
		return has("ALLOW_CUSTOM_AUTH", "CUSTOM_AUTH_FLOW_ONLY")
	}
	return false
}

func hasAllowFlows(c *UserPoolClientType) bool {
	for _, f := range c.ExplicitAuthFlows {
		if len(f) > 6 && f[:6] == "ALLOW_" {
			return true
		}
	}
	return false
}

type CreateUserPoolClientInput struct {
	UserPoolId     string `json:"UserPoolId"`
	GenerateSecret bool   `json:"GenerateSecret"`
	UserPoolClientSettings
}

type UserPoolClientOutput struct {
	UserPoolClient UserPoolClientType `json:"UserPoolClient"`
}

func (s *Service) CreateUserPoolClient(in *CreateUserPoolClientInput) (*UserPoolClientOutput, error) {
	if in.ClientName == "" {
		return nil, invalidParameter("1 validation error detected: Value null at 'clientName' failed to satisfy constraint: Member must not be null")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	if err := validateClientAttributes(p, in.ReadAttributes, in.WriteAttributes); err != nil {
		return nil, err
	}
	settings := in.UserPoolClientSettings
	applyClientDefaults(&settings)
	now := EpochTime(s.now())
	c := &UserPoolClientType{
		UserPoolId:             in.UserPoolId,
		ClientId:               newClientID(),
		UserPoolClientSettings: settings,
		CreationDate:           now,
		LastModifiedDate:       now,
	}
	if in.GenerateSecret {
		c.ClientSecret = newClientSecret()
	}
	p.Clients[c.ClientId] = c
	s.index.addClient(p.Config.Id, c.ClientId)
	s.markDirty()
	return &UserPoolClientOutput{UserPoolClient: cloneJSON(*c)}, nil
}

type ClientIDInput struct {
	UserPoolId string `json:"UserPoolId"`
	ClientId   string `json:"ClientId"`
}

func (s *Service) poolClient(poolID, clientID string) (*pool, *UserPoolClientType, error) {
	p, err := s.lookupPool(poolID)
	if err != nil {
		return nil, nil, err
	}
	c, ok := p.Clients[clientID]
	if !ok {
		return nil, nil, clientNotFound(clientID)
	}
	return p, c, nil
}

func (s *Service) DescribeUserPoolClient(in *ClientIDInput) (*UserPoolClientOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, c, err := s.poolClient(in.UserPoolId, in.ClientId)
	if err != nil {
		return nil, err
	}
	return &UserPoolClientOutput{UserPoolClient: cloneJSON(*c)}, nil
}

type UpdateUserPoolClientInput struct {
	UserPoolId string `json:"UserPoolId"`
	ClientId   string `json:"ClientId"`
	UserPoolClientSettings
}

// UpdateUserPoolClient replaces the client's settings. Settings the request
// leaves out return to their defaults, as in AWS.
func (s *Service) UpdateUserPoolClient(in *UpdateUserPoolClientInput) (*UserPoolClientOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, c, err := s.poolClient(in.UserPoolId, in.ClientId)
	if err != nil {
		return nil, err
	}
	if err := validateClientAttributes(p, in.ReadAttributes, in.WriteAttributes); err != nil {
		return nil, err
	}
	settings := in.UserPoolClientSettings
	if settings.ClientName == "" {
		settings.ClientName = c.ClientName
	}
	applyClientDefaults(&settings)
	c.UserPoolClientSettings = settings
	c.LastModifiedDate = EpochTime(s.now())
	s.markDirty()
	return &UserPoolClientOutput{UserPoolClient: cloneJSON(*c)}, nil
}

func (s *Service) DeleteUserPoolClient(in *ClientIDInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, _, err := s.poolClient(in.UserPoolId, in.ClientId)
	if err != nil {
		return struct{}{}, err
	}
	delete(p.Clients, in.ClientId)
	s.index.removeClient(in.ClientId)
	s.markDirty()
	return struct{}{}, nil
}

type ListUserPoolClientsInput struct {
	UserPoolId string `json:"UserPoolId"`
	MaxResults int    `json:"MaxResults"`
	NextToken  string `json:"NextToken"`
}

type UserPoolClientDescription struct {
	ClientId   string `json:"ClientId"`
	UserPoolId string `json:"UserPoolId"`
	ClientName string `json:"ClientName"`
}

type ListUserPoolClientsOutput struct {
	UserPoolClients []UserPoolClientDescription `json:"UserPoolClients"`
	NextToken       string                      `json:"NextToken,omitempty"`
}

func (s *Service) ListUserPoolClients(in *ListUserPoolClientsInput) (*ListUserPoolClientsOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	page, next, err := paginate(sortedKeys(p.Clients), in.MaxResults, in.NextToken, 60)
	if err != nil {
		return nil, err
	}
	out := &ListUserPoolClientsOutput{UserPoolClients: []UserPoolClientDescription{}, NextToken: next}
	for _, id := range page {
		c := p.Clients[id]
		out.UserPoolClients = append(out.UserPoolClients, UserPoolClientDescription{ClientId: id, UserPoolId: p.Config.Id, ClientName: c.ClientName})
	}
	return out, nil
}

func validateClientAttributes(p *pool, read, write []string) error {
	for _, name := range append(slices.Clone(read), write...) {
		if p.schemaAttribute(name) == nil {
			return invalidParameter("Invalid read attributes specified while creating a client")
		}
	}
	return nil
}

func (p *pool) schemaAttribute(name string) *SchemaAttributeType {
	for i := range p.Config.SchemaAttributes {
		if p.Config.SchemaAttributes[i].Name == name {
			return &p.Config.SchemaAttributes[i]
		}
	}
	return nil
}

// cloneJSON deep-copies v so callers can encode it after the lock is released.
func cloneJSON[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}
