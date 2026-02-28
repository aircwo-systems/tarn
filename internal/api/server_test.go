package api

import (
	"testing"

	"github.com/openstack-project/openstack/internal/apigateway"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/internal/infrastructure"
	"github.com/openstack-project/openstack/internal/lambda"
	"github.com/openstack-project/openstack/internal/logs"
	"github.com/openstack-project/openstack/internal/secrets"
	"github.com/openstack-project/openstack/internal/sqs"
)

func TestNewServerRegistersRoutes(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	store := lambda.NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init lambda store: %v", err)
	}
	lambdaSvc := lambda.NewService(cfg, store, nil, nil, nil)
	gatewaySvc := apigateway.NewService(cfg, lambdaSvc)
	logsSvc := logs.NewService(cfg)
	sqsSvc := sqs.NewService(cfg)
	secretsSvc := secrets.NewService(cfg)

	infraSvc := infrastructure.NewService("", false)
	s := NewServer(cfg, gatewaySvc, lambdaSvc, logsSvc, sqsSvc, secretsSvc, infraSvc)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
}
