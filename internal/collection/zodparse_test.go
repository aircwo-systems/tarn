package collection

import (
	"encoding/json"
	"os"
	"testing"
)

const fixtureSchema = `import { z } from 'zod';
export const createOrderSchema = z.object({
  batchStatus: z.enum(['PENDING', 'ACTIVE', 'PROCESSING', 'COMPLETED']),
  userId: z.string().uuid(),
  email: z.string().email(),
  amount: z.number().min(0),
  scheduledAt: z.string().datetime(),
  notify: z.boolean().optional(),
});
export const headerSchema = z.object({
  'x-api-key': z.string().min(1),
  authorization: z.string().optional(),
});`

func TestParseSchemasSource_Fields(t *testing.T) {
	exports := parseSchemasSource(fixtureSchema)
	if len(exports) != 2 {
		t.Fatalf("expected 2 exports, got %d", len(exports))
	}

	order := exports[0]
	if order.Name != "createOrderSchema" {
		t.Errorf("expected createOrderSchema, got %s", order.Name)
	}
	if order.IsHeader {
		t.Error("createOrderSchema should not be a header schema")
	}
	if len(order.Fields) != 6 {
		t.Errorf("expected 6 fields, got %d", len(order.Fields))
	}

	// Enum field
	var enumField *SchemaField
	for i := range order.Fields {
		if order.Fields[i].Name == "batchStatus" {
			enumField = &order.Fields[i]
		}
	}
	if enumField == nil {
		t.Fatal("batchStatus field not found")
	}
	if enumField.Kind != FieldEnum {
		t.Errorf("batchStatus kind: want enum, got %s", enumField.Kind)
	}
	if len(enumField.Enum) != 4 {
		t.Errorf("batchStatus enum values: want 4, got %d: %v", len(enumField.Enum), enumField.Enum)
	}

	// UUID field
	var idField *SchemaField
	for i := range order.Fields {
		if order.Fields[i].Name == "userId" {
			idField = &order.Fields[i]
		}
	}
	if idField == nil {
		t.Fatal("userId field not found")
	}
	if idField.Format != FormatUUID {
		t.Errorf("userId format: want uuid, got %s", idField.Format)
	}

	// Header schema
	hdr := exports[1]
	if hdr.Name != "headerSchema" {
		t.Errorf("expected headerSchema, got %s", hdr.Name)
	}
	if !hdr.IsHeader {
		t.Error("headerSchema should be detected as header schema")
	}
}

func TestGenerateProbes_Order(t *testing.T) {
	exports := parseSchemasSource(fixtureSchema)
	order := exports[0]

	probes := GenerateProbes(&order, nil)

	if len(probes) < 3 {
		t.Fatalf("expected at least 3 probes, got %d", len(probes))
	}

	// First: malformed
	if probes[0].Label != "malformed" || !probes[0].Malformed {
		t.Errorf("probe[0] should be malformed, got label=%s malformed=%v", probes[0].Label, probes[0].Malformed)
	}
	// Second: empty
	if probes[1].Label != "empty" {
		t.Errorf("probe[1] should be empty, got %s", probes[1].Label)
	}
	// Third: baseline
	if probes[2].Label != "baseline" {
		t.Errorf("probe[2] should be baseline, got %s", probes[2].Label)
	}

	// Baseline body should have all required fields
	var baseline map[string]interface{}
	if err := json.Unmarshal(probes[2].Body, &baseline); err != nil {
		t.Fatalf("baseline body not valid JSON: %v", err)
	}
	for _, reqField := range []string{"batchStatus", "userId", "email", "amount", "scheduledAt"} {
		if _, ok := baseline[reqField]; !ok {
			t.Errorf("baseline missing field %s", reqField)
		}
	}
	// notify is optional — may or may not be present
	// batchStatus should be first enum value
	if baseline["batchStatus"] != "PENDING" {
		t.Errorf("batchStatus baseline: want PENDING, got %v", baseline["batchStatus"])
	}

	// Enum variants: batchStatus has 4 values → 3 extras
	enumProbes := 0
	for _, p := range probes {
		if len(p.Label) > 5 && p.Label[:5] == "enum:" {
			enumProbes++
		}
	}
	if enumProbes != 3 {
		t.Errorf("expected 3 enum variant probes, got %d", enumProbes)
	}
}

func TestParseSchemasFile(t *testing.T) {
	f, err := os.CreateTemp("", "schemas*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(fixtureSchema)
	f.Close()

	exports, err := ParseSchemasFile(f.Name())
	if err != nil {
		t.Fatalf("ParseSchemasFile: %v", err)
	}
	if len(exports) != 2 {
		t.Errorf("expected 2 exports, got %d", len(exports))
	}
}
