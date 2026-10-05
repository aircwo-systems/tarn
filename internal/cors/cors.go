// Package cors evaluates browser cross-origin requests against the CORS
// policies AWS services let users configure: S3 bucket CORS rules and API
// Gateway HTTP API corsConfiguration. Each service decides which policy
// applies and how to answer a refusal; this package does the matching and
// builds the Access-Control-* response headers.
package cors

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// Policy is one set of allowed origins, methods and headers.
type Policy struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	MaxAge           int
	AllowCredentials bool
}

// Request is the CORS-relevant part of an incoming HTTP request.
type Request struct {
	Origin string
	// Method is Access-Control-Request-Method for a preflight, otherwise the
	// request's own method.
	Method string
	// Headers lists Access-Control-Request-Headers, lowercased. Empty for
	// non-preflight requests.
	Headers   []string
	Preflight bool
}

// FromHTTP extracts the CORS view of a request. ok is false when the request
// carries no Origin header and is therefore not a cross-origin request.
func FromHTTP(method string, h http.Header) (req Request, ok bool) {
	origin := h.Get("Origin")
	if origin == "" {
		return Request{}, false
	}
	req = Request{Origin: origin, Method: method}
	if method == http.MethodOptions {
		if requested := h.Get("Access-Control-Request-Method"); requested != "" {
			req.Preflight = true
			req.Method = requested
			req.Headers = splitHeaderList(h.Values("Access-Control-Request-Headers"))
		}
	}
	return req, true
}

// AllowsOrigin reports whether origin matches one of the allowed origins.
// An entry may contain a single "*" wildcard, such as "https://*.example.com".
func (p Policy) AllowsOrigin(origin string) bool {
	for _, pattern := range p.AllowOrigins {
		if matchWildcard(pattern, origin) {
			return true
		}
	}
	return false
}

// AllowsMethod reports whether method is allowed. A "*" entry allows any method.
func (p Policy) AllowsMethod(method string) bool {
	for _, m := range p.AllowMethods {
		if m == "*" || strings.EqualFold(m, method) {
			return true
		}
	}
	return false
}

// AllowsHeaders reports whether every requested header matches an allowed
// header. Allowed headers may contain a single "*" wildcard.
func (p Policy) AllowsHeaders(headers []string) bool {
	for _, h := range headers {
		allowed := false
		for _, pattern := range p.AllowHeaders {
			if matchWildcard(strings.ToLower(pattern), h) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

// AllowsPreflight reports whether the policy permits a preflight request.
func (p Policy) AllowsPreflight(req Request) bool {
	return p.AllowsOrigin(req.Origin) && p.AllowsMethod(req.Method) && p.AllowsHeaders(req.Headers)
}

// ResponseHeaders returns the headers to send for req, which the caller has
// already matched against the policy. Preflight-only headers are included
// when req is a preflight.
func (p Policy) ResponseHeaders(req Request) http.Header {
	h := http.Header{}
	if p.allowsAnyOrigin() && !p.AllowCredentials {
		h.Set("Access-Control-Allow-Origin", "*")
	} else {
		h.Set("Access-Control-Allow-Origin", req.Origin)
		h.Add("Vary", "Origin")
	}
	if p.AllowCredentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
	if !req.Preflight {
		if len(p.ExposeHeaders) > 0 {
			h.Set("Access-Control-Expose-Headers", strings.Join(p.ExposeHeaders, ", "))
		}
		return h
	}
	if p.AllowsMethod("*") {
		h.Set("Access-Control-Allow-Methods", req.Method)
	} else {
		h.Set("Access-Control-Allow-Methods", strings.Join(p.AllowMethods, ", "))
	}
	if len(req.Headers) > 0 {
		h.Set("Access-Control-Allow-Headers", strings.Join(req.Headers, ", "))
	}
	if p.MaxAge > 0 {
		h.Set("Access-Control-Max-Age", strconv.Itoa(p.MaxAge))
	}
	h.Add("Vary", "Access-Control-Request-Method")
	h.Add("Vary", "Access-Control-Request-Headers")
	return h
}

// IsCORSHeader reports whether name is an Access-Control-* response header.
func IsCORSHeader(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "access-control-")
}

func (p Policy) allowsAnyOrigin() bool {
	return slices.Contains(p.AllowOrigins, "*")
}

// matchWildcard matches value against pattern, where pattern may contain one
// "*" standing for any run of characters.
func matchWildcard(pattern, value string) bool {
	prefix, suffix, found := strings.Cut(pattern, "*")
	if !found {
		return pattern == value
	}
	return len(value) >= len(prefix)+len(suffix) &&
		strings.HasPrefix(value, prefix) &&
		strings.HasSuffix(value, suffix)
}

func splitHeaderList(values []string) []string {
	var out []string
	for _, v := range values {
		for part := range strings.SplitSeq(v, ",") {
			if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
