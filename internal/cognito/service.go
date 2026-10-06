// Package cognito emulates Amazon Cognito User Pools: pools, app clients,
// users and groups, the sign-up and sign-in flows, and RS256 tokens that
// verify against the pool's JWKS. IAM is not enforced on any operation, but
// Cognito's own authentication rules are.
package cognito

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/persist"
)

// Sealer encrypts values at rest. *secrets.Vault satisfies it.
type Sealer interface {
	Seal(plaintext string) (string, error)
	Unseal(sealed string) (string, error)
}

// Service holds one account's user pools.
type Service struct {
	cfg     *config.Config
	index   *Index
	vault   Sealer
	invoker TriggerInvoker

	mu         sync.RWMutex
	pools      map[string]*pool
	challenges map[string]*authChallenge // auth flow Session -> state, never persisted

	dirty   persist.Dirty
	flusher *persist.Flusher

	now func() time.Time
}

// pool is a user pool and everything inside it. Exported fields are persisted.
type pool struct {
	Config          UserPoolType                   `json:"config"`
	Mfa             MfaSettings                    `json:"mfa"`
	KeyID           string                         `json:"keyId"`
	SigningKey      string                         `json:"signingKey"` // PEM, sealed on disk when a vault is set
	Clients         map[string]*UserPoolClientType `json:"clients"`
	Users           map[string]*user               `json:"users"` // keyed by usernameKey
	Groups          map[string]*GroupType          `json:"groups"`
	ResourceServers map[string]*ResourceServerType `json:"resourceServers"`
	Domain          *DomainDescriptionType         `json:"domain,omitempty"`

	key     *rsa.PrivateKey
	refresh map[string]refreshRef // sha256(refresh token) -> session
}

type refreshRef struct {
	userKey   string
	originJTI string
}

// user is a pool user. Passwords are bcrypt hashes; the SRP verifier is kept
// alongside so USER_SRP_AUTH can run without the plaintext password.
type user struct {
	Username     string                  `json:"username"`
	Sub          string                  `json:"sub"`
	Attributes   map[string]string       `json:"attributes"`
	PasswordHash string                  `json:"passwordHash,omitempty"`
	SRPSalt      string                  `json:"srpSalt,omitempty"`
	SRPVerifier  string                  `json:"srpVerifier,omitempty"`
	TempExpires  time.Time               `json:"tempPasswordExpires,omitzero"`
	Status       string                  `json:"status"`
	Enabled      bool                    `json:"enabled"`
	Created      time.Time               `json:"created"`
	Modified     time.Time               `json:"modified"`
	Groups       []string                `json:"groups,omitempty"`
	Codes        map[string]*pendingCode `json:"codes,omitempty"` // keyed by purpose
	Sessions     map[string]*session     `json:"sessions,omitempty"`
	MFA          *mfaPrefs               `json:"mfa,omitempty"`
}

// session is one sign-in: the refresh token and every access token minted
// from it share an origin_jti, so revoking the session revokes them all.
type session struct {
	OriginJTI   string    `json:"originJti"`
	ClientID    string    `json:"clientId"`
	RefreshHash string    `json:"refreshHash"`
	AuthTime    time.Time `json:"authTime"`
	Expires     time.Time `json:"expires"`
	Revoked     bool      `json:"revoked,omitempty"`
}

// NewService creates a Service. index may be nil when account resolution by
// pool or client ID is not needed, as in tests.
func NewService(cfg *config.Config, index *Index) *Service {
	return &Service{
		cfg:        cfg,
		index:      index,
		pools:      make(map[string]*pool),
		challenges: make(map[string]*authChallenge),
		now:        time.Now,
	}
}

// SetVault encrypts pool signing keys at rest.
func (s *Service) SetVault(v Sealer) { s.vault = v }

// Init loads persisted pools and registers them in the index.
func (s *Service) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}
	s.flusher = persist.StartFlusher(&s.dirty, s.flushToDisk)

	data, err := os.ReadFile(s.cfg.CognitoStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cognito state: %w", err)
	}
	var snapshot struct {
		Pools []*pool `json:"pools"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode cognito state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range snapshot.Pools {
		if p == nil {
			continue
		}
		keyPEM := p.SigningKey
		if !strings.HasPrefix(keyPEM, "-----BEGIN") {
			if s.vault == nil {
				return fmt.Errorf("cognito pool %s: signing key is sealed but no vault is configured", p.Config.Id)
			}
			plain, err := s.vault.Unseal(keyPEM)
			if err != nil {
				return fmt.Errorf("unseal cognito pool %s signing key: %w", p.Config.Id, err)
			}
			keyPEM = plain
		}
		key, err := parseKeyPEM(keyPEM)
		if err != nil {
			return fmt.Errorf("cognito pool %s signing key: %w", p.Config.Id, err)
		}
		p.SigningKey = keyPEM
		p.key = key
		p.ensureMaps()
		p.rebuildRefreshIndex()
		s.pools[p.Config.Id] = p
		s.registerPool(p)
	}
	return nil
}

func (s *Service) registerPool(p *pool) {
	s.index.addPool(s.cfg.AccountID, p.Config.Id)
	for clientID := range p.Clients {
		s.index.addClient(p.Config.Id, clientID)
	}
}

// Flush writes pending state to disk now.
func (s *Service) Flush() {
	if s.flusher != nil {
		s.flusher.Flush()
		return
	}
	s.flushToDisk()
}

// Close writes pending state, stops the flusher and drops this account's pools
// from the shared index.
func (s *Service) Close() {
	s.flusher.Close()
	if s.cfg != nil {
		s.index.RemoveAccount(s.cfg.AccountID)
	}
}

func (s *Service) markDirty() { s.dirty.Store(true) }

func (s *Service) flushToDisk() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}
	s.mu.RLock()
	ids := make([]string, 0, len(s.pools))
	for id := range s.pools {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pools := make([]*pool, 0, len(ids))
	for _, id := range ids {
		p := *s.pools[id]
		if s.vault != nil {
			sealed, err := s.vault.Seal(p.SigningKey)
			if err != nil {
				s.mu.RUnlock()
				log.Printf("[cognito] ERROR: failed to seal signing key for %s, aborting persist: %v", id, err)
				return
			}
			p.SigningKey = sealed
		}
		pools = append(pools, &p)
	}
	data, err := json.Marshal(struct {
		Pools []*pool `json:"pools"`
	}{pools})
	s.mu.RUnlock()
	if err != nil {
		log.Printf("[cognito] ERROR: encode state: %v", err)
		return
	}

	path := s.cfg.CognitoStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (p *pool) ensureMaps() {
	if p.Clients == nil {
		p.Clients = make(map[string]*UserPoolClientType)
	}
	if p.Users == nil {
		p.Users = make(map[string]*user)
	}
	if p.Groups == nil {
		p.Groups = make(map[string]*GroupType)
	}
	if p.ResourceServers == nil {
		p.ResourceServers = make(map[string]*ResourceServerType)
	}
	p.refresh = make(map[string]refreshRef)
}

func (p *pool) rebuildRefreshIndex() {
	for key, u := range p.Users {
		for jti, sess := range u.Sessions {
			p.refresh[sess.RefreshHash] = refreshRef{userKey: key, originJTI: jti}
		}
	}
}

func encodeKeyPEM(key *rsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

func parseKeyPEM(s string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("signing key is not RSA")
	}
	return rk, nil
}

// lookupPool returns the pool or a ResourceNotFoundException. Callers hold s.mu.
func (s *Service) lookupPool(poolID string) (*pool, error) {
	p, ok := s.pools[poolID]
	if !ok {
		return nil, poolNotFound(poolID)
	}
	return p, nil
}

// lookupClient finds a client by ID across this account's pools. Callers hold s.mu.
func (s *Service) lookupClient(clientID string) (*pool, *UserPoolClientType, error) {
	for _, p := range s.pools {
		if c, ok := p.Clients[clientID]; ok {
			return p, c, nil
		}
	}
	return nil, nil, clientNotFound(clientID)
}
