package kms

import (
	"regexp"
	"slices"
	"strings"
	"time"
)

// CreateKey creates a symmetric encryption key. Tarn has no asymmetric, HMAC
// or multi-Region keys.
func (s *Service) CreateKey(in *CreateKeyInput) (*KeyMetadataOutput, error) {
	spec := in.KeySpec
	if spec == "" {
		spec = in.CustomerMasterKeySpec
	}
	if spec != "" && spec != symmetricDefault {
		return nil, NotImplemented("KeySpec " + spec)
	}
	if in.KeyUsage != "" && in.KeyUsage != usageEncrypt {
		return nil, NotImplemented("KeyUsage " + in.KeyUsage)
	}
	if in.Origin != "" && in.Origin != "AWS_KMS" {
		return nil, NotImplemented("Origin " + in.Origin)
	}
	if in.MultiRegion {
		return nil, NotImplemented("multi-Region keys")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.newKeyLocked(in.Description, managerCustomer, in.Policy, slices.Clone(in.Tags))
	if err != nil {
		return nil, err
	}
	return &KeyMetadataOutput{KeyMetadata: s.metadata(k)}, nil
}

// DescribeKey returns a key's metadata.
func (s *Service) DescribeKey(in *KeyIDInput) (*KeyMetadataOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.resolveLocked(in.KeyId)
	if err != nil {
		return nil, err
	}
	return &KeyMetadataOutput{KeyMetadata: s.metadata(k)}, nil
}

// ListKeys lists every key in the account, in one page.
func (s *Service) ListKeys(*ListKeysInput) (*ListKeysOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := &ListKeysOutput{Keys: []KeyListEntry{}}
	for _, id := range sortedIDs(s.keys) {
		if s.expiredLocked(s.keys[id]) {
			continue
		}
		out.Keys = append(out.Keys, KeyListEntry{KeyId: id, KeyArn: s.keyARN(id)})
	}
	return out, nil
}

// UpdateKeyDescription replaces a key's description.
func (s *Service) UpdateKeyDescription(in *UpdateKeyDescriptionInput) (*struct{}, error) {
	return s.mutateCustomerKey(in.KeyId, func(k *key) error {
		k.Description = in.Description
		return nil
	})
}

// EnableKey makes a disabled key usable again.
func (s *Service) EnableKey(in *KeyIDInput) (*struct{}, error) {
	return s.mutateCustomerKey(in.KeyId, func(k *key) error {
		if k.State == statePendingDeletion {
			return s.checkUsable(k)
		}
		k.State = stateEnabled
		return nil
	})
}

// DisableKey stops a key from being used for cryptographic operations.
func (s *Service) DisableKey(in *KeyIDInput) (*struct{}, error) {
	return s.mutateCustomerKey(in.KeyId, func(k *key) error {
		if k.State == statePendingDeletion {
			return s.checkUsable(k)
		}
		k.State = stateDisabled
		return nil
	})
}

// ScheduleKeyDeletion marks a key for deletion after a 7 to 30 day window.
// The key disappears once the window passes.
func (s *Service) ScheduleKeyDeletion(in *ScheduleKeyDeletionInput) (*ScheduleKeyDeletionOutput, error) {
	days := in.PendingWindowInDays
	if days == 0 {
		days = 30
	}
	if days < 7 || days > 30 {
		return nil, validation("1 validation error detected: Value '%d' at 'pendingWindowInDays' failed to satisfy constraint: Member must have value between 7 and 30", days)
	}
	var out *ScheduleKeyDeletionOutput
	_, err := s.mutateCustomerKey(in.KeyId, func(k *key) error {
		if k.State == statePendingDeletion {
			return s.checkUsable(k)
		}
		k.State = statePendingDeletion
		k.DeletionDate = s.now().Add(time.Duration(days) * 24 * time.Hour)
		out = &ScheduleKeyDeletionOutput{
			KeyId:               s.keyARN(k.ID),
			DeletionDate:        EpochTime(k.DeletionDate),
			KeyState:            k.State,
			PendingWindowInDays: days,
		}
		return nil
	})
	return out, err
}

// CancelKeyDeletion stops a scheduled deletion. The key is left disabled, as
// in AWS.
func (s *Service) CancelKeyDeletion(in *KeyIDInput) (*KeyIDOutput, error) {
	var out *KeyIDOutput
	_, err := s.mutateCustomerKey(in.KeyId, func(k *key) error {
		if k.State != statePendingDeletion {
			return newError("KMSInvalidStateException", "%s is not pending deletion.", s.keyARN(k.ID))
		}
		k.State = stateDisabled
		k.DeletionDate = time.Time{}
		out = &KeyIDOutput{KeyId: s.keyARN(k.ID)}
		return nil
	})
	return out, err
}

// mutateCustomerKey applies fn to a customer managed key. AWS managed keys
// cannot be changed.
func (s *Service) mutateCustomerKey(keyID string, fn func(*key) error) (*struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.resolveLocked(keyID)
	if err != nil {
		return nil, err
	}
	if k.Manager == managerAWS {
		return nil, newError("AccessDeniedException", "AWS managed key %s cannot be modified.", s.keyARN(k.ID))
	}
	if err := fn(k); err != nil {
		return nil, err
	}
	s.markDirty()
	return &struct{}{}, nil
}

// --- Aliases ---

var aliasNamePattern = regexp.MustCompile(`^alias/[a-zA-Z0-9/_-]+$`)

func (s *Service) checkAliasName(name string) error {
	if !aliasNamePattern.MatchString(name) || len(name) > 256 {
		return newError("InvalidAliasNameException", "Alias must start with the prefix \"alias/\". Please see https://docs.aws.amazon.com/kms/latest/developerguide/kms-alias.html")
	}
	if strings.HasPrefix(name, managedAliasPrefix) {
		return newError("NotAuthorizedException", "Cannot create alias with prefix 'alias/aws/'")
	}
	return nil
}

// aliasTargetLocked resolves an alias target, which must be a customer
// managed key named by key ID or key ARN.
func (s *Service) aliasTargetLocked(targetKeyID string) (*key, error) {
	if strings.Contains(targetKeyID, "alias/") {
		return nil, validation("Aliases must refer to keys. Not aliases")
	}
	k, err := s.resolveLocked(targetKeyID)
	if err != nil {
		return nil, err
	}
	if k.Manager == managerAWS {
		return nil, newError("NotAuthorizedException", "Cannot point an alias at an AWS managed key")
	}
	return k, nil
}

// CreateAlias names a key.
func (s *Service) CreateAlias(in *AliasInput) (*struct{}, error) {
	if err := s.checkAliasName(in.AliasName); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aliases[in.AliasName] != nil {
		return nil, newError("AlreadyExistsException", "An alias with the name %s already exists", s.aliasARN(in.AliasName))
	}
	k, err := s.aliasTargetLocked(in.TargetKeyId)
	if err != nil {
		return nil, err
	}
	now := s.now()
	s.aliases[in.AliasName] = &alias{Name: in.AliasName, TargetID: k.ID, Created: now, Updated: now}
	s.markDirty()
	return &struct{}{}, nil
}

// UpdateAlias points an existing alias at another key.
func (s *Service) UpdateAlias(in *AliasInput) (*struct{}, error) {
	if err := s.checkAliasName(in.AliasName); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.aliases[in.AliasName]
	if a == nil {
		return nil, aliasNotFound(s.aliasARN(in.AliasName))
	}
	k, err := s.aliasTargetLocked(in.TargetKeyId)
	if err != nil {
		return nil, err
	}
	a.TargetID = k.ID
	a.Updated = s.now()
	s.markDirty()
	return &struct{}{}, nil
}

// DeleteAlias removes an alias. The key is unaffected.
func (s *Service) DeleteAlias(in *AliasInput) (*struct{}, error) {
	if err := s.checkAliasName(in.AliasName); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aliases[in.AliasName] == nil {
		return nil, aliasNotFound(s.aliasARN(in.AliasName))
	}
	delete(s.aliases, in.AliasName)
	s.markDirty()
	return &struct{}{}, nil
}

// ListAliases lists aliases, optionally only those pointing at one key.
func (s *Service) ListAliases(in *ListAliasesInput) (*ListAliasesOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	targetID := ""
	if in.KeyId != "" {
		k, err := s.resolveLocked(in.KeyId)
		if err != nil {
			return nil, err
		}
		targetID = k.ID
	}
	out := &ListAliasesOutput{Aliases: []AliasListEntry{}}
	for _, name := range sortedIDs(s.aliases) {
		a := s.aliases[name]
		if k := s.keys[a.TargetID]; k == nil || s.expiredLocked(k) {
			continue
		}
		if targetID != "" && a.TargetID != targetID {
			continue
		}
		out.Aliases = append(out.Aliases, AliasListEntry{
			AliasName:       a.Name,
			AliasArn:        s.aliasARN(a.Name),
			TargetKeyId:     a.TargetID,
			CreationDate:    EpochTime(a.Created),
			LastUpdatedDate: EpochTime(a.Updated),
		})
	}
	return out, nil
}

// --- Tags ---

// TagResource adds or replaces tags on a key.
func (s *Service) TagResource(in *TagResourceInput) (*struct{}, error) {
	return s.mutateCustomerKey(in.KeyId, func(k *key) error {
		for _, t := range in.Tags {
			i := slices.IndexFunc(k.Tags, func(e Tag) bool { return e.TagKey == t.TagKey })
			if i >= 0 {
				k.Tags[i].TagValue = t.TagValue
			} else {
				k.Tags = append(k.Tags, t)
			}
		}
		return nil
	})
}

// UntagResource removes tags from a key by tag key.
func (s *Service) UntagResource(in *UntagResourceInput) (*struct{}, error) {
	return s.mutateCustomerKey(in.KeyId, func(k *key) error {
		k.Tags = slices.DeleteFunc(k.Tags, func(t Tag) bool { return slices.Contains(in.TagKeys, t.TagKey) })
		return nil
	})
}

// ListResourceTags lists a key's tags, in one page.
func (s *Service) ListResourceTags(in *ListResourceTagsInput) (*ListResourceTagsOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.resolveLocked(in.KeyId)
	if err != nil {
		return nil, err
	}
	tags := slices.Clone(k.Tags)
	if tags == nil {
		tags = []Tag{}
	}
	return &ListResourceTagsOutput{Tags: tags}, nil
}

// --- Key policy and rotation (stored, not enforced) ---

func checkPolicyName(name string) error {
	if name != "" && name != "default" {
		return notFound("Policy %s does not exist", name)
	}
	return nil
}

// GetKeyPolicy returns the key's policy document.
func (s *Service) GetKeyPolicy(in *KeyPolicyInput) (*GetKeyPolicyOutput, error) {
	if err := checkPolicyName(in.PolicyName); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.resolveLocked(in.KeyId)
	if err != nil {
		return nil, err
	}
	return &GetKeyPolicyOutput{Policy: k.Policy, PolicyName: "default"}, nil
}

// PutKeyPolicy replaces the key's policy document. Tarn does not evaluate it.
func (s *Service) PutKeyPolicy(in *KeyPolicyInput) (*struct{}, error) {
	if err := checkPolicyName(in.PolicyName); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Policy) == "" {
		return nil, newError("MalformedPolicyDocumentException", "The new key policy will not allow you to update the key policy in the future.")
	}
	return s.mutateCustomerKey(in.KeyId, func(k *key) error {
		k.Policy = in.Policy
		return nil
	})
}

// ListKeyPolicies lists the key's policy names; there is only ever "default".
func (s *Service) ListKeyPolicies(in *KeyIDInput) (*ListKeyPoliciesOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.resolveLocked(in.KeyId); err != nil {
		return nil, err
	}
	return &ListKeyPoliciesOutput{PolicyNames: []string{"default"}}, nil
}

// GetKeyRotationStatus reports the stored rotation setting. Tarn never
// rotates key material.
func (s *Service) GetKeyRotationStatus(in *KeyIDInput) (*GetKeyRotationStatusOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.resolveLocked(in.KeyId)
	if err != nil {
		return nil, err
	}
	out := &GetKeyRotationStatusOutput{KeyId: s.keyARN(k.ID), KeyRotationEnabled: k.RotationEnabled}
	if k.RotationEnabled {
		out.RotationPeriodInDays = k.RotationPeriod
	}
	return out, nil
}

// EnableKeyRotation records that rotation is on, every 90 to 2560 days.
func (s *Service) EnableKeyRotation(in *KeyRotationInput) (*struct{}, error) {
	days := in.RotationPeriodInDays
	if days == 0 {
		days = 365
	}
	if days < 90 || days > 2560 {
		return nil, validation("1 validation error detected: Value '%d' at 'rotationPeriodInDays' failed to satisfy constraint: Member must have value between 90 and 2560", days)
	}
	return s.mutateCustomerKey(in.KeyId, func(k *key) error {
		k.RotationEnabled = true
		k.RotationPeriod = days
		return nil
	})
}

// DisableKeyRotation records that rotation is off.
func (s *Service) DisableKeyRotation(in *KeyRotationInput) (*struct{}, error) {
	return s.mutateCustomerKey(in.KeyId, func(k *key) error {
		k.RotationEnabled = false
		k.RotationPeriod = 0
		return nil
	})
}
