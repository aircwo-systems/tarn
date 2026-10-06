package cognito

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the hashing cost for user passwords. Pool snapshots may be
// committed or shared by mistake, so passwords are hashed even locally.
var bcryptCost = 10

// passwordSymbols are the special characters Cognito accepts as symbols.
const passwordSymbols = "^$*.[]{}()?\"!@#%&/\\,><':;|_~`=+- "

// checkPasswordPolicy returns InvalidPasswordException with the AWS message
// for the first rule pw breaks.
func checkPasswordPolicy(pp *PasswordPolicyType, pw string) error {
	minLen := 8
	if pp.MinimumLength != nil {
		minLen = int(*pp.MinimumLength)
	}
	fail := func(reason string) error {
		return newError("InvalidPasswordException", "Password did not conform with policy: %s", reason)
	}
	if len([]rune(pw)) < minLen {
		return fail("Password not long enough")
	}
	var upper, lower, number, symbol bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			number = true
		case strings.ContainsRune(passwordSymbols, r):
			symbol = true
		}
	}
	switch {
	case pp.RequireUppercase && !upper:
		return fail("Password must have uppercase characters")
	case pp.RequireLowercase && !lower:
		return fail("Password must have lowercase characters")
	case pp.RequireNumbers && !number:
		return fail("Password must have numeric characters")
	case pp.RequireSymbols && !symbol:
		return fail("Password must have symbol characters")
	}
	return nil
}

// credentials are a password's bcrypt hash and SRP verifier. They are
// computed before taking the service lock, since bcrypt is deliberately slow.
type credentials struct {
	hash, salt, verifier string
}

func newCredentials(poolID, username, pw string) (credentials, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return credentials{}, invalidParameter("Password could not be hashed: %v", err)
	}
	salt, verifier := newSRPVerifier(poolID, username, pw)
	return credentials{hash: string(hash), salt: salt, verifier: verifier}, nil
}

// applyCredentials stores c on u. A temporary password expires after the
// pool's TemporaryPasswordValidityDays.
func (p *pool) applyCredentials(u *user, c credentials, temporary bool, now time.Time) {
	u.PasswordHash, u.SRPSalt, u.SRPVerifier = c.hash, c.salt, c.verifier
	u.TempExpires = time.Time{}
	if temporary {
		days := p.passwordPolicy().TemporaryPasswordValidityDays
		if days <= 0 {
			days = 7
		}
		u.TempExpires = now.Add(time.Duration(days) * 24 * time.Hour)
	}
}

// setPassword hashes pw and stores it on u.
func (p *pool) setPassword(u *user, pw string, temporary bool, now time.Time) error {
	c, err := newCredentials(p.Config.Id, u.Username, pw)
	if err != nil {
		return err
	}
	p.applyCredentials(u, c, temporary, now)
	return nil
}

func passwordMatches(hash, pw string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// generateTemporaryPassword returns a password that satisfies pp.
func generateTemporaryPassword(pp *PasswordPolicyType) string {
	n := 12
	if pp.MinimumLength != nil && int(*pp.MinimumLength) > n {
		n = int(*pp.MinimumLength)
	}
	return randomString("ABCDEFGHJKLMNPQRSTUVWXYZ", 2) +
		randomString("abcdefghijkmnopqrstuvwxyz", 2) +
		randomString(digits, 2) + "!" + randomString(alnum, n-7)
}

// secretHash computes SECRET_HASH: base64(HMAC-SHA256(secret, username + clientID)).
func secretHash(clientSecret, username, clientID string) string {
	mac := hmac.New(sha256.New, []byte(clientSecret))
	mac.Write([]byte(username + clientID))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// checkSecretHash enforces SECRET_HASH for clients that have a secret. Any of
// the candidate usernames may have been used to compute it: the name the user
// typed, their internal username, or their sub.
func checkSecretHash(c *UserPoolClientType, given string, usernames ...string) error {
	if c.ClientSecret == "" {
		return nil
	}
	if given == "" {
		return secretHashError(c.ClientId)
	}
	for _, name := range usernames {
		if name != "" && hmac.Equal([]byte(given), []byte(secretHash(c.ClientSecret, name, c.ClientId))) {
			return nil
		}
	}
	return secretHashError(c.ClientId)
}
