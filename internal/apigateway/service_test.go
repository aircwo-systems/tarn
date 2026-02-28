package apigateway

import (
	"fmt"
	"testing"

	"github.com/openstack-project/openstack/internal/config"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/pkg/types"
)

func TestCreateAPICreatesDefaultStage(t *testing.T) {
	svc := newTestService(t)

	api, err := svc.CreateAPI("orders", "orders api", "HTTP", "", map[string]string{"feature": "r10"})
	if err != nil {
		t.Fatalf("CreateAPI: %v", err)
	}
	if api.APIID == "" {
		t.Fatalf("expected api id")
	}
	if api.APIEndpoint == "" {
		t.Fatalf("expected api endpoint")
	}
	if api.Tags["feature"] != "r10" {
		t.Fatalf("expected api tag feature=r10, got %v", api.Tags)
	}

	stages, err := svc.ListStages(api.APIID)
	if err != nil {
		t.Fatalf("ListStages: %v", err)
	}
	if len(stages) != 1 {
		t.Fatalf("stages len = %d, want 1", len(stages))
	}
	if stages[0].StageName != "$default" {
		t.Fatalf("stage = %q, want $default", stages[0].StageName)
	}
	if !stages[0].AutoDeploy {
		t.Fatalf("expected auto deploy true")
	}
}

func TestIntegrationLifecycle(t *testing.T) {
	svc := newTestService(t)
	api, err := svc.CreateAPI("orders", "", "HTTP", "", nil)
	if err != nil {
		t.Fatalf("CreateAPI: %v", err)
	}

	lambdaArn := fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", svc.cfg.Region, svc.cfg.AccountID, "orders-handler")
	integration, err := svc.CreateIntegration(api.APIID, IntegrationCreateInput{
		IntegrationType: integrationTypeAWSProxy,
		IntegrationURI:  lambdaArn,
	})
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	if integration.LambdaFunctionName != "orders-handler" {
		t.Fatalf("function name = %q, want %q", integration.LambdaFunctionName, "orders-handler")
	}

	updatedURI := fmt.Sprintf("arn:aws:apigateway:%s:lambda:path/2015-03-31/functions/%s/invocations", svc.cfg.Region, lambdaArn)
	updated, err := svc.UpdateIntegration(api.APIID, integration.IntegrationID, IntegrationUpdateInput{
		IntegrationURI: &updatedURI,
	})
	if err != nil {
		t.Fatalf("UpdateIntegration: %v", err)
	}
	if updated.IntegrationURI != updatedURI {
		t.Fatalf("uri = %q, want %q", updated.IntegrationURI, updatedURI)
	}

	route, err := svc.CreateRoute(api.APIID, RouteCreateInput{RouteKey: "GET /orders/{id}", Target: "integrations/" + integration.IntegrationID})
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	if _, err := svc.GetRoute(api.APIID, route.RouteID); err != nil {
		t.Fatalf("GetRoute: %v", err)
	}

	if err := svc.DeleteIntegration(api.APIID, integration.IntegrationID); err == nil {
		t.Fatalf("expected delete integration to fail when route still references it")
	}

	if err := svc.DeleteRoute(api.APIID, route.RouteID); err != nil {
		t.Fatalf("DeleteRoute: %v", err)
	}
	if err := svc.DeleteIntegration(api.APIID, integration.IntegrationID); err != nil {
		t.Fatalf("DeleteIntegration: %v", err)
	}
}

func TestSelectRoutePrecedence(t *testing.T) {
	routes := []*types.APIGatewayRoute{
		{RouteKey: "$default", Target: "integrations/default"},
		{RouteKey: "ANY /orders/{id}", Target: "integrations/any-templated"},
		{RouteKey: "GET /orders/{id}", Target: "integrations/method-templated"},
		{RouteKey: "ANY /orders/list", Target: "integrations/any-exact"},
		{RouteKey: "GET /orders/list", Target: "integrations/method-exact"},
	}

	match, _, err := selectRoute(routes, "GET", "/orders/list")
	if err != nil {
		t.Fatalf("selectRoute exact: %v", err)
	}
	if match == nil || match.Target != "integrations/method-exact" {
		t.Fatalf("expected method exact route, got %+v", match)
	}

	match, _, err = selectRoute(routes, "POST", "/orders/list")
	if err != nil {
		t.Fatalf("selectRoute any exact: %v", err)
	}
	if match == nil || match.Target != "integrations/any-exact" {
		t.Fatalf("expected any exact route, got %+v", match)
	}

	match, params, err := selectRoute(routes, "GET", "/orders/123")
	if err != nil {
		t.Fatalf("selectRoute method templated: %v", err)
	}
	if match == nil || match.Target != "integrations/method-templated" {
		t.Fatalf("expected method templated route, got %+v", match)
	}
	if params["id"] != "123" {
		t.Fatalf("path param id = %q, want %q", params["id"], "123")
	}

	match, _, err = selectRoute(routes, "DELETE", "/unknown")
	if err != nil {
		t.Fatalf("selectRoute default: %v", err)
	}
	if match == nil || match.Target != "integrations/default" {
		t.Fatalf("expected default route, got %+v", match)
	}
}

func TestParseLambdaIntegrationURI(t *testing.T) {
	lambdaArn := "arn:aws:lambda:us-east-1:000000000000:function:orders-handler"
	arn, name, err := parseLambdaIntegrationURI(lambdaArn)
	if err != nil {
		t.Fatalf("parse lambda arn: %v", err)
	}
	if arn != lambdaArn || name != "orders-handler" {
		t.Fatalf("unexpected parse result: arn=%q name=%q", arn, name)
	}

	apiGatewayURI := "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/" + lambdaArn + "/invocations"
	arn, name, err = parseLambdaIntegrationURI(apiGatewayURI)
	if err != nil {
		t.Fatalf("parse apigateway uri: %v", err)
	}
	if arn != lambdaArn || name != "orders-handler" {
		t.Fatalf("unexpected parse result: arn=%q name=%q", arn, name)
	}

	if _, _, err := parseLambdaIntegrationURI("https://example.com"); err == nil {
		t.Fatalf("expected unsupported uri error")
	}
}

func TestAPIGatewayStatePersistsToDisk(t *testing.T) {
	cfg := config.Default()
	cfg.Host = "127.0.0.1"
	cfg.Port = 4566
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true

	store := lambdasvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	lambdaSvc := lambdasvc.NewService(cfg, store, nil, nil, nil)
	if err := store.SaveFunction(&types.FunctionConfig{
		FunctionName: "orders-handler",
		FunctionArn:  fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", cfg.Region, cfg.AccountID, "orders-handler"),
		Runtime:      types.RuntimeNodeJS20,
		Handler:      "index.handler",
		Role:         "arn:aws:iam::000000000000:role/lambda-role",
		State:        types.FunctionStateActive,
	}); err != nil {
		t.Fatalf("save function: %v", err)
	}

	svc := NewService(cfg, lambdaSvc)
	if err := svc.Init(); err != nil {
		t.Fatalf("init apigateway: %v", err)
	}

	api, err := svc.CreateAPI("persisted-api", "api", "HTTP", "", nil)
	if err != nil {
		t.Fatalf("create api: %v", err)
	}
	lambdaArn := fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", cfg.Region, cfg.AccountID, "orders-handler")
	integration, err := svc.CreateIntegration(api.APIID, IntegrationCreateInput{IntegrationURI: lambdaArn})
	if err != nil {
		t.Fatalf("create integration: %v", err)
	}
	if _, err := svc.CreateRoute(api.APIID, RouteCreateInput{RouteKey: "GET /orders/{id}", Target: "integrations/" + integration.IntegrationID}); err != nil {
		t.Fatalf("create route: %v", err)
	}

	reloaded := NewService(cfg, lambdaSvc)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("reload apigateway: %v", err)
	}

	apis := reloaded.ListAPIs()
	if len(apis) != 1 || apis[0].Name != "persisted-api" {
		t.Fatalf("unexpected apis after reload: %+v", apis)
	}
	routes, err := reloaded.ListRoutes(api.APIID)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 || routes[0].RouteKey != "GET /orders/{id}" {
		t.Fatalf("unexpected routes after reload: %+v", routes)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()

	cfg := config.Default()
	cfg.Host = "127.0.0.1"
	cfg.Port = 4566
	cfg.DataDir = t.TempDir()

	store := lambdasvc.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	lambdaSvc := lambdasvc.NewService(cfg, store, nil, nil, nil)

	// Pre-create functions used by integration tests.
	for _, name := range []string{"orders-handler"} {
		if err := store.SaveFunction(&types.FunctionConfig{
			FunctionName: name,
			FunctionArn:  fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", cfg.Region, cfg.AccountID, name),
			Runtime:      types.RuntimeNodeJS20,
			Handler:      "index.handler",
			Role:         "arn:aws:iam::000000000000:role/lambda-role",
			State:        types.FunctionStateActive,
		}); err != nil {
			t.Fatalf("save function %s: %v", name, err)
		}
	}

	return NewService(cfg, lambdaSvc)
}
