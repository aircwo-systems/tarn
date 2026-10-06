package cognito

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func ptr[T any](v T) *T { return &v }

// standardAttributes is the schema AWS returns for every pool, in AWS order.
// The Terraform provider compares against exactly these values to hide them
// from plans, so they must match field for field.
func standardAttributes() []SchemaAttributeType {
	str := func(name, minLen, maxLen string) SchemaAttributeType {
		return SchemaAttributeType{
			Name: name, AttributeDataType: "String", Mutable: true,
			StringAttributeConstraints: &StringAttributeConstraintsType{MinLength: ptr(minLen), MaxLength: ptr(maxLen)},
		}
	}
	boolean := func(name string) SchemaAttributeType {
		return SchemaAttributeType{Name: name, AttributeDataType: "Boolean", Mutable: true}
	}
	sub := str("sub", "1", "2048")
	sub.Mutable = false
	sub.Required = true
	return []SchemaAttributeType{
		sub,
		str("name", "0", "2048"),
		str("given_name", "0", "2048"),
		str("family_name", "0", "2048"),
		str("middle_name", "0", "2048"),
		str("nickname", "0", "2048"),
		str("preferred_username", "0", "2048"),
		str("profile", "0", "2048"),
		str("picture", "0", "2048"),
		str("website", "0", "2048"),
		str("email", "0", "2048"),
		boolean("email_verified"),
		str("gender", "0", "2048"),
		str("birthdate", "10", "10"),
		str("zoneinfo", "0", "2048"),
		str("locale", "0", "2048"),
		str("phone_number", "0", "2048"),
		boolean("phone_number_verified"),
		str("address", "0", "2048"),
		{
			Name: "updated_at", AttributeDataType: "Number", Mutable: true,
			NumberAttributeConstraints: &NumberAttributeConstraintsType{MinValue: ptr("0")},
		},
	}
}

// schemaAttribute converts an input attribute to the stored shape, adding the
// custom: (and dev:) prefix to non-standard names.
func schemaAttribute(in schemaAttributeInput) SchemaAttributeType {
	out := SchemaAttributeType{
		Name:                       in.Name,
		AttributeDataType:          in.AttributeDataType,
		Mutable:                    in.Mutable == nil || *in.Mutable,
		Required:                   in.Required != nil && *in.Required,
		DeveloperOnlyAttribute:     in.DeveloperOnlyAttribute != nil && *in.DeveloperOnlyAttribute,
		NumberAttributeConstraints: in.NumberAttributeConstraints,
		StringAttributeConstraints: in.StringAttributeConstraints,
	}
	if out.AttributeDataType == "" {
		out.AttributeDataType = "String"
	}
	name := strings.TrimPrefix(strings.TrimPrefix(in.Name, "dev:"), "custom:")
	if !isStandardAttribute(name) {
		out.Name = "custom:" + name
		if out.DeveloperOnlyAttribute {
			out.Name = "dev:" + out.Name
		}
	}
	return out
}

func isStandardAttribute(name string) bool {
	for _, a := range standardAttributes() {
		if a.Name == name {
			return true
		}
	}
	return false
}

func buildSchema(inputs []schemaAttributeInput) ([]SchemaAttributeType, error) {
	attrs := standardAttributes()
	for _, in := range inputs {
		if in.Name == "" {
			return nil, invalidParameter("Schema attribute name is required.")
		}
		a := schemaAttribute(in)
		replaced := false
		for i := range attrs {
			if attrs[i].Name == a.Name {
				if !strings.HasPrefix(a.Name, "custom:") && !strings.HasPrefix(a.Name, "dev:") {
					// Standard attributes keep their type and default constraints
					// unless the request sets them.
					if a.StringAttributeConstraints == nil {
						a.StringAttributeConstraints = attrs[i].StringAttributeConstraints
					}
					if a.NumberAttributeConstraints == nil {
						a.NumberAttributeConstraints = attrs[i].NumberAttributeConstraints
					}
					a.AttributeDataType = attrs[i].AttributeDataType
				}
				attrs[i] = a
				replaced = true
				break
			}
		}
		if !replaced {
			attrs = append(attrs, a)
		}
	}
	return attrs, nil
}

// applyPoolDefaults fills the values AWS returns for settings a create or
// update request leaves out.
func applyPoolDefaults(st *UserPoolSettings) {
	if st.Policies == nil {
		st.Policies = &UserPoolPolicyType{}
	}
	if st.Policies.PasswordPolicy == nil {
		st.Policies.PasswordPolicy = &PasswordPolicyType{
			RequireUppercase: true, RequireLowercase: true, RequireNumbers: true, RequireSymbols: true,
		}
	}
	pp := st.Policies.PasswordPolicy
	if pp.MinimumLength == nil {
		pp.MinimumLength = ptr(int32(8))
	}
	if pp.TemporaryPasswordValidityDays == 0 {
		pp.TemporaryPasswordValidityDays = 7
	}
	if st.Policies.SignInPolicy == nil {
		st.Policies.SignInPolicy = &SignInPolicyType{AllowedFirstAuthFactors: []string{"PASSWORD"}}
	}
	if st.DeletionProtection == "" {
		st.DeletionProtection = "INACTIVE"
	}
	if st.MfaConfiguration == "" {
		st.MfaConfiguration = "OFF"
	}
	if st.AdminCreateUserConfig == nil {
		st.AdminCreateUserConfig = &AdminCreateUserConfigType{}
	}
	if len(st.EmailConfiguration) == 0 {
		st.EmailConfiguration = json.RawMessage(`{"EmailSendingAccount":"COGNITO_DEFAULT"}`)
	}
	if st.UserPoolTier == "" {
		st.UserPoolTier = "ESSENTIALS"
	}

	// The top-level verification messages and the template are two views of
	// the same settings; AWS keeps them in step.
	t := st.VerificationMessageTemplate
	if t == nil {
		t = &VerificationMessageTemplateType{}
		st.VerificationMessageTemplate = t
	}
	if t.DefaultEmailOption == "" {
		t.DefaultEmailOption = "CONFIRM_WITH_CODE"
	}
	mirror := func(top **string, tmpl **string) {
		switch {
		case *top != nil:
			*tmpl = *top
		case *tmpl != nil:
			*top = *tmpl
		}
	}
	mirror(&st.EmailVerificationMessage, &t.EmailMessage)
	mirror(&st.EmailVerificationSubject, &t.EmailSubject)
	mirror(&st.SmsVerificationMessage, &t.SmsMessage)
}

func (p *pool) caseSensitive() bool {
	return p.Config.UsernameConfiguration != nil && p.Config.UsernameConfiguration.CaseSensitive
}

func (p *pool) passwordPolicy() *PasswordPolicyType {
	if p.Config.Policies != nil && p.Config.Policies.PasswordPolicy != nil {
		return p.Config.Policies.PasswordPolicy
	}
	return &PasswordPolicyType{MinimumLength: ptr(int32(8)), TemporaryPasswordValidityDays: 7}
}

// lambdaConfig decodes the pool's trigger configuration.
func (p *pool) lambdaConfig() LambdaConfigType {
	var lc LambdaConfigType
	if len(p.Config.LambdaConfig) > 0 {
		_ = json.Unmarshal(p.Config.LambdaConfig, &lc)
	}
	return lc
}

// describe returns the DescribeUserPool view of p.
func (p *pool) describe() UserPoolType {
	out := p.Config
	out.EstimatedNumberOfUsers = len(p.Users)
	if p.Domain != nil {
		out.Domain = p.Domain.Domain
	}
	return cloneJSON(out)
}

type CreateUserPoolInput struct {
	PoolName string `json:"PoolName"`
	UserPoolSettings
	Schema                []schemaAttributeInput     `json:"Schema"`
	AliasAttributes       []string                   `json:"AliasAttributes"`
	UsernameAttributes    []string                   `json:"UsernameAttributes"`
	UsernameConfiguration *UsernameConfigurationType `json:"UsernameConfiguration"`
}

type UserPoolOutput struct {
	UserPool UserPoolType `json:"UserPool"`
}

func (s *Service) CreateUserPool(in *CreateUserPoolInput) (*UserPoolOutput, error) {
	if strings.TrimSpace(in.PoolName) == "" {
		return nil, invalidParameter("1 validation error detected: Value null at 'poolName' failed to satisfy constraint: Member must not be null")
	}
	if len(in.AliasAttributes) > 0 && len(in.UsernameAttributes) > 0 {
		return nil, invalidParameter("Only one of the aliasAttributes or usernameAttributes can be set in a User Pool.")
	}
	schema, err := buildSchema(in.Schema)
	if err != nil {
		return nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}
	keyPEM, err := encodeKeyPEM(key)
	if err != nil {
		return nil, err
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	sum := sha256.Sum256(der)

	settings := in.UserPoolSettings
	applyPoolDefaults(&settings)
	usernameCfg := in.UsernameConfiguration
	if usernameCfg == nil {
		usernameCfg = &UsernameConfigurationType{CaseSensitive: false}
	}

	now := EpochTime(s.now())
	id := newPoolID(s.cfg.Region)
	p := &pool{
		Config: UserPoolType{
			Id:                    id,
			Name:                  in.PoolName,
			Arn:                   fmt.Sprintf("arn:aws:cognito-idp:%s:%s:userpool/%s", s.cfg.Region, s.cfg.AccountID, id),
			UserPoolSettings:      settings,
			SchemaAttributes:      schema,
			AliasAttributes:       in.AliasAttributes,
			UsernameAttributes:    in.UsernameAttributes,
			UsernameConfiguration: usernameCfg,
			CreationDate:          now,
			LastModifiedDate:      now,
		},
		Mfa:        MfaSettings{MfaConfiguration: settings.MfaConfiguration},
		KeyID:      base64.RawURLEncoding.EncodeToString(sum[:])[:43],
		SigningKey: keyPEM,
		key:        key,
	}
	p.ensureMaps()

	s.mu.Lock()
	s.pools[id] = p
	out := p.describe()
	s.mu.Unlock()
	s.index.addPool(s.cfg.AccountID, id)
	s.markDirty()
	return &UserPoolOutput{UserPool: out}, nil
}

type PoolIDInput struct {
	UserPoolId string `json:"UserPoolId"`
}

func (s *Service) DescribeUserPool(in *PoolIDInput) (*UserPoolOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	return &UserPoolOutput{UserPool: p.describe()}, nil
}

type UpdateUserPoolInput struct {
	UserPoolId string `json:"UserPoolId"`
	UserPoolSettings
}

// UpdateUserPool replaces the pool's settings. As in AWS, settings the
// request leaves out return to their defaults; tags and the MFA setting are
// kept when absent because they are managed by their own operations too.
func (s *Service) UpdateUserPool(in *UpdateUserPoolInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return struct{}{}, err
	}
	settings := in.UserPoolSettings
	if settings.UserPoolTags == nil {
		settings.UserPoolTags = p.Config.UserPoolTags
	}
	if settings.MfaConfiguration == "" {
		settings.MfaConfiguration = p.Config.MfaConfiguration
	}
	applyPoolDefaults(&settings)
	p.Config.UserPoolSettings = settings
	p.Mfa.MfaConfiguration = settings.MfaConfiguration
	p.Config.LastModifiedDate = EpochTime(s.now())
	s.markDirty()
	return struct{}{}, nil
}

func (s *Service) DeleteUserPool(in *PoolIDInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return struct{}{}, err
	}
	if p.Config.DeletionProtection == "ACTIVE" {
		return struct{}{}, invalidParameter("The user pool cannot be deleted because deletion protection is activated. Deletion protection must be inactivated first.")
	}
	delete(s.pools, in.UserPoolId)
	s.index.removePool(in.UserPoolId)
	s.markDirty()
	return struct{}{}, nil
}

type ListUserPoolsInput struct {
	MaxResults int    `json:"MaxResults"`
	NextToken  string `json:"NextToken"`
}

type UserPoolDescriptionType struct {
	Id               string          `json:"Id"`
	Name             string          `json:"Name"`
	LambdaConfig     json.RawMessage `json:"LambdaConfig,omitempty"`
	CreationDate     EpochTime       `json:"CreationDate"`
	LastModifiedDate EpochTime       `json:"LastModifiedDate"`
}

type ListUserPoolsOutput struct {
	UserPools []UserPoolDescriptionType `json:"UserPools"`
	NextToken string                    `json:"NextToken,omitempty"`
}

func (s *Service) ListUserPools(in *ListUserPoolsInput) (*ListUserPoolsOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := sortedKeys(s.pools)
	page, next, err := paginate(ids, in.MaxResults, in.NextToken, 60)
	if err != nil {
		return nil, err
	}
	out := &ListUserPoolsOutput{UserPools: []UserPoolDescriptionType{}, NextToken: next}
	for _, id := range page {
		p := s.pools[id]
		out.UserPools = append(out.UserPools, UserPoolDescriptionType{
			Id: id, Name: p.Config.Name, LambdaConfig: p.Config.LambdaConfig,
			CreationDate: p.Config.CreationDate, LastModifiedDate: p.Config.LastModifiedDate,
		})
	}
	return out, nil
}

type AddCustomAttributesInput struct {
	UserPoolId       string                 `json:"UserPoolId"`
	CustomAttributes []schemaAttributeInput `json:"CustomAttributes"`
}

func (s *Service) AddCustomAttributes(in *AddCustomAttributesInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return struct{}{}, err
	}
	for _, in := range in.CustomAttributes {
		a := schemaAttribute(in)
		if !strings.HasPrefix(a.Name, "custom:") && !strings.HasPrefix(a.Name, "dev:") {
			a.Name = "custom:" + a.Name
		}
		for _, existing := range p.Config.SchemaAttributes {
			if existing.Name == a.Name {
				return struct{}{}, invalidParameter("Existing attribute already has name %s.", a.Name)
			}
		}
		p.Config.SchemaAttributes = append(p.Config.SchemaAttributes, a)
	}
	p.Config.LastModifiedDate = EpochTime(s.now())
	s.markDirty()
	return struct{}{}, nil
}

func (s *Service) GetUserPoolMfaConfig(in *PoolIDInput) (*MfaSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	out := p.Mfa
	if out.MfaConfiguration == "" {
		out.MfaConfiguration = "OFF"
	}
	return &out, nil
}

type SetUserPoolMfaConfigInput struct {
	UserPoolId string `json:"UserPoolId"`
	MfaSettings
}

// SetUserPoolMfaConfig stores the MFA configuration. Tarn never challenges for
// MFA; the settings exist so Terraform can manage them.
func (s *Service) SetUserPoolMfaConfig(in *SetUserPoolMfaConfigInput) (*MfaSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	p.Mfa = in.MfaSettings
	if p.Mfa.MfaConfiguration == "" {
		p.Mfa.MfaConfiguration = "OFF"
	}
	p.Config.MfaConfiguration = p.Mfa.MfaConfiguration
	s.markDirty()
	out := p.Mfa
	return &out, nil
}

// --- Tags ---

type ResourceArnInput struct {
	ResourceArn string `json:"ResourceArn"`
}

type TagsOutput struct {
	Tags map[string]string `json:"Tags"`
}

func (s *Service) poolByArn(arn string) (*pool, error) {
	for _, p := range s.pools {
		if p.Config.Arn == arn {
			return p, nil
		}
	}
	return nil, resourceNotFound("Resource %s does not exist.", arn)
}

func (s *Service) ListTagsForResource(in *ResourceArnInput) (*TagsOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.poolByArn(in.ResourceArn)
	if err != nil {
		return nil, err
	}
	tags := make(map[string]string, len(p.Config.UserPoolTags))
	for k, v := range p.Config.UserPoolTags {
		tags[k] = v
	}
	return &TagsOutput{Tags: tags}, nil
}

type TagResourceInput struct {
	ResourceArn string            `json:"ResourceArn"`
	Tags        map[string]string `json:"Tags"`
}

func (s *Service) TagResource(in *TagResourceInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.poolByArn(in.ResourceArn)
	if err != nil {
		return struct{}{}, err
	}
	if p.Config.UserPoolTags == nil {
		p.Config.UserPoolTags = make(map[string]string)
	}
	for k, v := range in.Tags {
		p.Config.UserPoolTags[k] = v
	}
	s.markDirty()
	return struct{}{}, nil
}

type UntagResourceInput struct {
	ResourceArn string   `json:"ResourceArn"`
	TagKeys     []string `json:"TagKeys"`
}

func (s *Service) UntagResource(in *UntagResourceInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.poolByArn(in.ResourceArn)
	if err != nil {
		return struct{}{}, err
	}
	for _, k := range in.TagKeys {
		delete(p.Config.UserPoolTags, k)
	}
	if len(p.Config.UserPoolTags) == 0 {
		p.Config.UserPoolTags = nil
	}
	s.markDirty()
	return struct{}{}, nil
}

// --- Domains ---

type DomainInput struct {
	Domain              string          `json:"Domain"`
	UserPoolId          string          `json:"UserPoolId"`
	CustomDomainConfig  json.RawMessage `json:"CustomDomainConfig"`
	ManagedLoginVersion *int32          `json:"ManagedLoginVersion"`
}

type DomainOutput struct {
	CloudFrontDomain    string `json:"CloudFrontDomain,omitempty"`
	ManagedLoginVersion *int32 `json:"ManagedLoginVersion,omitempty"`
}

func (s *Service) poolByDomain(domain string) *pool {
	for _, p := range s.pools {
		if p.Domain != nil && p.Domain.Domain == domain {
			return p
		}
	}
	return nil
}

// CreateUserPoolDomain stores a domain for Terraform. Tarn serves no hosted UI
// on it.
func (s *Service) CreateUserPoolDomain(in *DomainInput) (*DomainOutput, error) {
	if in.Domain == "" {
		return nil, invalidParameter("Domain is required.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	if s.poolByDomain(in.Domain) != nil {
		return nil, invalidParameter("Domain already exists.")
	}
	if p.Domain != nil {
		return nil, invalidParameter("User pool already has a domain configured.")
	}
	cf := ""
	if len(in.CustomDomainConfig) > 0 {
		cf = strings.ToLower(newOpaque(14)) + ".cloudfront.net"
	}
	version := in.ManagedLoginVersion
	if version == nil {
		version = ptr(int32(1))
	}
	p.Domain = &DomainDescriptionType{
		UserPoolId:             p.Config.Id,
		AWSAccountId:           s.cfg.AccountID,
		Domain:                 in.Domain,
		CloudFrontDistribution: cf,
		Version:                strconv.FormatInt(s.now().Unix(), 10),
		Status:                 "ACTIVE",
		CustomDomainConfig:     in.CustomDomainConfig,
		ManagedLoginVersion:    version,
	}
	s.markDirty()
	return &DomainOutput{CloudFrontDomain: cf, ManagedLoginVersion: version}, nil
}

type DescribeDomainOutput struct {
	DomainDescription DomainDescriptionType `json:"DomainDescription"`
}

// DescribeUserPoolDomain returns an empty description for an unknown domain,
// as AWS does.
func (s *Service) DescribeUserPoolDomain(in *DomainInput) (*DescribeDomainOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p := s.poolByDomain(in.Domain); p != nil {
		return &DescribeDomainOutput{DomainDescription: *p.Domain}, nil
	}
	return &DescribeDomainOutput{}, nil
}

func (s *Service) UpdateUserPoolDomain(in *DomainInput) (*DomainOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.poolByDomain(in.Domain)
	if p == nil || p.Config.Id != in.UserPoolId {
		return nil, resourceNotFound("Domain %s does not exist.", in.Domain)
	}
	if len(in.CustomDomainConfig) > 0 {
		p.Domain.CustomDomainConfig = in.CustomDomainConfig
	}
	if in.ManagedLoginVersion != nil {
		p.Domain.ManagedLoginVersion = in.ManagedLoginVersion
	}
	s.markDirty()
	return &DomainOutput{CloudFrontDomain: p.Domain.CloudFrontDistribution, ManagedLoginVersion: p.Domain.ManagedLoginVersion}, nil
}

func (s *Service) DeleteUserPoolDomain(in *DomainInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.poolByDomain(in.Domain)
	if p == nil || p.Config.Id != in.UserPoolId {
		return struct{}{}, resourceNotFound("Domain %s does not exist.", in.Domain)
	}
	p.Domain = nil
	s.markDirty()
	return struct{}{}, nil
}

// --- Resource servers ---

type ResourceServerInput struct {
	UserPoolId string                    `json:"UserPoolId"`
	Identifier string                    `json:"Identifier"`
	Name       string                    `json:"Name"`
	Scopes     []ResourceServerScopeType `json:"Scopes"`
}

type ResourceServerOutput struct {
	ResourceServer ResourceServerType `json:"ResourceServer"`
}

func (s *Service) CreateResourceServer(in *ResourceServerInput) (*ResourceServerOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	if in.Identifier == "" || in.Name == "" {
		return nil, invalidParameter("Identifier and Name are required.")
	}
	if _, ok := p.ResourceServers[in.Identifier]; ok {
		return nil, invalidParameter("%s already exists in user pool %s.", in.Identifier, in.UserPoolId)
	}
	rs := &ResourceServerType{UserPoolId: in.UserPoolId, Identifier: in.Identifier, Name: in.Name, Scopes: in.Scopes}
	p.ResourceServers[in.Identifier] = rs
	s.markDirty()
	return &ResourceServerOutput{ResourceServer: *rs}, nil
}

func (s *Service) DescribeResourceServer(in *ResourceServerInput) (*ResourceServerOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	rs, ok := p.ResourceServers[in.Identifier]
	if !ok {
		return nil, resourceNotFound("Resource server %s does not exist.", in.Identifier)
	}
	return &ResourceServerOutput{ResourceServer: *rs}, nil
}

func (s *Service) UpdateResourceServer(in *ResourceServerInput) (*ResourceServerOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	rs, ok := p.ResourceServers[in.Identifier]
	if !ok {
		return nil, resourceNotFound("Resource server %s does not exist.", in.Identifier)
	}
	rs.Name = in.Name
	rs.Scopes = in.Scopes
	s.markDirty()
	return &ResourceServerOutput{ResourceServer: *rs}, nil
}

func (s *Service) DeleteResourceServer(in *ResourceServerInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return struct{}{}, err
	}
	if _, ok := p.ResourceServers[in.Identifier]; !ok {
		return struct{}{}, resourceNotFound("Resource server %s does not exist.", in.Identifier)
	}
	delete(p.ResourceServers, in.Identifier)
	s.markDirty()
	return struct{}{}, nil
}

type ListResourceServersInput struct {
	UserPoolId string `json:"UserPoolId"`
	MaxResults int    `json:"MaxResults"`
	NextToken  string `json:"NextToken"`
}

type ListResourceServersOutput struct {
	ResourceServers []ResourceServerType `json:"ResourceServers"`
	NextToken       string               `json:"NextToken,omitempty"`
}

func (s *Service) ListResourceServers(in *ListResourceServersInput) (*ListResourceServersOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	page, next, err := paginate(sortedKeys(p.ResourceServers), in.MaxResults, in.NextToken, 50)
	if err != nil {
		return nil, err
	}
	out := &ListResourceServersOutput{ResourceServers: []ResourceServerType{}, NextToken: next}
	for _, id := range page {
		out.ResourceServers = append(out.ResourceServers, *p.ResourceServers[id])
	}
	return out, nil
}

// --- Pagination ---

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// paginate returns one page of keys. The token is the offset of the next page.
func paginate(keys []string, limit int, token string, maxLimit int) ([]string, string, error) {
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}
	start := 0
	if token != "" {
		n, err := strconv.Atoi(token)
		if err != nil || n < 0 || n > len(keys) {
			return nil, "", invalidParameter("Invalid pagination token.")
		}
		start = n
	}
	end := min(start+limit, len(keys))
	next := ""
	if end < len(keys) {
		next = strconv.Itoa(end)
	}
	return keys[start:end], next, nil
}
