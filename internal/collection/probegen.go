package collection

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// ProbeBody is a single labeled probe payload to send during chaos probing.
type ProbeBody struct {
	Label   string            `json:"label"`
	Body    json.RawMessage   `json:"body,omitempty"`    // nil = no body (GET / malformed)
	Headers map[string]string `json:"headers,omitempty"` // extra headers to inject
	// Malformed indicates the body is intentionally invalid JSON (e.g. "{}}").
	Malformed bool `json:"malformed,omitempty"`
}

// GenerateProbes creates the full ordered probe set for one route from
// a matched schema export + optional event files.
//
// If schema is nil the generated probes are still valid (just the structural
// ones: malformed, empty, and events).
//
// Probe order:
//  1. Malformed JSON "{}" → 400 parse error
//  2. Empty body "{}" → 400 missing required
//  3. Baseline body (all required fields filled) → first real 400/200
//  4. Per-enum-field variant bodies (one per extra enum value per field)
//  5. Events/*.json bodies (real payloads from the developer's repo)
func GenerateProbes(schema *SchemaExport, eventFiles []string) []ProbeBody {
	var probes []ProbeBody

	// 1. Malformed JSON
	probes = append(probes, ProbeBody{
		Label:     "malformed",
		Body:      json.RawMessage(`{}`),
		Malformed: true,
	})

	// 2. Empty body
	probes = append(probes, ProbeBody{
		Label: "empty",
		Body:  json.RawMessage(`{}`),
	})

	if schema != nil {
		// Separate header fields from body fields.
		var bodyFields, headerFields []SchemaField
		for _, f := range schema.Fields {
			if isHeaderField(f.Name) {
				headerFields = append(headerFields, f)
			} else {
				bodyFields = append(bodyFields, f)
			}
		}

		// 3. Baseline: all required body fields filled.
		if len(bodyFields) > 0 {
			baseline := buildBaselineBody(bodyFields, false)
			if raw, err := json.Marshal(baseline); err == nil {
				probes = append(probes, ProbeBody{
					Label:   "baseline",
					Body:    raw,
					Headers: buildBaselineHeaders(headerFields),
				})
			}
		}

		// 4. Enum variants: for each enum field, one body per extra option.
		probes = append(probes, buildEnumVariants(bodyFields, headerFields)...)
	}

	// 5. Event files.
	for _, ef := range eventFiles {
		raw, err := LoadEventFile(ef)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(ef), ".json")
		probes = append(probes, ProbeBody{
			Label: "event:" + name,
			Body:  raw,
		})
	}

	return probes
}

// buildBaselineBody constructs a map with probe values for all required (or all)
// fields depending on the allFields flag.
func buildBaselineBody(fields []SchemaField, allFields bool) map[string]interface{} {
	obj := make(map[string]interface{}, len(fields))
	for _, f := range fields {
		if !allFields && f.Optional {
			continue
		}
		obj[f.Name] = ProbeValueForField(f)
	}
	return obj
}

func buildBaselineHeaders(fields []SchemaField) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	h := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.Optional {
			continue
		}
		v := ProbeValueForField(f)
		if s, ok := v.(string); ok {
			h[f.Name] = s
		} else {
			h[f.Name] = "probe-value"
		}
	}
	if len(h) == 0 {
		return nil
	}
	return h
}

// buildEnumVariants generates one probe body per extra enum value per enum field.
func buildEnumVariants(bodyFields, headerFields []SchemaField) []ProbeBody {
	var probes []ProbeBody

	baseBody := buildBaselineBody(bodyFields, false)
	baseHeaders := buildBaselineHeaders(headerFields)

	for _, f := range bodyFields {
		if f.Kind != FieldEnum || len(f.Enum) <= 1 {
			continue
		}
		// First value is the baseline; iterate the rest.
		for _, val := range f.Enum[1:] {
			variant := copyMap(baseBody)
			variant[f.Name] = val

			raw, err := json.Marshal(variant)
			if err != nil {
				continue
			}
			probes = append(probes, ProbeBody{
				Label:   "enum:" + f.Name + "=" + val,
				Body:    raw,
				Headers: baseHeaders,
			})
		}
	}
	return probes
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	c := make(map[string]interface{}, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func isHeaderField(name string) bool {
	return headerFieldRe.MatchString(name)
}

// GenerateProbesFromExports generates the full probe set from all exports in a
// schemas.ts file. It generates per-schema baselines and enum variants so that
// each body schema contributes its own probes.
//
// Probe order:
//  1. Malformed JSON
//  2. Empty body
//  3. For each body schema: baseline (required fields) + enum variants
//  4. Events/*.json bodies
func GenerateProbesFromExports(exports []SchemaExport, eventFiles []string) []ProbeBody {
	// Separate header schemas from body schemas.
	var bodySchemas, headerSchemas []SchemaExport
	for _, e := range exports {
		if e.IsHeader {
			headerSchemas = append(headerSchemas, e)
		} else {
			bodySchemas = append(bodySchemas, e)
		}
	}

	if len(bodySchemas) == 0 {
		return GenerateProbes(nil, eventFiles)
	}

	// Merge all header fields from all header schemas.
	var allHeaderFields []SchemaField
	for _, hs := range headerSchemas {
		allHeaderFields = append(allHeaderFields, hs.Fields...)
	}

	return generateProbesForSchemaGroup(bodySchemas, allHeaderFields, eventFiles)
}

// InferSchemaMethod returns the HTTP method this schema name most likely
// corresponds to based on naming conventions.
// Returns "PATH_PARAMS" for path-parameter schemas (skip from body probes).
// Returns "" for general/ambiguous schemas (used as-is by GenerateProbesFromExports).
func InferSchemaMethod(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "pathparam") || strings.Contains(lower, "pathparameter") {
		return "PATH_PARAMS"
	}
	if strings.HasPrefix(lower, "create") || strings.HasPrefix(lower, "post") {
		return "POST"
	}
	if strings.HasPrefix(lower, "update") || strings.HasPrefix(lower, "patch") {
		return "PATCH"
	}
	if strings.HasPrefix(lower, "put") || strings.HasPrefix(lower, "replace") {
		return "PUT"
	}
	if strings.HasPrefix(lower, "delete") || strings.HasPrefix(lower, "remov") {
		return "DELETE"
	}
	if strings.HasPrefix(lower, "get") || strings.HasPrefix(lower, "list") ||
		strings.HasPrefix(lower, "find") || strings.HasPrefix(lower, "search") ||
		strings.HasPrefix(lower, "query") || strings.HasPrefix(lower, "fetch") ||
		strings.HasPrefix(lower, "read") {
		return "GET"
	}
	return ""
}

// groupEventsByMethod groups event file paths by the inferred HTTP method based
// on the filename. The empty-string key collects unclassified events.
func groupEventsByMethod(eventFiles []string) map[string][]string {
	result := make(map[string][]string)
	for _, ef := range eventFiles {
		base := strings.ToLower(filepath.Base(ef))
		var method string
		switch {
		case strings.Contains(base, "post") || strings.Contains(base, "create"):
			method = "POST"
		case strings.Contains(base, "patch") || strings.Contains(base, "update"):
			method = "PATCH"
		case strings.Contains(base, "put") || strings.Contains(base, "replace"):
			method = "PUT"
		case strings.Contains(base, "delete") || strings.Contains(base, "remov"):
			method = "DELETE"
		case strings.Contains(base, "get") || strings.Contains(base, "list") || strings.Contains(base, "search"):
			method = "GET"
		}
		result[method] = append(result[method], ef)
	}
	return result
}

// GenerateProbesByMethod generates probe bodies grouped by HTTP method using
// schema name conventions to match each schema to its likely method.
// Path-parameter schemas (e.g. PatchPathParametersSchema) are skipped from
// body probes. Header schemas are merged into all methods.
// Returns an empty map when no clearly method-prefixed schemas are found
// (caller should fall back to GenerateProbesFromExports).
func GenerateProbesByMethod(exports []SchemaExport, eventFiles []string) map[string][]ProbeBody {
	var headerSchemas []SchemaExport
	methodBodies := make(map[string][]SchemaExport)

	for _, e := range exports {
		if e.IsHeader {
			headerSchemas = append(headerSchemas, e)
			continue
		}
		role := InferSchemaMethod(e.Name)
		// Skip path-param schemas and scalar-only exports (empty Fields = enum-only).
		if role == "PATH_PARAMS" || len(e.Fields) == 0 {
			continue
		}
		if role != "" {
			methodBodies[role] = append(methodBodies[role], e)
		}
		// Schemas with role="" are general — handled by GenerateProbesFromExports fallback.
	}

	if len(methodBodies) == 0 {
		return nil
	}

	var allHeaderFields []SchemaField
	for _, hs := range headerSchemas {
		allHeaderFields = append(allHeaderFields, hs.Fields...)
	}

	byMethod := groupEventsByMethod(eventFiles)
	result := make(map[string][]ProbeBody, len(methodBodies))

	for method, schemas := range methodBodies {
		evs := append(append([]string{}, byMethod[method]...), byMethod[""]...)
		result[method] = generateProbesForSchemaGroup(schemas, allHeaderFields, evs)
	}
	return result
}

// generateProbesForSchemaGroup is the shared probe-generation core used by both
// GenerateProbesFromExports and GenerateProbesByMethod.
func generateProbesForSchemaGroup(schemas []SchemaExport, allHeaderFields []SchemaField, eventFiles []string) []ProbeBody {
	var probes []ProbeBody

	probes = append(probes,
		ProbeBody{Label: "malformed", Body: json.RawMessage(`{}`), Malformed: true},
		ProbeBody{Label: "empty", Body: json.RawMessage(`{}`)},
	)

	multiSchema := len(schemas) > 1
	for i := range schemas {
		schema := &schemas[i]
		var bodyFields, headerFields []SchemaField
		for _, f := range schema.Fields {
			if isHeaderField(f.Name) {
				headerFields = append(headerFields, f)
			} else {
				bodyFields = append(bodyFields, f)
			}
		}
		headerFields = append(headerFields, allHeaderFields...)

		if len(bodyFields) > 0 {
			baseline := buildBaselineBody(bodyFields, false)
			if raw, err := json.Marshal(baseline); err == nil {
				label := "baseline"
				if multiSchema {
					label = "baseline:" + schema.Name
				}
				probes = append(probes, ProbeBody{
					Label:   label,
					Body:    raw,
					Headers: buildBaselineHeaders(headerFields),
				})
			}
		}

		baseBody := buildBaselineBody(bodyFields, false)
		baseHeaders := buildBaselineHeaders(headerFields)
		for _, f := range bodyFields {
			if f.Kind != FieldEnum || len(f.Enum) <= 1 {
				continue
			}
			for _, val := range f.Enum[1:] {
				variant := copyMap(baseBody)
				variant[f.Name] = val
				raw, err := json.Marshal(variant)
				if err != nil {
					continue
				}
				label := "enum:" + f.Name + "=" + val
				if multiSchema {
					label = "enum:" + schema.Name + "/" + f.Name + "=" + val
				}
				probes = append(probes, ProbeBody{Label: label, Body: raw, Headers: baseHeaders})
			}
		}
	}

	for _, ef := range eventFiles {
		raw, err := LoadEventFile(ef)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(ef), ".json")
		probes = append(probes, ProbeBody{Label: "event:" + name, Body: raw})
	}
	return probes
}

// SchemaProbeHeaders derives the required-headers map from header-typed fields
// in an export. Used to pre-populate RequiredHeaders on RouteSpec.
func SchemaProbeHeaders(export *SchemaExport) map[string]string {
	if export == nil {
		return nil
	}
	h := make(map[string]string)
	for _, f := range export.Fields {
		if isHeaderField(f.Name) && !f.Optional {
			v := ProbeValueForField(f)
			if s, ok := v.(string); ok {
				h[f.Name] = s
			}
		}
	}
	if len(h) == 0 {
		return nil
	}
	return h
}
