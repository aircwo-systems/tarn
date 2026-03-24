package secrets

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// Store is an in-memory secrets store.
type Store struct {
	mu      sync.RWMutex
	secrets map[string]*types.Secret // keyed by secret name
	cfg     *config.Config
	vault   *Vault // nil when encryption is disabled
}

// NewStore creates a new secrets store.
func NewStore(cfg *config.Config) *Store {
	return &Store{
		secrets: make(map[string]*types.Secret),
		cfg:     cfg,
	}
}

// SetVault attaches a Vault for encrypting secret values at rest.
func (s *Store) SetVault(v *Vault) {
	s.vault = v
}

// Init loads persisted secret state from disk if persistence is enabled.
func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}

	if err := os.MkdirAll(s.cfg.SecretsDir(), 0755); err != nil {
		return fmt.Errorf("create secrets dir: %w", err)
	}

	data, err := os.ReadFile(s.cfg.SecretsStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read secrets state: %w", err)
	}

	var snapshot struct {
		Secrets []*types.Secret `json:"secrets"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode secrets state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.secrets = make(map[string]*types.Secret, len(snapshot.Secrets))
	for _, secret := range snapshot.Secrets {
		if secret == nil {
			continue
		}
		sec := cloneSecret(secret)
		if s.vault != nil {
			if IsSealed(sec.SecretString) {
				plain, err := s.vault.Unseal(sec.SecretString)
				if err != nil {
					return fmt.Errorf("unseal secret %s: %w", sec.Name, err)
				}
				sec.SecretString = plain
			}
			if IsSealed(string(sec.SecretBinary)) {
				raw, err := s.vault.UnsealBytes(string(sec.SecretBinary))
				if err != nil {
					return fmt.Errorf("unseal secret binary %s: %w", sec.Name, err)
				}
				sec.SecretBinary = raw
			}
		}
		s.secrets[sec.Name] = sec
	}
	return nil
}

// CreateSecret stores a new secret. Returns error if name already exists.
func (s *Store) CreateSecret(secret *types.Secret) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.secrets[secret.Name]; exists {
		return fmt.Errorf("secret %s already exists", secret.Name)
	}

	s.secrets[secret.Name] = cloneSecret(secret)
	s.persistLocked()
	return nil
}

// GetSecretValue retrieves a secret by name or ARN.
func (s *Store) GetSecretValue(nameOrArn string) (*types.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret := s.resolve(nameOrArn)
	if secret == nil {
		return nil, fmt.Errorf("secret %s not found", nameOrArn)
	}

	// Update in memory only — persisted on next write to avoid re-sealing all secrets on every read.
	secret.LastAccessedDate = time.Now()
	return cloneSecret(secret), nil
}

// DescribeSecret retrieves secret metadata by name or ARN (no value).
func (s *Store) DescribeSecret(nameOrArn string) (*types.Secret, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	secret := s.resolve(nameOrArn)
	if secret == nil {
		return nil, fmt.Errorf("secret %s not found", nameOrArn)
	}
	return cloneSecret(secret), nil
}

// UpdateSecret updates the secret value. Generates a new VersionId.
func (s *Store) UpdateSecret(nameOrArn string, secretString string, secretBinary []byte) (*types.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret := s.resolve(nameOrArn)
	if secret == nil {
		return nil, fmt.Errorf("secret %s not found", nameOrArn)
	}

	if secretString != "" {
		secret.SecretString = secretString
		secret.SecretBinary = nil
	} else if secretBinary != nil {
		secret.SecretBinary = secretBinary
		secret.SecretString = ""
	}

	secret.VersionId = uuid.New().String()
	secret.LastChangedDate = time.Now()
	s.persistLocked()

	return cloneSecret(secret), nil
}

// DeleteSecret removes a secret by name or ARN.
func (s *Store) DeleteSecret(nameOrArn string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret := s.resolve(nameOrArn)
	if secret == nil {
		return fmt.Errorf("secret %s not found", nameOrArn)
	}

	delete(s.secrets, secret.Name)
	s.persistLocked()
	return nil
}

// ListSecrets returns all secrets.
func (s *Store) ListSecrets() []*types.Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*types.Secret, 0, len(s.secrets))
	for _, secret := range s.secrets {
		result = append(result, cloneSecret(secret))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// TagResource adds or updates tags on a secret.
func (s *Store) TagResource(nameOrArn string, tags []types.SecretTag) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret := s.resolve(nameOrArn)
	if secret == nil {
		return fmt.Errorf("secret %s not found", nameOrArn)
	}

	for _, newTag := range tags {
		found := false
		for i, existing := range secret.Tags {
			if existing.Key == newTag.Key {
				secret.Tags[i].Value = newTag.Value
				found = true
				break
			}
		}
		if !found {
			secret.Tags = append(secret.Tags, newTag)
		}
	}
	s.persistLocked()
	return nil
}

// UntagResource removes tags by key from a secret.
func (s *Store) UntagResource(nameOrArn string, tagKeys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret := s.resolve(nameOrArn)
	if secret == nil {
		return fmt.Errorf("secret %s not found", nameOrArn)
	}

	keySet := make(map[string]bool, len(tagKeys))
	for _, k := range tagKeys {
		keySet[k] = true
	}

	filtered := make([]types.SecretTag, 0, len(secret.Tags))
	for _, t := range secret.Tags {
		if !keySet[t.Key] {
			filtered = append(filtered, t)
		}
	}
	secret.Tags = filtered
	s.persistLocked()
	return nil
}

// resolve finds a secret by name or ARN. Must be called with lock held.
func (s *Store) resolve(nameOrArn string) *types.Secret {
	// Try direct name lookup first
	if secret, ok := s.secrets[nameOrArn]; ok {
		return secret
	}
	// Try ARN match
	for _, secret := range s.secrets {
		if secret.ARN == nameOrArn {
			return secret
		}
	}
	// Try partial ARN match (secretId can be just the name part of the ARN)
	for _, secret := range s.secrets {
		if strings.HasSuffix(secret.ARN, ":secret:"+nameOrArn) || strings.Contains(secret.ARN, ":secret:"+nameOrArn+"-") {
			return secret
		}
	}
	return nil
}

func (s *Store) persistLocked() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	snapshot := struct {
		Secrets []*types.Secret `json:"secrets"`
	}{
		Secrets: make([]*types.Secret, 0, len(s.secrets)),
	}

	names := make([]string, 0, len(s.secrets))
	for name := range s.secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sec := cloneSecret(s.secrets[name])
		if s.vault != nil {
			if sec.SecretString != "" {
				sealed, err := s.vault.Seal(sec.SecretString)
				if err != nil {
					log.Printf("[secrets] ERROR: failed to seal secret %s, aborting persist: %v", name, err)
					return
				}
				sec.SecretString = sealed
			}
			if len(sec.SecretBinary) > 0 {
				sealed, err := s.vault.SealBytes(sec.SecretBinary)
				if err != nil {
					log.Printf("[secrets] ERROR: failed to seal secret binary %s, aborting persist: %v", name, err)
					return
				}
				sec.SecretBinary = []byte(sealed)
			}
		}
		snapshot.Secrets = append(snapshot.Secrets, sec)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}

	if err := os.MkdirAll(filepath.Dir(s.cfg.SecretsStatePath()), 0755); err != nil {
		return
	}

	tmpPath := s.cfg.SecretsStatePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return
	}
	_ = os.Rename(tmpPath, s.cfg.SecretsStatePath())
}

func cloneSecret(src *types.Secret) *types.Secret {
	if src == nil {
		return nil
	}

	cloned := *src
	if src.SecretBinary != nil {
		cloned.SecretBinary = append([]byte(nil), src.SecretBinary...)
	}
	if src.VersionStages != nil {
		cloned.VersionStages = append([]string(nil), src.VersionStages...)
	}
	if src.Tags != nil {
		cloned.Tags = append([]types.SecretTag(nil), src.Tags...)
	}
	return &cloned
}
