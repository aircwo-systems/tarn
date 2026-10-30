package kms

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
)

// keyUsage counts a key's cryptographic operations since Tarn started. It is
// kept in memory only, so recording it never schedules a disk write.
type keyUsage struct {
	Encrypt     int64      `json:"encrypt"`
	Decrypt     int64      `json:"decrypt"`
	DataKeys    int64      `json:"dataKeys"`
	ReEncrypt   int64      `json:"reEncrypt"`
	Failures    int64      `json:"failures"`
	LastUsed    *time.Time `json:"lastUsed,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
	LastErrorAt *time.Time `json:"lastErrorAt,omitempty"`
}

// recordLocked counts one operation on k. A nil k (the key was never
// identified) records nothing.
func (s *Service) recordLocked(k *key, op string, err error) {
	if k == nil {
		return
	}
	u := s.usage[k.ID]
	if u == nil {
		u = &keyUsage{}
		s.usage[k.ID] = u
	}
	now := s.now()
	if err != nil {
		u.Failures++
		code := err.Error()
		var ke *Error
		if errors.As(err, &ke) {
			code = ke.Code
		}
		u.LastError, u.LastErrorAt = code, &now
		return
	}
	switch op {
	case "encrypt":
		u.Encrypt++
	case "decrypt":
		u.Decrypt++
	case "dataKey":
		u.DataKeys++
	case "reEncrypt":
		u.ReEncrypt++
	}
	u.LastUsed = &now
}

// LookupKeyID resolves a key reference (key ID, key ARN, alias name or alias
// ARN) to a key ID without side effects: unlike the API operations it never
// creates AWS managed keys. It reports false when nothing matches.
func (s *Service) LookupKeyID(ref string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(ref)
	if strings.HasPrefix(name, "arn:") {
		rest, ok := strings.CutPrefix(name, s.arnPrefix())
		if !ok {
			return "", false
		}
		name = rest
	}
	if strings.HasPrefix(name, "alias/") {
		a := s.aliases[name]
		if a == nil {
			return "", false
		}
		name = a.TargetID
	}
	id := strings.TrimPrefix(name, "key/")
	k := s.keys[id]
	if k == nil || (k.State == statePendingDeletion && !s.now().Before(k.DeletionDate)) {
		return "", false
	}
	return id, true
}

// KeySummary is a key as shown in the dashboard.
type KeySummary struct {
	ID                 string            `json:"id"`
	Arn                string            `json:"arn"`
	Description        string            `json:"description,omitempty"`
	State              string            `json:"state"`
	Manager            string            `json:"manager"`
	Created            time.Time         `json:"created"`
	DeletionDate       *time.Time        `json:"deletionDate,omitempty"`
	Aliases            []string          `json:"aliases"`
	RotationEnabled    bool              `json:"rotationEnabled"`
	RotationPeriodDays int               `json:"rotationPeriodDays,omitempty"`
	Tags               map[string]string `json:"tags,omitempty"`
	TagCount           int               `json:"tagCount"`
	Policy             string            `json:"policy"`
	Usage              keyUsage          `json:"usage"`
}

// KeySummaries lists every key for the dashboard: customer managed keys
// first, then AWS managed keys, each by name.
func (s *Service) KeySummaries() []KeySummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	aliases := map[string][]string{}
	for _, name := range sortedIDs(s.aliases) {
		a := s.aliases[name]
		aliases[a.TargetID] = append(aliases[a.TargetID], name)
	}
	out := make([]KeySummary, 0, len(s.keys))
	for _, id := range sortedIDs(s.keys) {
		k := s.keys[id]
		if k.State == statePendingDeletion && !s.now().Before(k.DeletionDate) {
			continue
		}
		sum := KeySummary{
			ID:                 k.ID,
			Arn:                s.keyARN(k.ID),
			Description:        k.Description,
			State:              k.State,
			Manager:            k.Manager,
			Created:            k.Created,
			Aliases:            slices.Clone(aliases[k.ID]),
			RotationEnabled:    k.RotationEnabled,
			RotationPeriodDays: k.RotationPeriod,
			TagCount:           len(k.Tags),
			Policy:             k.Policy,
		}
		if sum.Aliases == nil {
			sum.Aliases = []string{}
		}
		if k.State == statePendingDeletion {
			d := k.DeletionDate
			sum.DeletionDate = &d
		}
		if len(k.Tags) > 0 {
			sum.Tags = make(map[string]string, len(k.Tags))
			for _, t := range k.Tags {
				sum.Tags[t.TagKey] = t.TagValue
			}
		}
		if u := s.usage[k.ID]; u != nil {
			sum.Usage = *u
		}
		out = append(out, sum)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Manager == managerCustomer) != (out[j].Manager == managerCustomer) {
			return out[i].Manager == managerCustomer
		}
		return displayName(out[i]) < displayName(out[j])
	})
	return out
}

func displayName(k KeySummary) string {
	if len(k.Aliases) > 0 {
		return k.Aliases[0]
	}
	return "~" + k.ID // keys without an alias sort after named ones
}

// KeyCount returns the number of keys, for account resource counts.
func (s *Service) KeyCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.keys)
}
