package apigatewayv1

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// invokeMockIntegration answers from the method's integration responses
// without calling a backend. The request template chooses the status code,
// as in {"statusCode": 200}, and the matching integration response supplies
// the headers and body. This is how REST APIs answer CORS preflights.
func (s *Service) invokeMockIntegration(api *types.RestAPI, input *InvokeInput, integ *types.RestIntegration, pathParams map[string]string, traceStart time.Time) (*InvokeOutput, error) {
	status := mockStatusCode(integ, input, pathParams)
	ir := s.selectIntegrationResponse(integ, status)

	out := &InvokeOutput{
		StatusCode: http.StatusInternalServerError,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"message": "Internal server error"}`),
	}
	if ir != nil {
		if code, err := strconv.Atoi(ir.StatusCode); err == nil {
			out.StatusCode = code
			out.Headers = integrationResponseHeaders(ir)
			out.Body = nil
			if tmpl := ir.ResponseTemplates["application/json"]; tmpl != "" {
				// Response templates see the integration response, which is
				// empty for a mock, not the client's request body.
				noBody := *input
				noBody.Body = nil
				out.Body = []byte(evaluateVTL(tmpl, &noBody, pathParams))
				out.Headers["Content-Type"] = "application/json"
			}
		}
	}

	if s.traceStore != nil {
		correlationID := tracesvc.CorrelationIDFromHeaders(input.Headers)
		if correlationID == "" {
			correlationID = tracesvc.NewCorrelationID()
		}
		s.recordTrace(input, api, traceStart, time.Now(), correlationID, out.StatusCode, nil)
	}
	return out, nil
}

// mockStatusCode reads statusCode from the evaluated request template,
// defaulting to 200 when there is no template or it names no status.
func mockStatusCode(integ *types.RestIntegration, input *InvokeInput, pathParams map[string]string) int {
	contentType, _, _ := strings.Cut(input.Headers.Get("Content-Type"), ";")
	tmpl := integ.RequestTemplates[strings.TrimSpace(contentType)]
	if tmpl == "" {
		tmpl = integ.RequestTemplates["application/json"]
	}
	if tmpl == "" {
		return http.StatusOK
	}
	var req struct {
		StatusCode int `json:"statusCode"`
	}
	if err := json.Unmarshal([]byte(evaluateVTL(tmpl, input, pathParams)), &req); err != nil || req.StatusCode == 0 {
		return http.StatusOK
	}
	return req.StatusCode
}

// selectIntegrationResponse picks the integration response for a backend
// status code: the first whose selectionPattern matches it, otherwise the
// default response with no pattern. It returns nil when neither exists.
func (s *Service) selectIntegrationResponse(integ *types.RestIntegration, status int) *types.RestIntegrationResponse {
	responses, err := s.store.ListIntegrationResponses(integ.RestAPIID, integ.ResourceID, integ.MethodHTTPMethod)
	if err != nil {
		return nil
	}
	code := strconv.Itoa(status)
	var fallback *types.RestIntegrationResponse
	for _, ir := range responses {
		if ir.SelectionPattern == "" {
			if fallback == nil {
				fallback = ir
			}
			continue
		}
		re, err := regexp.Compile("^(?:" + ir.SelectionPattern + ")$")
		if err == nil && re.MatchString(code) {
			return ir
		}
	}
	return fallback
}

// integrationResponseHeaders returns the static header values an integration
// response maps onto the method response. Mappings from integration response
// headers or body are not supported and are skipped.
func integrationResponseHeaders(ir *types.RestIntegrationResponse) map[string]string {
	headers := map[string]string{}
	for param, value := range ir.ResponseParameters {
		name, ok := strings.CutPrefix(param, "method.response.header.")
		if !ok || len(value) < 2 || value[0] != '\'' || value[len(value)-1] != '\'' {
			continue
		}
		headers[name] = value[1 : len(value)-1]
	}
	return headers
}
