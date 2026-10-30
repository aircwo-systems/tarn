package dynamodb

import (
	"strconv"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const ttlTestAttr = "expiresAt"

func ttlTestItem(sk string, ttlValue map[string]any) map[string]any {
	item := map[string]any{
		"pk":      map[string]any{"S": "acct#1"},
		"sk":      map[string]any{"S": sk},
		"payload": map[string]any{"S": "data"},
	}
	if ttlValue != nil {
		item[ttlTestAttr] = ttlValue
	}
	return item
}

func ttlNumber(epoch int64) map[string]any {
	return map[string]any{"N": strconv.FormatInt(epoch, 10)}
}

func TestTTLSweepExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	past := now.Unix() - 60
	future := now.Unix() + 3600

	tests := []struct {
		name        string
		enableTTL   bool
		items       []map[string]any
		wantDeleted int
		wantKept    []string
	}{
		{
			name:        "expired number is deleted",
			enableTTL:   true,
			items:       []map[string]any{ttlTestItem("expired", ttlNumber(past))},
			wantDeleted: 1,
			wantKept:    nil,
		},
		{
			name:        "number equal to now is deleted",
			enableTTL:   true,
			items:       []map[string]any{ttlTestItem("boundary", ttlNumber(now.Unix()))},
			wantDeleted: 1,
			wantKept:    nil,
		},
		{
			name:        "future number is kept",
			enableTTL:   true,
			items:       []map[string]any{ttlTestItem("future", ttlNumber(future))},
			wantDeleted: 0,
			wantKept:    []string{"future"},
		},
		{
			name:      "string typed ttl attribute is kept",
			enableTTL: true,
			items: []map[string]any{
				ttlTestItem("string", map[string]any{"S": strconv.FormatInt(past, 10)}),
			},
			wantDeleted: 0,
			wantKept:    []string{"string"},
		},
		{
			name:        "missing ttl attribute is kept",
			enableTTL:   true,
			items:       []map[string]any{ttlTestItem("missing", nil)},
			wantDeleted: 0,
			wantKept:    []string{"missing"},
		},
		{
			name:      "ttl disabled keeps expired items",
			enableTTL: false,
			items: []map[string]any{
				ttlTestItem("expired", ttlNumber(past)),
				ttlTestItem("future", ttlNumber(future)),
			},
			wantDeleted: 0,
			wantKept:    []string{"expired", "future"},
		},
		{
			name:      "mixed items only delete expired numbers",
			enableTTL: true,
			items: []map[string]any{
				ttlTestItem("expired", ttlNumber(past)),
				ttlTestItem("future", ttlNumber(future)),
				ttlTestItem("string", map[string]any{"S": "1"}),
				ttlTestItem("missing", nil),
			},
			wantDeleted: 1,
			wantKept:    []string{"future", "string", "missing"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.DataDir = t.TempDir()
			cfg.PersistenceEnabled = false

			store := NewStore(cfg)
			if _, err := store.CreateTable(testTableDefinition()); err != nil {
				t.Fatalf("create table: %v", err)
			}
			if tc.enableTTL {
				if _, err := store.UpdateTimeToLive("orders", ttlTestAttr, true); err != nil {
					t.Fatalf("enable ttl: %v", err)
				}
			}
			for _, item := range tc.items {
				if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: item}); err != nil {
					t.Fatalf("put item: %v", err)
				}
			}

			got := store.sweepExpired(now)
			if got != tc.wantDeleted {
				t.Fatalf("sweepExpired deleted %d, want %d", got, tc.wantDeleted)
			}

			store.mu.RLock()
			state := store.tables["orders"]
			if state == nil {
				store.mu.RUnlock()
				t.Fatal("table state missing")
			}
			for _, sk := range tc.wantKept {
				keyID, err := serializeKey(map[string]any{
					"pk": map[string]any{"S": "acct#1"},
					"sk": map[string]any{"S": sk},
				}, state.Table.KeySchema)
				if err != nil {
					store.mu.RUnlock()
					t.Fatalf("serialize key: %v", err)
				}
				if _, ok := state.Items[keyID]; !ok {
					store.mu.RUnlock()
					t.Fatalf("item %q was removed, want kept", sk)
				}
			}
			if want := int64(len(tc.wantKept)); state.Table.ItemCount != want {
				store.mu.RUnlock()
				t.Fatalf("ItemCount = %d, want %d", state.Table.ItemCount, want)
			}
			if len(state.Items) != len(tc.wantKept) {
				store.mu.RUnlock()
				t.Fatalf("live items = %d, want %d", len(state.Items), len(tc.wantKept))
			}
			removes := ttlRemoveRecords(state)
			store.mu.RUnlock()

			if len(removes) != tc.wantDeleted {
				t.Fatalf("REMOVE stream records = %d, want %d", len(removes), tc.wantDeleted)
			}
			for _, rec := range removes {
				if rec.UserIdentity == nil {
					t.Fatalf("REMOVE record %s has no userIdentity", rec.EventID)
				}
				want := types.StreamUserIdentity{Type: "Service", PrincipalID: "dynamodb.amazonaws.com"}
				if *rec.UserIdentity != want {
					t.Fatalf("userIdentity = %+v, want %+v", *rec.UserIdentity, want)
				}
				if rec.Dynamodb.Keys["pk"] == nil || rec.Dynamodb.Keys["sk"] == nil {
					t.Fatalf("REMOVE record keys = %#v, want pk and sk", rec.Dynamodb.Keys)
				}
			}
		})
	}
}

func TestTTLSweepExpiredNoStreamSkipsRecords(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	def := testTableDefinition()
	def.StreamSpecification = nil

	store := NewStore(cfg)
	if _, err := store.CreateTable(def); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := store.UpdateTimeToLive("orders", ttlTestAttr, true); err != nil {
		t.Fatalf("enable ttl: %v", err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: ttlTestItem("expired", ttlNumber(now.Unix()-1))}); err != nil {
		t.Fatalf("put item: %v", err)
	}

	if got := store.sweepExpired(now); got != 1 {
		t.Fatalf("sweepExpired deleted %d, want 1", got)
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	if n := store.tables["orders"].Table.ItemCount; n != 0 {
		t.Fatalf("ItemCount = %d, want 0", n)
	}
}

func ttlRemoveRecords(state *tableState) []*types.StreamRecord {
	if state.Stream == nil {
		return nil
	}
	var out []*types.StreamRecord
	for _, rec := range state.Stream.Records {
		if rec.EventName == "REMOVE" {
			out = append(out, rec)
		}
	}
	return out
}
