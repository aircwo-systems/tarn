package apigateway

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Parameter mapping for HTTP_PROXY integrations on HTTP APIs. Keys and values
// follow the AWS "Transforming API requests and responses" reference for HTTP
// APIs: append:, overwrite: and remove: on header.<name> and
// querystring.<name>, plus overwrite:path.

const (
	mappingAppend    = "append"
	mappingOverwrite = "overwrite"
	mappingRemove    = "remove"

	mappingHeader      = "header"
	mappingQueryString = "querystring"
	mappingPath        = "path"
)

type mappingSource int

const (
	sourceStatic mappingSource = iota
	sourceHeader
	sourceQueryString
	sourcePath
	sourcePathParam
	sourceRequestID
	sourceAPIID
	sourceStage
)

// paramMapping is one parsed RequestParameters entry.
type paramMapping struct {
	key    string // the raw key, for messages
	action string // append, overwrite or remove
	target string // header, querystring or path
	name   string // header or query parameter name; empty for path
	source mappingSource
	arg    string // static value, or the header/query/path parameter name read
}

// mappingContext is what a mapping value can read: the request as received.
type mappingContext struct {
	input      *InvokeInput
	pathParams map[string]string
	requestID  string
}

// resolve returns the mapping's value and whether the source was present. A
// header or query parameter the request lacks resolves to absent, and the
// mapping is skipped.
func (m paramMapping) resolve(c mappingContext) (string, bool) {
	switch m.source {
	case sourceStatic:
		return m.arg, true
	case sourceHeader:
		values := c.input.Headers.Values(m.arg)
		if len(values) == 0 {
			return "", false
		}
		return strings.Join(values, ","), true
	case sourceQueryString:
		values, ok := c.input.Query[m.arg]
		if !ok || len(values) == 0 {
			return "", false
		}
		return strings.Join(values, ","), true
	case sourcePath:
		return normalizePath(c.input.Path), true
	case sourcePathParam:
		v, ok := c.pathParams[m.arg]
		return v, ok
	case sourceRequestID:
		return c.requestID, true
	case sourceAPIID:
		return c.input.APIID, true
	case sourceStage:
		return c.input.Stage, true
	}
	return "", false
}

// reservedHeaderPrefixes and reservedHeaders are the headers AWS does not let
// a mapping change.
var (
	reservedHeaderPrefixes = []string{"access-control-", "apigw-", "x-amz-", "x-amzn-"}
	reservedHeaders        = map[string]bool{
		"authorization":       true,
		"connection":          true,
		"content-encoding":    true,
		"content-length":      true,
		"content-location":    true,
		"forwarded":           true,
		"keep-alive":          true,
		"origin":              true,
		"proxy-authenticate":  true,
		"proxy-authorization": true,
		"te":                  true,
		"trailers":            true,
		"transfer-encoding":   true,
		"upgrade":             true,
		"x-forwarded-for":     true,
		"x-forwarded-host":    true,
		"x-forwarded-proto":   true,
		"via":                 true,
	}
)

func isReservedHeader(name string) bool {
	lower := strings.ToLower(name)
	if reservedHeaders[lower] {
		return true
	}
	for _, prefix := range reservedHeaderPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// parseHTTPProxyMappings validates RequestParameters for an HTTP_PROXY
// integration and returns them in the order they apply: removals, then
// overwrites, then appends, each sorted by key so the result is stable.
func parseHTTPProxyMappings(params map[string]string) ([]paramMapping, error) {
	if len(params) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	mappings := make([]paramMapping, 0, len(keys))
	// seen maps "target.lowercase-name" to the action already given for it.
	seen := map[string]string{}
	for _, key := range keys {
		m, err := parseMappingKey(key)
		if err != nil {
			return nil, err
		}
		if m.action != mappingRemove {
			if err := parseMappingValue(&m, params[key]); err != nil {
				return nil, err
			}
		}

		ident := m.target + "." + m.name
		if m.target == mappingHeader {
			ident = m.target + "." + strings.ToLower(m.name)
		}
		if prev, ok := seen[ident]; ok {
			if prev == m.action {
				return nil, fmt.Errorf("RequestParameters has more than one %s mapping for %s (names are case-insensitive)", m.action, ident)
			}
			if (prev == mappingOverwrite && m.action == mappingRemove) || (prev == mappingRemove && m.action == mappingOverwrite) {
				return nil, fmt.Errorf("RequestParameters cannot both overwrite and remove %s", ident)
			}
		}
		seen[ident] = m.action
		mappings = append(mappings, m)
	}

	rank := map[string]int{mappingRemove: 0, mappingOverwrite: 1, mappingAppend: 2}
	sort.SliceStable(mappings, func(i, j int) bool { return rank[mappings[i].action] < rank[mappings[j].action] })
	return mappings, nil
}

func parseMappingKey(key string) (paramMapping, error) {
	m := paramMapping{key: key}
	action, rest, ok := strings.Cut(key, ":")
	if !ok {
		return m, fmt.Errorf("invalid RequestParameters key %q: expected append:, overwrite: or remove: followed by header.<name>, querystring.<name> or path", key)
	}
	switch action {
	case mappingAppend, mappingOverwrite, mappingRemove:
		m.action = action
	default:
		return m, fmt.Errorf("invalid RequestParameters key %q: unknown action %q (use append, overwrite or remove)", key, action)
	}

	if rest == mappingPath {
		if action != mappingOverwrite {
			return m, fmt.Errorf("invalid RequestParameters key %q: the path can only be overwritten", key)
		}
		m.target = mappingPath
		return m, nil
	}

	target, name, ok := strings.Cut(rest, ".")
	if !ok || name == "" {
		return m, fmt.Errorf("invalid RequestParameters key %q: expected header.<name>, querystring.<name> or path after %q", key, action+":")
	}
	switch target {
	case mappingHeader:
		if !validHeaderName(name) {
			return m, fmt.Errorf("invalid RequestParameters key %q: %q is not a valid header name", key, name)
		}
		if isReservedHeader(name) {
			return m, fmt.Errorf("invalid RequestParameters key %q: header %s is reserved and cannot be mapped", key, name)
		}
	case mappingQueryString:
	default:
		return m, fmt.Errorf("invalid RequestParameters key %q: unknown target %q (use header, querystring or path)", key, target)
	}
	m.target = target
	m.name = name
	return m, nil
}

// parseMappingValue reads a mapping value: a static string, or exactly one
// supported $request or $context expression.
func parseMappingValue(m *paramMapping, value string) error {
	if !strings.HasPrefix(value, "$") {
		for _, marker := range []string{"$request.", "$context.", "$stageVariables.", "${"} {
			if strings.Contains(value, marker) {
				return fmt.Errorf("RequestParameters %q: value %q mixes text and an expression, which Tarn does not support", m.key, value)
			}
		}
		m.source = sourceStatic
		m.arg = value
		return nil
	}

	switch {
	case value == "$request.path":
		m.source = sourcePath
		return nil
	case value == "$context.requestId":
		m.source = sourceRequestID
		return nil
	case value == "$context.apiId":
		m.source = sourceAPIID
		return nil
	case value == "$context.stage":
		m.source = sourceStage
		return nil
	}

	for _, s := range []struct {
		prefix string
		source mappingSource
	}{
		{"$request.header.", sourceHeader},
		{"$request.querystring.", sourceQueryString},
		{"$request.path.", sourcePathParam},
	} {
		if name, ok := strings.CutPrefix(value, s.prefix); ok {
			if name == "" || strings.ContainsAny(name, " \t$") {
				return fmt.Errorf("RequestParameters %q: invalid source %q", m.key, value)
			}
			m.source = s.source
			m.arg = name
			return nil
		}
	}
	return fmt.Errorf("RequestParameters %q: unsupported source %q (Tarn supports static values, $request.header.*, $request.querystring.*, $request.path, $request.path.*, $context.requestId, $context.apiId and $context.stage)", m.key, value)
}

func validHeaderName(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return name != ""
}

// applyHeaderMappings changes out, the headers bound for the upstream.
func applyHeaderMappings(out http.Header, mappings []paramMapping, c mappingContext) {
	for _, m := range mappings {
		if m.target != mappingHeader {
			continue
		}
		switch m.action {
		case mappingRemove:
			out.Del(m.name)
		case mappingOverwrite:
			if v, ok := m.resolve(c); ok {
				out.Set(m.name, v)
			}
		case mappingAppend:
			if v, ok := m.resolve(c); ok {
				out.Add(m.name, v)
			}
		}
	}
}

// queryPair is one query parameter, kept in order with repeats. raw is the
// parameter as the caller spelled it ("a=b", a bare "flag", "x=%2F"), so a
// pair no mapping touches is forwarded byte for byte.
type queryPair struct{ key, raw string }

// applyQueryMappings applies the querystring mappings to rawQuery. Pairs a
// mapping does not touch keep their exact spelling and order; added pairs go
// at the end. Without any querystring mapping rawQuery is returned unchanged.
func applyQueryMappings(rawQuery string, mappings []paramMapping, c mappingContext) string {
	has := false
	for _, m := range mappings {
		if m.target == mappingQueryString {
			has = true
			break
		}
	}
	if !has {
		return rawQuery
	}

	var pairs []queryPair
	for _, part := range strings.Split(rawQuery, "&") {
		if part == "" {
			continue
		}
		k, _, _ := strings.Cut(part, "=")
		key, err := url.QueryUnescape(k)
		if err != nil {
			key = k
		}
		pairs = append(pairs, queryPair{key: key, raw: part})
	}

	for _, m := range mappings {
		if m.target != mappingQueryString {
			continue
		}
		switch m.action {
		case mappingRemove:
			pairs = removePairs(pairs, m.name)
		case mappingOverwrite:
			if v, ok := m.resolve(c); ok {
				pairs = append(removePairs(pairs, m.name), encodedPair(m.name, v))
			}
		case mappingAppend:
			if v, ok := m.resolve(c); ok {
				pairs = append(pairs, encodedPair(m.name, v))
			}
		}
	}

	raw := make([]string, len(pairs))
	for i, p := range pairs {
		raw[i] = p.raw
	}
	return strings.Join(raw, "&")
}

func encodedPair(key, value string) queryPair {
	return queryPair{key: key, raw: url.QueryEscape(key) + "=" + url.QueryEscape(value)}
}

func removePairs(pairs []queryPair, name string) []queryPair {
	out := pairs[:0]
	for _, p := range pairs {
		if p.key != name {
			out = append(out, p)
		}
	}
	return out
}

// pathOverwrite returns the overwrite:path value, if the integration has one
// and its source resolves.
func pathOverwrite(mappings []paramMapping, c mappingContext) (string, bool) {
	for _, m := range mappings {
		if m.target == mappingPath {
			return m.resolve(c)
		}
	}
	return "", false
}

// mergeMappingUpdate applies an UpdateIntegration's RequestParameters to the
// stored ones, as AWS does for HTTP APIs: a key with a value replaces or adds
// that mapping, a key sent with an empty value deletes it, and keys the update
// leaves out are kept. So an update cannot set an empty static value; create
// can. A remove: key always has an empty value, so it is added or kept, never
// deleted: an update cannot tell deleting one from keeping it.
func mergeMappingUpdate(stored, update map[string]string) map[string]string {
	out := make(map[string]string, len(stored)+len(update))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range update {
		if v == "" && !strings.HasPrefix(k, mappingRemove+":") {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}
