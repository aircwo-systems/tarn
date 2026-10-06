package trace

import (
	"cmp"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Merge imports distributed trace chunks in one transaction. Retries replace
// spans by ID and chunks arriving from different processes extend the same
// trace.
func (s *Store) Merge(traces ...*Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return fmt.Errorf("trace store unavailable")
	}
	for _, t := range traces {
		for _, span := range t.Spans {
			if span.ID == "" || span.StartedAt == nil {
				return fmt.Errorf("imported spans require an ID and start time")
			}
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range traces {
		existing, err := findTrace(tx, t.ID)
		if err != nil {
			return err
		}
		mergeSpans(t, existing)
		if err := insertTrace(tx, t); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.countAdded(len(traces))
	return nil
}

func findTrace(tx *sql.Tx, id string) (*Trace, error) {
	rows, err := tx.Query(`SELECT `+traceColumns+` FROM traces WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return scanTrace(rows), nil
}

// mergeSpans folds t's spans into existing ones by ID, then derives the
// trace's timing and request summary from the merged span tree.
func mergeSpans(t, existing *Trace) {
	byID := make(map[string]Span)
	if existing != nil {
		for _, span := range existing.Spans {
			byID[span.ID] = span
		}
	}
	for _, span := range t.Spans {
		byID[span.ID] = span
	}
	t.Spans = make([]Span, 0, len(byID))
	for _, span := range byID {
		t.Spans = append(t.Spans, span)
	}
	sort.Slice(t.Spans, func(i, j int) bool {
		a, b := t.Spans[i], t.Spans[j]
		if !a.StartedAt.Equal(*b.StartedAt) {
			return a.StartedAt.Before(*b.StartedAt)
		}
		if a.DurationNs != b.DurationNs {
			return a.DurationNs > b.DurationNs
		}
		return a.ID < b.ID
	})
	if len(t.Spans) == 0 {
		return
	}
	t.StartedAt = *t.Spans[0].StartedAt
	end := t.StartedAt
	root := t.Spans[0]
	for _, span := range t.Spans {
		if until := span.StartedAt.Add(time.Duration(span.DurationNs)); until.After(end) {
			end = until
		}
		if span.ParentID == "" {
			root = span
		}
	}
	t.DurationMs = max(1, (end.Sub(t.StartedAt).Nanoseconds()+int64(time.Millisecond)-1)/int64(time.Millisecond))
	t.Method = root.Meta["http.method"]
	t.Path = cmp.Or(root.Meta["http.route"], root.Meta["http.url"], root.Meta["resource"])
	t.Status, _ = strconv.Atoi(root.Meta["http.status_code"])
	if t.Status == 0 {
		t.Status = 200
	}
	// An asynchronous consumer can fail after the HTTP request succeeded.
	for _, span := range t.Spans {
		if span.Status == "error" {
			t.Status = 500
			break
		}
	}
}

// FindByCorrelation returns an exact trace for a structured log. Imported
// traces are scoped to their account; legacy invocation traces stay shared.
func (s *Store) FindByCorrelation(accountID, correlationID string) *Trace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT `+traceColumns+` FROM traces
		WHERE correlation_id = ? AND account_id IN ('', ?)
		ORDER BY started_at DESC LIMIT 2`, correlationID, accountID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	if !rows.Next() {
		return nil
	}
	t := scanTrace(rows)
	// A low 64-bit Datadog log ID cannot distinguish traces whose high bits
	// differ. Do not attach an ambiguous log to an arbitrary request.
	if rows.Next() {
		return nil
	}
	return t
}
