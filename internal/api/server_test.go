package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openstack-project/openstack/internal/apigateway"
	"github.com/openstack-project/openstack/internal/apigatewayv1"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/internal/eventsource"
	"github.com/openstack-project/openstack/internal/infrastructure"
	"github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/internal/logs"
	s3store "github.com/openstack-project/openstack/internal/s3"
	"github.com/openstack-project/openstack/internal/secrets"
	"github.com/openstack-project/openstack/internal/sqs"
	"github.com/openstack-project/openstack/pkg/types"
)

func TestNewServerRegistersRoutes(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := lambda.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	lambdaSvc := lambda.NewService(cfg, store, nil, nil, nil)
	gatewaySvc := apigateway.NewService(cfg, lambdaSvc, nil)
	gatewayV1Svc := apigatewayv1.NewService(cfg, lambdaSvc, nil)
	logsSvc := logs.NewService(cfg)
	sqsSvc := sqs.NewService(cfg)
	secretsSvc := secrets.NewService(cfg)

	s3Svc := s3store.NewService(cfg)
	infraSvc := infrastructure.NewService("", false)
	esmStore := eventsource.NewStore(cfg)
	esmSvc := eventsource.NewService(cfg, esmStore, nil, nil)
	s := NewServer(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, nil, nil)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}

	// start a test HTTP server using the same mux as NewServer
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	ts := httptest.NewServer(s.withLogging(mux))
	defer ts.Close()

	// create a lambda function via API so we can exercise both GET endpoints
	createBody := []byte(`{"FunctionName":"foo","Runtime":"nodejs20.x","Handler":"index.handler","Role":"arn:aws:iam::000000000000:role/test","Code":{"ZipFile":""}}`)
	resp, err := http.Post(ts.URL+"/2015-03-31/functions", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create function request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create function status=%d body=%s", resp.StatusCode, string(body))
	}

	// GET wrapper should include Configuration wrapper
	resp, err = http.Get(ts.URL + "/2015-03-31/functions/foo")
	if err != nil {
		t.Fatalf("wrapper GET failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wrapper GET status=%d", resp.StatusCode)
	}
	var wrapper struct {
		Configuration types.FunctionConfig `json:"Configuration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		t.Fatalf("decode wrapper response: %v", err)
	}
	if wrapper.Configuration.FunctionName != "foo" {
		t.Fatalf("unexpected wrapper config: %+v", wrapper.Configuration)
	}

	// GET configuration path should return the object directly (no wrapper)
	resp, err = http.Get(ts.URL + "/2015-03-31/functions/foo/configuration")
	if err != nil {
		t.Fatalf("configuration GET failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("configuration GET status=%d", resp.StatusCode)
	}
	var cfg1 types.FunctionConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg1); err != nil {
		t.Fatalf("decode configuration response: %v", err)
	}
	if cfg1.FunctionName != "foo" {
		t.Fatalf("unexpected config response: %+v", cfg1)
	}

	// verify state progression via the API: first fetch should be pending,
	// second fetch should be active
	resp, err = http.Get(ts.URL + "/2015-03-31/functions/foo/configuration")
	if err != nil {
		t.Fatalf("second configuration GET failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second configuration GET status=%d", resp.StatusCode)
	}
	var cfg2 types.FunctionConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg2); err != nil {
		t.Fatalf("decode second configuration response: %v", err)
	}
	if cfg2.State != types.FunctionStateActive {
		t.Fatalf("expected state active after second fetch, got %s", cfg2.State)
	}
}
