package trace

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ExternalSpanFromRequest derives a caller span for direct HTTP clients such as
// browsers, frontend apps, Postman, curl, or SDK clients.
func ExternalSpanFromRequest(headers http.Header, remoteAddr string) *Span {
	if headers == nil {
		return externalSpanFromValues("", "", "", "", remoteAddr)
	}
	return externalSpanFromValues(
		strings.TrimSpace(headers.Get("Origin")),
		strings.TrimSpace(headers.Get("Referer")),
		strings.TrimSpace(headers.Get("User-Agent")),
		strings.TrimSpace(headers.Get("X-Tarn-Source-Name")),
		remoteAddr,
	)
}

func externalSpanFromValues(origin, referer, userAgent, explicitName, remoteAddr string) *Span {
	name, kind := resolveExternalSourceName(origin, referer, userAgent, explicitName)
	clientIP := extractClientIP(remoteAddr)
	if name == "" && clientIP == "" {
		return nil
	}
	if name == "" {
		name = clientIP
	}

	meta := map[string]string{
		"sourceKind": kind,
	}
	if origin != "" {
		meta["origin"] = origin
	}
	if referer != "" {
		meta["referer"] = referer
	}
	if userAgent != "" {
		meta["userAgent"] = userAgent
	}
	if clientIP != "" {
		meta["clientIp"] = clientIP
	}

	return &Span{
		Kind:   "external",
		Name:   name,
		Status: "ok",
		Meta:   meta,
	}
}

func resolveExternalSourceName(origin, referer, userAgent, explicitName string) (string, string) {
	if explicit := strings.TrimSpace(explicitName); explicit != "" {
		return explicit, "named"
	}

	originHost := requestHost(origin)
	refererHost := requestHost(referer)
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	browser := isBrowserUserAgent(ua)

	if originHost != "" {
		if browser {
			return originHost, "frontend"
		}
		return originHost, "external"
	}
	if refererHost != "" {
		if browser {
			return refererHost, "frontend"
		}
		return refererHost, "external"
	}

	switch {
	case strings.Contains(ua, "postmanruntime"):
		return "Postman", "postman"
	case strings.Contains(ua, "insomnia"):
		return "Insomnia", "insomnia"
	case strings.Contains(ua, "curl/"):
		return "curl", "cli"
	case strings.Contains(ua, "python-requests"):
		return "python-requests", "sdk"
	case strings.Contains(ua, "go-http-client"):
		return "go-http-client", "sdk"
	case strings.Contains(ua, "axios/"):
		return "axios", "sdk"
	case strings.Contains(ua, "undici"):
		return "undici", "sdk"
	case strings.Contains(ua, "node-fetch"):
		return "node-fetch", "sdk"
	case browser:
		return "Browser", "browser"
	default:
		return "", "external"
	}
}

func requestHost(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Host != "" {
		return parsed.Host
	}
	return ""
}

func extractClientIP(remoteAddr string) string {
	host := strings.TrimSpace(remoteAddr)
	if host == "" {
		return ""
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if net.ParseIP(host) == nil {
		return ""
	}
	return host
}

func isBrowserUserAgent(ua string) bool {
	if ua == "" {
		return false
	}
	return strings.Contains(ua, "mozilla/") ||
		strings.Contains(ua, "applewebkit/") ||
		strings.Contains(ua, "chrome/") ||
		strings.Contains(ua, "safari/") ||
		strings.Contains(ua, "firefox/")
}
