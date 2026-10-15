package apigatewayv1

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

type APIKey struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Enabled         bool              `json:"enabled"`
	Value           string            `json:"value,omitempty"`
	CreatedDate     types.UnixTime    `json:"createdDate"`
	LastUpdatedDate types.UnixTime    `json:"lastUpdatedDate"`
	Tags            map[string]string `json:"tags,omitempty"`
}
type APIStage struct {
	APIID string `json:"apiId"`
	Stage string `json:"stage"`
}
type UsagePlan struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	APIStages   []APIStage        `json:"apiStages"`
	Tags        map[string]string `json:"tags,omitempty"`
}
type usagePlanRecord struct {
	Plan UsagePlan       `json:"plan"`
	Keys map[string]bool `json:"keys"`
}

// CreateAPIKeyInput contains the fields accepted when creating an API key.
type CreateAPIKeyInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Enabled     *bool             `json:"enabled"`
	Value       string            `json:"value"`
	Tags        map[string]string `json:"tags"`
}

// CreateUsagePlanInput contains the fields accepted when creating a usage plan.
type CreateUsagePlanInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	APIStages   []APIStage        `json:"apiStages"`
	Tags        map[string]string `json:"tags"`
}

// CreateUsagePlanKeyInput identifies an API key to attach to a usage plan.
type CreateUsagePlanKeyInput struct {
	KeyID   string `json:"keyId"`
	KeyType string `json:"keyType"`
}

type UsagePlanKey struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type PageOptions struct {
	Position string
	Limit    int
}

type ListAPIKeysInput struct {
	PageOptions
	IncludeValue bool
	NameQuery    string
}

type Page[T any] struct {
	Items    []T    `json:"item"`
	Position string `json:"position,omitempty"`
}

var (
	ErrAccessNotFound = errors.New("resource not found")
	ErrAPIKeyConflict = errors.New("API key value already exists")
)

func apiKeyCopy(k *APIKey, includeValue bool) APIKey {
	cp := *k
	cp.Tags = maps.Clone(k.Tags)
	if !includeValue {
		cp.Value = ""
	}
	return cp
}
func usagePlanCopy(p *usagePlanRecord) UsagePlan {
	cp := p.Plan
	cp.Tags = maps.Clone(cp.Tags)
	cp.APIStages = slices.Clone(cp.APIStages)
	return cp
}
func planKey(k *APIKey) *UsagePlanKey {
	return &UsagePlanKey{ID: k.ID, Name: k.Name, Type: "API_KEY", Value: k.Value}
}

func (s *Service) CreateAPIKey(in CreateAPIKeyInput) (*APIKey, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if in.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	now := types.UnixTime{Time: time.Now().UTC()}
	value := in.Value
	if value == "" {
		value = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	for _, key := range s.store.apiKeys {
		if key.Value == value {
			return nil, ErrAPIKeyConflict
		}
	}
	enabled := false
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	key := &APIKey{ID: randomID(10), Name: in.Name, Description: in.Description, Enabled: enabled, Value: value, CreatedDate: now, LastUpdatedDate: now, Tags: maps.Clone(in.Tags)}
	s.store.apiKeys[key.ID] = key
	s.store.dirty.Store(true)
	result := apiKeyCopy(key, true)
	return &result, nil
}

func (s *Service) ListAPIKeys(in ListAPIKeysInput) (*Page[APIKey], error) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	ids := []string{}
	for id, k := range s.store.apiKeys {
		if strings.HasPrefix(k.Name, in.NameQuery) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	result := make([]APIKey, 0, len(ids))
	for _, id := range ids {
		result = append(result, apiKeyCopy(s.store.apiKeys[id], in.IncludeValue))
	}
	return accessPage(result, in.PageOptions)
}

func (s *Service) GetAPIKey(id string, includeValue bool) (*APIKey, error) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	key := s.store.apiKeys[id]
	if key == nil {
		return nil, ErrAccessNotFound
	}
	result := apiKeyCopy(key, includeValue)
	return &result, nil
}

func (s *Service) DeleteAPIKey(id string) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.apiKeys[id] == nil {
		return ErrAccessNotFound
	}
	delete(s.store.apiKeys, id)
	for _, p := range s.store.usagePlans {
		delete(p.Keys, id)
	}
	s.store.dirty.Store(true)
	return nil
}

func (s *Service) UpdateAPIKey(id string, ops []PatchOp) (*APIKey, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	key := s.store.apiKeys[id]
	if key == nil {
		return nil, ErrAccessNotFound
	}
	cp := apiKeyCopy(key, true)
	for _, op := range ops {
		if op.Op != "replace" {
			return nil, fmt.Errorf("only replace is supported for API key fields")
		}
		switch op.Path {
		case "/name":
			cp.Name = op.Value
		case "/description":
			cp.Description = op.Value
		case "/enabled":
			v, err := strconv.ParseBool(op.Value)
			if err != nil {
				return nil, fmt.Errorf("enabled must be boolean")
			}
			cp.Enabled = v
		default:
			return nil, fmt.Errorf("unsupported API key patch path: %s", op.Path)
		}
	}
	cp.LastUpdatedDate = types.UnixTime{Time: time.Now().UTC()}
	s.store.apiKeys[id] = &cp
	s.store.dirty.Store(true)
	result := apiKeyCopy(&cp, true)
	return &result, nil
}

func (s *Service) CreateUsagePlan(in CreateUsagePlanInput) (*UsagePlan, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if in.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	for _, binding := range in.APIStages {
		rec := s.store.apis[binding.APIID]
		if rec == nil || rec.stages[binding.Stage] == nil {
			return nil, fmt.Errorf("API stage not found")
		}
	}
	p := &usagePlanRecord{Plan: UsagePlan{ID: randomID(10), Name: in.Name, Description: in.Description, APIStages: slices.Clone(in.APIStages), Tags: maps.Clone(in.Tags)}, Keys: map[string]bool{}}
	s.store.usagePlans[p.Plan.ID] = p
	s.store.dirty.Store(true)
	result := usagePlanCopy(p)
	return &result, nil
}

func (s *Service) ListUsagePlans(in PageOptions) (*Page[UsagePlan], error) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	ids := []string{}
	for id := range s.store.usagePlans {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]UsagePlan, 0, len(ids))
	for _, id := range ids {
		result = append(result, usagePlanCopy(s.store.usagePlans[id]))
	}
	return accessPage(result, in)
}

func (s *Service) GetUsagePlan(id string) (*UsagePlan, error) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	p := s.store.usagePlans[id]
	if p == nil {
		return nil, ErrAccessNotFound
	}
	result := usagePlanCopy(p)
	return &result, nil
}

func (s *Service) DeleteUsagePlan(id string) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.usagePlans[id] == nil {
		return ErrAccessNotFound
	}
	delete(s.store.usagePlans, id)
	s.store.dirty.Store(true)
	return nil
}

func (s *Service) UpdateUsagePlan(id string, ops []PatchOp) (*UsagePlan, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p := s.store.usagePlans[id]
	if p == nil {
		return nil, ErrAccessNotFound
	}
	cp := usagePlanCopy(p)
	for _, op := range ops {
		switch op.Path {
		case "/name":
			if op.Op != "replace" {
				return nil, fmt.Errorf("name requires replace")
			}
			cp.Name = op.Value
		case "/description":
			if op.Op != "replace" {
				return nil, fmt.Errorf("description requires replace")
			}
			cp.Description = op.Value
		case "/apiStages":
			api, stage, ok := strings.Cut(op.Value, ":")
			if !ok {
				return nil, fmt.Errorf("apiStages value must be apiId:stage")
			}
			binding := APIStage{APIID: api, Stage: stage}
			switch op.Op {
			case "add":
				rec := s.store.apis[api]
				if rec == nil || rec.stages[stage] == nil {
					return nil, fmt.Errorf("API stage not found")
				}
				if !slices.Contains(cp.APIStages, binding) {
					cp.APIStages = append(cp.APIStages, binding)
				}
			case "remove":
				cp.APIStages = slices.DeleteFunc(cp.APIStages, func(b APIStage) bool { return b == binding })
			default:
				return nil, fmt.Errorf("unsupported apiStages operation")
			}
		default:
			return nil, fmt.Errorf("unsupported usage plan patch path: %s", op.Path)
		}
	}
	p.Plan = cp
	s.store.dirty.Store(true)
	result := usagePlanCopy(p)
	return &result, nil
}

func (s *Service) CreateUsagePlanKey(planID string, in CreateUsagePlanKeyInput) (*UsagePlanKey, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p := s.store.usagePlans[planID]
	if p == nil {
		return nil, ErrAccessNotFound
	}
	k := s.store.apiKeys[in.KeyID]
	if k == nil {
		return nil, ErrAccessNotFound
	}
	if in.KeyType != "API_KEY" {
		return nil, fmt.Errorf("keyType must be API_KEY")
	}
	if p.Keys == nil {
		p.Keys = map[string]bool{}
	}
	p.Keys[k.ID] = true
	s.store.dirty.Store(true)
	return planKey(k), nil
}

func (s *Service) ListUsagePlanKeys(planID string, in PageOptions) (*Page[UsagePlanKey], error) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	p := s.store.usagePlans[planID]
	if p == nil {
		return nil, ErrAccessNotFound
	}
	ids := []string{}
	for keyID := range p.Keys {
		ids = append(ids, keyID)
	}
	sort.Strings(ids)
	result := make([]UsagePlanKey, 0, len(ids))
	for _, keyID := range ids {
		if k := s.store.apiKeys[keyID]; k != nil {
			result = append(result, *planKey(k))
		}
	}
	return accessPage(result, in)
}

func (s *Service) GetUsagePlanKey(planID, keyID string) (*UsagePlanKey, error) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	p := s.store.usagePlans[planID]
	if p == nil {
		return nil, ErrAccessNotFound
	}
	k := s.store.apiKeys[keyID]
	if k == nil || !p.Keys[keyID] {
		return nil, ErrAccessNotFound
	}
	return planKey(k), nil
}

func (s *Service) DeleteUsagePlanKey(planID, keyID string) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p := s.store.usagePlans[planID]
	if p == nil || s.store.apiKeys[keyID] == nil || !p.Keys[keyID] {
		return ErrAccessNotFound
	}
	delete(p.Keys, keyID)
	s.store.dirty.Store(true)
	return nil
}

func accessPage[T any](items []T, in PageOptions) (*Page[T], error) {
	offset := 0
	var err error
	if in.Position != "" {
		offset, err = strconv.Atoi(in.Position)
		if err != nil || offset < 0 || offset > len(items) {
			return nil, fmt.Errorf("invalid position")
		}
	}
	limit := in.Limit
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 500 {
		return nil, fmt.Errorf("limit must be between 1 and 500")
	}
	end := min(offset+limit, len(items))
	out := &Page[T]{Items: items[offset:end]}
	if end < len(items) {
		out.Position = strconv.Itoa(end)
	}
	return out, nil
}

func (s *Service) AccessTags(resource, method string, tags map[string]string, remove []string) (map[string]string, error) {
	prefix := fmt.Sprintf("arn:aws:apigateway:%s::/", s.cfg.Region)
	if !strings.HasPrefix(resource, prefix) {
		return nil, fmt.Errorf("resource not found")
	}
	parts := strings.Split(strings.TrimPrefix(resource, prefix), "/")
	isStage := len(parts) == 4 && parts[0] == "restapis" && parts[2] == "stages"
	if len(parts) != 2 && !isStage {
		return nil, fmt.Errorf("resource not found")
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	var target *map[string]string
	switch parts[0] {
	case "apikeys":
		if k := s.store.apiKeys[parts[1]]; k != nil {
			target = &k.Tags
		}
	case "usageplans":
		if p := s.store.usagePlans[parts[1]]; p != nil {
			target = &p.Plan.Tags
		}
	case "restapis":
		if api := s.store.apis[parts[1]]; api != nil {
			if len(parts) == 4 {
				if stage := api.stages[parts[3]]; stage != nil {
					target = &stage.Tags
				}
			} else {
				target = &api.api.Tags
			}
		}
	}
	if target == nil {
		return nil, fmt.Errorf("resource not found")
	}
	if method != "GET" {
		if *target == nil {
			*target = map[string]string{}
		}
		maps.Copy(*target, tags)
		for _, k := range remove {
			delete(*target, k)
		}
		s.store.dirty.Store(true)
	}
	result := maps.Clone(*target)
	if result == nil {
		result = map[string]string{}
	}
	return result, nil
}
