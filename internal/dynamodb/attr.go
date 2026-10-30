package dynamodb

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// maxItemBytes is DynamoDB's item size limit.
const maxItemBytes = 400 * 1024

// canonicalNumber renders a DynamoDB number the way DynamoDB returns it, so
// "1.50", "01.5" and "15e-1" are one value and one key.
func canonicalNumber(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return "", validationError("The parameter cannot be converted to a numeric value: %s", raw)
		}
		exp, s = e, s[:i]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	digits := intPart + frac
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return "", validationError("The parameter cannot be converted to a numeric value: %s", raw)
	}
	exp -= len(frac)
	digits = strings.TrimLeft(digits, "0")
	for strings.HasSuffix(digits, "0") {
		digits = digits[:len(digits)-1]
		exp++
	}
	if digits == "" {
		return "0", nil
	}
	if len(digits) > 38 {
		return "", validationError("Attempting to store more than 38 significant digits in a Number")
	}
	var out string
	switch point := len(digits) + exp; {
	case exp >= 0:
		out = digits + strings.Repeat("0", exp)
	case point > 0:
		out = digits[:point] + "." + digits[point:]
	default:
		out = "0." + strings.Repeat("0", -point) + digits
	}
	if neg {
		out = "-" + out
	}
	return out, nil
}

// canonicalizeAttr validates an attribute value and returns it with numbers in
// canonical form and sets sorted, so equal values have equal encodings.
func canonicalizeAttr(value any) (any, error) {
	attr, ok := value.(map[string]any)
	if !ok || len(attr) != 1 {
		return nil, validationError("Supplied AttributeValue has more or less than one datatype set")
	}
	for kind, raw := range attr {
		switch kind {
		case "S", "B", "BOOL", "NULL":
			return attr, nil
		case "N":
			text, _ := raw.(string)
			n, err := canonicalNumber(text)
			if err != nil {
				return nil, err
			}
			return map[string]any{"N": n}, nil
		case "SS", "NS", "BS":
			list, _ := raw.([]any)
			if len(list) == 0 {
				return nil, validationError("One or more parameter values were invalid: An string set  may not be empty")
			}
			seen := make(map[string]struct{}, len(list))
			out := make([]any, 0, len(list))
			for _, entry := range list {
				text, _ := entry.(string)
				if kind == "NS" {
					n, err := canonicalNumber(text)
					if err != nil {
						return nil, err
					}
					text = n
				}
				if _, dup := seen[text]; dup {
					return nil, validationError("One or more parameter values were invalid: Input collection contains duplicates")
				}
				seen[text] = struct{}{}
				out = append(out, text)
			}
			sort.Slice(out, func(i, j int) bool { return compareSetMembers(kind, out[i].(string), out[j].(string)) < 0 })
			return map[string]any{kind: out}, nil
		case "L":
			list, _ := raw.([]any)
			out := make([]any, len(list))
			for i, entry := range list {
				v, err := canonicalizeAttr(entry)
				if err != nil {
					return nil, err
				}
				out[i] = v
			}
			return map[string]any{"L": out}, nil
		case "M":
			m, _ := raw.(map[string]any)
			out, err := canonicalizeItem(m)
			if err != nil {
				return nil, err
			}
			return map[string]any{"M": out}, nil
		}
		return nil, validationError("Supplied AttributeValue has an unsupported datatype: %s", kind)
	}
	return nil, validationError("Supplied AttributeValue is empty")
}

func canonicalizeItem(item map[string]any) (map[string]any, error) {
	if item == nil {
		return nil, nil
	}
	out := make(map[string]any, len(item))
	for name, value := range item {
		v, err := canonicalizeAttr(value)
		if err != nil {
			return nil, err
		}
		out[name] = v
	}
	return out, nil
}

func compareSetMembers(kind, a, b string) int {
	if kind == "NS" {
		return compareAttributeValues(map[string]any{"N": a}, map[string]any{"N": b})
	}
	return strings.Compare(a, b)
}

// validateItem checks an item against the table's key attributes, including
// index keys, and the item size limit.
func validateItem(table *types.DynamoDBTable, item map[string]any) error {
	defined := attributeTypeMap(table.AttributeDefinitions)
	for _, part := range table.KeySchema {
		value, ok := item[part.AttributeName]
		if !ok || value == nil {
			return validationError("One or more parameter values were invalid: Missing the key %s in the item", part.AttributeName)
		}
		if err := checkKeyType(part.AttributeName, defined[part.AttributeName], value, ""); err != nil {
			return err
		}
	}
	checkIndex := func(name string, schema []types.DynamoDBKeySchemaElement) error {
		for _, part := range schema {
			if value, ok := item[part.AttributeName]; ok {
				if err := checkKeyType(part.AttributeName, defined[part.AttributeName], value, name); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, idx := range table.GlobalSecondaryIndexes {
		if err := checkIndex(idx.IndexName, idx.KeySchema); err != nil {
			return err
		}
	}
	for _, idx := range table.LocalSecondaryIndexes {
		if err := checkIndex(idx.IndexName, idx.KeySchema); err != nil {
			return err
		}
	}
	if itemSize(item) > maxItemBytes {
		return validationError("Item size has exceeded the maximum allowed size")
	}
	return nil
}

func checkKeyType(name, want string, value any, index string) error {
	got := attributeKind(value)
	if want != "" && got != want {
		if index != "" {
			return validationError("One or more parameter values were invalid: Type mismatch for Index Key %s Expected: %s Actual: %s IndexName: %s", name, want, got, index)
		}
		return validationError("One or more parameter values were invalid: Type mismatch for key %s expected: %s actual: %s", name, want, got)
	}
	if attr, ok := value.(map[string]any); ok && (attr["S"] == "" || attr["B"] == "") {
		return validationError("One or more parameter values are not valid. The AttributeValue for a key attribute cannot contain an empty string value. Key: %s", name)
	}
	return nil
}

func attributeKind(value any) string {
	if attr, ok := value.(map[string]any); ok && len(attr) == 1 {
		for kind := range attr {
			return kind
		}
	}
	return ""
}

// canonicalKey validates a request key against the table key schema and
// canonicalises it, so it serialises to the stored item's key.
func canonicalKey(table *types.DynamoDBTable, key map[string]any) (map[string]any, error) {
	if len(key) != len(table.KeySchema) {
		return nil, validationError("The provided key element does not match the schema")
	}
	out, err := canonicalizeItem(key)
	if err != nil {
		return nil, err
	}
	defined := attributeTypeMap(table.AttributeDefinitions)
	for _, part := range table.KeySchema {
		value, ok := out[part.AttributeName]
		if !ok || attributeKind(value) != defined[part.AttributeName] {
			return nil, validationError("The provided key element does not match the schema")
		}
	}
	return out, nil
}

// itemSize approximates DynamoDB's item size: attribute names plus values.
func itemSize(item map[string]any) int {
	n := 0
	for name, value := range item {
		n += len(name) + attrSize(value)
	}
	return n
}

func attrSize(value any) int {
	attr, ok := value.(map[string]any)
	if !ok {
		return 0
	}
	n := 0
	for kind, raw := range attr {
		switch kind {
		case "S", "N":
			s, _ := raw.(string)
			n += len(s)
		case "B":
			s, _ := raw.(string)
			n += base64.StdEncoding.DecodedLen(len(s))
		case "BOOL", "NULL":
			n++
		case "SS", "NS", "BS":
			list, _ := raw.([]any)
			for _, entry := range list {
				s, _ := entry.(string)
				n += len(s)
			}
		case "L":
			list, _ := raw.([]any)
			n += 3
			for _, entry := range list {
				n += 1 + attrSize(entry)
			}
		case "M":
			m, _ := raw.(map[string]any)
			n += 3 + itemSize(m) + len(m)
		}
	}
	return n
}

// scalarOrder returns a value's sort key: numbers compare numerically,
// strings and binaries by their bytes.
func scalarOrder(value any) (kind, raw string) {
	attr, ok := value.(map[string]any)
	if !ok || len(attr) != 1 {
		return "", ""
	}
	for k, v := range attr {
		s, _ := v.(string)
		if k == "B" {
			if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
				s = string(decoded)
			}
		}
		return k, s
	}
	return "", ""
}

// compareScalars orders two S, N or B values; ok is false when they are not
// the same scalar type, which makes every comparison between them false.
func compareScalars(left, right any) (int, bool) {
	lk, ls := scalarOrder(left)
	rk, rs := scalarOrder(right)
	if lk != rk {
		return 0, false
	}
	switch lk {
	case "N":
		return compareAttributeValues(left, right), true
	case "S", "B":
		return strings.Compare(ls, rs), true
	}
	return 0, false
}

// attributeValueEqual compares any two attribute values; sets ignore order and
// numbers ignore formatting.
func attributeValueEqual(left, right any) bool {
	l, lerr := canonicalizeAttr(left)
	r, rerr := canonicalizeAttr(right)
	if lerr != nil || rerr != nil {
		return false
	}
	lb, _ := json.Marshal(l)
	rb, _ := json.Marshal(r)
	return bytes.Equal(lb, rb)
}
