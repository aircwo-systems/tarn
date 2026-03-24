package apigateway

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// Store is an API Gateway store with optional disk-backed persistence.
type Store struct {
	mu   sync.RWMutex
	apis map[string]*apiRecord
	cfg  *config.Config
}

type apiRecord struct {
	api          *types.APIGatewayAPI
	integrations map[string]*types.APIGatewayIntegration
	routes       map[string]*types.APIGatewayRoute
	stages       map[string]*types.APIGatewayStage
}

type apiSnapshot struct {
	API          *types.APIGatewayAPI           `json:"api"`
	Integrations []*types.APIGatewayIntegration `json:"integrations"`
	Routes       []*types.APIGatewayRoute       `json:"routes"`
	Stages       []*types.APIGatewayStage       `json:"stages"`
}

// NewStore creates a new API Gateway store.
func NewStore(cfg *config.Config) *Store {
	return &Store{apis: make(map[string]*apiRecord), cfg: cfg}
}

// Init loads persisted API Gateway state if persistence is enabled.
func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}

	if err := os.MkdirAll(s.cfg.APIGatewayDir(), 0755); err != nil {
		return fmt.Errorf("create apigateway dir: %w", err)
	}

	data, err := os.ReadFile(s.cfg.APIGatewayStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read apigateway state: %w", err)
	}

	var snapshot struct {
		APIs []apiSnapshot `json:"apis"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode apigateway state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.apis = make(map[string]*apiRecord, len(snapshot.APIs))
	for _, item := range snapshot.APIs {
		if item.API == nil {
			continue
		}

		rec := &apiRecord{
			api:          cloneAPI(item.API),
			integrations: make(map[string]*types.APIGatewayIntegration, len(item.Integrations)),
			routes:       make(map[string]*types.APIGatewayRoute, len(item.Routes)),
			stages:       make(map[string]*types.APIGatewayStage, len(item.Stages)),
		}
		for _, integration := range item.Integrations {
			rec.integrations[integration.IntegrationID] = cloneIntegration(integration)
		}
		for _, route := range item.Routes {
			rec.routes[route.RouteID] = cloneRoute(route)
		}
		for _, stage := range item.Stages {
			rec.stages[stage.StageName] = cloneStage(stage)
		}
		s.apis[item.API.APIID] = rec
	}
	return nil
}

func (s *Store) CreateAPI(api *types.APIGatewayAPI, stage *types.APIGatewayStage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.apis[api.APIID]; exists {
		return fmt.Errorf("api %s already exists", api.APIID)
	}

	rec := &apiRecord{
		api:          cloneAPI(api),
		integrations: make(map[string]*types.APIGatewayIntegration),
		routes:       make(map[string]*types.APIGatewayRoute),
		stages:       make(map[string]*types.APIGatewayStage),
	}
	if stage != nil {
		rec.stages[stage.StageName] = cloneStage(stage)
	}

	s.apis[api.APIID] = rec
	s.persistLocked()
	return nil
}

func (s *Store) ListAPIs() []*types.APIGatewayAPI {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*types.APIGatewayAPI, 0, len(s.apis))
	for _, rec := range s.apis {
		result = append(result, cloneAPI(rec.api))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].APIID < result[j].APIID
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (s *Store) GetAPI(apiID string) (*types.APIGatewayAPI, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("api %s not found", apiID)
	}
	return cloneAPI(rec.api), nil
}

func (s *Store) SaveAPI(api *types.APIGatewayAPI) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.apis[api.APIID]
	if !ok {
		return fmt.Errorf("api %s not found", api.APIID)
	}
	rec.api = cloneAPI(api)
	s.persistLocked()
	return nil
}

func (s *Store) DeleteAPI(apiID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.apis[apiID]; !ok {
		return fmt.Errorf("api %s not found", apiID)
	}
	delete(s.apis, apiID)
	s.persistLocked()
	return nil
}

func (s *Store) CreateIntegration(apiID string, integration *types.APIGatewayIntegration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("api %s not found", apiID)
	}
	if _, exists := rec.integrations[integration.IntegrationID]; exists {
		return fmt.Errorf("integration %s already exists", integration.IntegrationID)
	}
	rec.integrations[integration.IntegrationID] = cloneIntegration(integration)
	s.persistLocked()
	return nil
}

func (s *Store) ListIntegrations(apiID string) ([]*types.APIGatewayIntegration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("api %s not found", apiID)
	}

	result := make([]*types.APIGatewayIntegration, 0, len(rec.integrations))
	for _, integration := range rec.integrations {
		result = append(result, cloneIntegration(integration))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].IntegrationID < result[j].IntegrationID })
	return result, nil
}

func (s *Store) GetIntegration(apiID, integrationID string) (*types.APIGatewayIntegration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("api %s not found", apiID)
	}
	integration, ok := rec.integrations[integrationID]
	if !ok {
		return nil, fmt.Errorf("integration %s not found", integrationID)
	}
	return cloneIntegration(integration), nil
}

func (s *Store) SaveIntegration(apiID string, integration *types.APIGatewayIntegration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("api %s not found", apiID)
	}
	if _, exists := rec.integrations[integration.IntegrationID]; !exists {
		return fmt.Errorf("integration %s not found", integration.IntegrationID)
	}
	rec.integrations[integration.IntegrationID] = cloneIntegration(integration)
	s.persistLocked()
	return nil
}

func (s *Store) DeleteIntegration(apiID, integrationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("api %s not found", apiID)
	}
	if _, exists := rec.integrations[integrationID]; !exists {
		return fmt.Errorf("integration %s not found", integrationID)
	}
	delete(rec.integrations, integrationID)
	s.persistLocked()
	return nil
}

func (s *Store) CreateRoute(apiID string, route *types.APIGatewayRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("api %s not found", apiID)
	}
	if _, exists := rec.routes[route.RouteID]; exists {
		return fmt.Errorf("route %s already exists", route.RouteID)
	}
	rec.routes[route.RouteID] = cloneRoute(route)
	s.persistLocked()
	return nil
}

func (s *Store) ListRoutes(apiID string) ([]*types.APIGatewayRoute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("api %s not found", apiID)
	}

	result := make([]*types.APIGatewayRoute, 0, len(rec.routes))
	for _, route := range rec.routes {
		result = append(result, cloneRoute(route))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RouteKey < result[j].RouteKey })
	return result, nil
}

func (s *Store) GetRoute(apiID, routeID string) (*types.APIGatewayRoute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("api %s not found", apiID)
	}
	route, ok := rec.routes[routeID]
	if !ok {
		return nil, fmt.Errorf("route %s not found", routeID)
	}
	return cloneRoute(route), nil
}

func (s *Store) SaveRoute(apiID string, route *types.APIGatewayRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("api %s not found", apiID)
	}
	if _, exists := rec.routes[route.RouteID]; !exists {
		return fmt.Errorf("route %s not found", route.RouteID)
	}
	rec.routes[route.RouteID] = cloneRoute(route)
	s.persistLocked()
	return nil
}

func (s *Store) DeleteRoute(apiID, routeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("api %s not found", apiID)
	}
	if _, exists := rec.routes[routeID]; !exists {
		return fmt.Errorf("route %s not found", routeID)
	}
	delete(rec.routes, routeID)
	s.persistLocked()
	return nil
}

func (s *Store) ListStages(apiID string) ([]*types.APIGatewayStage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("api %s not found", apiID)
	}

	result := make([]*types.APIGatewayStage, 0, len(rec.stages))
	for _, stage := range rec.stages {
		result = append(result, cloneStage(stage))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StageName < result[j].StageName })
	return result, nil
}

func (s *Store) GetStage(apiID, stageName string) (*types.APIGatewayStage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return nil, fmt.Errorf("api %s not found", apiID)
	}
	stage, ok := rec.stages[stageName]
	if !ok {
		return nil, fmt.Errorf("stage %s not found", stageName)
	}
	return cloneStage(stage), nil
}

func (s *Store) SaveStage(apiID string, stage *types.APIGatewayStage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.apis[apiID]
	if !ok {
		return fmt.Errorf("api %s not found", apiID)
	}
	if _, exists := rec.stages[stage.StageName]; !exists {
		return fmt.Errorf("stage %s not found", stage.StageName)
	}
	rec.stages[stage.StageName] = cloneStage(stage)
	s.persistLocked()
	return nil
}

func cloneAPI(in *types.APIGatewayAPI) *types.APIGatewayAPI {
	if in == nil {
		return nil
	}
	out := *in
	if in.Tags != nil {
		out.Tags = make(map[string]string, len(in.Tags))
		for k, v := range in.Tags {
			out.Tags[k] = v
		}
	}
	return &out
}

func cloneIntegration(in *types.APIGatewayIntegration) *types.APIGatewayIntegration {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneRoute(in *types.APIGatewayRoute) *types.APIGatewayRoute {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneStage(in *types.APIGatewayStage) *types.APIGatewayStage {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func (s *Store) persistLocked() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	apiIDs := make([]string, 0, len(s.apis))
	for apiID := range s.apis {
		apiIDs = append(apiIDs, apiID)
	}
	sort.Strings(apiIDs)

	snapshot := struct {
		APIs []apiSnapshot `json:"apis"`
	}{
		APIs: make([]apiSnapshot, 0, len(apiIDs)),
	}

	for _, apiID := range apiIDs {
		rec := s.apis[apiID]
		item := apiSnapshot{
			API:          cloneAPI(rec.api),
			Integrations: make([]*types.APIGatewayIntegration, 0, len(rec.integrations)),
			Routes:       make([]*types.APIGatewayRoute, 0, len(rec.routes)),
			Stages:       make([]*types.APIGatewayStage, 0, len(rec.stages)),
		}

		integrationIDs := make([]string, 0, len(rec.integrations))
		for id := range rec.integrations {
			integrationIDs = append(integrationIDs, id)
		}
		sort.Strings(integrationIDs)
		for _, id := range integrationIDs {
			item.Integrations = append(item.Integrations, cloneIntegration(rec.integrations[id]))
		}

		routeIDs := make([]string, 0, len(rec.routes))
		for id := range rec.routes {
			routeIDs = append(routeIDs, id)
		}
		sort.Strings(routeIDs)
		for _, id := range routeIDs {
			item.Routes = append(item.Routes, cloneRoute(rec.routes[id]))
		}

		stageNames := make([]string, 0, len(rec.stages))
		for name := range rec.stages {
			stageNames = append(stageNames, name)
		}
		sort.Strings(stageNames)
		for _, name := range stageNames {
			item.Stages = append(item.Stages, cloneStage(rec.stages[name]))
		}

		snapshot.APIs = append(snapshot.APIs, item)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}

	if err := os.MkdirAll(filepath.Dir(s.cfg.APIGatewayStatePath()), 0755); err != nil {
		return
	}

	tmpPath := s.cfg.APIGatewayStatePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return
	}
	_ = os.Rename(tmpPath, s.cfg.APIGatewayStatePath())
}
