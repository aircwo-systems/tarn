package apigateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aircwo-systems/tarn/internal/reqlog"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// RequestTooLargeMessage is the error message for a request body over
// MaxPayloadBytes.
const RequestTooLargeMessage = "Request Entity Too Large"

const (
	integrationTypeHTTPProxy = "HTTP_PROXY"
	connectionTypeInternet   = "INTERNET"
	connectionTypeVPCLink    = "VPC_LINK"
	integrationMethodAny     = "ANY"
	payloadFormatVersion1    = "1.0"

	// MaxPayloadBytes is API Gateway's HTTP API payload limit.
	MaxPayloadBytes     = 10 << 20
	maxHTTPProxyPayload = MaxPayloadBytes

	// Error bodies for failed HTTP_PROXY calls. AWS's exact wording is not
	// confirmed (PRD open question 4); keep the text in one place.
	msgIntegrationTimeout     = "Endpoint request timed out"
	msgIntegrationUnreachable = "Bad Gateway"
	msgRequestTooLarge        = RequestTooLargeMessage
	msgResponseTooLarge       = "Integration response too large"
)

var httpProxyMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodHead: true, http.MethodOptions: true, integrationMethodAny: true,
}

// hopByHopHeaders are never forwarded in either direction.
var hopByHopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Connection", "TE", "Trailer", "Transfer-Encoding", "Upgrade",
	"Proxy-Authorization", "Proxy-Authenticate",
}

// newHTTPProxyTransport is the transport for HTTP_PROXY integrations. It is
// built from scratch so it ignores HTTP_PROXY environment settings, and it
// leaves Content-Encoding alone so compressed bodies pass through unchanged.
func newHTTPProxyTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		DisableCompression:    true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// normalizeIntegrationMethod validates IntegrationMethod for HTTP_PROXY.
func normalizeIntegrationMethod(method string) (string, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return integrationMethodAny, nil
	}
	if !httpProxyMethods[method] {
		return "", fmt.Errorf("invalid IntegrationMethod %q (use GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS or ANY)", method)
	}
	return method, nil
}

// normalizeConnectionType validates ConnectionType for any integration.
func normalizeConnectionType(connectionType string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(connectionType)) {
	case "", connectionTypeInternet:
		return connectionTypeInternet, nil
	case connectionTypeVPCLink:
		return "", errors.New("ConnectionType VPC_LINK is not supported: Tarn has no VPC links yet. Use ConnectionType INTERNET with an http:// URL, such as Tarn's /_ecs/{account}/{cluster}/{service}/ proxy")
	default:
		return "", fmt.Errorf("invalid ConnectionType %q (use INTERNET)", connectionType)
	}
}

// validateHTTPProxyURI checks that uri is an absolute http or https URL once
// its {name} path variables are filled.
func validateHTTPProxyURI(uri string) error {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return errors.New("IntegrationUri is required")
	}
	prefix, _, suffix := splitURIPath(uri)
	if strings.ContainsAny(prefix, "{}") || strings.ContainsAny(suffix, "{}") {
		return fmt.Errorf("invalid IntegrationUri %q: {name} variables are allowed only in the URL's path", uri)
	}
	u, err := url.Parse(placeholderPathVariables(uri))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid IntegrationUri %q: HTTP_PROXY integrations need an absolute http:// or https:// URL", uri)
	}
	return nil
}

// splitURIPath splits an absolute URI into scheme and authority, path, and
// query and fragment, without decoding anything.
func splitURIPath(uri string) (prefix, path, suffix string) {
	authorityStart := 0
	if i := strings.Index(uri, "://"); i >= 0 {
		authorityStart = i + len("://")
	}
	pathStart := len(uri)
	if i := strings.IndexAny(uri[authorityStart:], "/?#"); i >= 0 {
		pathStart = authorityStart + i
	}
	pathEnd := len(uri)
	if i := strings.IndexAny(uri[pathStart:], "?#"); i >= 0 {
		pathEnd = pathStart + i
	}
	return uri[:pathStart], uri[pathStart:pathEnd], uri[pathEnd:]
}

// placeholderPathVariables replaces each {name} in the URI's path with "x",
// so the URI can be parsed for validation.
func placeholderPathVariables(uri string) string {
	return replacePathVariables(uri, func(string) string { return "x" })
}

// fillPathVariables replaces each {name} or {name+} in the URI's path with
// the matched route's path parameter. A greedy value keeps its slashes; each
// segment is escaped, so a value cannot add a query or change the host. A
// variable the route did not capture (a $default or exact-match route has
// none) becomes empty. Braces outside the path are left alone; validation
// rejects them.
func fillPathVariables(uri string, params map[string]string) string {
	return replacePathVariables(uri, func(name string) string {
		segments := strings.Split(params[name], "/")
		for i, seg := range segments {
			segments[i] = url.PathEscape(seg)
		}
		return strings.Join(segments, "/")
	})
}

func replacePathVariables(uri string, value func(name string) string) string {
	prefix, path, suffix := splitURIPath(uri)
	var b strings.Builder
	b.WriteString(prefix)
	for {
		start := strings.IndexByte(path, '{')
		if start < 0 {
			break
		}
		end := strings.IndexByte(path[start:], '}')
		if end < 0 {
			break
		}
		end += start
		b.WriteString(path[:start])
		b.WriteString(value(strings.TrimSuffix(path[start+1:end], "+")))
		path = path[end+1:]
	}
	b.WriteString(path)
	b.WriteString(suffix)
	return b.String()
}

// httpProxyResult is what one HTTP_PROXY call did, for its trace and log.
type httpProxyResult struct {
	out            *InvokeOutput
	upstream       *url.URL
	upstreamStatus int    // 0 when no response arrived
	cause          string // why the call failed, when it did
	local          bool
}

func (s *Service) invokeHTTPProxyIntegration(ctx context.Context, integration *types.APIGatewayIntegration, input *InvokeInput, route *types.APIGatewayRoute, pathParams map[string]string) *InvokeOutput {
	start := time.Now()
	correlationID := tracesvc.CorrelationIDFromHeaders(input.Headers)
	inboundHasCorrelation := correlationID != ""
	if !inboundHasCorrelation {
		correlationID = tracesvc.NewCorrelationID()
	}

	res := s.forwardHTTPProxy(ctx, integration, input, pathParams, correlationID, inboundHasCorrelation)
	upstreamMs := time.Since(start).Milliseconds()

	target := integration.IntegrationURI
	if res.upstream != nil {
		target = (&url.URL{Scheme: res.upstream.Scheme, Host: res.upstream.Host, Path: res.upstream.Path}).String()
	}
	reqlog.Add(ctx, "apiId", input.APIID)
	reqlog.Add(ctx, "stage", input.Stage)
	reqlog.Add(ctx, "route", route.RouteKey)
	reqlog.Add(ctx, "integration", integration.IntegrationID)
	reqlog.Add(ctx, "upstream", target)
	if res.upstreamStatus != 0 {
		reqlog.Add(ctx, "upstreamStatus", strconv.Itoa(res.upstreamStatus))
	}
	reqlog.Add(ctx, "upstreamMs", strconv.FormatInt(upstreamMs, 10))
	reqlog.Add(ctx, "correlationId", correlationID)
	if res.cause != "" {
		reqlog.Add(ctx, "cause", res.cause)
	}

	if s.traceStore != nil {
		kind, name := "http", target
		if res.upstream != nil {
			name = res.upstream.Host
			if res.local {
				if service, ok := ecsServiceFromPath(res.upstream.Path); ok {
					kind, name = "ecs", service
				}
			}
		}
		s.recordTrace(input, start, time.Now(), correlationID, res.out.StatusCode, []tracesvc.Span{
			{Kind: kind, Name: name, DurationMs: upstreamMs, Status: tracesvc.StatusForHTTP(res.out.StatusCode)},
		})
	}
	return res.out
}

// ecsServiceFromPath returns the service name in a /_ecs/{account}/{cluster}/{service}/... path.
func ecsServiceFromPath(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "/_ecs/")
	if !ok {
		return "", false
	}
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 || parts[2] == "" {
		return "", false
	}
	return parts[2], true
}

func (s *Service) forwardHTTPProxy(ctx context.Context, integration *types.APIGatewayIntegration, input *InvokeInput, pathParams map[string]string, correlationID string, inboundHasCorrelation bool) httpProxyResult {
	if len(input.Body) > maxHTTPProxyPayload {
		return httpProxyResult{out: gatewayError(http.StatusRequestEntityTooLarge, msgRequestTooLarge), cause: "request body over 10 MB"}
	}

	mappings, err := parseHTTPProxyMappings(integration.RequestParameters)
	if err != nil {
		// Create and update validate mappings, so this only happens to state
		// written by an older build.
		return httpProxyResult{out: gatewayError(http.StatusInternalServerError, "Internal Server Error"), cause: err.Error()}
	}
	mc := mappingContext{input: input, pathParams: pathParams, requestID: correlationID}

	upstream, err := url.Parse(fillPathVariables(strings.TrimSpace(integration.IntegrationURI), pathParams))
	if err != nil {
		return httpProxyResult{out: gatewayError(http.StatusInternalServerError, "Internal Server Error"), cause: "invalid IntegrationUri: " + err.Error()}
	}
	rawQuery := upstream.RawQuery
	if input.RawQuery != "" {
		if rawQuery != "" {
			rawQuery += "&"
		}
		rawQuery += input.RawQuery
	}
	upstream.RawQuery = applyQueryMappings(rawQuery, mappings, mc)
	if p, ok := pathOverwrite(mappings, mc); ok {
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		upstream.Path = p
		upstream.RawPath = ""
	}

	method := integration.IntegrationMethod
	if method == "" || method == integrationMethodAny {
		method = strings.ToUpper(input.Method)
	}

	headers := forwardRequestHeaders(input, correlationID, inboundHasCorrelation)
	applyHeaderMappings(headers, mappings, mc)

	timeout := time.Duration(integration.TimeoutInMillis) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader = http.NoBody
	if len(input.Body) > 0 {
		body = bytes.NewReader(input.Body)
	}
	req, err := http.NewRequestWithContext(callCtx, method, upstream.String(), body)
	if err != nil {
		return httpProxyResult{out: gatewayError(http.StatusInternalServerError, "Internal Server Error"), upstream: upstream, cause: err.Error()}
	}
	req.Header = headers
	req.Host = upstream.Host
	req.ContentLength = int64(len(input.Body))

	res := httpProxyResult{upstream: upstream}
	var rt http.RoundTripper = s.httpTransport
	if s.local.Matches(upstream) {
		rt, res.local = s.local, true
	}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		res.out, res.cause = upstreamFailure(callCtx, err)
		return res
	}
	defer resp.Body.Close()
	res.upstreamStatus = resp.StatusCode

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPProxyPayload+1))
	if err == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		// TimeoutInMillis covers the whole call, body included.
		err = context.DeadlineExceeded
	}
	if err != nil {
		res.out, res.cause = upstreamFailure(callCtx, err)
		return res
	}
	if len(respBody) > maxHTTPProxyPayload {
		res.out, res.cause = gatewayError(http.StatusBadGateway, msgResponseTooLarge), "response body over 10 MB"
		return res
	}

	respHeaders := resp.Header.Clone()
	removeHopByHop(respHeaders)
	res.out = &InvokeOutput{StatusCode: resp.StatusCode, Headers: respHeaders, Body: respBody}
	return res
}

// forwardRequestHeaders copies the inbound headers the upstream should see.
func forwardRequestHeaders(input *InvokeInput, correlationID string, inboundHasCorrelation bool) http.Header {
	headers := input.Headers.Clone()
	if headers == nil {
		headers = http.Header{}
	}
	removeHopByHop(headers)
	headers.Del("Host")
	headers.Del("Content-Length")

	if ip, _, err := net.SplitHostPort(input.RemoteAddr); err == nil {
		if prior := headers.Values("X-Forwarded-For"); len(prior) > 0 {
			ip = strings.Join(prior, ", ") + ", " + ip
		}
		headers.Set("X-Forwarded-For", ip)
	}
	headers.Set("X-Forwarded-Proto", "http")
	if !inboundHasCorrelation {
		headers.Set("X-Correlation-Id", correlationID)
	}
	return headers
}

// removeHopByHop drops hop-by-hop headers, including any the Connection header names.
func removeHopByHop(h http.Header) {
	for _, value := range h.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				h.Del(name)
			}
		}
	}
	for _, name := range hopByHopHeaders {
		h.Del(name)
	}
}

// upstreamFailure maps a failed call to API Gateway's response and a short cause.
func upstreamFailure(ctx context.Context, err error) (*InvokeOutput, string) {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return gatewayError(http.StatusGatewayTimeout, msgIntegrationTimeout), "timeout"
	}
	return gatewayError(http.StatusBadGateway, msgIntegrationUnreachable), failureCause(err)
}

func failureCause(err error) string {
	var dnsErr *net.DNSError
	var tlsErr *tls.RecordHeaderError
	var certErr *tls.CertificateVerificationError
	switch {
	case errors.Is(err, errLocalDispatchLoop):
		return "request loop"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection reset"
	case errors.As(err, &dnsErr):
		return "dns failure"
	case errors.As(err, &tlsErr), errors.As(err, &certErr):
		return "tls failure"
	case errors.Is(err, context.Canceled):
		return "caller cancelled"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "timeout"
	}
	return err.Error()
}

func gatewayError(status int, message string) *InvokeOutput {
	return &InvokeOutput{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"message":"` + message + `"}`),
	}
}
