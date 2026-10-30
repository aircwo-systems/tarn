package awsconnection

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/processcreds"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/logging"
)

type Identity struct {
	AccountID string `json:"accountId"`
	ARN       string `json:"arn"`
}

type Credentials struct {
	Kind            string `json:"kind"`
	Profile         string `json:"profile,omitempty"`
	AccessKeyID     string `json:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	SessionToken    string `json:"sessionToken,omitempty"`
}

type Validator func(context.Context, string, Credentials) (Identity, error)

// ValidationError deliberately contains no upstream error or request details.
type ValidationError struct {
	Status  string `json:"status"`
	Message string `json:"error"`
}

func (e *ValidationError) Error() string { return e.Message }

var commercialRegion = regexp.MustCompile(`^(af|ap|ca|eu|il|me|mx|sa|us)-(central|east|west|north|south|northeast|northwest|southeast|southwest)-[1-9][0-9]*$`)

func validRegion(region string) bool { return commercialRegion.MatchString(region) }

// ValidateSTS constructs the service client directly, so shared config, endpoint
// overrides and the process's emulator credentials cannot influence validation.
func ValidateSTS(ctx context.Context, region string, c Credentials) (Identity, error) {
	return validateSTS(ctx, region, c, &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})
}

func validateSTS(ctx context.Context, region string, c Credentials, httpClient aws.HTTPClient) (Identity, error) {
	provider := aws.CredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, c.SessionToken))
	if c.Kind == "profile" {
		cfg, sso, err := loadProfileConfig(ctx, region, c.Profile, httpClient)
		if err != nil {
			return Identity{}, err
		}
		// Resolve before STS so a provider login failure can be distinguished
		// from a transport failure. The SDK owns refresh and its credential cache.
		if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
			return Identity{}, profileValidationError(err, c.Profile, sso)
		}
		identity, err := validateSTSProvider(ctx, region, cfg.Credentials, httpClient)
		if err != nil {
			return Identity{}, profileValidationError(err, c.Profile, sso)
		}
		return identity, nil
	}
	return validateSTSProvider(ctx, region, aws.NewCredentialsCache(provider), httpClient)
}

func validateSTSProvider(ctx context.Context, region string, provider aws.CredentialsProvider, httpClient aws.HTTPClient) (Identity, error) {
	client := sts.New(sts.Options{
		Region:           region,
		Credentials:      provider,
		HTTPClient:       httpClient,
		RetryMaxAttempts: 2,
		Logger:           logging.NewStandardLogger(io.Discard),
	})
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return Identity{}, classifyValidationError(err)
	}
	identity := Identity{AccountID: aws.ToString(out.Account), ARN: aws.ToString(out.Arn)}
	if len(identity.AccountID) != 12 || !strings.HasPrefix(identity.ARN, "arn:aws:") {
		return Identity{}, &ValidationError{"unavailable", "AWS returned an unsupported identity."}
	}
	return identity, nil
}

func loadProfileConfig(ctx context.Context, region, name string, client aws.HTTPClient) (aws.Config, bool, error) {
	configFiles, credentialFiles := profileFiles()
	shared, err := awsconfig.LoadSharedConfigProfile(ctx, name, func(o *awsconfig.LoadSharedConfigOptions) {
		o.ConfigFiles, o.CredentialsFiles = configFiles, credentialFiles
	})
	if err != nil || shared.Source != nil || shared.RoleARN != "" || shared.CredentialSource != "" {
		return aws.Config{}, false, &ValidationError{"unavailable", "Cannot load this AWS profile. Check its name and credential configuration on the Tarn server. Assume-role profiles are not supported yet."}
	}
	// Construct SDK providers from the selected profile only. Neither emulator
	// credentials nor endpoint overrides may influence SSO token refresh clients.
	var provider aws.CredentialsProvider
	usesSSO := shared.SSOSession != nil || shared.SSOStartURL != ""
	switch {
	case shared.Credentials.HasKeys():
		usesSSO = false
		provider = credentials.NewStaticCredentialsProvider(shared.Credentials.AccessKeyID, shared.Credentials.SecretAccessKey, shared.Credentials.SessionToken)
	case usesSSO:
		ssoRegion, startURL := shared.SSORegion, shared.SSOStartURL
		if shared.SSOSession != nil {
			ssoRegion, startURL = shared.SSOSession.SSORegion, shared.SSOSession.SSOStartURL
		}
		if ssoRegion == "" || startURL == "" || shared.SSOAccountID == "" || shared.SSORoleName == "" {
			return aws.Config{}, true, &ValidationError{"unavailable", "The AWS SSO profile is incomplete. Configure its session, account and role with the AWS CLI."}
		}
		var opts []func(*ssocreds.Options)
		if shared.SSOSession != nil {
			path, err := ssocreds.StandardCachedTokenFilepath(shared.SSOSession.Name)
			if err != nil {
				return aws.Config{}, true, &ValidationError{"unavailable", "Cannot locate the AWS SSO session cache on the Tarn server."}
			}
			tokenProvider := ssocreds.NewSSOTokenProvider(ssooidc.New(ssooidc.Options{Region: ssoRegion, HTTPClient: client, Logger: logging.NewStandardLogger(io.Discard)}), path)
			opts = append(opts, func(o *ssocreds.Options) { o.SSOTokenProvider = tokenProvider })
		}
		provider = ssocreds.New(sso.New(sso.Options{Region: ssoRegion, HTTPClient: client, Logger: logging.NewStandardLogger(io.Discard)}), shared.SSOAccountID, shared.SSORoleName, startURL, opts...)
	case shared.CredentialProcess != "":
		provider = processcreds.NewProvider(shared.CredentialProcess)
	default:
		return aws.Config{}, false, &ValidationError{"unavailable", "This AWS profile must configure SSO, access keys or credential_process. No ambient credentials were used."}
	}
	return aws.Config{Region: region, Credentials: aws.NewCredentialsCache(provider)}, usesSSO, nil
}

func profileValidationError(err error, profile string, sso bool) *ValidationError {
	var invalid *ssocreds.InvalidTokenError
	var apiErr smithy.APIError
	var safe *ValidationError
	login := errors.As(err, &invalid) || errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "cached SSO token is expired, or not present, and cannot be refreshed")
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "UnauthorizedException", "InvalidGrantException", "InvalidTokenException", "ExpiredToken", "ExpiredTokenException":
			login = true
		}
	}
	if errors.As(err, &safe) && safe.Status == "expired" {
		login = true
	}
	if sso && login {
		return &ValidationError{"needs-login", "Sign in again with aws sso login --profile " + shellQuote(profile) + " in your terminal, then test this connection again."}
	}
	if errors.As(err, &safe) {
		return safe
	}
	return classifyValidationError(err)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func classifyValidationError(err error) *ValidationError {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "ExpiredToken", "ExpiredTokenException":
			return &ValidationError{"expired", "The AWS session has expired. Replace its credentials."}
		case "InvalidClientTokenId", "InvalidIdentityToken", "SignatureDoesNotMatch", "UnrecognizedClientException", "InvalidSignatureException":
			return &ValidationError{"rejected", "AWS rejected these credentials. Check the access key, secret key and session token."}
		}
	}
	return &ValidationError{"unavailable", "Could not verify the identity with AWS. Check your connection and try again."}
}
