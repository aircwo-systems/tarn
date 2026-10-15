package apigatewayv1

import (
	"encoding/json"
	"fmt"
	"github.com/aircwo-systems/tarn/pkg/types"
	"maps"
	"sort"
	"strings"
)

type openAPIIntegration struct {
	Type              string            `json:"type"`
	HTTPMethod        string            `json:"httpMethod"`
	URI               string            `json:"uri"`
	Credentials       string            `json:"credentials"`
	RequestParameters map[string]string `json:"requestParameters"`
	RequestTemplates  map[string]string `json:"requestTemplates"`
	Responses         map[string]struct {
		StatusCode         string            `json:"statusCode"`
		ResponseTemplates  map[string]string `json:"responseTemplates"`
		ResponseParameters map[string]string `json:"responseParameters"`
	} `json:"responses"`
}
type openAPIOperation struct {
	Security  []map[string][]string `json:"security"`
	Responses map[string]struct {
		Description string `json:"description"`
	} `json:"responses"`
	Integration *openAPIIntegration `json:"x-amazon-apigateway-integration"`
}

// PutRestAPI imports the supported JSON OpenAPI operations into a staged store.
// A rejected document leaves the existing API and deployments untouched.
func (s *Service) PutRestAPI(id, mode string, body []byte) (*types.RestAPI, error) {
	var spec struct {
		OpenAPI string `json:"openapi"`
		Swagger string `json:"swagger"`
		Info    struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"info"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Security   []map[string][]string                 `json:"security"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type string `json:"type"`
			} `json:"securitySchemes"`
		} `json:"components"`
		SecurityDefinitions map[string]struct {
			Type string `json:"type"`
		} `json:"securityDefinitions"`
	}
	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("invalid OpenAPI JSON: %w", err)
	}
	if spec.OpenAPI == "" && spec.Swagger != "2.0" {
		return nil, fmt.Errorf("an OpenAPI or Swagger version is required")
	}
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "overwrite" {
		return nil, fmt.Errorf("mode must be merge or overwrite")
	}
	s.store.mu.RLock()
	old := s.store.apis[id]
	if old == nil {
		s.store.mu.RUnlock()
		return nil, fmt.Errorf("rest api %s not found", id)
	}
	api := *old.api
	api.Tags = maps.Clone(old.api.Tags)
	staged := newRecord(&api)
	staged.deployments = maps.Clone(old.deployments)
	staged.stages = maps.Clone(old.stages)
	if mode == "merge" {
		staged.resources = maps.Clone(old.resources)
		staged.methods = maps.Clone(old.methods)
		staged.integrations = maps.Clone(old.integrations)
		staged.methodResponses = maps.Clone(old.methodResponses)
		staged.integrationResponses = maps.Clone(old.integrationResponses)
	} else {
		root := *old.resources[api.RootResourceID]
		staged.resources[root.ID] = &root
	}
	s.store.mu.RUnlock()
	scratch := NewService(s.cfg, s.lambda, s.sqsSend)
	scratch.store.apis[id] = staged
	paths := make([]string, 0, len(spec.Paths))
	for path := range spec.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !strings.HasPrefix(path, "/") {
			return nil, fmt.Errorf("OpenAPI paths must start with /")
		}
		resourceID := api.RootResourceID
		for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
			if part == "" {
				continue
			}
			found := ""
			for _, res := range staged.resources {
				if res.ParentID == resourceID && res.PathPart == part {
					found = res.ID
					break
				}
			}
			if found == "" {
				res, err := scratch.CreateResource(id, resourceID, part)
				if err != nil {
					return nil, err
				}
				found = res.ID
			}
			resourceID = found
		}
		for verb, raw := range spec.Paths[path] {
			method := strings.ToUpper(verb)
			switch method {
			case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "X-AMAZON-APIGATEWAY-ANY-METHOD":
			default:
				continue
			}
			if method == "X-AMAZON-APIGATEWAY-ANY-METHOD" {
				method = "ANY"
			}
			var op openAPIOperation
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("invalid operation %s %s: %w", verb, path, err)
			}
			if _, err := scratch.PutMethod(id, resourceID, method, "NONE", nil); err != nil {
				return nil, err
			}
			security := op.Security
			if security == nil {
				security = spec.Security
			}
			required := false
			for _, requirements := range security {
				for name := range requirements {
					if spec.Components.SecuritySchemes[name].Type == "apiKey" || spec.SecurityDefinitions[name].Type == "apiKey" {
						required = true
					} else {
						return nil, fmt.Errorf("unsupported security scheme %q", name)
					}
				}
			}
			m, err := scratch.GetMethod(id, resourceID, method)
			if err != nil {
				return nil, err
			}
			m.APIKeyRequired = required
			if err := scratch.store.PutMethod(id, m); err != nil {
				return nil, err
			}
			for status := range op.Responses {
				if status == "default" {
					continue
				}
				if _, err := scratch.PutMethodResponse(id, resourceID, method, status, nil, nil); err != nil {
					return nil, err
				}
			}
			if op.Integration != nil {
				i := op.Integration
				typ := strings.ToUpper(i.Type)
				if typ != "AWS" && typ != "AWS_PROXY" && typ != "MOCK" {
					return nil, fmt.Errorf("unsupported integration type %q", i.Type)
				}
				integ, err := scratch.PutIntegration(id, resourceID, method, typ, i.HTTPMethod, i.URI, i.RequestParameters, i.RequestTemplates)
				if err != nil {
					return nil, err
				}
				integ.Credentials = i.Credentials
				if err := scratch.store.PutIntegration(id, integ); err != nil {
					return nil, err
				}
				for pattern, response := range i.Responses {
					selection := pattern
					if pattern == "default" {
						selection = ""
					}
					if _, err := scratch.PutIntegrationResponse(id, resourceID, method, response.StatusCode, selection, response.ResponseTemplates, response.ResponseParameters); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	current := s.store.apis[id]
	if current == nil {
		return nil, fmt.Errorf("rest api %s not found", id)
	}
	// Keep deployments/stages changed while the import was parsed.
	staged.deployments = current.deployments
	staged.stages = current.stages
	s.store.apis[id] = staged
	s.store.dirty.Store(true)
	result := api
	return &result, nil
}

// APIKeyAllowed checks the key's enabled flag and its plan's API/stage binding.
func (s *Service) APIKeyAllowed(value, api, stage string) bool {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	for id, key := range s.store.apiKeys {
		if key.Value != value || !key.Enabled {
			continue
		}
		for _, plan := range s.store.usagePlans {
			if _, ok := plan.Keys[id]; !ok {
				continue
			}
			for _, binding := range plan.Plan.APIStages {
				if binding.APIID == api && binding.Stage == stage {
					return true
				}
			}
		}
	}
	return false
}

func (s *Service) PatchRestAPI(id string, ops []PatchOp) (*types.RestAPI, error) {
	api, err := s.GetAPI(id)
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if op.Op != "replace" {
			return nil, fmt.Errorf("unsupported REST API patch operation %q", op.Op)
		}
		switch op.Path {
		case "/name":
			api.Name = op.Value
		case "/description":
			api.Description = op.Value
		case "/endpointConfiguration/types", "/endpointConfiguration/types/0":
			if op.Value != "REGIONAL" && op.Value != "EDGE" && op.Value != "PRIVATE" {
				return nil, fmt.Errorf("invalid endpoint type")
			}
			api.EndpointConfiguration = &types.RestEndpointConfiguration{Types: []string{op.Value}}
		default:
			return nil, fmt.Errorf("unsupported REST API patch path %q", op.Path)
		}
	}
	if err := s.store.SaveAPI(api); err != nil {
		return nil, err
	}
	return s.GetAPI(id)
}
func (s *Service) DeleteDeployment(apiID, id string) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	rec := s.store.apis[apiID]
	if rec == nil || rec.deployments[id] == nil {
		return fmt.Errorf("deployment not found")
	}
	for _, stage := range rec.stages {
		if stage.DeploymentID == id {
			return fmt.Errorf("deployment is referenced by an active stage")
		}
	}
	delete(rec.deployments, id)
	s.store.dirty.Store(true)
	return nil
}
