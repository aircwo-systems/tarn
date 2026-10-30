// Package awsconnection owns real AWS credentials separately from emulated resources.
package awsconnection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/secrets"
	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("AWS connection not found")
	ErrBusy     = errors.New("a connection operation is already in progress; try again shortly")
	ErrName     = errors.New("a connection with this name already exists")
	ErrStorage  = errors.New("could not save the AWS connection store")
)

type Metadata struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Region          string    `json:"region"`
	Kind            string    `json:"kind"`
	Profile         string    `json:"profile,omitempty"`
	AccessKeySuffix string    `json:"accessKeySuffix"`
	Identity        Identity  `json:"identity"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	LastVerifiedAt  time.Time `json:"lastVerifiedAt"`
	LastTestedAt    time.Time `json:"lastTestedAt"`
	Status          string    `json:"status"`
	Message         string    `json:"message,omitempty"`
}

type Input struct {
	Name             string       `json:"name"`
	Region           string       `json:"region"`
	Credentials      *Credentials `json:"credentials,omitempty"`
	AcceptedIdentity *Identity    `json:"acceptedIdentity,omitempty"`
}

type IdentityChangedError struct {
	Identity Identity `json:"identity"`
}

func (*IdentityChangedError) Error() string {
	return "The AWS identity has changed. Confirm the new account and identity before replacing credentials."
}

type record struct {
	Metadata    Metadata `json:"metadata"`
	Credentials string   `json:"credentials,omitempty"` // Manual credentials are sealed, including in memory.
}

type snapshot struct {
	Version int               `json:"version"`
	Records map[string]record `json:"records"`
}

type Service struct {
	mu       sync.Mutex
	path     string
	vault    *secrets.Vault
	validate Validator
	records  map[string]record
	busy     map[string]bool
	slots    chan struct{}
	// write is replaceable by package tests to exercise failed durable writes.
	write func([]byte) error
}

func StorePath(dataDir string) string {
	return filepath.Join(dataDir, "aws-connections", "connections.json")
}

func Open(dataDir string, vault *secrets.Vault, validator Validator) (*Service, error) {
	if validator == nil {
		validator = ValidateSTS
	}
	s := &Service{path: StorePath(dataDir), vault: vault, validate: validator,
		records: map[string]record{}, busy: map[string]bool{}, slots: make(chan struct{}, 4)}
	s.write = s.atomicWrite
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, errors.New("cannot read the AWS connection store. Check its file permissions")
	}
	var disk snapshot
	if json.Unmarshal(data, &disk) != nil || disk.Version != 1 || disk.Records == nil {
		return nil, errors.New("cannot read the AWS connection store. Restore a valid backup; the file has been preserved")
	}
	names := map[string]bool{}
	for id, r := range disk.Records {
		c, err := s.source(r)
		if err != nil {
			return nil, err
		}
		if id != r.Metadata.ID || id == "" || validateInput(Input{Name: r.Metadata.Name, Region: r.Metadata.Region, Credentials: &c}, true) != nil || names[strings.ToLower(r.Metadata.Name)] {
			return nil, errors.New("the AWS connection store is invalid; the file has been preserved")
		}
		names[strings.ToLower(r.Metadata.Name)] = true
		r.Metadata.Status = "untested"
		r.Metadata.Message = "Not tested this session."
		disk.Records[id] = r
	}
	s.records = disk.Records
	return s, nil
}

func (s *Service) ManualCredentialsAvailable() bool { return s.vault != nil }

// RequiresVault lets startup preserve keys for manual records while profile-only
// stores remain usable without a Vault key. Invalid stores fail closed.
func RequiresVault(dataDir string) (bool, error) {
	data, err := os.ReadFile(StorePath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	var disk snapshot
	if json.Unmarshal(data, &disk) != nil || disk.Version != 1 || disk.Records == nil {
		return true, errors.New("cannot read the AWS connection store")
	}
	for _, r := range disk.Records {
		if r.Metadata.Kind != "profile" || r.Credentials != "" {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) List() []Metadata {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Metadata, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r.Metadata)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func validateInput(in Input, requireCredentials bool) error {
	if in.Name != strings.TrimSpace(in.Name) || len(in.Name) < 1 || len(in.Name) > 80 {
		return errors.New("enter a connection name between 1 and 80 characters, without surrounding spaces")
	}
	if !validRegion(in.Region) {
		return errors.New("enter a commercial AWS region, such as us-east-1 or eu-west-2")
	}
	if in.Credentials == nil {
		if requireCredentials {
			return errors.New("AWS credentials are required")
		}
		return nil
	}
	c := in.Credentials
	if c.Kind == "profile" {
		if !validProfileName(c.Profile) || c.AccessKeyID != "" || c.SecretAccessKey != "" || c.SessionToken != "" {
			return errors.New("enter an AWS profile name without including manual credentials")
		}
		return nil
	}
	if c.Profile != "" {
		return errors.New("manual credentials must not include an AWS profile")
	}
	if len(c.AccessKeyID) < 16 || len(c.AccessKeyID) > 128 || strings.ContainsAny(c.AccessKeyID, " \t\r\n") || len(c.SecretAccessKey) < 1 || len(c.SecretAccessKey) > 4096 || strings.ContainsAny(c.SecretAccessKey, " \t\r\n") {
		return errors.New("enter a valid access key ID and secret access key")
	}
	if len(c.SessionToken) > 16384 || strings.ContainsAny(c.SessionToken, " \t\r\n") {
		return errors.New("enter a valid session token")
	}
	if (c.Kind == "long-lived" && c.SessionToken == "") || (c.Kind == "temporary" && c.SessionToken != "") {
		return nil
	}
	return errors.New("temporary credentials require a session token; long-lived credentials must omit it")
}

func (s *Service) check(ctx context.Context, region string, c Credentials) (Identity, error) {
	if c.Kind != "profile" && s.vault == nil {
		return Identity{}, errors.New("manual AWS credentials require Vault encryption; use an AWS profile or enable a vault key")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return Identity{}, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	identity, err := s.validate(ctx, region, c)
	if err != nil {
		var safe *ValidationError
		if errors.As(err, &safe) {
			return Identity{}, safe
		}
		return Identity{}, classifyValidationError(err)
	}
	if err := ctx.Err(); err != nil {
		return Identity{}, classifyValidationError(err)
	}
	return identity, nil
}

func (s *Service) Create(ctx context.Context, in Input) (Metadata, error) {
	if err := validateInput(in, true); err != nil {
		return Metadata{}, err
	}
	identity, err := s.check(ctx, in.Region, *in.Credentials)
	if err != nil {
		return Metadata{}, err
	}
	sealed, err := s.seal(*in.Credentials)
	if err != nil {
		return Metadata{}, err
	}
	now := time.Now().UTC()
	m := Metadata{ID: uuid.NewString(), Name: in.Name, Region: in.Region, Kind: in.Credentials.Kind,
		Profile:         in.Credentials.Profile,
		AccessKeySuffix: suffix(in.Credentials.AccessKeyID), Identity: identity, CreatedAt: now, UpdatedAt: now,
		LastVerifiedAt: now, LastTestedAt: now, Status: "verified"}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unique(in.Name, ""); err != nil {
		return Metadata{}, err
	}
	return m, s.commit(m.ID, record{m, sealed}, false)
}

func (s *Service) begin(id string) (record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return record{}, ErrNotFound
	}
	if s.busy[id] {
		return record{}, ErrBusy
	}
	s.busy[id] = true
	return r, nil
}

func (s *Service) end(id string) { s.mu.Lock(); delete(s.busy, id); s.mu.Unlock() }

func (s *Service) Update(ctx context.Context, id string, in Input) (Metadata, error) {
	if err := validateInput(in, false); err != nil {
		return Metadata{}, err
	}
	r, err := s.begin(id)
	if err != nil {
		return Metadata{}, err
	}
	defer s.end(id)
	if in.Credentials != nil || in.Region != r.Metadata.Region {
		var c Credentials
		if in.Credentials != nil {
			c = *in.Credentials
		} else {
			c, err = s.source(r)
			if err != nil {
				return Metadata{}, err
			}
		}
		identity, err := s.check(ctx, in.Region, c)
		if err != nil {
			return Metadata{}, err
		}
		if identity != r.Metadata.Identity && (in.AcceptedIdentity == nil || *in.AcceptedIdentity != identity) {
			return Metadata{}, &IdentityChangedError{identity}
		}
		r.Credentials, err = s.seal(c)
		if err != nil {
			return Metadata{}, err
		}
		r.Metadata.Identity = identity
		r.Metadata.Kind = c.Kind
		r.Metadata.Profile = c.Profile
		r.Metadata.AccessKeySuffix = suffix(c.AccessKeyID)
		r.Metadata.LastVerifiedAt = time.Now().UTC()
		r.Metadata.LastTestedAt = r.Metadata.LastVerifiedAt
		r.Metadata.Status, r.Metadata.Message = "verified", ""
	}
	r.Metadata.Name, r.Metadata.Region, r.Metadata.UpdatedAt = in.Name, in.Region, time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unique(in.Name, id); err != nil {
		return Metadata{}, err
	}
	return r.Metadata, s.commit(id, r, false)
}

func (s *Service) Test(ctx context.Context, id string) (Metadata, error) {
	r, err := s.begin(id)
	if err != nil {
		return Metadata{}, err
	}
	defer s.end(id)
	c, err := s.source(r)
	if err != nil {
		return Metadata{}, err
	}
	identity, err := s.check(ctx, r.Metadata.Region, c)
	if errors.Is(err, ErrBusy) {
		return Metadata{}, err
	}
	r.Metadata.LastTestedAt = time.Now().UTC()
	r.Metadata.Status, r.Metadata.Message = "verified", ""
	if err != nil {
		var ve *ValidationError
		if !errors.As(err, &ve) {
			return Metadata{}, err
		}
		r.Metadata.Status, r.Metadata.Message = ve.Status, ve.Message
	} else if identity != r.Metadata.Identity && (r.Metadata.Kind != "profile" || !sameAssumedRole(identity, r.Metadata.Identity)) {
		// A test cannot silently change the saved identity.
		r.Metadata.Status, r.Metadata.Message = "rejected", "The identity has changed. Replace credentials to confirm the new identity."
	} else {
		r.Metadata.Identity = identity
		r.Metadata.LastVerifiedAt = r.Metadata.LastTestedAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return r.Metadata, s.commit(id, r, false)
}

// A provider may issue a new session ARN on renewal. Keep the account and role
// boundary intact without requiring confirmation for a different session name.
func sameAssumedRole(a, b Identity) bool {
	if a.AccountID != b.AccountID {
		return false
	}
	role := func(id Identity) string {
		prefix := "arn:aws:sts::" + id.AccountID + ":assumed-role/"
		if !strings.HasPrefix(id.ARN, prefix) {
			return ""
		}
		resource := strings.TrimPrefix(id.ARN, prefix)
		i := strings.LastIndex(resource, "/")
		if i <= 0 || i == len(resource)-1 {
			return ""
		}
		return resource[:i]
	}
	name := role(a)
	return name != "" && name == role(b)
}

func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return ErrNotFound
	}
	if s.busy[id] {
		return ErrBusy
	}
	return s.commit(id, record{}, true)
}

func (s *Service) unique(name, except string) error {
	for id, r := range s.records {
		if id != except && strings.EqualFold(r.Metadata.Name, name) {
			return ErrName
		}
	}
	return nil
}

func suffix(key string) string {
	if len(key) > 4 {
		return key[len(key)-4:]
	}
	return ""
}

func (s *Service) seal(c Credentials) (string, error) {
	if c.Kind == "profile" {
		return "", nil
	}
	if s.vault == nil {
		return "", errors.New("manual AWS credentials require Vault encryption")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return s.vault.SealBytes(data)
}

func (s *Service) source(r record) (Credentials, error) {
	if r.Metadata.Kind == "profile" {
		if r.Credentials != "" {
			return Credentials{}, errors.New("invalid profile record; the store has been preserved")
		}
		return Credentials{Kind: "profile", Profile: r.Metadata.Profile}, nil
	}
	return s.unseal(r.Credentials)
}

func (s *Service) unseal(sealed string) (Credentials, error) {
	var c Credentials
	if s.vault == nil {
		return c, errors.New("manual AWS connections exist; restore or enable their original Vault key; the store has been preserved")
	}
	plain, err := s.vault.Unseal(sealed)
	if err != nil || json.Unmarshal([]byte(plain), &c) != nil {
		return c, errors.New("cannot decrypt saved credentials. Restore the original Vault key")
	}
	return c, nil
}

// commit writes a candidate snapshot first; failures never change live state.
// The caller holds mu, and a connection's busy flag excludes competing mutations.
func (s *Service) commit(id string, r record, remove bool) error {
	next := make(map[string]record, len(s.records)+1)
	for key, value := range s.records {
		next[key] = value
	}
	if remove {
		delete(next, id)
	} else {
		next[id] = r
	}
	data, err := json.Marshal(snapshot{Version: 1, Records: next})
	if err != nil {
		return errors.New("could not encode the AWS connection store")
	}
	if err := s.write(data); err != nil {
		return fmt.Errorf("%w; check available disk space and file permissions", ErrStorage)
	}
	s.records = next
	return nil
}

func (s *Service) atomicWrite(data []byte) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".connections-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open store directory: %w", err)
	}
	defer d.Close()
	return d.Sync()
}
