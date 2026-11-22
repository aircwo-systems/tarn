package api

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/aircwo-systems/tarn/internal/apigateway"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestHTTPAPILambdaProxyResponsePreservesRepeatedHeaders(t *testing.T) {
	for _, tc := range []struct {
		version string
		payload string
		cookies []string
	}{
		{
			version: "2.0",
			payload: `{"statusCode":201,"headers":{"set-cookie":"legacy=1; Path=/","access-control-allow-origin":"https://other.example"},"cookies":["session=abc; Path=/; HttpOnly","theme=dark; Expires=Wed, 09 Jun 2027 10:18:14 GMT"],"body":"saved"}`,
			cookies: []string{"legacy=1; Path=/", "session=abc; Path=/; HttpOnly", "theme=dark; Expires=Wed, 09 Jun 2027 10:18:14 GMT"},
		},
		{
			version: "1.0",
			payload: `{"statusCode":201,"headers":{"set-cookie":"session=abc; Path=/"},"multiValueHeaders":{"Set-Cookie":["session=abc; Path=/","theme=dark; Path=/"],"Access-Control-Allow-Origin":["https://other.example","https://another.example"]},"body":"saved"}`,
			cookies: []string{"session=abc; Path=/", "theme=dark; Path=/"},
		},
	} {
		t.Run(tc.version, func(t *testing.T) {
			h, svc := newHTTPAPITestHandler(t, func(context.Context, *types.InvokeInput) (*types.InvokeOutput, error) {
				return &types.InvokeOutput{StatusCode: 200, Payload: []byte(tc.payload)}, nil
			})
			api, err := svc.CreateAPI("sessions", "", "HTTP", "", nil, &types.APIGatewayCORS{AllowOrigins: []string{"https://staff.example"}})
			if err != nil {
				t.Fatal(err)
			}
			integration, err := svc.CreateIntegration(api.APIID, apigateway.IntegrationCreateInput{
				IntegrationURI:       "arn:aws:lambda:us-east-1:000000000000:function:sessions",
				PayloadFormatVersion: tc.version,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CreateRoute(api.APIID, apigateway.RouteCreateInput{
				RouteKey: "POST /session",
				Target:   "integrations/" + integration.IntegrationID,
			}); err != nil {
				t.Fatal(err)
			}

			resp := compatibilityRequest(h, http.MethodPost, "/_apigateway/"+api.APIID+"/$default/session", "", "")
			if resp.Code != http.StatusCreated || resp.Body.String() != "saved" {
				t.Fatalf("got %d %q, want 201 saved", resp.Code, resp.Body)
			}
			if got := resp.Result().Header.Values("Set-Cookie"); !slices.Equal(got, tc.cookies) {
				t.Errorf("Set-Cookie = %q, want %q", got, tc.cookies)
			}
			if got := resp.Result().Header.Values("Access-Control-Allow-Origin"); len(got) != 0 {
				t.Errorf("unexpected CORS headers from Lambda: %q", got)
			}
		})
	}
}
