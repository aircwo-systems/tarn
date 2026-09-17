package admin

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	infrasvc "github.com/aircwo-systems/tarn/internal/infrastructure"
)

// secretValueLookup resolves an env value that names a secret (by name or ARN)
// to the secret's name and string value.
type secretValueLookup func(ref string) (name, value string, ok bool)

// serviceURLPattern finds URLs embedded anywhere in a value, including inside
// JSON documents such as a secret holding {"baseUrl": "https://..."}.
var serviceURLPattern = regexp.MustCompile(`(?i)\b(?:https?|tcp)://[^\s"'<>\\,]+`)

// isUserServiceKind reports whether a probe kind describes a generic service
// (as registered from the console) rather than a specific database protocol.
func isUserServiceKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "http", "https", "tcp":
		return true
	default:
		return false
	}
}

func isLinkableProbeKind(kind string) bool {
	return isDatabaseKind(kind) || isUserServiceKind(kind)
}

// inferURLConnectionHints extracts host/port hints from every URL in value.
func inferURLConnectionHints(value, source string) []connectionHint {
	matches := serviceURLPattern.FindAllString(value, -1)
	hints := make([]connectionHint, 0, len(matches))
	for _, raw := range matches {
		u, err := url.Parse(strings.TrimRight(raw, ".);]}"))
		if err != nil || u.Hostname() == "" {
			continue
		}
		port := parseConnectionPort(u.Port())
		if port == 0 {
			switch strings.ToLower(u.Scheme) {
			case "https":
				port = 443
			case "http":
				port = 80
			default:
				continue
			}
		}
		hints = append(hints, connectionHint{kind: "http", host: u.Hostname(), port: port, source: source})
	}
	return hints
}

// inferSecretConnectionHints finds hints inside a secret's value: URLs anywhere,
// plus host/port pairs in JSON secrets ({"host": "10.0.0.5", "port": 8080}).
func inferSecretConnectionHints(secretName, value string) []connectionHint {
	source := "secret:" + secretName
	hints := inferURLConnectionHints(value, source)

	var doc map[string]any
	if json.Unmarshal([]byte(value), &doc) != nil {
		return hints
	}
	host := ""
	for _, key := range []string{"host", "hostname", "HOST", "Host"} {
		if v, ok := doc[key].(string); ok && strings.TrimSpace(v) != "" {
			host = strings.TrimSpace(v)
			break
		}
	}
	if host == "" {
		return hints
	}
	port := 0
	for _, key := range []string{"port", "PORT", "Port"} {
		switch v := doc[key].(type) {
		case float64:
			port = int(v)
		case string:
			port, _ = strconv.Atoi(strings.TrimSpace(v))
		}
		if port != 0 {
			break
		}
	}
	kind := normalizeDatabaseKind(stringField(doc, "engine"))
	if port == 0 {
		port = defaultPortForKind(kind)
	}
	if port > 0 {
		hints = append(hints, connectionHint{kind: kind, host: host, port: port, source: source})
	}
	return hints
}

func stringField(doc map[string]any, key string) string {
	v, _ := doc[key].(string)
	return v
}

// serviceHintMatches links a hint to a probe. Database probes must agree on the
// database kind; user-registered services match on host and port alone, so an
// http hint can reach a service registered as https or tcp and vice versa.
func serviceHintMatches(probe infrasvc.ProbeResult, hint connectionHint) bool {
	if isUserServiceKind(probe.Kind) {
		return hint.port == probe.Port && connectionHostsMatch(probe.Host, hint.host)
	}
	if hint.kind == "http" {
		return false
	}
	return probeMatchesHint(probe, hint)
}

// preferLinkEvidence reports whether a candidate link description should replace
// the current one: secret evidence beats env, then the lower source name wins.
func preferLinkEvidence(evidence, source, currentEvidence, currentSource string) bool {
	if (evidence == "secret") != (currentEvidence == "secret") {
		return evidence == "secret"
	}
	return source < currentSource
}
