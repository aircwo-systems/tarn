package cognito

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"strings"
)

// Server side of Cognito's SRP-6a variant, matching amazon-cognito-identity-js
// and Amplify: the RFC 5054 3072-bit group, g = 2, SHA-256, and an HKDF-derived
// key that signs the pool name, user ID, secret block and timestamp.

const srpNHex = "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1" +
	"29024E088A67CC74020BBEA63B139B22514A08798E3404DD" +
	"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245" +
	"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
	"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D" +
	"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F" +
	"83655D23DCA3AD961C62F356208552BB9ED529077096966D" +
	"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B" +
	"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9" +
	"DE2BCBF6955817183995497CEA956AE515D2261898FA0510" +
	"15728E5A8AAAC42DAD33170D04507A33A85521ABDF1CBA64" +
	"ECFB850458DBEF0A8AEA71575D060C7DB3970F85A6E1E4C7" +
	"ABF5AE8CDB0933D71E8C94E04A25619DCEE3D2261AD2EE6B" +
	"F12FFA06D98A0864D87602733EC86A64521F2B18177B200C" +
	"BBE117577A615D6C770988C0BAD946E208E24FA074E5AB31" +
	"43DB5BFCE0FD108E4B82D120A93AD2CAFFFFFFFFFFFFFFFF"

var (
	srpN, _ = new(big.Int).SetString(srpNHex, 16)
	srpG    = big.NewInt(2)
	srpK    = new(big.Int).SetBytes(hexHash(padHex(srpN) + padHex(srpG)))
)

// padHex is the even-length, sign-safe hex encoding the Cognito clients hash:
// a leading "00" is added when the top bit is set.
func padHex(n *big.Int) string {
	s := n.Text(16)
	if len(s)%2 == 1 {
		s = "0" + s
	}
	if strings.IndexByte("89abcdef", s[0]) >= 0 {
		s = "00" + s
	}
	return s
}

func hexHash(h string) []byte {
	b, _ := hex.DecodeString(h)
	sum := sha256.Sum256(b)
	return sum[:]
}

// poolName is the part of the pool ID after the region, which the SRP
// clients mix into x and the signature.
func poolName(poolID string) string {
	_, name, _ := strings.Cut(poolID, "_")
	return name
}

// srpX derives the private value x from the salt and credentials.
func srpX(poolID, userID, password string, salt *big.Int) *big.Int {
	inner := sha256.Sum256([]byte(poolName(poolID) + userID + ":" + password))
	return new(big.Int).SetBytes(hexHash(padHex(salt) + hex.EncodeToString(inner[:])))
}

// newSRPVerifier returns a fresh salt and the verifier g^x for the password,
// both hex encoded for storage.
func newSRPVerifier(poolID, userID, password string) (salt, verifier string) {
	sb := make([]byte, 16)
	_, _ = rand.Read(sb)
	s := new(big.Int).SetBytes(sb)
	v := new(big.Int).Exp(srpG, srpX(poolID, userID, password, s), srpN)
	return s.Text(16), v.Text(16)
}

// srpServerState is the server's half of one PASSWORD_VERIFIER exchange.
type srpServerState struct {
	A, B, b     *big.Int
	salt, v     *big.Int
	secretBlock []byte
}

// startSRP validates the client's A and computes B = kv + g^b.
func startSRP(aHex, saltHex, verifierHex string) (*srpServerState, bool) {
	A, ok := new(big.Int).SetString(aHex, 16)
	if !ok || new(big.Int).Mod(A, srpN).Sign() == 0 {
		return nil, false
	}
	salt, ok1 := new(big.Int).SetString(saltHex, 16)
	v, ok2 := new(big.Int).SetString(verifierHex, 16)
	if !ok1 || !ok2 {
		return nil, false
	}
	bb := make([]byte, 128)
	_, _ = rand.Read(bb)
	b := new(big.Int).Mod(new(big.Int).SetBytes(bb), srpN)
	B := new(big.Int).Exp(srpG, b, srpN)
	B.Add(B, new(big.Int).Mul(srpK, v))
	B.Mod(B, srpN)
	block := make([]byte, 64)
	_, _ = rand.Read(block)
	return &srpServerState{A: A, B: B, b: b, salt: salt, v: v, secretBlock: block}, true
}

func (st *srpServerState) challengeParameters() (saltHex, bHex, secretBlock string) {
	return st.salt.Text(16), st.B.Text(16), base64.StdEncoding.EncodeToString(st.secretBlock)
}

// verify checks PASSWORD_CLAIM_SIGNATURE.
func (st *srpServerState) verify(poolID, userID, secretBlockB64, timestamp, signatureB64 string) bool {
	block, err := base64.StdEncoding.DecodeString(secretBlockB64)
	if err != nil || subtle.ConstantTimeCompare(block, st.secretBlock) != 1 {
		return false
	}
	u := new(big.Int).SetBytes(hexHash(padHex(st.A) + padHex(st.B)))
	if u.Sign() == 0 {
		return false
	}
	// S = (A * v^u)^b mod N
	S := new(big.Int).Exp(st.v, u, srpN)
	S.Mul(S, st.A)
	S.Mod(S, srpN)
	S.Exp(S, st.b, srpN)

	key, err := srpKey(S, u)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(poolName(poolID)))
	mac.Write([]byte(userID))
	mac.Write(block)
	mac.Write([]byte(timestamp))
	want := mac.Sum(nil)
	got, err := base64.StdEncoding.DecodeString(signatureB64)
	return err == nil && hmac.Equal(got, want)
}

func srpKey(S, u *big.Int) ([]byte, error) {
	ikm, _ := hex.DecodeString(padHex(S))
	salt, _ := hex.DecodeString(padHex(u))
	return hkdf.Key(sha256.New, ikm, salt, "Caldera Derived Key", 16)
}
