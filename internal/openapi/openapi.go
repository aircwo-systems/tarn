// Package openapi generates OpenAPI 3.1 documents from Tarn's view of an
// emulated API Gateway. The document is the canonical API description that
// Swagger tooling, Postman sync, and AWS export fidelity are built on.
package openapi

import (
	"encoding/json"
	"maps"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// API is the gateway-neutral input to Build.
type API struct {
	ID          string
	Name        string
	Description string
	Version     string // "v1" (REST) or "v2" (HTTP)
	Stage       string
	InvokeURL   string
	Routes      []Route
}

// Route is one method+path pair exposed by the gateway.
type Route struct {
	RouteKey          string // "POST /orders/{id}", "ANY /x", "$default"
	Method            string
	Path              string
	IntegrationType   string
	IntegrationTarget string          // "lambda:fn", "sqs:queue"
	Params            []Param         // client-facing request parameters
	RequestExample    json.RawMessage // sample body, used for example + inferred schema
}

// Param is a client-facing request parameter.
type Param struct {
	Name     string
	In       string // "header", "query", "path"
	Required bool
}

// Document is an OpenAPI 3.1 document. Maps marshal with sorted keys, so
// output is deterministic for identical input.
type Document struct {
	OpenAPI    string                    `json:"openapi"`
	Info       Info                      `json:"info"`
	Servers    []Server                  `json:"servers,omitempty"`
	Paths      map[string]map[string]any `json:"paths"`
	Extensions map[string]any            `json:"-"`
}

type Info struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// MarshalJSON inlines x- extensions at the document root.
func (d Document) MarshalJSON() ([]byte, error) {
	type plain Document
	base, err := json.Marshal(plain(d))
	if err != nil || len(d.Extensions) == 0 {
		return base, err
	}
	var merged map[string]any
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	maps.Copy(merged, d.Extensions)
	return json.Marshal(merged)
}

// anyMethodKey is the AWS extension API Gateway uses for ANY routes.
const anyMethodKey = "x-amazon-apigateway-any-method"

var pathParamRe = regexp.MustCompile(`\{([A-Za-z0-9_]+)(\+?)\}`)

// Build converts an API into an OpenAPI 3.1 document.
func Build(api API) Document {
	doc := Document{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:       api.Name,
			Description: api.Description,
			Version:     firstNonEmpty(api.Stage, "1.0.0"),
		},
		Paths: map[string]map[string]any{},
		Extensions: map[string]any{
			"x-tarn-api-id":  api.ID,
			"x-tarn-version": api.Version,
		},
	}
	if u := normalizeServerURL(api.InvokeURL); u != "" {
		doc.Servers = []Server{{URL: u, Description: "Tarn local (" + firstNonEmpty(api.Stage, "default") + ")"}}
	}

	routes := append([]Route(nil), api.Routes...)
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].RouteKey < routes[j].RouteKey })

	usedIDs := map[string]int{}
	for _, rt := range routes {
		method, path := routeMethodPath(rt)
		if path == "" {
			continue
		}
		item := doc.Paths[path]
		if item == nil {
			item = map[string]any{}
			doc.Paths[path] = item
		}
		key := strings.ToLower(method)
		if method == "ANY" {
			key = anyMethodKey
		}
		if _, dup := item[key]; dup {
			continue
		}
		item[key] = buildOperation(rt, method, path, usedIDs)
	}
	return doc
}

func buildOperation(rt Route, method, path string, usedIDs map[string]int) map[string]any {
	op := map[string]any{
		"operationId":      uniqueID(operationID(method, path), usedIDs),
		"summary":          firstNonEmpty(rt.RouteKey, method+" "+path),
		"x-tarn-route-key": rt.RouteKey,
		"responses": map[string]any{
			"default": map[string]any{"description": "Response from " + firstNonEmpty(rt.IntegrationTarget, "integration")},
		},
	}
	if rt.IntegrationType != "" || rt.IntegrationTarget != "" {
		integ := map[string]any{}
		if rt.IntegrationType != "" {
			integ["type"] = rt.IntegrationType
		}
		if rt.IntegrationTarget != "" {
			integ["target"] = rt.IntegrationTarget
		}
		op["x-tarn-integration"] = integ
	}
	if kind, _, ok := strings.Cut(rt.IntegrationTarget, ":"); ok {
		op["tags"] = []string{kind}
	}

	if params := buildParams(path, rt.Params); len(params) > 0 {
		op["parameters"] = params
	}
	if body := buildRequestBody(method, rt.RequestExample); body != nil {
		op["requestBody"] = body
	}
	return op
}

func buildParams(path string, declared []Param) []map[string]any {
	seen := map[string]bool{}
	var out []map[string]any
	add := func(p map[string]any) {
		k := p["in"].(string) + ":" + strings.ToLower(p["name"].(string))
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, p)
	}
	for _, m := range pathParamRe.FindAllStringSubmatch(path, -1) {
		p := map[string]any{"name": m[1], "in": "path", "required": true, "schema": map[string]any{"type": "string"}}
		add(p)
	}
	sorted := append([]Param(nil), declared...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].In != sorted[j].In {
			return sorted[i].In < sorted[j].In
		}
		return sorted[i].Name < sorted[j].Name
	})
	for _, p := range sorted {
		if p.Name == "" || p.In == "" {
			continue
		}
		add(map[string]any{
			"name":     p.Name,
			"in":       p.In,
			"required": p.Required || p.In == "path",
			"schema":   map[string]any{"type": "string"},
		})
	}
	return out
}

func buildRequestBody(method string, example json.RawMessage) map[string]any {
	switch method {
	case "POST", "PUT", "PATCH", "ANY":
	default:
		return nil
	}
	media := map[string]any{"schema": map[string]any{"type": "object"}}
	if len(example) > 0 {
		var v any
		if json.Unmarshal(example, &v) == nil {
			media["schema"] = InferSchema(v)
			media["example"] = v
		}
	}
	return map[string]any{
		"required": method != "ANY",
		"content":  map[string]any{"application/json": media},
	}
}

// InferSchema derives a JSON Schema from a decoded JSON example. Every key
// present in an object example is marked required; arrays take the schema of
// their first element.
func InferSchema(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		props := map[string]any{}
		required := make([]string, 0, len(t))
		for k, child := range t {
			props[k] = InferSchema(child)
			required = append(required, k)
		}
		sort.Strings(required)
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	case []any:
		s := map[string]any{"type": "array"}
		if len(t) > 0 {
			s["items"] = InferSchema(t[0])
		}
		return s
	case string:
		return map[string]any{"type": "string"}
	case float64:
		if t == float64(int64(t)) {
			return map[string]any{"type": "integer"}
		}
		return map[string]any{"type": "number"}
	case bool:
		return map[string]any{"type": "boolean"}
	default:
		return map[string]any{"type": "null"}
	}
}

// routeMethodPath returns the OpenAPI method and path for a route, with greedy
// "{proxy+}" segments rewritten to plain "{proxy}" template parameters.
func routeMethodPath(rt Route) (string, string) {
	method, path := strings.ToUpper(rt.Method), rt.Path
	if rt.RouteKey == "$default" || method == "$DEFAULT" {
		return "ANY", "/$default"
	}
	if method == "" || path == "" {
		if m, p, ok := strings.Cut(rt.RouteKey, " "); ok {
			method, path = strings.ToUpper(m), p
		}
	}
	if method == "" || path == "" {
		return "", ""
	}
	return method, pathParamRe.ReplaceAllString(path, "{$1}")
}

var nonIdent = regexp.MustCompile(`[^A-Za-z0-9]+`)

func operationID(method, path string) string {
	parts := []string{strings.ToLower(method)}
	for _, seg := range strings.Split(path, "/") {
		seg = strings.Trim(seg, "{}$")
		for _, w := range nonIdent.Split(seg, -1) {
			if w != "" {
				parts = append(parts, strings.ToUpper(w[:1])+w[1:])
			}
		}
	}
	return strings.Join(parts, "")
}

func uniqueID(id string, used map[string]int) string {
	used[id]++
	if n := used[id]; n > 1 {
		return id + "_" + strconv.Itoa(n)
	}
	return id
}

func normalizeServerURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimSuffix(raw, "/")
	}
	switch u.Hostname() {
	case "0.0.0.0", "::":
		if port := u.Port(); port != "" {
			u.Host = "127.0.0.1:" + port
		} else {
			u.Host = "127.0.0.1"
		}
	}
	return strings.TrimSuffix(u.String(), "/")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
