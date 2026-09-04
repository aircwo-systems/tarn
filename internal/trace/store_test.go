package trace

import (
	"testing"
	"time"
)

// TestFindNearAcrossTimezones guards the started_at storage format.
//
// started_at is TEXT and SQLite compares it lexicographically, so a trace
// written with a "+01:00" offset never matched a UTC query bound even when
// both described the same instant. The UI hits exactly this path: log events
// are serialized as "Z" and handed straight to the trace lookup, so the trace
// panel silently returned nothing on any machine not running UTC.
func TestFindNearAcrossTimezones(t *testing.T) {
	bst := time.FixedZone("BST", 1*60*60)
	started := time.Date(2026, 9, 4, 22, 33, 19, 860123000, bst)

	s := NewStore()
	s.Add(&Trace{
		ID:         "tz-1",
		StartedAt:  started,
		DurationMs: 642,
		Status:     200,
		Spans:      []Span{{Kind: "lambda", Name: "order-processor", Status: "ok"}},
	})

	// The same instant expressed in UTC, as the logs API serializes it.
	utcQuery := started.UTC().Add(637 * time.Millisecond)

	got := s.FindNear("order-processor", utcQuery, 5000)
	if len(got) == 0 {
		t.Fatalf("FindNear returned no traces for a UTC query matching a trace stored with a +01:00 offset")
	}
	if got[0].ID != "tz-1" {
		t.Fatalf("FindNear returned trace %q, want %q", got[0].ID, "tz-1")
	}

	// The offset form must resolve to the same trace.
	alsoGot := s.FindNear("order-processor", started.Add(637*time.Millisecond), 5000)
	if len(alsoGot) == 0 || alsoGot[0].ID != "tz-1" {
		t.Fatalf("FindNear disagreed between UTC and offset representations of one instant")
	}
}

// TestAddStoresUTC pins the on-disk representation, since every range scan,
// ORDER BY, and prune in this package depends on one shared offset.
func TestAddStoresUTC(t *testing.T) {
	bst := time.FixedZone("BST", 1*60*60)
	s := NewStore()
	s.Add(&Trace{
		ID:        "utc-1",
		StartedAt: time.Date(2026, 9, 4, 22, 33, 19, 0, bst),
		Status:    200,
	})

	var stored string
	if err := s.db.QueryRow(`SELECT started_at FROM traces WHERE id = 'utc-1'`).Scan(&stored); err != nil {
		t.Fatalf("failed to read back trace: %v", err)
	}
	if want := "2026-09-04T21:33:19Z"; stored != want {
		t.Fatalf("started_at stored as %q, want %q", stored, want)
	}
}

// TestNormalizeStartedAtMigratesLegacyRows covers databases written before
// timestamps were normalized on insert.
func TestNormalizeStartedAtMigratesLegacyRows(t *testing.T) {
	s := NewStore()
	if _, err := s.db.Exec(
		`INSERT INTO traces (id, correlation_id, started_at, duration_ms, status, method, path, gateway_id, gateway_name, spans_json)
		 VALUES ('legacy-1', 'legacy-1', '2026-09-04T22:33:19.860123+01:00', 642, 200, '', '', '', '', '[{"kind":"lambda","name":"order-processor","status":"ok"}]')`,
	); err != nil {
		t.Fatalf("failed to seed legacy row: %v", err)
	}

	s.normalizeStartedAt()

	var stored string
	if err := s.db.QueryRow(`SELECT started_at FROM traces WHERE id = 'legacy-1'`).Scan(&stored); err != nil {
		t.Fatalf("failed to read back legacy trace: %v", err)
	}
	if want := "2026-09-04T21:33:19.860123Z"; stored != want {
		t.Fatalf("legacy started_at is %q, want %q", stored, want)
	}

	utcQuery := time.Date(2026, 9, 4, 21, 33, 20, 497000000, time.UTC)
	if got := s.FindNear("order-processor", utcQuery, 5000); len(got) == 0 {
		t.Fatalf("migrated row still not reachable from a UTC query")
	}
}
