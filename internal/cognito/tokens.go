package cognito

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Issuer returns the `iss` claim for tokens from poolID, following the
// configured issuer mode.
func (s *Service) Issuer(poolID string) string {
	switch mode := s.cfg.CognitoIssuer; mode {
	case "", "local":
		return s.LocalBaseURL() + "/" + poolID
	case "aws":
		return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", s.cfg.Region, poolID)
	default:
		return strings.TrimRight(mode, "/") + "/" + poolID
	}
}

// LocalBaseURL is the Tarn endpoint as seen from the host, used for the local
// issuer and for JWKS URLs in discovery documents.
func (s *Service) LocalBaseURL() string {
	host := s.cfg.Host
	if ip := net.ParseIP(host); host == "" || (ip != nil && (ip.IsUnspecified() || ip.IsLoopback())) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.cfg.Port))
}

// PoolIDFromIssuer returns the pool ID at the end of an issuer URL.
func PoolIDFromIssuer(iss string) string {
	iss = strings.TrimRight(iss, "/")
	return iss[strings.LastIndexByte(iss, '/')+1:]
}

// PoolIDFromToken reads the pool ID from a JWT's issuer without verifying the
// token, so a request can be routed to the account that owns the pool.
func PoolIDFromToken(token string) string {
	claims, err := decodeClaims(token)
	if err != nil {
		return ""
	}
	iss, _ := claims["iss"].(string)
	return PoolIDFromIssuer(iss)
}

type jwtHeader struct {
	Kid string `json:"kid"`
	Alg string `json:"alg"`
}

func signJWT(key *rsa.PrivateKey, kid string, claims map[string]any) (string, error) {
	h, _ := json.Marshal(jwtHeader{Kid: kid, Alg: "RS256"})
	c, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func decodeClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var claims map[string]any
	if err := dec.Decode(&claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// verifyJWT checks the signature against the pool key and returns the claims.
// Expiry is left to the caller so it can report it distinctly.
func verifyJWT(p *pool, token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var h jwtHeader
	if err := json.Unmarshal(hb, &h); err != nil {
		return nil, err
	}
	if h.Alg != "RS256" || h.Kid != p.KeyID {
		return nil, fmt.Errorf("unexpected key")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&p.key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		return nil, err
	}
	return decodeClaims(token)
}

func claimInt(claims map[string]any, name string) int64 {
	switch v := claims[name].(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

// JWK is one RSA public key in a JWKS document.
type JWK struct {
	Alg string `json:"alg"`
	E   string `json:"e"`
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	Use string `json:"use"`
}

func (p *pool) jwk() JWK {
	pub := p.key.PublicKey
	return JWK{
		Alg: "RS256",
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		Kid: p.KeyID,
		Kty: "RSA",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		Use: "sig",
	}
}

// JWKS returns the pool's public key set.
func (s *Service) JWKS(poolID string) (map[string][]JWK, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(poolID)
	if err != nil {
		return nil, err
	}
	return map[string][]JWK{"keys": {p.jwk()}}, nil
}

// OpenIDConfiguration returns the pool's discovery document. The JWKS URL
// always points at Tarn, whichever issuer mode is configured.
func (s *Service) OpenIDConfiguration(poolID string) (map[string]any, error) {
	s.mu.RLock()
	_, err := s.lookupPool(poolID)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	local := s.LocalBaseURL() + "/" + poolID
	return map[string]any{
		"issuer":                                s.Issuer(poolID),
		"jwks_uri":                              local + "/.well-known/jwks.json",
		"authorization_endpoint":                local + "/oauth2/authorize",
		"token_endpoint":                        local + "/oauth2/token",
		"userinfo_endpoint":                     local + "/oauth2/userInfo",
		"response_types_supported":              []string{"code", "token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "email", "phone", "profile"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
	}, nil
}

// readableAttributes returns the attributes the client may read. An empty
// ReadAttributes list means all of them.
func readableAttributes(c *UserPoolClientType, u *user) map[string]string {
	out := make(map[string]string, len(u.Attributes))
	for k, v := range u.Attributes {
		if len(c.ReadAttributes) == 0 || slices.Contains(c.ReadAttributes, k) {
			out[k] = v
		}
	}
	return out
}

// groupNames returns the user's groups ordered by precedence, then name.
func (p *pool) groupNames(u *user) []string {
	names := slices.Clone(u.Groups)
	prec := func(n string) int32 {
		if g := p.Groups[n]; g != nil && g.Precedence != nil {
			return *g.Precedence
		}
		return 1 << 30
	}
	sort.SliceStable(names, func(i, j int) bool {
		pi, pj := prec(names[i]), prec(names[j])
		if pi != pj {
			return pi < pj
		}
		return names[i] < names[j]
	})
	return names
}

// mintTokens issues an ID and access token for sess. refreshToken is
// included in the result when non-empty.
func (s *Service) mintTokens(p *pool, c *UserPoolClientType, u *user, sess *session, refreshToken string) (*AuthenticationResultType, error) {
	now := s.now()
	eventID := uuid.NewString()
	iss := s.Issuer(p.Config.Id)
	groups := p.groupNames(u)
	accessTTL, idTTL := s.accessTTL(c), s.idTTL(c)

	id := map[string]any{
		"sub":              u.Sub,
		"iss":              iss,
		"aud":              c.ClientId,
		"token_use":        "id",
		"auth_time":        sess.AuthTime.Unix(),
		"iat":              now.Unix(),
		"exp":              now.Add(idTTL).Unix(),
		"cognito:username": u.Username,
		"event_id":         eventID,
		"origin_jti":       sess.OriginJTI,
		"jti":              uuid.NewString(),
	}
	for k, v := range readableAttributes(c, u) {
		if k == "sub" {
			continue
		}
		if k == "email_verified" || k == "phone_number_verified" {
			id[k] = v == "true"
			continue
		}
		id[k] = v
	}
	if len(groups) > 0 {
		id["cognito:groups"] = groups
	}

	access := map[string]any{
		"sub":        u.Sub,
		"iss":        iss,
		"client_id":  c.ClientId,
		"token_use":  "access",
		"scope":      "aws.cognito.signin.user.admin",
		"auth_time":  sess.AuthTime.Unix(),
		"iat":        now.Unix(),
		"exp":        now.Add(accessTTL).Unix(),
		"username":   u.Username,
		"event_id":   eventID,
		"origin_jti": sess.OriginJTI,
		"jti":        uuid.NewString(),
	}
	if len(groups) > 0 {
		access["cognito:groups"] = groups
	}

	idToken, err := signJWT(p.key, p.KeyID, id)
	if err != nil {
		return nil, err
	}
	accessToken, err := signJWT(p.key, p.KeyID, access)
	if err != nil {
		return nil, err
	}
	return &AuthenticationResultType{
		AccessToken:  accessToken,
		IdToken:      idToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(accessTTL / time.Second),
		TokenType:    "Bearer",
	}, nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// startSession records a new sign-in for u and returns its refresh token.
// Expired sessions are pruned at the same time.
func (s *Service) startSession(p *pool, c *UserPoolClientType, u *user) (*session, string) {
	now := s.now()
	for jti, old := range u.Sessions {
		if now.After(old.Expires) {
			delete(p.refresh, old.RefreshHash)
			delete(u.Sessions, jti)
		}
	}
	refresh := newOpaque(96)
	sess := &session{
		OriginJTI:   uuid.NewString(),
		ClientID:    c.ClientId,
		RefreshHash: hashToken(refresh),
		AuthTime:    now,
		Expires:     now.Add(refreshTTL(c)),
	}
	if u.Sessions == nil {
		u.Sessions = make(map[string]*session)
	}
	u.Sessions[sess.OriginJTI] = sess
	p.refresh[sess.RefreshHash] = refreshRef{userKey: p.userKey(u.Username), originJTI: sess.OriginJTI}
	s.markDirty()
	return sess, refresh
}

// revokeSessions revokes every session of u, or only the one with originJTI.
// Revoked sessions stay indexed until they expire so a refresh attempt can
// report the revocation.
func (p *pool) revokeSessions(u *user, originJTI string) {
	for jti, sess := range u.Sessions {
		if originJTI == "" || jti == originJTI {
			sess.Revoked = true
		}
	}
}

// accessContext is a verified access token and the user it belongs to.
type accessContext struct {
	pool   *pool
	client *UserPoolClientType
	user   *user
	claims map[string]any
}

// authenticateAccess verifies an access token the way Cognito's own APIs do,
// including revocation. Callers hold s.mu.
func (s *Service) authenticateAccess(token string) (*accessContext, error) {
	if token == "" {
		return nil, errInvalidAccessToken
	}
	p, ok := s.pools[PoolIDFromToken(token)]
	if !ok {
		return nil, errInvalidAccessToken
	}
	claims, err := verifyJWT(p, token)
	if err != nil || claims["token_use"] != "access" {
		return nil, errInvalidAccessToken
	}
	if s.now().Unix() >= claimInt(claims, "exp") {
		return nil, errAccessTokenExpired
	}
	username, _ := claims["username"].(string)
	u := p.Users[p.userKey(username)]
	if u == nil {
		return nil, notAuthorized("User does not exist.")
	}
	jti, _ := claims["origin_jti"].(string)
	if sess := u.Sessions[jti]; sess == nil || sess.Revoked {
		return nil, errAccessTokenRevoked
	}
	if !u.Enabled {
		return nil, errUserDisabled
	}
	clientID, _ := claims["client_id"].(string)
	return &accessContext{pool: p, client: p.Clients[clientID], user: u, claims: claims}, nil
}

// VerifyToken checks a token Tarn issued and returns its claims. It is the
// seam API Gateway authorizers use: signature, expiry and token_use are
// checked, revocation is not, as with offline JWT verification in AWS.
func (s *Service) VerifyToken(token string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.pools[PoolIDFromToken(token)]
	if !ok {
		return nil, fmt.Errorf("unknown issuer")
	}
	claims, err := verifyJWT(p, token)
	if err != nil {
		return nil, err
	}
	if s.now().Unix() >= claimInt(claims, "exp") {
		return nil, fmt.Errorf("token expired")
	}
	return claims, nil
}
