package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aircwo-systems/tarn/internal/apigateway"
	"github.com/aircwo-systems/tarn/internal/apigatewayv1"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/eventbridge"
	"github.com/aircwo-systems/tarn/internal/eventsource"
	"github.com/aircwo-systems/tarn/internal/infrastructure"
	"github.com/aircwo-systems/tarn/internal/lambda"
	"github.com/aircwo-systems/tarn/internal/logs"
	s3store "github.com/aircwo-systems/tarn/internal/s3"
	"github.com/aircwo-systems/tarn/internal/secrets"
	"github.com/aircwo-systems/tarn/internal/sns"
	"github.com/aircwo-systems/tarn/internal/sqs"
	"github.com/aircwo-systems/tarn/pkg/types"
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
	ebStore := eventbridge.NewStore(cfg)
	ebSvc := eventbridge.NewService(cfg, ebStore, lambdaSvc)
	s := NewServer(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, snsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, ebSvc, nil, nil)
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

func TestEventBridgeProtocolDispatchSupportsNonRootPath(t *testing.T) {
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
	ebStore := eventbridge.NewStore(cfg)
	ebSvc := eventbridge.NewService(cfg, ebStore, lambdaSvc)
	if err := ebSvc.Init(); err != nil {
		t.Fatalf("init eventbridge service: %v", err)
	}

	s := NewServer(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, snsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, ebSvc, nil, nil)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	handler := s.withLogging(mux)

	putBody := []byte(`{"Name":"rule-a","ScheduleExpression":"rate(1 minute)","State":"ENABLED"}`)
	putReq := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	putReq.Header.Set("X-Amz-Target", "AWSEvents.PutRule")
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put rule via /events status=%d body=%s", putRec.Code, putRec.Body.String())
	}

	disableBody := []byte(`{"Name":"rule-a","EventBusName":"default"}`)
	disableReq := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(disableBody))
	disableReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	disableReq.Header.Set("X-Amz-Target", "AWSEvents.DisableRule")
	disableRec := httptest.NewRecorder()
	handler.ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable rule via /events status=%d body=%s", disableRec.Code, disableRec.Body.String())
	}

	describeBody := []byte(`{"Name":"rule-a"}`)
	describeReq := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(describeBody))
	describeReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	describeReq.Header.Set("X-Amz-Target", "AWSEvents.DescribeRule")
	describeRec := httptest.NewRecorder()
	handler.ServeHTTP(describeRec, describeReq)
	if describeRec.Code != http.StatusOK {
		t.Fatalf("describe rule via /events status=%d body=%s", describeRec.Code, describeRec.Body.String())
	}

	var describeResp struct {
		State string `json:"State"`
	}
	if err := json.NewDecoder(describeRec.Body).Decode(&describeResp); err != nil {
		t.Fatalf("decode describe response: %v", err)
	}
	if describeResp.State != types.EventBridgeRuleStateDisabled {
		t.Fatalf("expected disabled state, got %q", describeResp.State)
	}
}

func TestEventBridgeProtocolDispatchSupportsTarnPrefixedPath(t *testing.T) {
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
	ebStore := eventbridge.NewStore(cfg)
	ebSvc := eventbridge.NewService(cfg, ebStore, lambdaSvc)
	if err := ebSvc.Init(); err != nil {
		t.Fatalf("init eventbridge service: %v", err)
	}

	s := NewServer(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, snsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, ebSvc, nil, nil)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	handler := s.withLogging(mux)

	putBody := []byte(`{"Name":"rule-b","ScheduleExpression":"rate(1 minute)","State":"ENABLED"}`)
	putReq := httptest.NewRequest(http.MethodPost, "/_tarn", bytes.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	putReq.Header.Set("X-Amz-Target", "AWSEvents.PutRule")
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put rule via /_tarn status=%d body=%s", putRec.Code, putRec.Body.String())
	}

	disableBody := []byte(`{"Name":"rule-b","EventBusName":"default"}`)
	disableReq := httptest.NewRequest(http.MethodPost, "/_tarn", bytes.NewReader(disableBody))
	disableReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	disableReq.Header.Set("X-Amz-Target", "AWSEvents.DisableRule")
	disableRec := httptest.NewRecorder()
	handler.ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable rule via /_tarn status=%d body=%s", disableRec.Code, disableRec.Body.String())
	}
}

func TestEventBridgeProtocolDispatchSupportsTarnEventsPath(t *testing.T) {
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
	ebStore := eventbridge.NewStore(cfg)
	ebSvc := eventbridge.NewService(cfg, ebStore, lambdaSvc)
	if err := ebSvc.Init(); err != nil {
		t.Fatalf("init eventbridge service: %v", err)
	}

	s := NewServer(cfg, gatewaySvc, gatewayV1Svc, lambdaSvc, logsSvc, sqsSvc, snsSvc, secretsSvc, infraSvc, s3Svc, esmSvc, ebSvc, nil, nil)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	handler := s.withLogging(mux)

	putBody := []byte(`{"Name":"rule-c","ScheduleExpression":"rate(1 minute)","State":"ENABLED"}`)
	putReq := httptest.NewRequest(http.MethodPost, "/_tarn/events", bytes.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	putReq.Header.Set("X-Amz-Target", "AWSEvents.PutRule")
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put rule via /_tarn/events status=%d body=%s", putRec.Code, putRec.Body.String())
	}

	disableBody := []byte(`{"Name":"rule-c","EventBusName":"default"}`)
	disableReq := httptest.NewRequest(http.MethodPost, "/_tarn/events", bytes.NewReader(disableBody))
	disableReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	disableReq.Header.Set("X-Amz-Target", "AWSEvents.DisableRule")
	disableRec := httptest.NewRecorder()
	handler.ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable rule via /_tarn/events status=%d body=%s", disableRec.Code, disableRec.Body.String())
	}

	describeBody := []byte(`{"Name":"rule-c"}`)
	describeReq := httptest.NewRequest(http.MethodPost, "/_tarn/events", bytes.NewReader(describeBody))
	describeReq.Header.Set("Content-Type", "application/x-amz-json-1.1")
	describeReq.Header.Set("X-Amz-Target", "AWSEvents.DescribeRule")
	describeRec := httptest.NewRecorder()
	handler.ServeHTTP(describeRec, describeReq)
	if describeRec.Code != http.StatusOK {
		t.Fatalf("describe rule via /_tarn/events status=%d body=%s", describeRec.Code, describeRec.Body.String())
	}

	var describeResp struct {
		State string `json:"State"`
	}
	if err := json.NewDecoder(describeRec.Body).Decode(&describeResp); err != nil {
		t.Fatalf("decode describe response: %v", err)
	}
	if describeResp.State != types.EventBridgeRuleStateDisabled {
		t.Fatalf("expected disabled state, got %q", describeResp.State)
	}
}
