package cognito

import (
	"encoding/json"
	"math"
	"strconv"
	"time"
)

// Wire shapes below follow the Cognito User Pools JSON 1.1 API, so field names
// are the AWS member names. Settings Tarn stores but never acts on are kept as
// raw JSON and returned exactly as given, which keeps Terraform plans clean.

// EpochTime is a timestamp encoded as fractional epoch seconds, the JSON 1.1
// timestamp format.
type EpochTime time.Time

func (t EpochTime) MarshalJSON() ([]byte, error) {
	tt := time.Time(t)
	return []byte(strconv.FormatFloat(float64(tt.UnixMilli())/1000, 'f', -1, 64)), nil
}

func (t *EpochTime) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		parsed, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return err
		}
		*t = EpochTime(parsed)
		return nil
	}
	sec, frac := math.Modf(f)
	*t = EpochTime(time.Unix(int64(sec), int64(frac*1e9)).UTC())
	return nil
}

// Time returns t as a time.Time.
func (t EpochTime) Time() time.Time { return time.Time(t) }

// AttributeType is a user attribute name and value.
type AttributeType struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

type PasswordPolicyType struct {
	MinimumLength                 *int32 `json:"MinimumLength,omitempty"`
	RequireUppercase              bool   `json:"RequireUppercase"`
	RequireLowercase              bool   `json:"RequireLowercase"`
	RequireNumbers                bool   `json:"RequireNumbers"`
	RequireSymbols                bool   `json:"RequireSymbols"`
	PasswordHistorySize           *int32 `json:"PasswordHistorySize,omitempty"`
	TemporaryPasswordValidityDays int32  `json:"TemporaryPasswordValidityDays"`
}

type SignInPolicyType struct {
	AllowedFirstAuthFactors []string `json:"AllowedFirstAuthFactors,omitempty"`
}

type UserPoolPolicyType struct {
	PasswordPolicy *PasswordPolicyType `json:"PasswordPolicy,omitempty"`
	SignInPolicy   *SignInPolicyType   `json:"SignInPolicy,omitempty"`
}

type NumberAttributeConstraintsType struct {
	MinValue *string `json:"MinValue,omitempty"`
	MaxValue *string `json:"MaxValue,omitempty"`
}

type StringAttributeConstraintsType struct {
	MinLength *string `json:"MinLength,omitempty"`
	MaxLength *string `json:"MaxLength,omitempty"`
}

type SchemaAttributeType struct {
	Name                       string                          `json:"Name"`
	AttributeDataType          string                          `json:"AttributeDataType,omitempty"`
	DeveloperOnlyAttribute     bool                            `json:"DeveloperOnlyAttribute"`
	Mutable                    bool                            `json:"Mutable"`
	Required                   bool                            `json:"Required"`
	NumberAttributeConstraints *NumberAttributeConstraintsType `json:"NumberAttributeConstraints,omitempty"`
	StringAttributeConstraints *StringAttributeConstraintsType `json:"StringAttributeConstraints,omitempty"`
}

// schemaAttributeInput mirrors SchemaAttributeType with optional booleans so
// unset values take the AWS defaults (Mutable true, others false).
type schemaAttributeInput struct {
	Name                       string                          `json:"Name"`
	AttributeDataType          string                          `json:"AttributeDataType"`
	DeveloperOnlyAttribute     *bool                           `json:"DeveloperOnlyAttribute"`
	Mutable                    *bool                           `json:"Mutable"`
	Required                   *bool                           `json:"Required"`
	NumberAttributeConstraints *NumberAttributeConstraintsType `json:"NumberAttributeConstraints"`
	StringAttributeConstraints *StringAttributeConstraintsType `json:"StringAttributeConstraints"`
}

type VerificationMessageTemplateType struct {
	SmsMessage         *string `json:"SmsMessage,omitempty"`
	EmailMessage       *string `json:"EmailMessage,omitempty"`
	EmailSubject       *string `json:"EmailSubject,omitempty"`
	EmailMessageByLink *string `json:"EmailMessageByLink,omitempty"`
	EmailSubjectByLink *string `json:"EmailSubjectByLink,omitempty"`
	DefaultEmailOption string  `json:"DefaultEmailOption,omitempty"`
}

type AdminCreateUserConfigType struct {
	AllowAdminCreateUserOnly  bool            `json:"AllowAdminCreateUserOnly"`
	UnusedAccountValidityDays *int32          `json:"UnusedAccountValidityDays,omitempty"`
	InviteMessageTemplate     json.RawMessage `json:"InviteMessageTemplate,omitempty"`
}

type UsernameConfigurationType struct {
	CaseSensitive bool `json:"CaseSensitive"`
}

// LambdaConfigType lists the trigger functions configured on a pool. Only the
// ARNs Tarn invokes are typed; the full object is kept raw for round trips.
type LambdaConfigType struct {
	PreSignUp                   string `json:"PreSignUp,omitempty"`
	CustomMessage               string `json:"CustomMessage,omitempty"`
	PostConfirmation            string `json:"PostConfirmation,omitempty"`
	PreAuthentication           string `json:"PreAuthentication,omitempty"`
	PostAuthentication          string `json:"PostAuthentication,omitempty"`
	DefineAuthChallenge         string `json:"DefineAuthChallenge,omitempty"`
	CreateAuthChallenge         string `json:"CreateAuthChallenge,omitempty"`
	VerifyAuthChallengeResponse string `json:"VerifyAuthChallengeResponse,omitempty"`
	PreTokenGeneration          string `json:"PreTokenGeneration,omitempty"`
	UserMigration               string `json:"UserMigration,omitempty"`
	PreTokenGenerationConfig    *struct {
		LambdaArn     string `json:"LambdaArn"`
		LambdaVersion string `json:"LambdaVersion"`
	} `json:"PreTokenGenerationConfig,omitempty"`
}

// UserPoolSettings holds the settings shared by CreateUserPool, UpdateUserPool
// and DescribeUserPool.
type UserPoolSettings struct {
	Policies                    *UserPoolPolicyType              `json:"Policies,omitempty"`
	DeletionProtection          string                           `json:"DeletionProtection,omitempty"`
	LambdaConfig                json.RawMessage                  `json:"LambdaConfig,omitempty"`
	AutoVerifiedAttributes      []string                         `json:"AutoVerifiedAttributes,omitempty"`
	SmsVerificationMessage      *string                          `json:"SmsVerificationMessage,omitempty"`
	EmailVerificationMessage    *string                          `json:"EmailVerificationMessage,omitempty"`
	EmailVerificationSubject    *string                          `json:"EmailVerificationSubject,omitempty"`
	VerificationMessageTemplate *VerificationMessageTemplateType `json:"VerificationMessageTemplate,omitempty"`
	SmsAuthenticationMessage    *string                          `json:"SmsAuthenticationMessage,omitempty"`
	UserAttributeUpdateSettings json.RawMessage                  `json:"UserAttributeUpdateSettings,omitempty"`
	MfaConfiguration            string                           `json:"MfaConfiguration,omitempty"`
	DeviceConfiguration         json.RawMessage                  `json:"DeviceConfiguration,omitempty"`
	EmailConfiguration          json.RawMessage                  `json:"EmailConfiguration,omitempty"`
	SmsConfiguration            json.RawMessage                  `json:"SmsConfiguration,omitempty"`
	UserPoolTags                map[string]string                `json:"UserPoolTags,omitempty"`
	AdminCreateUserConfig       *AdminCreateUserConfigType       `json:"AdminCreateUserConfig,omitempty"`
	UserPoolAddOns              json.RawMessage                  `json:"UserPoolAddOns,omitempty"`
	AccountRecoverySetting      json.RawMessage                  `json:"AccountRecoverySetting,omitempty"`
	UserPoolTier                string                           `json:"UserPoolTier,omitempty"`
}

// UserPoolType is the DescribeUserPool shape.
type UserPoolType struct {
	Id   string `json:"Id"`
	Name string `json:"Name"`
	Arn  string `json:"Arn"`
	UserPoolSettings
	SchemaAttributes       []SchemaAttributeType      `json:"SchemaAttributes"`
	AliasAttributes        []string                   `json:"AliasAttributes,omitempty"`
	UsernameAttributes     []string                   `json:"UsernameAttributes,omitempty"`
	UsernameConfiguration  *UsernameConfigurationType `json:"UsernameConfiguration,omitempty"`
	EstimatedNumberOfUsers int                        `json:"EstimatedNumberOfUsers"`
	Domain                 string                     `json:"Domain,omitempty"`
	CreationDate           EpochTime                  `json:"CreationDate"`
	LastModifiedDate       EpochTime                  `json:"LastModifiedDate"`
}

// MfaSettings is the GetUserPoolMfaConfig shape, stored but never challenged.
type MfaSettings struct {
	SmsMfaConfiguration           json.RawMessage `json:"SmsMfaConfiguration,omitempty"`
	SoftwareTokenMfaConfiguration json.RawMessage `json:"SoftwareTokenMfaConfiguration,omitempty"`
	EmailMfaConfiguration         json.RawMessage `json:"EmailMfaConfiguration,omitempty"`
	WebAuthnConfiguration         json.RawMessage `json:"WebAuthnConfiguration,omitempty"`
	MfaConfiguration              string          `json:"MfaConfiguration,omitempty"`
}

type TokenValidityUnitsType struct {
	AccessToken  string `json:"AccessToken,omitempty"`
	IdToken      string `json:"IdToken,omitempty"`
	RefreshToken string `json:"RefreshToken,omitempty"`
}

// UserPoolClientSettings holds the settings shared by CreateUserPoolClient,
// UpdateUserPoolClient and DescribeUserPoolClient.
type UserPoolClientSettings struct {
	ClientName                               string                  `json:"ClientName"`
	RefreshTokenValidity                     int32                   `json:"RefreshTokenValidity,omitempty"`
	AccessTokenValidity                      *int32                  `json:"AccessTokenValidity,omitempty"`
	IdTokenValidity                          *int32                  `json:"IdTokenValidity,omitempty"`
	TokenValidityUnits                       *TokenValidityUnitsType `json:"TokenValidityUnits,omitempty"`
	ReadAttributes                           []string                `json:"ReadAttributes,omitempty"`
	WriteAttributes                          []string                `json:"WriteAttributes,omitempty"`
	ExplicitAuthFlows                        []string                `json:"ExplicitAuthFlows,omitempty"`
	SupportedIdentityProviders               []string                `json:"SupportedIdentityProviders,omitempty"`
	CallbackURLs                             []string                `json:"CallbackURLs,omitempty"`
	LogoutURLs                               []string                `json:"LogoutURLs,omitempty"`
	DefaultRedirectURI                       *string                 `json:"DefaultRedirectURI,omitempty"`
	AllowedOAuthFlows                        []string                `json:"AllowedOAuthFlows,omitempty"`
	AllowedOAuthScopes                       []string                `json:"AllowedOAuthScopes,omitempty"`
	AllowedOAuthFlowsUserPoolClient          bool                    `json:"AllowedOAuthFlowsUserPoolClient"`
	AnalyticsConfiguration                   json.RawMessage         `json:"AnalyticsConfiguration,omitempty"`
	PreventUserExistenceErrors               string                  `json:"PreventUserExistenceErrors,omitempty"`
	EnableTokenRevocation                    *bool                   `json:"EnableTokenRevocation,omitempty"`
	EnablePropagateAdditionalUserContextData bool                    `json:"EnablePropagateAdditionalUserContextData"`
	AuthSessionValidity                      int32                   `json:"AuthSessionValidity,omitempty"`
	RefreshTokenRotation                     json.RawMessage         `json:"RefreshTokenRotation,omitempty"`
}

// UserPoolClientType is the DescribeUserPoolClient shape.
type UserPoolClientType struct {
	UserPoolId   string `json:"UserPoolId"`
	ClientId     string `json:"ClientId"`
	ClientSecret string `json:"ClientSecret,omitempty"`
	UserPoolClientSettings
	CreationDate     EpochTime `json:"CreationDate"`
	LastModifiedDate EpochTime `json:"LastModifiedDate"`
}

type GroupType struct {
	GroupName        string    `json:"GroupName"`
	UserPoolId       string    `json:"UserPoolId"`
	Description      *string   `json:"Description,omitempty"`
	RoleArn          *string   `json:"RoleArn,omitempty"`
	Precedence       *int32    `json:"Precedence,omitempty"`
	CreationDate     EpochTime `json:"CreationDate"`
	LastModifiedDate EpochTime `json:"LastModifiedDate"`
}

type ResourceServerScopeType struct {
	ScopeName        string `json:"ScopeName"`
	ScopeDescription string `json:"ScopeDescription"`
}

type ResourceServerType struct {
	UserPoolId string                    `json:"UserPoolId"`
	Identifier string                    `json:"Identifier"`
	Name       string                    `json:"Name"`
	Scopes     []ResourceServerScopeType `json:"Scopes,omitempty"`
}

type DomainDescriptionType struct {
	UserPoolId             string          `json:"UserPoolId,omitempty"`
	AWSAccountId           string          `json:"AWSAccountId,omitempty"`
	Domain                 string          `json:"Domain,omitempty"`
	S3Bucket               string          `json:"S3Bucket,omitempty"`
	CloudFrontDistribution string          `json:"CloudFrontDistribution,omitempty"`
	Version                string          `json:"Version,omitempty"`
	Status                 string          `json:"Status,omitempty"`
	CustomDomainConfig     json.RawMessage `json:"CustomDomainConfig,omitempty"`
	ManagedLoginVersion    *int32          `json:"ManagedLoginVersion,omitempty"`
}

// UserType is the ListUsers and AdminCreateUser user shape.
type UserType struct {
	Username             string          `json:"Username"`
	Attributes           []AttributeType `json:"Attributes"`
	UserCreateDate       EpochTime       `json:"UserCreateDate"`
	UserLastModifiedDate EpochTime       `json:"UserLastModifiedDate"`
	Enabled              bool            `json:"Enabled"`
	UserStatus           string          `json:"UserStatus"`
}

type CodeDeliveryDetailsType struct {
	Destination    string `json:"Destination"`
	DeliveryMedium string `json:"DeliveryMedium"`
	AttributeName  string `json:"AttributeName"`
}

type AuthenticationResultType struct {
	AccessToken  string `json:"AccessToken"`
	ExpiresIn    int    `json:"ExpiresIn"`
	TokenType    string `json:"TokenType"`
	RefreshToken string `json:"RefreshToken,omitempty"`
	IdToken      string `json:"IdToken"`
}

// User status values.
const (
	StatusUnconfirmed         = "UNCONFIRMED"
	StatusConfirmed           = "CONFIRMED"
	StatusForceChangePassword = "FORCE_CHANGE_PASSWORD"
	StatusResetRequired       = "RESET_REQUIRED"
)
