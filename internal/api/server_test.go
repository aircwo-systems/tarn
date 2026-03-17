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
	"github.com/openstack-project/openstack/internal/sns"
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
	snsSvc := sns.NewService(cfg, sqsSvc, lambdaSvc)
	secretsSvc := secrets.NewService(cfg)

	s3Svc := s3store.NewService(cfg)
	infraSvc := infrastructure.NewService("", false)
	esmStore := eventsource.NewStore(cfg)
	esmSvc := eventsource.NewService(cfg, esmStore, nil, nil)
	s := NewServer(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, snsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, nil, nil)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}

	// start a test HTTP server using the same mux as NewServer
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	handler := s.withLogging(mux)

	// create a lambda function via API so we can exercise both GET endpoints
	createBody := []byte(`{"FunctionName":"foo","Runtime":"nodejs20.x","Handler":"index.handler","Role":"arn:aws:iam::000000000000:role/test","Code":{"ZipFile":""}}`)
	createReq := httptest.NewRequest(http.MethodPost, "/2015-03-31/functions", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	createResp := createRec.Result()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create function status=%d body=%s", createResp.StatusCode, string(body))
	}

	// GET wrapper should include Configuration wrapper
	wrapperReq := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/foo", nil)
	wrapperRec := httptest.NewRecorder()
	handler.ServeHTTP(wrapperRec, wrapperReq)
	wrapperResp := wrapperRec.Result()
	if wrapperResp.StatusCode != http.StatusOK {
		t.Fatalf("wrapper GET status=%d", wrapperResp.StatusCode)
	}
	var wrapper struct {
		Configuration types.FunctionConfig `json:"Configuration"`
	}
	if err := json.NewDecoder(wrapperResp.Body).Decode(&wrapper); err != nil {
		t.Fatalf("decode wrapper response: %v", err)
	}
	if wrapper.Configuration.FunctionName != "foo" {
		t.Fatalf("unexpected wrapper config: %+v", wrapper.Configuration)
	}

	// GET configuration path should return the object directly (no wrapper)
	cfgReq := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/foo/configuration", nil)
	cfgRec := httptest.NewRecorder()
	handler.ServeHTTP(cfgRec, cfgReq)
	cfgResp := cfgRec.Result()
	if cfgResp.StatusCode != http.StatusOK {
		t.Fatalf("configuration GET status=%d", cfgResp.StatusCode)
	}
	var cfg1 types.FunctionConfig
	if err := json.NewDecoder(cfgResp.Body).Decode(&cfg1); err != nil {
		t.Fatalf("decode configuration response: %v", err)
	}
	if cfg1.FunctionName != "foo" {
		t.Fatalf("unexpected config response: %+v", cfg1)
	}

	// verify state progression via the API: first fetch should be pending,
	// second fetch should be active
	cfgReq2 := httptest.NewRequest(http.MethodGet, "/2015-03-31/functions/foo/configuration", nil)
	cfgRec2 := httptest.NewRecorder()
	handler.ServeHTTP(cfgRec2, cfgReq2)
	cfgResp2 := cfgRec2.Result()
	if cfgResp2.StatusCode != http.StatusOK {
		t.Fatalf("second configuration GET status=%d", cfgResp2.StatusCode)
	}
	var cfg2 types.FunctionConfig
	if err := json.NewDecoder(cfgResp2.Body).Decode(&cfg2); err != nil {
		t.Fatalf("decode second configuration response: %v", err)
	}
	if cfg2.State != types.FunctionStateActive {
		t.Fatalf("expected state active after second fetch, got %s", cfg2.State)
	}
}
