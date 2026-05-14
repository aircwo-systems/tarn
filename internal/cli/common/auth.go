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
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/20000101/us-east-1/tarn/aws4_request, SignedHeaders=host, Signature=0",
		acct,
	))
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
