package trace

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// maxTraces is the maximum number of traces retained in the database.
	maxTraces = 2000

	// defaultRetention is the default TTL for traces.
	defaultRetention = 24 * time.Hour
)

// Store is a thread-safe, SQLite-backed trace store.
type Store struct {
	mu        sync.RWMutex
	db        *sql.DB
	retention time.Duration
}

// Trace records the full request lifecycle for one invocation.
type Trace struct {
	ID          string    `json:"id"`
	CorrelationID string  `json:"correlationId,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	DurationMs  int64     `json:"durationMs"`
	Status      int       `json:"status"`
	Method      string    `json:"method,omitempty"`
	Path        string    `json:"path,omitempty"`
	GatewayID   string    `json:"gatewayId,omitempty"`
	GatewayName string    `json:"gatewayName,omitempty"`
	Spans       []Span    `json:"spans"`
}

// Span is one hop in the trace path.
type Span struct {
	Kind       string            `json:"kind"`       // "gateway", "lambda", "queue"
	Name       string            `json:"name"`       // resource name
	DurationMs int64             `json:"durationMs"` // wall time for this span
	Status     string            `json:"status"`     // "ok", "error", "client_error"
	Meta       map[string]string `json:"meta,omitempty"`
}

// NewStore creates a new in-memory trace store (ring buffer fallback).
// Use OpenStore for SQLite-backed persistence.
func NewStore() *Store {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Printf("[trace] failed to open in-memory sqlite: %v; using fallback", err)
		return &Store{retention: defaultRetention}
	}
	s := &Store{db: db, retention: defaultRetention}
	s.initDB()
	return s
}

// OpenStore opens a SQLite-backed trace store at the given data directory.
// The database file is created at <dataDir>/traces.db.
func OpenStore(dataDir string) *Store {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		log.Printf("[trace] failed to create data dir %s: %v; falling back to memory", dataDir, err)
		return NewStore()
	}

	dbPath := filepath.Join(dataDir, "traces.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Printf("[trace] failed to open sqlite at %s: %v; falling back to memory", dbPath, err)
		return NewStore()
	}

	s := &Store{db: db, retention: defaultRetention}
	s.initDB()

	// Prune old traces on startup.
	s.prune()

	log.Printf("[trace] opened persistent store at %s", dbPath)
	return s
}

func (s *Store) initDB() {
	if s.db == nil {
		return
	}
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS traces (
			id          TEXT PRIMARY KEY,
			correlation_id TEXT NOT NULL DEFAULT '',
			started_at  TEXT    NOT NULL,
			duration_ms INTEGER NOT NULL,
			status      INTEGER NOT NULL,
			method      TEXT    NOT NULL DEFAULT '',
			path        TEXT    NOT NULL DEFAULT '',
			gateway_id  TEXT    NOT NULL DEFAULT '',
			gateway_name TEXT   NOT NULL DEFAULT '',
			spans_json  TEXT    NOT NULL DEFAULT '[]'
		);
		CREATE INDEX IF NOT EXISTS idx_traces_started_at ON traces(started_at);
	`)
	if err != nil {
		log.Printf("[trace] failed to initialize schema: %v", err)
	}

	if _, err := s.db.Exec(`ALTER TABLE traces ADD COLUMN correlation_id TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		log.Printf("[trace] failed to ensure correlation_id column: %v", err)
	}
}

// Add records a trace, evicting old entries beyond the retention window.
func (s *Store) Add(t *Trace) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || t == nil {
		return
	}
	if t.CorrelationID == "" {
		t.CorrelationID = t.ID
	}

	spansJSON, err := json.Marshal(t.Spans)
	if err != nil {
		spansJSON = []byte("[]")
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO traces (id, correlation_id, started_at, duration_ms, status, method, path, gateway_id, gateway_name, spans_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID,
		t.CorrelationID,
		t.StartedAt.Format(time.RFC3339Nano),
		t.DurationMs,
		t.Status,
		t.Method,
		t.Path,
		t.GatewayID,
		t.GatewayName,
		string(spansJSON),
	)
	if err != nil {
		log.Printf("[trace] failed to insert trace %s: %v", t.ID, err)
	}
}

// PruneOlderThan removes traces older than the provided cutoff.
func (s *Store) PruneOlderThan(cutoff time.Time) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return 0
	}

	result, err := s.db.Exec(`DELETE FROM traces WHERE started_at < ?`, cutoff.Format(time.RFC3339Nano))
	if err != nil {
		log.Printf("[trace] failed to prune traces older than %s: %v", cutoff.Format(time.RFC3339Nano), err)
		return 0
	}

	rows, _ := result.RowsAffected()
	return rows
}

// scanTrace reads one trace from a sql.Rows cursor. Returns nil and logs on scan error.
func scanTrace(rows *sql.Rows) *Trace {
	var (
		t             Trace
		correlationID string
		startedAt     string
		spansJSON     string
	)
	if err := rows.Scan(&t.ID, &correlationID, &startedAt, &t.DurationMs, &t.Status, &t.Method, &t.Path, &t.GatewayID, &t.GatewayName, &spansJSON); err != nil {
		log.Printf("[trace] failed to scan trace row: %v", err)
		return nil
	}
	if strings.TrimSpace(correlationID) != "" {
		t.CorrelationID = correlationID
	} else {
		t.CorrelationID = t.ID
	}
	if parsed, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
		t.StartedAt = parsed
	}
	if err := json.Unmarshal([]byte(spansJSON), &t.Spans); err != nil {
		t.Spans = nil
	}
	return &t
}

// Recent returns up to n of the most recent traces, newest first.
func (s *Store) Recent(n int) []*Trace {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return nil
	}

	rows, err := s.db.Query(`
		SELECT id, correlation_id, started_at, duration_ms, status, method, path, gateway_id, gateway_name, spans_json
		FROM traces
		ORDER BY started_at DESC
		LIMIT ?`, n)
	if err != nil {
		log.Printf("[trace] failed to query recent traces: %v", err)
		return nil
	}
	defer rows.Close()

	var out []*Trace
	for rows.Next() {
		if t := scanTrace(rows); t != nil {
			out = append(out, t)
		}
	}
	return out
}

// FindNear returns traces whose execution window overlaps with [around-windowMs, around+windowMs]
// and which contain a lambda span named functionName. Results are ordered by proximity to around.
func (s *Store) FindNear(functionName string, around time.Time, windowMs int64) []*Trace {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return nil
	}

	// Query traces that started within [around-windowMs, around+windowMs].
	// The log event T may occur during lambda execution, so startedAt can be before T.
	lo := around.Add(-time.Duration(windowMs) * time.Millisecond).Format(time.RFC3339Nano)
	hi := around.Add(time.Duration(windowMs) * time.Millisecond).Format(time.RFC3339Nano)

	// Pre-filter on spans_json containing "lambda" to reduce Go-side JSON unmarshaling.
	// LIMIT is applied after this filter so all matching candidates in the window are found.
	rows, err := s.db.Query(`
		SELECT id, correlation_id, started_at, duration_ms, status, method, path, gateway_id, gateway_name, spans_json
		FROM traces
		WHERE started_at BETWEEN ? AND ?
		  AND spans_json LIKE '%"kind":"lambda"%'
		ORDER BY started_at DESC
		LIMIT 100`, lo, hi)
	if err != nil {
		log.Printf("[trace] FindNear query error: %v", err)
		return nil
	}
	defer rows.Close()

	aroundUnix := around.UnixMilli()
	type candidate struct {
		trace *Trace
		dist  int64
	}
	var candidates []candidate

	for rows.Next() {
		t := scanTrace(rows)
		if t == nil {
			continue
		}

		hasLambda := false
		for _, sp := range t.Spans {
			if strings.EqualFold(sp.Kind, "lambda") && sp.Name == functionName {
				hasLambda = true
				break
			}
		}
		if !hasLambda {
			continue
		}

		midUnix := t.StartedAt.UnixMilli() + t.DurationMs/2
		dist := aroundUnix - midUnix
		if dist < 0 {
			dist = -dist
		}
		candidates = append(candidates, candidate{trace: t, dist: dist})
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort by proximity.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	out := make([]*Trace, len(candidates))
	for i, c := range candidates {
		out[i] = c.trace
	}
	return out
}

// prune removes traces older than the retention window and caps total count.
func (s *Store) prune() {
	if s.db == nil {
		return
	}

	cutoff := time.Now().Add(-s.retention).Format(time.RFC3339Nano)
	result, err := s.db.Exec(`DELETE FROM traces WHERE started_at < ?`, cutoff)
	if err != nil {
		log.Printf("[trace] failed to prune old traces: %v", err)
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		log.Printf("[trace] pruned %d traces older than %s", n, s.retention)
	}

	// Cap total count.
	_, err = s.db.Exec(`
		DELETE FROM traces WHERE id IN (
			SELECT id FROM traces ORDER BY started_at DESC LIMIT -1 OFFSET ?
		)`, maxTraces)
	if err != nil {
		log.Printf("[trace] failed to cap trace count: %v", err)
	}
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Count returns the total number of traces in the store.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return 0
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM traces`).Scan(&n); err != nil {
		return 0
	}
	return n
}

// String returns a summary of the store for debugging.
func (s *Store) String() string {
	return fmt.Sprintf("TraceStore(count=%d, retention=%s)", s.Count(), s.retention)
}
