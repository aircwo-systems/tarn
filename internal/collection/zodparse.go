package collection

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FieldKind is the resolved type of a Zod field.
type FieldKind string

const (
	FieldString  FieldKind = "string"
	FieldNumber  FieldKind = "number"
	FieldBool    FieldKind = "bool"
	FieldEnum    FieldKind = "enum"
	FieldLiteral FieldKind = "literal"
	FieldArray   FieldKind = "array"
	FieldObject  FieldKind = "object"
	FieldUnknown FieldKind = "unknown"
)

// FieldFormat is a refinement hint for string fields.
type FieldFormat string

const (
	FormatDatetime FieldFormat = "datetime"
	FormatEmail    FieldFormat = "email"
	FormatURL      FieldFormat = "url"
	FormatUUID     FieldFormat = "uuid"
	FormatNone     FieldFormat = ""
)

// SchemaField describes a single field within a Zod object schema.
type SchemaField struct {
	Name     string
	Kind     FieldKind
	Format   FieldFormat
	Optional bool
	Enum     []string
	Literal  string
	Children []SchemaField
}

// SchemaExport is one named export from a schemas.ts file.
type SchemaExport struct {
	Name     string
	Fields   []SchemaField
	IsHeader bool
}

// parseCtx holds resolved TypeScript values during schema parsing.
type parseCtx struct {
	consts   map[string]string        // CONST_NAME → "string literal"
	tsEnums  map[string][]string      // TsEnumName → []string of values
	rawExprs map[string]string        // anyConstName → raw Zod expression (for resolving references)
	schemas  map[string][]SchemaField // exportName → parsed fields (for object schemas)
}

// ParseSchemasFile reads a schemas.ts file and extracts named Zod exports.
func ParseSchemasFile(path string) ([]SchemaExport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := string(data)
	extraConsts := scanImportedConstants(path, src)
	return parseSchemasSourceCtx(src, extraConsts), nil
}

// scanImportedConstants reads same-dir .ts files referenced in local imports.
func scanImportedConstants(schemaPath, src string) map[string]string {
	result := make(map[string]string)
	dir := filepath.Dir(schemaPath)
	importRe := regexp.MustCompile(`from\s+["'](\./[^"']+|\.\.\/[^"']+)["']`)
	for _, m := range importRe.FindAllStringSubmatch(src, -1) {
		rel := m[1]
		for _, ext := range []string{".ts", ".js"} {
			p := filepath.Join(dir, rel+ext)
			if b, err := os.ReadFile(p); err == nil {
				for k, v := range extractConstants(string(b)) {
					result[k] = v
				}
				break
			}
		}
	}
	return result
}

// ---- parser entry points ----------------------------------------------------

func parseSchemasSource(src string) []SchemaExport {
	return parseSchemasSourceCtx(src, nil)
}

func parseSchemasSourceCtx(src string, extraConsts map[string]string) []SchemaExport {
	src = stripLineComments(src)

	ctx := &parseCtx{
		consts:   extractConstants(src),
		tsEnums:  extractTsEnums(src),
		rawExprs: extractAllConstExprs(src),
		schemas:  make(map[string][]SchemaField),
	}
	for k, v := range extraConsts {
		if _, exists := ctx.consts[k]; !exists {
			ctx.consts[k] = v
		}
	}

	// Find all "export const NAME = z." exports.
	var exportZodRe = regexp.MustCompile(`(?m)export\s+const\s+(\w+)\s*=\s*z\.`)
	matches := exportZodRe.FindAllStringIndex(src, -1)
	var out []SchemaExport

	for i, m := range matches {
		nameRe := regexp.MustCompile(`export\s+const\s+(\w+)`)
		nm := nameRe.FindStringSubmatch(src[m[0]:m[1]])
		if len(nm) < 2 {
			continue
		}
		name := nm[1]

		start := m[1] - 2 // include 'z.'
		var end int
		if i+1 < len(matches) {
			end = matches[i+1][0]
		} else {
			end = len(src)
		}
		chunk := strings.TrimSpace(src[start:end])
		// Use the first top-level semicolon so we don't bleed into later definitions.
		if idx := findFirstTopLevelSemicolon(chunk); idx != -1 {
			chunk = chunk[:idx]
		}
		chunk = strings.TrimSpace(chunk)

		// Try as object schema first.
		fields := parseZodObjectExpr(chunk, ctx)
		if len(fields) > 0 {
			export := SchemaExport{Name: name, Fields: fields}
			export.IsHeader = looksLikeHeaderSchema(fields)
			out = append(out, export)
			ctx.schemas[name] = fields
		} else {
			// Scalar Zod schema (z.enum, z.string, etc.) — store as single-field schema.
			f := parseScalarZodExpr(name, chunk, ctx, 0)
			if f.Kind != FieldUnknown {
				ctx.schemas[name] = []SchemaField{f}
				// Also export single-enum schemas (they're useful by themselves).
				if f.Kind == FieldEnum && len(f.Enum) > 0 {
					out = append(out, SchemaExport{Name: name, Fields: nil, IsHeader: false})
					// Don't add to main out — they're referenced by object schemas.
				}
			}
		}
	}

	return out
}

// ---- constant and enum extraction -------------------------------------------

var constStrRe = regexp.MustCompile(`(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*["']([^"']+)["']`)

func extractConstants(src string) map[string]string {
	m := make(map[string]string)
	for _, match := range constStrRe.FindAllStringSubmatch(src, -1) {
		m[match[1]] = match[2]
	}
	return m
}

var tsEnumRe = regexp.MustCompile(`(?m)(?:export\s+)?enum\s+(\w+)\s*\{`)

func extractTsEnums(src string) map[string][]string {
	result := make(map[string][]string)
	locs := tsEnumRe.FindAllStringSubmatchIndex(src, -1)
	for _, loc := range locs {
		name := src[loc[2]:loc[3]]
		body := extractBalanced(src[loc[1]-1:], '{', '}')
		if body == "" {
			continue
		}
		// Extract string values: KEY = "value" or KEY = 'value'
		valRe := regexp.MustCompile(`\w+\s*=\s*["']([^"']+)["']`)
		var vals []string
		for _, vm := range valRe.FindAllStringSubmatch(body, -1) {
			vals = append(vals, vm[1])
		}
		if len(vals) > 0 {
			result[name] = vals
		}
	}
	return result
}

// wsBeforeDotRe collapses whitespace (including newlines) before a `.` to join
// multi-line Zod chains like `z\n  .string()` into `z.string()`.
var wsBeforeDotRe = regexp.MustCompile(`\s+\.`)

// normalizeChain joins split method chains onto one line.
func normalizeChain(s string) string {
	return wsBeforeDotRe.ReplaceAllString(s, ".")
}

// extractAllConstExprs finds every `const/let/var NAME = <expr>` in src and
// stores the raw expression string for later reference resolution.
// Also handles `const NAME = () => <expr>` arrow functions.
func extractAllConstExprs(src string) map[string]string {
	result := make(map[string]string)
	// Match: const/let/var NAME = (captures everything after '=' on the same/next lines)
	re := regexp.MustCompile(`(?m)(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*`)
	locs := re.FindAllStringSubmatchIndex(src, -1)
	for i, loc := range locs {
		name := src[loc[2]:loc[3]]
		start := loc[1]
		// Extract value until the next top-level semicolon or next const declaration.
		var end int
		if i+1 < len(locs) {
			end = locs[i+1][0]
		} else {
			end = len(src)
		}
		raw := strings.TrimSpace(src[start:end])
		// Trim at the first top-level semicolon to avoid capturing later definitions.
		if idx := findFirstTopLevelSemicolon(raw); idx != -1 {
			raw = strings.TrimSpace(raw[:idx])
		}
		// Handle arrow function: () => expr or () => { return expr; }
		// Strip the arrow prefix if present.
		if arrowIdx := strings.Index(raw, "=>"); arrowIdx != -1 {
			// Simple heuristic: if the part before => is all whitespace/parens/types
			prefix := strings.TrimSpace(raw[:arrowIdx])
			if looksLikeArrowParams(prefix) {
				raw = strings.TrimSpace(raw[arrowIdx+2:])
				// If body is a block { ... }, extract the return value.
				if strings.HasPrefix(raw, "{") {
					raw = extractReturnFromBlock(raw)
				}
			}
		}
		// Normalize multi-line chains: collapse `\n  .method()` → `.method()`.
		raw = normalizeChain(raw)
		result[name] = raw
	}
	return result
}

func looksLikeArrowParams(s string) bool {
	// Matches: (), (val), (val: Type), (val?: Type)
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") {
		return false
	}
	// Must be entirely balanced parens with no z. prefixes.
	return !strings.Contains(s, "z.")
}

func extractReturnFromBlock(block string) string {
	// block is "{ return expr; ... }"
	body := extractBalanced(block, '{', '}')
	if body == "" {
		return block
	}
	retRe := regexp.MustCompile(`return\s+(.+?);`)
	m := retRe.FindStringSubmatch(body)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return body
}

// ---- Zod expression parsing -------------------------------------------------

// parseZodObjectExpr parses a z.object({...}).extend({...}) expression and
// returns the merged field list. Returns nil if not an object schema.
func parseZodObjectExpr(expr string, ctx *parseCtx) []SchemaField {
	body := mergeObjectBodies(expr)
	if body == "" {
		return nil
	}
	return parseObjectBody(body, ctx)
}

func mergeObjectBodies(expr string) string {
	var parts []string
	if i := strings.Index(expr, "z.object("); i != -1 {
		inner := extractBalanced(expr[i+len("z.object("):], '{', '}')
		if inner != "" {
			parts = append(parts, inner)
		}
	}
	rest := expr
	for {
		idx := strings.Index(rest, ".extend(")
		if idx == -1 {
			break
		}
		inner := extractBalanced(rest[idx+len(".extend("):], '{', '}')
		if inner != "" {
			parts = append(parts, inner)
		}
		rest = rest[idx+len(".extend("):]
	}
	return strings.Join(parts, ",")
}

func parseObjectBody(body string, ctx *parseCtx) []SchemaField {
	var fields []SchemaField
	for _, entry := range splitTopLevelEntries(body) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		colon := findTopLevelColon(entry)
		if colon == -1 {
			continue
		}
		rawKey := strings.TrimSpace(entry[:colon])
		rawVal := strings.TrimSpace(entry[colon+1:])
		name := resolveKey(rawKey, ctx)
		if name == "" {
			continue
		}
		field := parseFieldExpr(name, rawVal, ctx, 0)
		fields = append(fields, field)
	}
	return fields
}

func resolveKey(raw string, ctx *parseCtx) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		inner := strings.TrimSpace(raw[1 : len(raw)-1])
		if v, ok := ctx.consts[inner]; ok {
			return v
		}
		// Heuristic: ALL_CAPS_HEADER → kebab-case, strip _HEADER suffix
		normalized := strings.TrimSuffix(inner, "_HEADER")
		normalized = strings.ToLower(strings.ReplaceAll(normalized, "_", "-"))
		return normalized
	}
	clean := strings.Trim(raw, `"'`)
	clean = strings.TrimSuffix(clean, "?")
	return clean
}

// parseScalarZodExpr parses a non-object Zod expression (z.enum, z.string, z.iso.datetime, etc.)
// as a standalone schema value. Used for resolving named schema references.
func parseScalarZodExpr(name, expr string, ctx *parseCtx, depth int) SchemaField {
	return parseFieldExpr(name, expr, ctx, depth)
}

const maxRefDepth = 4

// parseFieldExpr resolves a Zod field expression with named-reference lookup.
func parseFieldExpr(name, expr string, ctx *parseCtx, depth int) SchemaField {
	expr = strings.TrimSpace(strings.TrimRight(expr, ","))

	f := SchemaField{Name: name}

	// Outer optional/nullable wrappers.
	if isOptionalWrapper(expr) {
		f.Optional = true
		expr = unwrapOptional(expr)
	}

	// Strip non-type chained methods (.refine, .pipe, .transform, etc.)
	expr = cleanChain(expr)
	expr = strings.TrimSpace(expr)

	// Check trailing .optional()/.nullable() after cleaning.
	if hasTrailingOptional(expr) {
		f.Optional = true
	}

	// Named schema reference (doesn't start with z.)
	if !strings.HasPrefix(expr, "z.") && !strings.HasPrefix(expr, "(") &&
		depth < maxRefDepth {
		refName := strings.TrimSpace(expr)
		// Trim trailing method calls like SomeSchema.optional()
		if dot := strings.Index(refName, "."); dot != -1 {
			refName = refName[:dot]
		}

		// Check pre-parsed object schemas first.
		if fields, ok := ctx.schemas[refName]; ok {
			if len(fields) == 1 {
				// Single-field schema: use its kind directly (enum, string, etc.)
				sf := fields[0]
				sf.Name = name
				sf.Optional = f.Optional || sf.Optional
				return sf
			} else if len(fields) > 1 {
				f.Kind = FieldObject
				f.Children = fields
				return f
			}
		}

		// Fall back to resolving from raw expression.
		if raw, ok := ctx.rawExprs[refName]; ok && raw != "" {
			f2 := parseFieldExpr(name, raw, ctx, depth+1)
			f2.Optional = f.Optional || f2.Optional
			return f2
		}

		// Unknown named reference.
		f.Kind = FieldUnknown
		return f
	}

	switch {
	case strings.HasPrefix(expr, "z.iso.datetime("), strings.HasPrefix(expr, "z.iso.date("):
		f.Kind = FieldString
		f.Format = FormatDatetime

	case strings.HasPrefix(expr, "z.string("):
		f.Kind = FieldString
		f.Format = detectStringFormat(expr)

	case strings.HasPrefix(expr, "z.number("), strings.HasPrefix(expr, "z.int("):
		f.Kind = FieldNumber

	case strings.HasPrefix(expr, "z.boolean("):
		f.Kind = FieldBool

	case strings.HasPrefix(expr, "z.enum("):
		f.Kind = FieldEnum
		f.Enum = resolveEnumValues(expr, ctx)

	case strings.HasPrefix(expr, "z.literal("):
		f.Kind = FieldLiteral
		f.Literal = extractLiteralValue(expr)

	case strings.HasPrefix(expr, "z.array("):
		f.Kind = FieldArray
		inner := extractBalanced(expr[len("z.array("):], '(', ')')
		if inner != "" {
			itemField := parseFieldExpr("item", strings.TrimSpace(inner), ctx, depth+1)
			f.Children = []SchemaField{itemField}
		}

	case strings.HasPrefix(expr, "z.object("):
		f.Kind = FieldObject
		inner := extractBalanced(expr[len("z.object("):], '{', '}')
		if inner != "" {
			f.Children = parseObjectBody(inner, ctx)
		}

	case strings.HasPrefix(expr, "z.union("), strings.HasPrefix(expr, "z.discriminatedUnion("):
		return parseFirstUnionMember(name, expr, ctx, depth)

	case strings.HasPrefix(expr, "z.optional("), strings.HasPrefix(expr, "z.nullable("):
		f.Optional = true
		inner := extractBalanced(expr[strings.Index(expr, "(")+1:], '(', ')')
		if inner != "" {
			f2 := parseFieldExpr(name, strings.TrimSpace(inner), ctx, depth+1)
			f2.Optional = true
			return f2
		}

	case strings.HasPrefix(expr, "z.uuid("):
		f.Kind = FieldString
		f.Format = FormatUUID

	case strings.HasPrefix(expr, "z.email("):
		f.Kind = FieldString
		f.Format = FormatEmail

	case strings.HasPrefix(expr, "z.url("):
		f.Kind = FieldString
		f.Format = FormatURL

	default:
		f.Kind = FieldUnknown
	}

	return f
}

func resolveEnumValues(expr string, ctx *parseCtx) []string {
	if idx := strings.Index(expr, "z.enum("); idx == -1 {
		return nil
	}
	after := strings.TrimSpace(expr[strings.Index(expr, "z.enum(")+len("z.enum("):])

	// z.enum([...]) — array literal
	if strings.HasPrefix(after, "[") {
		inner := extractBalanced(after, '[', ']')
		if inner != "" {
			return extractQuotedStrings(inner)
		}
	}

	// z.enum(TsEnumName, opts) — TypeScript enum reference
	firstArg := strings.TrimSpace(firstCallArg(after))
	// Strip any trailing ) from partial extraction
	firstArg = strings.TrimRight(firstArg, " )")
	if vals, ok := ctx.tsEnums[firstArg]; ok {
		return vals
	}
	return nil
}

func firstCallArg(s string) string {
	depth := 0
	for i, ch := range s {
		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return s[:i]
			}
			depth--
		case ',':
			if depth == 0 {
				return s[:i]
			}
		}
	}
	return s
}

func extractQuotedStrings(s string) []string {
	re := regexp.MustCompile(`['"]([^'"]+)['"]`)
	var vals []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		vals = append(vals, m[1])
	}
	return vals
}

// cleanChain strips non-type method calls from a Zod chain.
var stripMethods = []string{
	"refine", "superRefine", "transform", "default", "describe",
	"catch", "brand", "preprocess",
}

func cleanChain(expr string) string {
	// Special case: .pipe(z.xxx()) — we want to keep the inner z.xxx type.
	// Replace .pipe(z.string()...) with the inner type.
	for {
		pipeIdx := strings.Index(expr, ".pipe(")
		if pipeIdx == -1 {
			break
		}
		// extractUntilMatchingClose starts at depth=1 (just inside the `(`)
		inner := extractUntilMatchingClose(expr[pipeIdx+len(".pipe("):], '(', ')')
		if inner == "" {
			break
		}
		inner = strings.TrimSpace(inner)
		// If the pipe target is a z.xxx, use it as the type.
		if strings.HasPrefix(inner, "z.") {
			expr = inner
		} else {
			// Unknown pipe target — just strip the .pipe(...)
			callEnd := pipeIdx + len(".pipe(") + len(inner) + 1
			if callEnd > len(expr) {
				break
			}
			expr = expr[:pipeIdx] + expr[callEnd:]
		}
	}

	for {
		changed := false
		for _, m := range stripMethods {
			pat := "." + m + "("
			for {
				idx := strings.Index(expr, pat)
				if idx == -1 {
					break
				}
				inner := extractUntilMatchingClose(expr[idx+len(pat):], '(', ')')
				callEnd := idx + len(pat) + len(inner) + 1
				if callEnd > len(expr) {
					break
				}
				expr = expr[:idx] + expr[callEnd:]
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return strings.TrimSpace(expr)
}

// extractUntilMatchingClose finds the content up to the matching close bracket,
// starting at depth=1 (caller is already inside the opening bracket).
func extractUntilMatchingClose(s string, open, close rune) string {
	depth := 1
	for i, ch := range s {
		switch ch {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[:i]
			}
		}
	}
	return ""
}

// findFirstTopLevelSemicolon returns the index of the first `;` at bracket
// depth 0 (not inside any parens/braces/brackets).
func findFirstTopLevelSemicolon(s string) int {
	depth := 0
	inStr := false
	strChar := rune(0)
	for i, ch := range s {
		if inStr {
			if ch == strChar && (i == 0 || s[i-1] != '\\') {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			inStr = true
			strChar = ch
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ';':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ---- small helpers ----------------------------------------------------------

func extractBalanced(s string, open, close rune) string {
	start := -1
	depth := 0
	for i, ch := range s {
		switch ch {
		case open:
			if depth == 0 {
				start = i + 1
			}
			depth++
		case close:
			depth--
			if depth == 0 && start != -1 {
				return s[start:i]
			}
		}
	}
	return ""
}

func splitTopLevelEntries(s string) []string {
	var parts []string
	depth := 0
	inStr := false
	strChar := rune(0)
	start := 0
	for i, ch := range s {
		if inStr {
			if ch == strChar && (i == 0 || s[i-1] != '\\') {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			inStr = true
			strChar = ch
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

func findTopLevelColon(s string) int {
	depth := 0
	inStr := false
	strChar := rune(0)
	for i, ch := range s {
		if inStr {
			if ch == strChar {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			inStr = true
			strChar = ch
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ':':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isOptionalWrapper(expr string) bool {
	return strings.HasPrefix(expr, "z.optional(") || strings.HasPrefix(expr, "z.nullable(")
}

func unwrapOptional(expr string) string {
	for _, prefix := range []string{"z.optional(", "z.nullable("} {
		if strings.HasPrefix(expr, prefix) {
			inner := extractBalanced(expr[len(prefix):], '(', ')')
			if inner != "" {
				return strings.TrimSpace(inner)
			}
		}
	}
	return expr
}

func hasTrailingOptional(expr string) bool {
	return strings.Contains(expr, ".optional()") || strings.Contains(expr, ".nullable()")
}

func detectStringFormat(expr string) FieldFormat {
	switch {
	case strings.Contains(expr, ".datetime(") || strings.Contains(expr, "z.iso.datetime("):
		return FormatDatetime
	case strings.Contains(expr, ".email("):
		return FormatEmail
	case strings.Contains(expr, ".url("):
		return FormatURL
	case strings.Contains(expr, ".uuid("):
		return FormatUUID
	default:
		return FormatNone
	}
}

var literalRe = regexp.MustCompile(`z\.literal\(\s*['"]([^'"]*)['"]\s*\)`)

func extractLiteralValue(expr string) string {
	m := literalRe.FindStringSubmatch(expr)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func parseFirstUnionMember(name, expr string, ctx *parseCtx, depth int) SchemaField {
	var inner string
	if strings.HasPrefix(expr, "z.discriminatedUnion(") {
		inner = extractBalanced(expr[len("z.discriminatedUnion("):], '[', ']')
	} else {
		inner = extractBalanced(expr[len("z.union("):], '[', ']')
	}
	if inner == "" {
		return SchemaField{Name: name, Kind: FieldUnknown}
	}
	for _, m := range splitTopLevelEntries(inner) {
		m = strings.TrimSpace(m)
		if m != "" {
			return parseFieldExpr(name, m, ctx, depth+1)
		}
	}
	return SchemaField{Name: name, Kind: FieldUnknown}
}

func stripLineComments(src string) string {
	var b strings.Builder
	lines := strings.Split(src, "\n")
	for _, line := range lines {
		inStr := false
		strChar := rune(0)
		for i, ch := range line {
			if inStr {
				if ch == strChar && (i == 0 || line[i-1] != '\\') {
					inStr = false
				}
			} else {
				if ch == '"' || ch == '\'' || ch == '`' {
					inStr = true
					strChar = ch
				} else if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
					line = line[:i]
					break
				}
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

var headerFieldRe = regexp.MustCompile(`(?i)^(x-|authorization|content-type|accept|user-agent|referer|origin|cookie|correlation)`)

func looksLikeHeaderSchema(fields []SchemaField) bool {
	headerCount := 0
	for _, f := range fields {
		if headerFieldRe.MatchString(f.Name) {
			headerCount++
		}
	}
	return headerCount > 0 && headerCount >= len(fields)/2
}

// ProbeValueForField returns a suitable probe value for a field based on its schema.
func ProbeValueForField(f SchemaField) interface{} {
	switch f.Kind {
	case FieldEnum:
		if len(f.Enum) > 0 {
			return f.Enum[0]
		}
		return "probe-value"
	case FieldLiteral:
		return f.Literal
	case FieldBool:
		return true
	case FieldNumber:
		return 1
	case FieldArray:
		return []interface{}{}
	case FieldObject:
		obj := make(map[string]interface{})
		for _, child := range f.Children {
			if !child.Optional {
				obj[child.Name] = ProbeValueForField(child)
			}
		}
		return obj
	case FieldString:
		return probeStringValue(f.Name, f.Format)
	default:
		return probeStringValue(f.Name, FormatNone)
	}
}

func probeStringValue(name string, format FieldFormat) interface{} {
	switch format {
	case FormatDatetime:
		return "2024-01-01T00:00:00.000Z"
	case FormatEmail:
		return "probe@example.com"
	case FormatURL:
		return "https://example.com"
	case FormatUUID:
		return "00000000-0000-0000-0000-000000000001"
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "at") || strings.HasSuffix(lower, "time") ||
		strings.HasSuffix(lower, "date") || strings.HasSuffix(lower, "timestamp"):
		return "2024-01-01T00:00:00.000Z"
	case lower == "email" || strings.HasSuffix(lower, "email"):
		return "probe@example.com"
	case lower == "id" || strings.HasSuffix(lower, "id") || strings.HasSuffix(lower, "uuid"):
		return "00000000-0000-0000-0000-000000000001"
	case strings.HasSuffix(lower, "url") || strings.HasSuffix(lower, "uri"):
		return "https://example.com"
	default:
		return "probe-value"
	}
}
