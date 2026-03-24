package secrets

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// Service implements Secrets Manager business logic.
type Service struct {
	cfg   *config.Config
	store *Store
}

// NewService creates a new Secrets Manager service.
func NewService(cfg *config.Config) *Service {
	return &Service{
		cfg:   cfg,
		store: NewStore(cfg),
	}
}

// SetVault attaches a Vault for encrypting secret values at rest.
func (s *Service) SetVault(v *Vault) {
	s.store.SetVault(v)
}

// Init loads persisted secret state if configured.
func (s *Service) Init() error {
	return s.store.Init()
}

// CreateSecret creates a new secret with generated ARN and version ID.
func (s *Service) CreateSecret(name, description, secretString string, secretBinary []byte, tags []types.SecretTag) (*types.Secret, error) {
	now := time.Now()

	secret := &types.Secret{
		ARN:              s.generateARN(name),
		Name:             name,
		Description:      description,
		SecretString:     secretString,
		SecretBinary:     secretBinary,
		VersionId:        uuid.New().String(),
		VersionStages:    []string{"AWSCURRENT"},
		Tags:             tags,
		CreatedDate:      now,
		LastChangedDate:  now,
		LastAccessedDate: now,
	}

	if err := s.store.CreateSecret(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// GetSecretValue retrieves a secret value by name or ARN.
func (s *Service) GetSecretValue(nameOrArn string) (*types.Secret, error) {
	return s.store.GetSecretValue(nameOrArn)
}

// DescribeSecret retrieves secret metadata.
func (s *Service) DescribeSecret(nameOrArn string) (*types.Secret, error) {
	return s.store.DescribeSecret(nameOrArn)
}

// UpdateSecret updates a secret's value.
func (s *Service) UpdateSecret(nameOrArn string, secretString string, secretBinary []byte) (*types.Secret, error) {
	return s.store.UpdateSecret(nameOrArn, secretString, secretBinary)
}

// PutSecretValue is an alias for UpdateSecret (matches AWS API).
func (s *Service) PutSecretValue(nameOrArn string, secretString string, secretBinary []byte) (*types.Secret, error) {
	return s.store.UpdateSecret(nameOrArn, secretString, secretBinary)
}

// DeleteSecret removes a secret.
func (s *Service) DeleteSecret(nameOrArn string) error {
	return s.store.DeleteSecret(nameOrArn)
}

// ListSecrets returns all secrets.
func (s *Service) ListSecrets() []*types.Secret {
	return s.store.ListSecrets()
}

// TagResource adds tags to a secret.
func (s *Service) TagResource(nameOrArn string, tags []types.SecretTag) error {
	return s.store.TagResource(nameOrArn, tags)
}

// UntagResource removes tags from a secret.
func (s *Service) UntagResource(nameOrArn string, tagKeys []string) error {
	return s.store.UntagResource(nameOrArn, tagKeys)
}

// generateARN creates a Secrets Manager ARN with a 6-char random suffix.
func (s *Service) generateARN(name string) string {
	suffix := randomHex(6)
	return fmt.Sprintf("arn:aws:secretsmanager:%s:%s:secret:%s-%s",
		s.cfg.Region, s.cfg.AccountID, name, suffix)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)[:n]
}
