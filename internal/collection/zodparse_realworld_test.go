package collection

import (
	"encoding/json"
	"testing"
)

func TestRealWorldSchema(t *testing.T) {
	path := "/Users/arcwo/projects_26/hces-batch-status-api-lambda/src/utils/validate/schemas.ts"
	exports, err := ParseSchemasFile(path)
	if err != nil {
		t.Skipf("real-world test file not present: %v", err)
	}

	t.Logf("parsed %d exports", len(exports))
	for _, e := range exports {
		t.Logf("  %s (isHeader=%v, fields=%d)", e.Name, e.IsHeader, len(e.Fields))
		for _, f := range e.Fields {
			t.Logf("    %-20s kind=%-8s fmt=%-10s opt=%v enum=%v", f.Name, f.Kind, f.Format, f.Optional, f.Enum)
		}
	}

	probes := GenerateProbesFromExports(exports, nil)
	t.Logf("%d probes generated", len(probes))
	for _, p := range probes {
		b, _ := json.Marshal(p.Body)
		t.Logf("  [%-36s] malformed=%v %s", p.Label, p.Malformed, b)
	}

	// Expect at least 9 probes: malformed, empty, 3 baselines, 4 enum variants across schemas.
	if len(probes) < 9 {
		t.Errorf("expected ≥9 probes, got %d", len(probes))
	}

	// Expect enum variants for batchStatus
	enumCount := 0
	for _, p := range probes {
		if len(p.Label) > 5 && p.Label[:5] == "enum:" {
			enumCount++
		}
	}
	if enumCount < 2 {
		t.Errorf("expected ≥2 enum variants for batchStatus, got %d", enumCount)
	}

	// Include real event files
	eventFiles := []string{
		"/Users/arcwo/projects_26/hces-batch-status-api-lambda/events/event-post.json",
		"/Users/arcwo/projects_26/hces-batch-status-api-lambda/events/event-patch.json",
	}
	probesWithEvents := GenerateProbesFromExports(exports, eventFiles)
	t.Logf("%d probes with events", len(probesWithEvents))
}
