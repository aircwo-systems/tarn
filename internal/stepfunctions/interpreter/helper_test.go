package interpreter

import (
	"encoding/json"
	"testing"
)

// mustJSON decodes a JSON literal into the any-shaped value model the
// interpreter operates on (map[string]any, []any, float64, string, bool, nil).
func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid test JSON %q: %v", s, err)
	}
	return v
}
