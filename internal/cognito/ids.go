package cognito

import (
	"crypto/rand"
	"math/big"
	"regexp"
)

const (
	alnum      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	lowerAlnum = "abcdefghijklmnopqrstuvwxyz0123456789"
	digits     = "0123456789"
)

// poolIDPattern matches a user pool ID such as us-east-1_Ab12Cd34E.
var poolIDPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d_[0-9A-Za-z]+$`)

// IsPoolID reports whether s has the shape of a user pool ID. Underscores are
// not legal in S3 bucket names, so a match never shadows a bucket.
func IsPoolID(s string) bool { return poolIDPattern.MatchString(s) }

func randomString(alphabet string, n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b)
}

func newPoolID(region string) string { return region + "_" + randomString(alnum, 9) }

func newClientID() string { return randomString(lowerAlnum, 26) }

func newClientSecret() string { return randomString(lowerAlnum, 51) }

func newCode() string { return randomString(digits, 6) }

func newOpaque(n int) string { return randomString(alnum, n) }
