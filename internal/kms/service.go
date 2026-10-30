// Package kms emulates AWS Key Management Service symmetric keys: key and
// alias management, Encrypt, Decrypt and data key generation. Ciphertext blobs
// use Tarn's own format, bound to the key and the encryption context. Key
// policies are stored but not enforced.
package kms

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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
	"github.com/google/uuid"
)

// Sealer encrypts values at rest. *secrets.Vault satisfies it.
type Sealer interface {
	Seal(plaintext string) (string, error)
	Unseal(sealed string) (string, error)
}

// Key states and the constants Tarn's symmetric keys always report.
const (
	stateEnabled         = "Enabled"
	stateDisabled        = "Disabled"
	statePendingDeletion = "PendingDeletion"

	symmetricDefault = "SYMMETRIC_DEFAULT"
	usageEncrypt     = "ENCRYPT_DECRYPT"

	managerCustomer = "CUSTOMER"
	managerAWS      = "AWS"

	managedAliasPrefix = "alias/aws/"
)

// Service holds one account's keys and aliases.
type Service struct {
	cfg   *config.Config
	vault Sealer

	mu      sync.Mutex
	keys    map[string]*key      // by key ID (UUID)
	aliases map[string]*alias    // by alias name, "alias/..."
	usage   map[string]*keyUsage // by key ID, in memory only

	dirty   persist.Dirty
	flusher *persist.Flusher

	now func() time.Time
}

// key is a symmetric KMS key. Exported fields are persisted.
type key struct {
	ID              string    `json:"id"`
	Description     string    `json:"description"`
	State           string    `json:"state"`
	Manager         string    `json:"manager"`
	Created         time.Time `json:"created"`
	DeletionDate    time.Time `json:"deletionDate,omitzero"`
	Policy          string    `json:"policy"`
	Tags            []Tag     `json:"tags,omitempty"`
	RotationEnabled bool      `json:"rotationEnabled,omitempty"`
	RotationPeriod  int       `json:"rotationPeriodDays,omitempty"`
	Material        string    `json:"material"` // base64 AES-256 key, sealed on disk when a vault is set
	material        []byte
}

type alias struct {
	Name     string    `json:"name"`
	TargetID string    `json:"targetId"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

// NewService creates a Service.
func NewService(cfg *config.Config) *Service {
	return &Service{
		cfg:     cfg,
		keys:    make(map[string]*key),
		aliases: make(map[string]*alias),
		usage:   make(map[string]*keyUsage),
		now:     time.Now,
	}
}

// SetVault encrypts key material at rest.
func (s *Service) SetVault(v Sealer) { s.vault = v }

// Init loads persisted keys and aliases.
func (s *Service) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}
	s.flusher = persist.StartFlusher(&s.dirty, s.flushToDisk)

	data, err := os.ReadFile(s.cfg.KMSStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read kms state: %w", err)
	}
	var snapshot struct {
		Keys    []*key   `json:"keys"`
		Aliases []*alias `json:"aliases"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode kms state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range snapshot.Keys {
		material := k.Material
		if s.vault != nil && strings.HasPrefix(material, "vault:") {
			plain, err := s.vault.Unseal(material)
			if err != nil {
				return fmt.Errorf("unseal kms key %s: %w", k.ID, err)
			}
			material = plain
		} else if strings.HasPrefix(material, "vault:") {
			return fmt.Errorf("kms key %s: material is sealed but no vault is configured", k.ID)
		}
		raw, err := base64.StdEncoding.DecodeString(material)
		if err != nil || len(raw) != 32 {
			return fmt.Errorf("kms key %s: invalid key material", k.ID)
		}
		k.Material = material
		k.material = raw
		s.keys[k.ID] = k
	}
	for _, a := range snapshot.Aliases {
		s.aliases[a.Name] = a
	}
	return nil
}

// Flush writes pending state to disk now.
func (s *Service) Flush() {
	if s.flusher != nil {
		s.flusher.Flush()
		return
	}
	s.flushToDisk()
}

// Close writes pending state and stops the flusher.
func (s *Service) Close() { s.flusher.Close() }

func (s *Service) markDirty() { s.dirty.Store(true) }

func (s *Service) flushToDisk() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}
	s.mu.Lock()
	keys := make([]*key, 0, len(s.keys))
	for _, id := range sortedIDs(s.keys) {
		k := *s.keys[id]
		if s.vault != nil {
			sealed, err := s.vault.Seal(k.Material)
			if err != nil {
				s.mu.Unlock()
				log.Printf("[kms] ERROR: failed to seal key %s, aborting persist: %v", id, err)
				return
			}
			k.Material = sealed
		}
		keys = append(keys, &k)
	}
	aliases := make([]*alias, 0, len(s.aliases))
	for _, name := range sortedIDs(s.aliases) {
		a := *s.aliases[name]
		aliases = append(aliases, &a)
	}
	s.mu.Unlock()

	data, err := json.Marshal(struct {
		Keys    []*key   `json:"keys"`
		Aliases []*alias `json:"aliases"`
	}{keys, aliases})
	if err != nil {
		log.Printf("[kms] ERROR: encode state: %v", err)
		return
	}
	path := s.cfg.KMSStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func sortedIDs[V any](m map[string]V) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (s *Service) arnPrefix() string {
	return fmt.Sprintf("arn:aws:kms:%s:%s:", s.cfg.Region, s.cfg.AccountID)
}

func (s *Service) keyARN(id string) string { return s.arnPrefix() + "key/" + id }

func (s *Service) aliasARN(name string) string { return s.arnPrefix() + name }

// newKeyLocked creates and stores a key with fresh material.
func (s *Service) newKeyLocked(description, manager, policy string, tags []Tag) (*key, error) {
	material := make([]byte, 32)
	if _, err := rand.Read(material); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	if policy == "" {
		policy = s.defaultPolicy()
	}
	k := &key{
		ID:          id,
		Description: description,
		State:       stateEnabled,
		Manager:     manager,
		Created:     s.now(),
		Policy:      policy,
		Tags:        tags,
		Material:    base64.StdEncoding.EncodeToString(material),
		material:    material,
	}
	s.keys[id] = k
	s.markDirty()
	return k, nil
}

// defaultPolicy is the key policy AWS attaches when CreateKey has none: it
// delegates access to the account's IAM policies.
func (s *Service) defaultPolicy() string {
	return fmt.Sprintf(`{
  "Version" : "2012-10-17",
  "Id" : "key-default-1",
  "Statement" : [ {
    "Sid" : "Enable IAM User Permissions",
    "Effect" : "Allow",
    "Principal" : {
      "AWS" : "arn:aws:iam::%s:root"
    },
    "Action" : "kms:*",
    "Resource" : "*"
  } ]
}`, s.cfg.AccountID)
}

// resolveLocked finds the key a KeyId names: a key ID, key ARN, alias name or
// alias ARN. AWS managed aliases (alias/aws/...) get their key on first use,
// as in AWS.
func (s *Service) resolveLocked(keyID string) (*key, error) {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return nil, validation("1 validation error detected: Value null at 'keyId' failed to satisfy constraint: Member must not be null")
	}
	name := keyID
	if strings.HasPrefix(keyID, "arn:") {
		rest, ok := strings.CutPrefix(keyID, s.arnPrefix())
		if !ok {
			return nil, keyNotFound(keyID)
		}
		name = rest
	}
	if strings.HasPrefix(name, "alias/") {
		a := s.aliases[name]
		if a == nil {
			if !strings.HasPrefix(name, managedAliasPrefix) {
				return nil, aliasNotFound(s.aliasARN(name))
			}
			k, err := s.newKeyLocked("Default key that protects my "+strings.TrimPrefix(name, managedAliasPrefix)+" resources when no other key is defined", managerAWS, "", nil)
			if err != nil {
				return nil, err
			}
			now := s.now()
			a = &alias{Name: name, TargetID: k.ID, Created: now, Updated: now}
			s.aliases[name] = a
		}
		name = "key/" + a.TargetID
	}
	id, ok := strings.CutPrefix(name, "key/")
	if !ok {
		id = name
	}
	k := s.keys[id]
	if k == nil || s.expiredLocked(k) {
		return nil, keyNotFound(s.keyARN(id))
	}
	return k, nil
}

// expiredLocked deletes k, and the aliases that point at it, once its
// scheduled deletion date has passed.
func (s *Service) expiredLocked(k *key) bool {
	if k.State != statePendingDeletion || s.now().Before(k.DeletionDate) {
		return false
	}
	delete(s.keys, k.ID)
	for name, a := range s.aliases {
		if a.TargetID == k.ID {
			delete(s.aliases, name)
		}
	}
	s.markDirty()
	return true
}

// usableLocked resolves keyID to a key that can encrypt and decrypt.
func (s *Service) usableLocked(keyID string) (*key, error) {
	k, err := s.resolveLocked(keyID)
	if err != nil {
		return nil, err
	}
	return k, s.checkUsable(k)
}

func (s *Service) checkUsable(k *key) error {
	switch k.State {
	case stateDisabled:
		return newError("DisabledException", "%s is disabled.", s.keyARN(k.ID))
	case statePendingDeletion:
		return newError("KMSInvalidStateException", "%s is pending deletion.", s.keyARN(k.ID))
	}
	return nil
}

func (s *Service) metadata(k *key) KeyMetadata {
	m := KeyMetadata{
		AWSAccountId:          s.cfg.AccountID,
		KeyId:                 k.ID,
		Arn:                   s.keyARN(k.ID),
		CreationDate:          EpochTime(k.Created),
		Enabled:               k.State == stateEnabled,
		Description:           k.Description,
		KeyUsage:              usageEncrypt,
		KeyState:              k.State,
		Origin:                "AWS_KMS",
		KeyManager:            k.Manager,
		CustomerMasterKeySpec: symmetricDefault,
		KeySpec:               symmetricDefault,
		EncryptionAlgorithms:  []string{symmetricDefault},
	}
	if k.State == statePendingDeletion {
		d := EpochTime(k.DeletionDate)
		m.DeletionDate = &d
	}
	return m
}
