package account

import (
	"net/http"
	"regexp"
	"strings"
)

// numericAKID matches an access key that is exactly 12 decimal digits.
// When an AKID has this form, Tarn uses it directly as the AWS account ID,
// enabling per-account isolation without any additional configuration.
// Standard 20-char alphanumeric AKIDs (e.g. AKIAIOSFODNN7EXAMPLE) fall
// back to the configured default account ID.
var numericAKID = regexp.MustCompile(`^\d{12}$`)

// FromRequest resolves the AWS account ID for r.
// It inspects the SigV4 Authorization header; if the AKID is exactly 12
// decimal digits it is used directly as the account ID.
// All other AKID formats fall through to defaultID.
func FromRequest(r *http.Request, defaultID string) string {
	akid := akidFromAuth(r.Header.Get("Authorization"))
	if numericAKID.MatchString(akid) {
		return akid
	}
	return defaultID
}

// akidFromAuth extracts the access key ID from a SigV4 Authorization header.
// Expected format: "AWS4-HMAC-SHA256 Credential=<AKID>/<date>/<region>/<service>/aws4_request, ..."
func akidFromAuth(auth string) string {
	_, rest, ok := strings.Cut(auth, "Credential=")
	if !ok {
		return ""
	}
	akid, _, _ := strings.Cut(rest, "/")
	return akid
}
