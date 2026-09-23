package apigatewayv1

import (
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestLegacyRestAPIStatusIsAvailable(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	store := NewStore(cfg)
	if err := store.CreateAPI(&types.RestAPI{ID: "legacy-id", Name: "legacy-api"}, nil); err != nil {
		t.Fatal(err)
	}
	api, err := store.GetAPI("legacy-id")
	if err != nil {
		t.Fatal(err)
	}
	if api.APIStatus != "AVAILABLE" {
		t.Fatalf("GetAPI status = %q, want AVAILABLE", api.APIStatus)
	}
}
