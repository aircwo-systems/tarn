package cognito

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
)

func (p *pool) userKey(username string) string {
	if p.caseSensitive() {
		return username
	}
	return strings.ToLower(username)
}

// findUser resolves a sign-in name: the username itself, an email or phone
// number when the pool signs in with those, or a verified alias.
func (p *pool) findUser(login string) *user {
	if login == "" {
		return nil
	}
	if u := p.Users[p.userKey(login)]; u != nil {
		return u
	}
	match := func(attr string, verifiedOnly bool) *user {
		for _, u := range p.Users {
			v := u.Attributes[attr]
			if v == "" || !strings.EqualFold(v, login) {
				continue
			}
			if verifiedOnly && attr != "preferred_username" && u.Attributes[attr+"_verified"] != "true" {
				continue
			}
			return u
		}
		return nil
	}
	for _, attr := range p.Config.UsernameAttributes {
		if u := match(attr, false); u != nil {
			return u
		}
	}
	for _, attr := range p.Config.AliasAttributes {
		if u := match(attr, true); u != nil {
			return u
		}
	}
	return nil
}

func (p *pool) mustFindUser(login string) (*user, error) {
	if u := p.findUser(login); u != nil {
		return u, nil
	}
	return nil, errUserNotFound
}

// resolveNewUsername applies UsernameAttributes: in such pools the sign-in
// name must be an email or phone number, which becomes an attribute, and the
// stored username is the user's sub.
func (p *pool) resolveNewUsername(login string, attrs map[string]string, sub string) (string, error) {
	if len(p.Config.UsernameAttributes) == 0 {
		if strings.TrimSpace(login) == "" {
			return "", invalidParameter("Username is required.")
		}
		return login, nil
	}
	switch {
	case slices.Contains(p.Config.UsernameAttributes, "email") && strings.Contains(login, "@"):
		attrs["email"] = login
	case slices.Contains(p.Config.UsernameAttributes, "phone_number") && strings.HasPrefix(login, "+"):
		attrs["phone_number"] = login
	default:
		return "", invalidParameter("Username should be either an email or a phone number.")
	}
	return sub, nil
}

// checkUsernameFree reports a clash with an existing username or, in pools
// that sign in with email or phone, with another user's attribute.
func (p *pool) checkUsernameFree(username string, attrs map[string]string, admin bool) error {
	exists := p.Users[p.userKey(username)] != nil
	if !exists {
		for _, attr := range p.Config.UsernameAttributes {
			if v := attrs[attr]; v != "" && p.findUser(v) != nil {
				if attr == "email" {
					return newError("UsernameExistsException", "An account with the given email already exists.")
				}
				return newError("UsernameExistsException", "An account with the given phone_number already exists.")
			}
		}
	}
	if exists {
		if admin {
			return newError("UsernameExistsException", "User account already exists")
		}
		return newError("UsernameExistsException", "User already exists")
	}
	return nil
}

// validateAttributes checks names against the schema and the formats AWS
// enforces. required checks that every required attribute is present.
func (p *pool) validateAttributes(attrs []AttributeType, required bool) error {
	seen := make(map[string]bool, len(attrs))
	for _, a := range attrs {
		seen[a.Name] = true
		sa := p.schemaAttribute(a.Name)
		if sa == nil {
			return invalidParameter("Attributes did not conform to the schema: %s: Attribute does not exist in the schema.", a.Name)
		}
		if a.Name == "sub" {
			return invalidParameter("Cannot modify an immutable attribute: sub")
		}
		switch a.Name {
		case "email":
			if !strings.Contains(a.Value, "@") {
				return invalidParameter("Invalid email address format.")
			}
		case "phone_number":
			if !regexp.MustCompile(`^\+\d{4,15}$`).MatchString(a.Value) {
				return invalidParameter("Invalid phone number format.")
			}
		case "email_verified", "phone_number_verified":
			if a.Value != "true" && a.Value != "false" {
				return invalidParameter("Invalid value for %s.", a.Name)
			}
		}
	}
	if required {
		for _, sa := range p.Config.SchemaAttributes {
			if sa.Required && sa.Name != "sub" && !seen[sa.Name] {
				return invalidParameter("Attributes did not conform to the schema: %s: The attribute %s is required", sa.Name, sa.Name)
			}
		}
	}
	return nil
}

func checkWritable(c *UserPoolClientType, attrs []AttributeType) error {
	if c == nil || len(c.WriteAttributes) == 0 {
		return nil
	}
	for _, a := range attrs {
		if !slices.Contains(c.WriteAttributes, a.Name) {
			return notAuthorized("A client attempted to write unauthorized attribute")
		}
	}
	return nil
}

func (s *Service) newUser(username, sub string, attrs map[string]string, status string) *user {
	now := s.now()
	attrs["sub"] = sub
	return &user{
		Username:   username,
		Sub:        sub,
		Attributes: attrs,
		Status:     status,
		Enabled:    true,
		Created:    now,
		Modified:   now,
	}
}

func attrMap(attrs []AttributeType) map[string]string {
	m := make(map[string]string, len(attrs)+1)
	for _, a := range attrs {
		m[a.Name] = a.Value
	}
	return m
}

// attributeList returns attributes in AWS order: sub first, the rest by name.
// names, when non-empty, limits the result.
func (u *user) attributeList(names []string) []AttributeType {
	out := make([]AttributeType, 0, len(u.Attributes))
	keys := sortedKeys(u.Attributes)
	sort.SliceStable(keys, func(i, j int) bool { return keys[i] == "sub" && keys[j] != "sub" })
	for _, k := range keys {
		if len(names) > 0 && !slices.Contains(names, k) {
			continue
		}
		out = append(out, AttributeType{Name: k, Value: u.Attributes[k]})
	}
	return out
}

func (u *user) userType(names []string) UserType {
	return UserType{
		Username:             u.Username,
		Attributes:           u.attributeList(names),
		UserCreateDate:       EpochTime(u.Created),
		UserLastModifiedDate: EpochTime(u.Modified),
		Enabled:              u.Enabled,
		UserStatus:           u.Status,
	}
}

// applyAttributes writes attrs to u. Changing an email or phone number clears
// its verified flag unless the same request sets it.
func (u *user) applyAttributes(attrs []AttributeType) (changed []string) {
	set := attrMap(attrs)
	for _, a := range attrs {
		if u.Attributes[a.Name] == a.Value {
			continue
		}
		u.Attributes[a.Name] = a.Value
		if a.Name == "email" || a.Name == "phone_number" {
			changed = append(changed, a.Name)
			if _, ok := set[a.Name+"_verified"]; !ok && u.Attributes[a.Name+"_verified"] == "true" {
				u.Attributes[a.Name+"_verified"] = "false"
			}
		}
	}
	return changed
}

// --- Admin user operations ---

type AdminCreateUserInput struct {
	UserPoolId             string            `json:"UserPoolId"`
	Username               string            `json:"Username"`
	UserAttributes         []AttributeType   `json:"UserAttributes"`
	ValidationData         []AttributeType   `json:"ValidationData"`
	TemporaryPassword      string            `json:"TemporaryPassword"`
	ForceAliasCreation     bool              `json:"ForceAliasCreation"`
	MessageAction          string            `json:"MessageAction"`
	DesiredDeliveryMediums []string          `json:"DesiredDeliveryMediums"`
	ClientMetadata         map[string]string `json:"ClientMetadata"`
}

type AdminCreateUserOutput struct {
	User UserType `json:"User"`
}

func (s *Service) AdminCreateUser(in *AdminCreateUserInput) (*AdminCreateUserOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	pp := p.passwordPolicy()

	if in.MessageAction == "RESEND" {
		u, err := p.mustFindUser(in.Username)
		if err != nil {
			return nil, err
		}
		if u.Status != StatusForceChangePassword {
			return nil, newError("UnsupportedUserStateException", "Resend not possible. %s status is not FORCE_CHANGE_PASSWORD", u.Username)
		}
		if err := s.setTemporaryPassword(p, u, in.TemporaryPassword, in.MessageAction); err != nil {
			return nil, err
		}
		return &AdminCreateUserOutput{User: u.userType(nil)}, nil
	}

	if err := p.validateAttributes(in.UserAttributes, false); err != nil {
		return nil, err
	}
	if in.TemporaryPassword != "" {
		if err := checkPasswordPolicy(pp, in.TemporaryPassword); err != nil {
			return nil, err
		}
	}
	sub := uuid.NewString()
	attrs := attrMap(in.UserAttributes)
	username, err := p.resolveNewUsername(in.Username, attrs, sub)
	if err != nil {
		return nil, err
	}
	if err := p.checkUsernameFree(username, attrs, true); err != nil {
		return nil, err
	}
	u := s.newUser(username, sub, attrs, StatusForceChangePassword)
	if err := s.setTemporaryPassword(p, u, in.TemporaryPassword, in.MessageAction); err != nil {
		return nil, err
	}
	p.Users[p.userKey(username)] = u
	s.markDirty()
	return &AdminCreateUserOutput{User: u.userType(nil)}, nil
}

// setTemporaryPassword sets (or generates) a temporary password and, unless
// the message is suppressed, records it as an invite code.
func (s *Service) setTemporaryPassword(p *pool, u *user, pw, messageAction string) error {
	if pw == "" {
		pw = generateTemporaryPassword(p.passwordPolicy())
	}
	if err := p.setPassword(u, pw, true, s.now()); err != nil {
		return err
	}
	if messageAction != "SUPPRESS" {
		attr := "email"
		if u.Attributes["email"] == "" {
			attr = "phone_number"
		}
		s.storeCode(p, u, purposeInvite, attr, pw, u.TempExpires.Sub(s.now()))
	}
	return nil
}

type AdminUserInput struct {
	UserPoolId string `json:"UserPoolId"`
	Username   string `json:"Username"`
}

type AdminGetUserOutput struct {
	Username             string          `json:"Username"`
	UserAttributes       []AttributeType `json:"UserAttributes"`
	UserCreateDate       EpochTime       `json:"UserCreateDate"`
	UserLastModifiedDate EpochTime       `json:"UserLastModifiedDate"`
	Enabled              bool            `json:"Enabled"`
	UserStatus           string          `json:"UserStatus"`
	UserMFASettingList   []string        `json:"UserMFASettingList,omitempty"`
}

// adminUser resolves the pool and user for an Admin* call. Callers hold s.mu.
func (s *Service) adminUser(poolID, username string) (*pool, *user, error) {
	p, err := s.lookupPool(poolID)
	if err != nil {
		return nil, nil, err
	}
	u, err := p.mustFindUser(username)
	if err != nil {
		return nil, nil, err
	}
	return p, u, nil
}

func (s *Service) AdminGetUser(in *AdminUserInput) (*AdminGetUserOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return nil, err
	}
	return &AdminGetUserOutput{
		Username:             u.Username,
		UserAttributes:       u.attributeList(nil),
		UserCreateDate:       EpochTime(u.Created),
		UserLastModifiedDate: EpochTime(u.Modified),
		Enabled:              u.Enabled,
		UserStatus:           u.Status,
	}, nil
}

type AdminUpdateUserAttributesInput struct {
	UserPoolId     string            `json:"UserPoolId"`
	Username       string            `json:"Username"`
	UserAttributes []AttributeType   `json:"UserAttributes"`
	ClientMetadata map[string]string `json:"ClientMetadata"`
}

func (s *Service) AdminUpdateUserAttributes(in *AdminUpdateUserAttributesInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	if err := p.validateAttributes(in.UserAttributes, false); err != nil {
		return struct{}{}, err
	}
	u.applyAttributes(in.UserAttributes)
	u.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}

type AdminDeleteUserAttributesInput struct {
	UserPoolId         string   `json:"UserPoolId"`
	Username           string   `json:"Username"`
	UserAttributeNames []string `json:"UserAttributeNames"`
}

func (s *Service) AdminDeleteUserAttributes(in *AdminDeleteUserAttributesInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	for _, name := range in.UserAttributeNames {
		if sa := p.schemaAttribute(name); sa != nil && sa.Required {
			return struct{}{}, invalidParameter("Cannot delete a required attribute: %s", name)
		}
	}
	for _, name := range in.UserAttributeNames {
		delete(u.Attributes, name)
	}
	u.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}

type AdminSetUserPasswordInput struct {
	UserPoolId string `json:"UserPoolId"`
	Username   string `json:"Username"`
	Password   string `json:"Password"`
	Permanent  bool   `json:"Permanent"`
}

func (s *Service) AdminSetUserPassword(in *AdminSetUserPasswordInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	if err := checkPasswordPolicy(p.passwordPolicy(), in.Password); err != nil {
		return struct{}{}, err
	}
	if err := p.setPassword(u, in.Password, !in.Permanent, s.now()); err != nil {
		return struct{}{}, err
	}
	if in.Permanent {
		u.Status = StatusConfirmed
	} else {
		u.Status = StatusForceChangePassword
	}
	delete(u.Codes, purposeInvite)
	u.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}

func (s *Service) AdminEnableUser(in *AdminUserInput) (struct{}, error) {
	return struct{}{}, s.setEnabled(in, true)
}

// AdminDisableUser disables the user and revokes their tokens, as AWS does.
func (s *Service) AdminDisableUser(in *AdminUserInput) (struct{}, error) {
	return struct{}{}, s.setEnabled(in, false)
}

func (s *Service) setEnabled(in *AdminUserInput, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return err
	}
	u.Enabled = enabled
	if !enabled {
		p.revokeSessions(u, "")
	}
	u.Modified = s.now()
	s.markDirty()
	return nil
}

func (s *Service) AdminDeleteUser(in *AdminUserInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	p.deleteUser(u)
	s.markDirty()
	return struct{}{}, nil
}

func (p *pool) deleteUser(u *user) {
	for _, sess := range u.Sessions {
		delete(p.refresh, sess.RefreshHash)
	}
	delete(p.Users, p.userKey(u.Username))
}

type AdminConfirmSignUpInput struct {
	UserPoolId     string            `json:"UserPoolId"`
	Username       string            `json:"Username"`
	ClientMetadata map[string]string `json:"ClientMetadata"`
}

func (s *Service) AdminConfirmSignUp(in *AdminConfirmSignUpInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	if u.Status != StatusUnconfirmed {
		return struct{}{}, notAuthorized("User cannot be confirmed. Current status is %s", u.Status)
	}
	u.Status = StatusConfirmed
	delete(u.Codes, purposeSignUp)
	u.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}

type AdminResetUserPasswordInput struct {
	UserPoolId     string            `json:"UserPoolId"`
	Username       string            `json:"Username"`
	ClientMetadata map[string]string `json:"ClientMetadata"`
}

// AdminResetUserPassword moves the user to RESET_REQUIRED and issues a reset
// code for ConfirmForgotPassword.
func (s *Service) AdminResetUserPassword(in *AdminResetUserPasswordInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	attr := recoveryAttribute(u)
	if attr == "" {
		return struct{}{}, invalidParameter("Cannot reset password for the user as there is no registered/verified email or phone_number")
	}
	u.Status = StatusResetRequired
	p.revokeSessions(u, "")
	s.issueCode(p, u, purposeReset, attr)
	u.Modified = s.now()
	s.markDirty()
	return struct{}{}, nil
}

// recoveryAttribute picks the verified email or phone number a reset code
// goes to.
func recoveryAttribute(u *user) string {
	for _, attr := range []string{"email", "phone_number"} {
		if u.Attributes[attr] != "" && u.Attributes[attr+"_verified"] == "true" {
			return attr
		}
	}
	return ""
}

func (s *Service) AdminUserGlobalSignOut(in *AdminUserInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	p.revokeSessions(u, "")
	s.markDirty()
	return struct{}{}, nil
}

type AdminListUserAuthEventsOutput struct {
	AuthEvents []any `json:"AuthEvents"`
}

// AdminListUserAuthEvents returns no events: advanced security is not emulated.
func (s *Service) AdminListUserAuthEvents(in *AdminUserInput) (*AdminListUserAuthEventsOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, _, err := s.adminUser(in.UserPoolId, in.Username); err != nil {
		return nil, err
	}
	return &AdminListUserAuthEventsOutput{AuthEvents: []any{}}, nil
}

// --- ListUsers ---

type ListUsersInput struct {
	UserPoolId      string   `json:"UserPoolId"`
	AttributesToGet []string `json:"AttributesToGet"`
	Limit           int      `json:"Limit"`
	PaginationToken string   `json:"PaginationToken"`
	Filter          string   `json:"Filter"`
}

type ListUsersOutput struct {
	Users           []UserType `json:"Users"`
	PaginationToken string     `json:"PaginationToken,omitempty"`
}

var filterPattern = regexp.MustCompile(`^\s*"?([\w:]+)"?\s*(\^?=)\s*"((?:[^"\\]|\\.)*)"\s*$`)

var filterableAttributes = []string{
	"username", "email", "phone_number", "name", "given_name", "family_name",
	"preferred_username", "cognito:user_status", "status", "sub",
}

// userFilter parses the ListUsers Filter subset `attr = "v"` and `attr ^= "v"`.
func userFilter(expr string) (func(*user) bool, error) {
	if strings.TrimSpace(expr) == "" {
		return func(*user) bool { return true }, nil
	}
	m := filterPattern.FindStringSubmatch(expr)
	if m == nil {
		return nil, invalidParameter("Error while parsing filter.")
	}
	attr, op, want := m[1], m[2], strings.ReplaceAll(m[3], `\"`, `"`)
	if !slices.Contains(filterableAttributes, attr) {
		return nil, invalidParameter("Invalid search attribute: %s", attr)
	}
	return func(u *user) bool {
		var v string
		switch attr {
		case "username":
			v = u.Username
		case "cognito:user_status":
			v = u.Status
		case "status":
			v = "Disabled"
			if u.Enabled {
				v = "Enabled"
			}
		default:
			v = u.Attributes[attr]
		}
		if op == "^=" {
			return strings.HasPrefix(strings.ToLower(v), strings.ToLower(want))
		}
		return strings.EqualFold(v, want)
	}, nil
}

func (s *Service) ListUsers(in *ListUsersInput) (*ListUsersOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	match, err := userFilter(in.Filter)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, k := range sortedKeys(p.Users) {
		if match(p.Users[k]) {
			keys = append(keys, k)
		}
	}
	page, next, err := paginate(keys, in.Limit, in.PaginationToken, 60)
	if err != nil {
		return nil, err
	}
	out := &ListUsersOutput{Users: []UserType{}, PaginationToken: next}
	for _, k := range page {
		out.Users = append(out.Users, p.Users[k].userType(in.AttributesToGet))
	}
	return out, nil
}

// --- Groups ---

type GroupInput struct {
	UserPoolId  string  `json:"UserPoolId"`
	GroupName   string  `json:"GroupName"`
	Description *string `json:"Description"`
	RoleArn     *string `json:"RoleArn"`
	Precedence  *int32  `json:"Precedence"`
}

type GroupOutput struct {
	Group GroupType `json:"Group"`
}

func groupNotFound() error { return resourceNotFound("Group not found.") }

func (s *Service) CreateGroup(in *GroupInput) (*GroupOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	if in.GroupName == "" {
		return nil, invalidParameter("GroupName is required.")
	}
	if _, ok := p.Groups[in.GroupName]; ok {
		return nil, newError("GroupExistsException", "A group with the name %s already exists.", in.GroupName)
	}
	now := EpochTime(s.now())
	g := &GroupType{
		GroupName: in.GroupName, UserPoolId: in.UserPoolId, Description: in.Description,
		RoleArn: in.RoleArn, Precedence: in.Precedence, CreationDate: now, LastModifiedDate: now,
	}
	p.Groups[in.GroupName] = g
	s.markDirty()
	return &GroupOutput{Group: *g}, nil
}

func (s *Service) GetGroup(in *GroupInput) (*GroupOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	g, ok := p.Groups[in.GroupName]
	if !ok {
		return nil, groupNotFound()
	}
	return &GroupOutput{Group: *g}, nil
}

func (s *Service) UpdateGroup(in *GroupInput) (*GroupOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	g, ok := p.Groups[in.GroupName]
	if !ok {
		return nil, groupNotFound()
	}
	if in.Description != nil {
		g.Description = in.Description
	}
	if in.RoleArn != nil {
		g.RoleArn = in.RoleArn
	}
	if in.Precedence != nil {
		g.Precedence = in.Precedence
	}
	g.LastModifiedDate = EpochTime(s.now())
	s.markDirty()
	return &GroupOutput{Group: *g}, nil
}

func (s *Service) DeleteGroup(in *GroupInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return struct{}{}, err
	}
	if _, ok := p.Groups[in.GroupName]; !ok {
		return struct{}{}, groupNotFound()
	}
	delete(p.Groups, in.GroupName)
	for _, u := range p.Users {
		u.Groups = slices.DeleteFunc(u.Groups, func(g string) bool { return g == in.GroupName })
	}
	s.markDirty()
	return struct{}{}, nil
}

type ListGroupsInput struct {
	UserPoolId string `json:"UserPoolId"`
	Username   string `json:"Username"`
	Limit      int    `json:"Limit"`
	NextToken  string `json:"NextToken"`
}

type ListGroupsOutput struct {
	Groups    []GroupType `json:"Groups"`
	NextToken string      `json:"NextToken,omitempty"`
}

func (s *Service) ListGroups(in *ListGroupsInput) (*ListGroupsOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	return listGroups(p, sortedKeys(p.Groups), in.Limit, in.NextToken)
}

func listGroups(p *pool, names []string, limit int, token string) (*ListGroupsOutput, error) {
	page, next, err := paginate(names, limit, token, 60)
	if err != nil {
		return nil, err
	}
	out := &ListGroupsOutput{Groups: []GroupType{}, NextToken: next}
	for _, n := range page {
		if g := p.Groups[n]; g != nil {
			out.Groups = append(out.Groups, *g)
		}
	}
	return out, nil
}

type UserGroupInput struct {
	UserPoolId string `json:"UserPoolId"`
	Username   string `json:"Username"`
	GroupName  string `json:"GroupName"`
}

func (s *Service) AdminAddUserToGroup(in *UserGroupInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	if _, ok := p.Groups[in.GroupName]; !ok {
		return struct{}{}, groupNotFound()
	}
	if !slices.Contains(u.Groups, in.GroupName) {
		u.Groups = append(u.Groups, in.GroupName)
		sort.Strings(u.Groups)
		s.markDirty()
	}
	return struct{}{}, nil
}

func (s *Service) AdminRemoveUserFromGroup(in *UserGroupInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return struct{}{}, err
	}
	if _, ok := p.Groups[in.GroupName]; !ok {
		return struct{}{}, groupNotFound()
	}
	u.Groups = slices.DeleteFunc(u.Groups, func(g string) bool { return g == in.GroupName })
	s.markDirty()
	return struct{}{}, nil
}

func (s *Service) AdminListGroupsForUser(in *ListGroupsInput) (*ListGroupsOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, u, err := s.adminUser(in.UserPoolId, in.Username)
	if err != nil {
		return nil, err
	}
	return listGroups(p, slices.Sorted(slices.Values(u.Groups)), in.Limit, in.NextToken)
}

type ListUsersInGroupInput struct {
	UserPoolId string `json:"UserPoolId"`
	GroupName  string `json:"GroupName"`
	Limit      int    `json:"Limit"`
	NextToken  string `json:"NextToken"`
}

type ListUsersInGroupOutput struct {
	Users     []UserType `json:"Users"`
	NextToken string     `json:"NextToken,omitempty"`
}

func (s *Service) ListUsersInGroup(in *ListUsersInGroupInput) (*ListUsersInGroupOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(in.UserPoolId)
	if err != nil {
		return nil, err
	}
	if _, ok := p.Groups[in.GroupName]; !ok {
		return nil, groupNotFound()
	}
	var keys []string
	for _, k := range sortedKeys(p.Users) {
		if slices.Contains(p.Users[k].Groups, in.GroupName) {
			keys = append(keys, k)
		}
	}
	page, next, err := paginate(keys, in.Limit, in.NextToken, 60)
	if err != nil {
		return nil, err
	}
	out := &ListUsersInGroupOutput{Users: []UserType{}, NextToken: next}
	for _, k := range page {
		out.Users = append(out.Users, p.Users[k].userType(nil))
	}
	return out, nil
}
