package common

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// AccountID returns the active account ID from TARN_ACCOUNT_ID, falling back to the default.
func AccountID() string {
	if v := os.Getenv("TARN_ACCOUNT_ID"); v != "" {
		return v
	}
	return "000000000000"
}

// SetAccountHeader injects a minimal SigV4-shaped Authorization header when
// TARN_ACCOUNT_ID is set, so the server's account resolver picks up the right account.
func SetAccountHeader(req *http.Request) {
	acct := os.Getenv("TARN_ACCOUNT_ID")
	if acct == "" {
		return
	}
	setAccountAuthorization(req, acct)
}

// SetAccountHeaderForAccount injects the account Authorization header for an
// explicit 12-digit account (e.g. parsed from an ARN). When account is empty it
// falls back to the TARN_ACCOUNT_ID-based behaviour, so commands that carry an
// ARN can target the right account without requiring TARN_ACCOUNT_ID to be set.
func SetAccountHeaderForAccount(req *http.Request, account string) {
	if account == "" {
		SetAccountHeader(req)
		return
	}
	setAccountAuthorization(req, account)
}

func setAccountAuthorization(req *http.Request, acct string) {
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/20000101/us-east-1/tarn/aws4_request, SignedHeaders=host, Signature=0",
		acct,
	))
}

// AccountFromARN extracts the account ID from an AWS ARN of the form
// arn:partition:service:region:ACCOUNT:resource. It returns "" when the ARN has
// no 12-digit account segment, letting callers fall back to the default.
func AccountFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 5 {
		return ""
	}
	acct := parts[4]
	if len(acct) != 12 {
		return ""
	}
	for _, r := range acct {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return acct
}

// PostForm is a drop-in replacement for http.PostForm that injects the account
// Authorization header when TARN_ACCOUNT_ID is set.
func PostForm(urlStr string, data url.Values) (*http.Response, error) {
	req, err := http.NewRequest("POST", urlStr, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	SetAccountHeader(req)
	return http.DefaultClient.Do(req)
}
