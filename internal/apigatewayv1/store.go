package apigatewayv1

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Store persists REST API (v1) state.
type Store struct {
	mu   sync.RWMutex
	apis map[string]*restAPIRecord
	cfg  *config.Config
}

type restAPIRecord struct {
	api                  *types.RestAPI
	resources            map[string]*types.RestResource
	methods              map[methodKey]*types.RestMethod
	integrations         map[methodKey]*types.RestIntegration
	methodResponses      map[responseKey]*types.RestMethodResponse
	integrationResponses map[responseKey]*types.RestIntegrationResponse
	deployments          map[string]*types.RestDeployment
	stages               map[string]*types.RestStage
}

type methodKey struct {
	resourceID string
	httpMethod string
}

type responseKey struct {
	resourceID string
	httpMethod string
	statusCode string
}

type storeSnapshot struct {
	APIs []apiSnapshot `json:"apis"`
}

type apiSnapshot struct {
	API                  *types.RestAPI                   `json:"api"`
	Resources            []*types.RestResource            `json:"resources"`
	Methods              []*types.RestMethod              `json:"methods"`
	Integrations         []*types.RestIntegration         `json:"integrations"`
	MethodResponses      []*types.RestMethodResponse      `json:"methodResponses"`
	IntegrationResponses []*types.RestIntegrationResponse `json:"integrationResponses"`
	Deployments          []*types.RestDeployment          `json:"deployments"`
	Stages               []*types.RestStage               `json:"stages"`
}

// NewStore creates a new v1 REST API store.
func NewStore(cfg *config.Config) *Store {
	return &Store{
		apis: make(map[string]*restAPIRecord),
		cfg:  cfg,
	}
}

// Init loads persisted state.
func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}
	if err := os.MkdirAll(s.cfg.APIGatewayV1Dir(), 0755); err != nil {
		return fmt.Errorf("create apigatewayv1 dir: %w", err)
	}
	data, err := os.ReadFile(s.cfg.APIGatewayV1StatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read apigatewayv1 state: %w", err)
	}
	var snap storeSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("decode apigatewayv1 state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apis = make(map[string]*restAPIRecord, len(snap.APIs))
	for _, item := range snap.APIs {
		if item.API == nil {
			continue
		}
		rec := newRecord(item.API)
		for _, r := range item.Resources {
			rec.resources[r.ID] = r
		}
		for _, m := range item.Methods {
			rec.methods[methodKey{m.ResourceID, m.HTTPMethod}] = m
		}
		for _, i := range item.Integrations {
			rec.integrations[methodKey{i.ResourceID, i.MethodHTTPMethod}] = i
		}
		for _, mr := range item.MethodResponses {
			rec.methodResponses[responseKey{mr.ResourceID, mr.HTTPMethod, mr.StatusCode}] = mr
		}
		for _, ir := range item.IntegrationResponses {
			rec.integrationResponses[responseKey{ir.ResourceID, ir.HTTPMethod, ir.StatusCode}] = ir
		}
		for _, d := range item.Deployments {
			rec.deployments[d.ID] = d
		}
		for _, st := range item.Stages {
			rec.stages[st.StageName] = st
		}
		s.apis[item.API.ID] = rec
	}
	return nil
}

func newRecord(api *types.RestAPI) *restAPIRecord {
	return &restAPIRecord{
		api:                  api,
		resources:            make(map[string]*types.RestResource),
		methods:              make(map[methodKey]*types.RestMethod),
		integrations:         make(map[methodKey]*types.RestIntegration),
		methodResponses:      make(map[responseKey]*types.RestMethodResponse),
		integrationResponses: make(map[responseKey]*types.RestIntegrationResponse),
		deployments:          make(map[string]*types.RestDeployment),
		stages:               make(map[string]*types.RestStage),
	}
}

// CreateAPI stores a new REST API and its root resource.
func (s *Store) CreateAPI(api *types.RestAPI, rootResource *types.RestResource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.apis[api.ID]; exists {
		return fmt.Errorf("rest api %s already exists", api.ID)
	}
	rec := newRecord(api)
	if rootResource != nil {
		rec.resources[rootResource.ID] = rootResource
	}
	s.apis[api.ID] = rec
	s.persistLocked()
	return nil
}

// ListAPIs returns all REST APIs.
func (s *Store) ListAPIs() []*types.RestAPI {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*types.RestAPI, 0, len(s.apis))
	for _, rec := range s.apis {
		cp := *rec.api
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// GetAPI returns a REST API by ID.
func (s *Store) GetAPI(apiID string) (*types.RestAPI, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	cp := *rec.api
	return &cp, nil
}

// GetAPIByName returns a REST API whose Name matches (first match).
func (s *Store) GetAPIByName(name string) (*types.RestAPI, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rec := range s.apis {
		if rec.api.Name == name {
			cp := *rec.api
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("rest api with name %q not found", name)
}

// SaveAPI persists mutable API fields.
func (s *Store) SaveAPI(api *types.RestAPI) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[api.ID]
	if !ok {
		return fmt.Errorf("rest api %s not found", api.ID)
	}
	cp := *api
	rec.api = &cp
	s.persistLocked()
	return nil
}

// DeleteAPI removes a REST API and all child resources.
func (s *Store) DeleteAPI(apiID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.apis[apiID]; !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	delete(s.apis, apiID)
	s.persistLocked()
	return nil
}

// CreateResource adds a resource to a REST API.
func (s *Store) CreateResource(apiID string, res *types.RestResource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	cp := *res
	rec.resources[res.ID] = &cp
	s.persistLocked()
	return nil
}

// ListResources returns all resources for an API.
func (s *Store) ListResources(apiID string) ([]*types.RestResource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	result := make([]*types.RestResource, 0, len(rec.resources))
	for _, r := range rec.resources {
		cp := *r
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

// GetResource returns a single resource.
func (s *Store) GetResource(apiID, resourceID string) (*types.RestResource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	r, ok := rec.resources[resourceID]
	if !ok {
		return nil, fmt.Errorf("resource %s not found", resourceID)
	}
	cp := *r
	return &cp, nil
}

// DeleteResource removes a resource.
func (s *Store) DeleteResource(apiID, resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	if _, ok := rec.resources[resourceID]; !ok {
		return fmt.Errorf("resource %s not found", resourceID)
	}
	delete(rec.resources, resourceID)
	s.persistLocked()
	return nil
}

// PutMethod creates or replaces a method on a resource.
func (s *Store) PutMethod(apiID string, method *types.RestMethod) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	cp := *method
	rec.methods[methodKey{method.ResourceID, method.HTTPMethod}] = &cp
	s.persistLocked()
	return nil
}

// GetMethod returns a method on a resource.
func (s *Store) GetMethod(apiID, resourceID, httpMethod string) (*types.RestMethod, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	m, ok := rec.methods[methodKey{resourceID, httpMethod}]
	if !ok {
		return nil, fmt.Errorf("method %s not found on resource %s", httpMethod, resourceID)
	}
	cp := *m
	return &cp, nil
}

// DeleteMethod removes a method.
func (s *Store) DeleteMethod(apiID, resourceID, httpMethod string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	k := methodKey{resourceID, httpMethod}
	if _, ok := rec.methods[k]; !ok {
		return fmt.Errorf("method %s not found on resource %s", httpMethod, resourceID)
	}
	delete(rec.methods, k)
	s.persistLocked()
	return nil
}

// PutIntegration creates or replaces an integration on a method.
func (s *Store) PutIntegration(apiID string, integration *types.RestIntegration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	cp := *integration
	rec.integrations[methodKey{integration.ResourceID, integration.MethodHTTPMethod}] = &cp
	s.persistLocked()
	return nil
}

// GetIntegration returns the integration for a method.
func (s *Store) GetIntegration(apiID, resourceID, httpMethod string) (*types.RestIntegration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	i, ok := rec.integrations[methodKey{resourceID, httpMethod}]
	if !ok {
		return nil, fmt.Errorf("integration not found for %s on resource %s", httpMethod, resourceID)
	}
	cp := *i
	return &cp, nil
}

// ListIntegrations returns all integrations across all resources for an API.
func (s *Store) ListIntegrations(apiID string) ([]*types.RestIntegration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	result := make([]*types.RestIntegration, 0, len(rec.integrations))
	for _, i := range rec.integrations {
		cp := *i
		result = append(result, &cp)
	}
	return result, nil
}

// DeleteIntegration removes an integration.
func (s *Store) DeleteIntegration(apiID, resourceID, httpMethod string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	k := methodKey{resourceID, httpMethod}
	if _, ok := rec.integrations[k]; !ok {
		return fmt.Errorf("integration not found for %s on resource %s", httpMethod, resourceID)
	}
	delete(rec.integrations, k)
	s.persistLocked()
	return nil
}

// PutMethodResponse creates or replaces a method response.
func (s *Store) PutMethodResponse(apiID string, mr *types.RestMethodResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	cp := *mr
	rec.methodResponses[responseKey{mr.ResourceID, mr.HTTPMethod, mr.StatusCode}] = &cp
	s.persistLocked()
	return nil
}

// GetMethodResponse returns a method response.
func (s *Store) GetMethodResponse(apiID, resourceID, httpMethod, statusCode string) (*types.RestMethodResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	mr, ok := rec.methodResponses[responseKey{resourceID, httpMethod, statusCode}]
	if !ok {
		return nil, fmt.Errorf("method response %s not found", statusCode)
	}
	cp := *mr
	return &cp, nil
}

// PutIntegrationResponse creates or replaces an integration response.
func (s *Store) PutIntegrationResponse(apiID string, ir *types.RestIntegrationResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	cp := *ir
	rec.integrationResponses[responseKey{ir.ResourceID, ir.HTTPMethod, ir.StatusCode}] = &cp
	s.persistLocked()
	return nil
}

// GetIntegrationResponse returns an integration response.
func (s *Store) GetIntegrationResponse(apiID, resourceID, httpMethod, statusCode string) (*types.RestIntegrationResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	ir, ok := rec.integrationResponses[responseKey{resourceID, httpMethod, statusCode}]
	if !ok {
		return nil, fmt.Errorf("integration response %s not found", statusCode)
	}
	cp := *ir
	return &cp, nil
}

// CreateDeployment stores a new deployment.
func (s *Store) CreateDeployment(apiID string, dep *types.RestDeployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	cp := *dep
	rec.deployments[dep.ID] = &cp
	s.persistLocked()
	return nil
}

// ListDeployments returns all deployments.
func (s *Store) ListDeployments(apiID string) ([]*types.RestDeployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	result := make([]*types.RestDeployment, 0, len(rec.deployments))
	for _, d := range rec.deployments {
		cp := *d
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// GetDeployment returns a deployment by ID.
func (s *Store) GetDeployment(apiID, deploymentID string) (*types.RestDeployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	d, ok := rec.deployments[deploymentID]
	if !ok {
		return nil, fmt.Errorf("deployment %s not found", deploymentID)
	}
	cp := *d
	return &cp, nil
}

// CreateStage stores a new stage.
func (s *Store) CreateStage(apiID string, stage *types.RestStage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	cp := *stage
	rec.stages[stage.StageName] = &cp
	s.persistLocked()
	return nil
}

// ListStages returns all stages.
func (s *Store) ListStages(apiID string) ([]*types.RestStage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	result := make([]*types.RestStage, 0, len(rec.stages))
	for _, st := range rec.stages {
		cp := *st
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StageName < result[j].StageName })
	return result, nil
}

// GetStage returns a stage by name.
func (s *Store) GetStage(apiID, stageName string) (*types.RestStage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("rest api %s not found", apiID)
	}
	st, ok := rec.stages[stageName]
	if !ok {
		return nil, fmt.Errorf("stage %s not found", stageName)
	}
	cp := *st
	return &cp, nil
}

// DeleteStage removes a stage.
func (s *Store) DeleteStage(apiID, stageName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("rest api %s not found", apiID)
	}
	if _, ok := rec.stages[stageName]; !ok {
		return fmt.Errorf("stage %s not found", stageName)
	}
	delete(rec.stages, stageName)
	s.persistLocked()
	return nil
}

// GetAllIntegrationsForAPI returns all integrations for use in the admin overview.
func (s *Store) GetAllIntegrationsForAPI(apiID string) ([]*types.RestIntegration, error) {
	return s.ListIntegrations(apiID)
}

func (s *Store) persistLocked() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}
	apiIDs := make([]string, 0, len(s.apis))
	for id := range s.apis {
		apiIDs = append(apiIDs, id)
	}
	sort.Strings(apiIDs)

	snap := storeSnapshot{APIs: make([]apiSnapshot, 0, len(apiIDs))}
	for _, apiID := range apiIDs {
		rec := s.apis[apiID]
		item := apiSnapshot{
			API:                  rec.api,
			Resources:            make([]*types.RestResource, 0, len(rec.resources)),
			Methods:              make([]*types.RestMethod, 0, len(rec.methods)),
			Integrations:         make([]*types.RestIntegration, 0, len(rec.integrations)),
			MethodResponses:      make([]*types.RestMethodResponse, 0, len(rec.methodResponses)),
			IntegrationResponses: make([]*types.RestIntegrationResponse, 0, len(rec.integrationResponses)),
			Deployments:          make([]*types.RestDeployment, 0, len(rec.deployments)),
			Stages:               make([]*types.RestStage, 0, len(rec.stages)),
		}
		for _, r := range rec.resources {
			item.Resources = append(item.Resources, r)
		}
		for _, m := range rec.methods {
			item.Methods = append(item.Methods, m)
		}
		for _, i := range rec.integrations {
			item.Integrations = append(item.Integrations, i)
		}
		for _, mr := range rec.methodResponses {
			item.MethodResponses = append(item.MethodResponses, mr)
		}
		for _, ir := range rec.integrationResponses {
			item.IntegrationResponses = append(item.IntegrationResponses, ir)
		}
		for _, d := range rec.deployments {
			item.Deployments = append(item.Deployments, d)
		}
		for _, st := range rec.stages {
			item.Stages = append(item.Stages, st)
		}
		snap.APIs = append(snap.APIs, item)
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.cfg.APIGatewayV1StatePath()), 0755); err != nil {
		return
	}
	tmpPath := s.cfg.APIGatewayV1StatePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return
	}
	_ = os.Rename(tmpPath, s.cfg.APIGatewayV1StatePath())
}
