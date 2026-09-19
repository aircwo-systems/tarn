package openapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

func roundTrip(t *testing.T, doc Document) map[string]any {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func dig(t *testing.T, v any, keys ...string) any {
	t.Helper()
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("at %q: not an object: %#v", k, v)
		}
		v = m[k]
	}
	return v
}

func TestBuild_DocumentShape(t *testing.T) {
	doc := roundTrip(t, Build(API{
		ID: "abc123", Name: "orders", Version: "v2", Stage: "$default",
		InvokeURL: "http://0.0.0.0:4566/abc123/",
		Routes:    []Route{{RouteKey: "GET /orders", Method: "GET", Path: "/orders"}},
	}))

	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v", doc["openapi"])
	}
	if doc["x-tarn-api-id"] != "abc123" || doc["x-tarn-version"] != "v2" {
		t.Fatalf("missing root extensions: %v", doc)
	}
	servers := doc["servers"].([]any)
	if got := dig(t, servers[0], "url"); got != "http://127.0.0.1:4566/abc123" {
		t.Fatalf("server url = %v", got)
	}
	if got := dig(t, doc, "paths", "/orders", "get", "operationId"); got != "getOrders" {
		t.Fatalf("operationId = %v", got)
	}
}

func TestBuild_GreedyPathAndParams(t *testing.T) {
	doc := roundTrip(t, Build(API{Routes: []Route{{
		RouteKey: "GET /files/{proxy+}", Method: "GET", Path: "/files/{proxy+}",
		Params: []Param{
			{Name: "X-Api-Key", In: "header", Required: true},
			{Name: "limit", In: "query"},
		},
	}}}))

	params := dig(t, doc, "paths", "/files/{proxy}", "get", "parameters").([]any)
	if len(params) != 3 {
		t.Fatalf("want 3 params, got %d: %v", len(params), params)
	}
	want := [][2]any{{"proxy", "path"}, {"X-Api-Key", "header"}, {"limit", "query"}}
	for i, w := range want {
		if dig(t, params[i], "name") != w[0] || dig(t, params[i], "in") != w[1] {
			t.Fatalf("param %d = %v, want %v", i, params[i], w)
		}
	}
	if dig(t, params[1], "required") != true || dig(t, params[2], "required") != false {
		t.Fatalf("required flags wrong: %v", params)
	}
}

func TestBuild_RequestBodyFromExample(t *testing.T) {
	doc := roundTrip(t, Build(API{Routes: []Route{{
		RouteKey: "POST /orders", Method: "POST", Path: "/orders",
		IntegrationType: "AWS_PROXY", IntegrationTarget: "lambda:create-order",
		RequestExample: json.RawMessage(`{"sku":"A1","qty":2,"price":9.5,"gift":false,"lines":[{"id":"x"}]}`),
	}}}))

	op := dig(t, doc, "paths", "/orders", "post")
	schema := dig(t, op, "requestBody", "content", "application/json", "schema")
	props := dig(t, schema, "properties").(map[string]any)
	types := map[string]string{"sku": "string", "qty": "integer", "price": "number", "gift": "boolean", "lines": "array"}
	for k, want := range types {
		if got := dig(t, props[k], "type"); got != want {
			t.Fatalf("%s type = %v, want %s", k, got, want)
		}
	}
	if got := dig(t, props["lines"], "items", "properties", "id", "type"); got != "string" {
		t.Fatalf("array item schema = %v", got)
	}
	if got := dig(t, op, "x-tarn-integration", "target"); got != "lambda:create-order" {
		t.Fatalf("integration target = %v", got)
	}
	if got := dig(t, op, "tags"); !reflect.DeepEqual(got, []any{"lambda"}) {
		t.Fatalf("tags = %v", got)
	}
}

func TestBuild_AnyAndDefaultRoutes(t *testing.T) {
	doc := roundTrip(t, Build(API{Routes: []Route{
		{RouteKey: "$default"},
		{RouteKey: "ANY /proxy", Method: "ANY", Path: "/proxy"},
	}}))

	for _, p := range []string{"/$default", "/proxy"} {
		if dig(t, doc, "paths", p, anyMethodKey) == nil {
			t.Fatalf("%s missing %s", p, anyMethodKey)
		}
	}
}

func TestBuild_DuplicateOperationIDsAreUnique(t *testing.T) {
	doc := roundTrip(t, Build(API{Routes: []Route{
		{RouteKey: "GET /a-b", Method: "GET", Path: "/a-b"},
		{RouteKey: "GET /a_b", Method: "GET", Path: "/a_b"},
	}}))

	first := dig(t, doc, "paths", "/a-b", "get", "operationId")
	second := dig(t, doc, "paths", "/a_b", "get", "operationId")
	if first == second {
		t.Fatalf("operationIds collide: %v", first)
	}
}

func TestBuild_Deterministic(t *testing.T) {
	api := API{Routes: []Route{
		{RouteKey: "POST /b", Method: "POST", Path: "/b", RequestExample: json.RawMessage(`{"z":1,"a":2}`)},
		{RouteKey: "GET /a", Method: "GET", Path: "/a"},
	}}
	first, _ := json.Marshal(Build(api))
	api.Routes[0], api.Routes[1] = api.Routes[1], api.Routes[0]
	second, _ := json.Marshal(Build(api))
	if string(first) != string(second) {
		t.Fatalf("output depends on route order:\n%s\n%s", first, second)
	}
}
