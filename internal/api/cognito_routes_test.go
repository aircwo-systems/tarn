package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cognitohandler "github.com/aircwo-systems/tarn/internal/api/cognito"
	"github.com/aircwo-systems/tarn/internal/cognito"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/logs"
)

// newCognitoTestServer builds a multi-account server whose accounts share one
// Cognito index, as in `tarn start`.
func newCognitoTestServer(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	idx := cognito.NewIndex()
	registry := NewHandlerRegistry(func(accountID string) (*AccountBundle, error) {
		acctCfg := cfg.ForAccount(accountID)
		b := newTestBundle(t, acctCfg)
		svc := cognito.NewService(acctCfg, idx)
		b.handlers.Cognito = cognitohandler.NewHandler(svc)
		b.handlers.Admin.SetCognitoService(svc)
		return b, nil
	})
	s := NewServer(cfg, registry, logs.NewService(cfg), nil)
	s.SetCognitoIndex(idx)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return s.withLogging(mux)
}

const otherAccountAuth = "AWS4-HMAC-SHA256 Credential=111111111111/20261006/us-east-1/cognito-idp/aws4_request, SignedHeaders=host, Signature=x"

func cognitoCall(t *testing.T, h http.Handler, action, auth string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "AWSCognitoIdentityProviderService."+action)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func mustCognito(t *testing.T, h http.Handler, action, auth string, body any) map[string]any {
	t.Helper()
	code, out := cognitoCall(t, h, action, auth, body)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d %v", action, code, out)
	}
	return out
}

func TestCognitoUnsignedCallsResolveOwningAccount(t *testing.T) {
	h := newCognitoTestServer(t)

	pool := mustCognito(t, h, "CreateUserPool", otherAccountAuth, map[string]any{"PoolName": "app"})["UserPool"].(map[string]any)
	poolID := pool["Id"].(string)
	if !strings.Contains(pool["Arn"].(string), ":111111111111:") {
		t.Fatalf("pool created in wrong account: %v", pool["Arn"])
	}
	client := mustCognito(t, h, "CreateUserPoolClient", otherAccountAuth, map[string]any{
		"UserPoolId": poolID, "ClientName": "web", "ExplicitAuthFlows": []string{"ALLOW_USER_PASSWORD_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"},
	})["UserPoolClient"].(map[string]any)
	clientID := client["ClientId"].(string)
	mustCognito(t, h, "AdminCreateUser", otherAccountAuth, map[string]any{
		"UserPoolId": poolID, "Username": "alice", "MessageAction": "SUPPRESS",
	})
	mustCognito(t, h, "AdminSetUserPassword", otherAccountAuth, map[string]any{
		"UserPoolId": poolID, "Username": "alice", "Password": "Passw0rd!", "Permanent": true,
	})

	// A signed admin call stays in the signer's account, which does not own
	// the pool...
	if code, _ := cognitoCall(t, h, "DescribeUserPool", "AWS4-HMAC-SHA256 Credential=222222222222/20261006/us-east-1/cognito-idp/aws4_request", map[string]any{"UserPoolId": poolID}); code != http.StatusBadRequest {
		t.Fatalf("other account resolved the pool: status %d", code)
	}
	// ...but unsigned public calls find it through the ClientId and token.
	auth := mustCognito(t, h, "InitiateAuth", "", map[string]any{
		"ClientId": clientID, "AuthFlow": "USER_PASSWORD_AUTH",
		"AuthParameters": map[string]string{"USERNAME": "alice", "PASSWORD": "Passw0rd!"},
	})["AuthenticationResult"].(map[string]any)
	user := mustCognito(t, h, "GetUser", "", map[string]any{"AccessToken": auth["AccessToken"]})
	if user["Username"] != "alice" {
		t.Fatalf("GetUser %v", user)
	}
	// An unsigned admin call names the pool, which is enough.
	mustCognito(t, h, "DescribeUserPool", "", map[string]any{"UserPoolId": poolID})
}

func TestCognitoErrorsAreAWSShaped(t *testing.T) {
	h := newCognitoTestServer(t)
	code, out := cognitoCall(t, h, "DescribeUserPool", "", map[string]any{"UserPoolId": "us-east-1_nope"})
	if code != http.StatusBadRequest || out["__type"] != "ResourceNotFoundException" || out["message"] != "User pool us-east-1_nope does not exist." {
		t.Fatalf("status %d body %v", code, out)
	}
	code, out = cognitoCall(t, h, "StartWebAuthnRegistration", "", map[string]any{})
	if code != http.StatusBadRequest || out["__type"] != "NotImplementedException" {
		t.Fatalf("unhandled action: status %d body %v", code, out)
	}
}

func TestCognitoWellKnownRoutesAndS3Fallthrough(t *testing.T) {
	h := newCognitoTestServer(t)
	poolID := mustCognito(t, h, "CreateUserPool", "", map[string]any{"PoolName": "app"})["UserPool"].(map[string]any)["Id"].(string)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	rec := get("/" + poolID + "/.well-known/jwks.json")
	var jwks struct {
		Keys []map[string]string `json:"keys"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &jwks) != nil || len(jwks.Keys) != 1 || jwks.Keys[0]["alg"] != "RS256" {
		t.Fatalf("jwks status %d body %s", rec.Code, rec.Body.String())
	}
	rec = get("/" + poolID + "/.well-known/openid-configuration")
	var disco map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &disco)
	if disco["issuer"] != "http://localhost:4566/"+poolID || disco["jwks_uri"] != "http://localhost:4566/"+poolID+"/.well-known/jwks.json" {
		t.Fatalf("discovery %v", disco)
	}
	// A bucket with the same key layout still reaches S3.
	if rec := get("/my-bucket/.well-known/jwks.json"); !strings.Contains(rec.Body.String(), "NoSuchBucket") {
		t.Fatalf("bucket path should reach S3, got %d %s", rec.Code, rec.Body.String())
	}
	// So does an unknown pool-shaped path.
	if rec := get("/us-east-1_unknown/.well-known/jwks.json"); rec.Code == http.StatusOK {
		t.Fatalf("unknown pool served: %s", rec.Body.String())
	}
}

func TestCognitoPendingCodesRoute(t *testing.T) {
	h := newCognitoTestServer(t)
	poolID := mustCognito(t, h, "CreateUserPool", otherAccountAuth, map[string]any{
		"PoolName": "app", "AutoVerifiedAttributes": []string{"email"},
	})["UserPool"].(map[string]any)["Id"].(string)
	clientID := mustCognito(t, h, "CreateUserPoolClient", otherAccountAuth, map[string]any{
		"UserPoolId": poolID, "ClientName": "web",
	})["UserPoolClient"].(map[string]any)["ClientId"].(string)
	mustCognito(t, h, "SignUp", "", map[string]any{
		"ClientId": clientID, "Username": "bob", "Password": "Passw0rd!",
		"UserAttributes": []map[string]string{{"Name": "email", "Value": "bob@example.com"}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_tarn/admin/cognito/pools/"+poolID+"/users/bob/codes", nil))
	var out struct {
		Codes []cognito.PendingCode `json:"codes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Codes) != 1 || out.Codes[0].Purpose != "signup" || len(out.Codes[0].Code) != 6 {
		t.Fatalf("codes status %d body %s", rec.Code, rec.Body.String())
	}
	mustCognito(t, h, "ConfirmSignUp", "", map[string]any{"ClientId": clientID, "Username": "bob", "ConfirmationCode": out.Codes[0].Code})
}

func TestHealthListsCognito(t *testing.T) {
	h := newCognitoTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_tarn/health", nil))
	if !strings.Contains(rec.Body.String(), `"cognito-idp"`) {
		t.Fatalf("health %s", rec.Body.String())
	}
}

func TestCognitoBrowserCORS(t *testing.T) {
	h := newCognitoTestServer(t)

	pre := httptest.NewRequest(http.MethodOptions, "/", nil)
	pre.Header.Set("Origin", "http://localhost:5173")
	pre.Header.Set("Access-Control-Request-Method", "POST")
	pre.Header.Set("Access-Control-Request-Headers", "content-type,x-amz-target,x-amz-user-agent")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, pre)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "x-amz-target") {
		t.Fatalf("preflight status %d headers %v", rec.Code, rec.Header())
	}

	// Errors must be readable cross-origin so the SDK can name them.
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ClientId":"nope","AuthFlow":"USER_SRP_AUTH"}`))
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "AWSCognitoIdentityProviderService.InitiateAuth")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "x-amzn-errortype") {
		t.Fatalf("error response status %d headers %v", rec.Code, rec.Header())
	}

	// An S3 preflight on the root still reaches S3.
	s3pre := httptest.NewRequest(http.MethodOptions, "/", nil)
	s3pre.Header.Set("Origin", "http://localhost:5173")
	s3pre.Header.Set("Access-Control-Request-Method", "PUT")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, s3pre)
	if rec.Header().Get("Access-Control-Allow-Origin") == "*" {
		t.Fatalf("S3 preflight answered by Cognito: %v", rec.Header())
	}
}

func adminCall(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, rdr))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// seedOtherAccountPool creates, in account 111111111111, a pool with a client
// that has a secret and a confirmed user, as an SDK would.
func seedOtherAccountPool(t *testing.T, h http.Handler) (poolID, clientID string) {
	t.Helper()
	poolID = mustCognito(t, h, "CreateUserPool", otherAccountAuth, map[string]any{
		"PoolName": "app", "AutoVerifiedAttributes": []string{"email"},
	})["UserPool"].(map[string]any)["Id"].(string)
	clientID = mustCognito(t, h, "CreateUserPoolClient", otherAccountAuth, map[string]any{
		"UserPoolId": poolID, "ClientName": "web", "GenerateSecret": true,
	})["UserPoolClient"].(map[string]any)["ClientId"].(string)
	mustCognito(t, h, "AdminCreateUser", otherAccountAuth, map[string]any{
		"UserPoolId": poolID, "Username": "ann", "MessageAction": "SUPPRESS",
		"UserAttributes": []map[string]string{{"Name": "email", "Value": "ann@example.com"}},
	})
	mustCognito(t, h, "AdminSetUserPassword", otherAccountAuth, map[string]any{
		"UserPoolId": poolID, "Username": "ann", "Password": "Passw0rd!", "Permanent": true,
	})
	return poolID, clientID
}

func TestCognitoDashboardRoutes(t *testing.T) {
	h := newCognitoTestServer(t)
	poolID, clientID := seedOtherAccountPool(t, h)
	base := "/_tarn/admin/cognito/pools/" + poolID

	// Unsigned dashboard calls reach the pool's account through the index.
	code, detail := adminCall(t, h, http.MethodGet, base, nil)
	if code != http.StatusOK || detail["usersTotal"] != float64(1) || len(detail["clientList"].([]any)) != 1 {
		t.Fatalf("detail %d %v", code, detail)
	}
	raw, _ := json.Marshal(detail)
	for _, leak := range []string{"PRIVATE KEY", "passwordHash", "srpVerifier", "$2a$"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("detail leaks %q", leak)
		}
	}
	client := detail["clientList"].([]any)[0].(map[string]any)
	if client["hasSecret"] != true || client["clientSecret"] != nil {
		t.Fatalf("client view %v", client)
	}
	if code, sec := adminCall(t, h, http.MethodGet, base+"/clients/"+clientID+"/secret", nil); code != http.StatusOK || len(sec["clientSecret"].(string)) != 51 {
		t.Fatalf("secret %d %v", code, sec)
	}

	code, minted := adminCall(t, h, http.MethodPost, base+"/tokens", map[string]string{"clientId": clientID, "username": "ann"})
	if code != http.StatusOK || minted["AccessToken"] == nil {
		t.Fatalf("mint %d %v", code, minted)
	}
	// Minted tokens are real: Cognito's own APIs accept them.
	mustCognito(t, h, "GetUser", "", map[string]any{"AccessToken": minted["AccessToken"]})

	code, decoded := adminCall(t, h, http.MethodPost, "/_tarn/admin/cognito/decode", map[string]string{"token": "Bearer " + minted["IdToken"].(string)})
	if code != http.StatusOK || decoded["signatureValid"] != true || decoded["expired"] != false || decoded["poolId"] != poolID {
		t.Fatalf("decode %d %v", code, decoded)
	}
	if _, bad := adminCall(t, h, http.MethodPost, "/_tarn/admin/cognito/decode", map[string]string{"token": "nope"}); bad["error"] == nil {
		t.Fatalf("garbage token decoded: %v", bad)
	}

	if code, _ := adminCall(t, h, http.MethodPost, base+"/users/ann/disable", nil); code != http.StatusOK {
		t.Fatalf("disable %d", code)
	}
	if code, _ := cognitoCall(t, h, "GetUser", "", map[string]any{"AccessToken": minted["AccessToken"]}); code != http.StatusBadRequest {
		t.Fatal("disabling should revoke the minted token")
	}
	if code, _ := adminCall(t, h, http.MethodPost, base+"/users/ann/set-password", map[string]any{"password": "short"}); code != http.StatusBadRequest {
		t.Fatalf("weak password accepted: %d", code)
	}
	if code, _ := adminCall(t, h, http.MethodPost, base+"/users/ann/delete", nil); code != http.StatusOK {
		t.Fatalf("delete %d", code)
	}
	if code, _ := adminCall(t, h, http.MethodPost, base+"/users/ann/enable", nil); code != http.StatusNotFound {
		t.Fatalf("action on deleted user %d", code)
	}
	if code, _ := adminCall(t, h, http.MethodGet, "/_tarn/admin/cognito/pools/us-east-1_missing", nil); code != http.StatusNotFound {
		t.Fatalf("missing pool %d", code)
	}
}

func TestOverviewIncludesCognitoPools(t *testing.T) {
	h := newCognitoTestServer(t)
	mustCognito(t, h, "CreateUserPool", "", map[string]any{"PoolName": "app"})
	code, out := adminCall(t, h, http.MethodGet, "/_tarn/admin/overview", nil)
	pools, _ := out["cognitoPools"].([]any)
	if code != http.StatusOK || len(pools) != 1 || out["counts"].(map[string]any)["cognitoPools"] != float64(1) {
		t.Fatalf("overview %d pools=%v counts=%v", code, out["cognitoPools"], out["counts"])
	}
	if p := pools[0].(map[string]any); p["name"] != "app" || p["mfaMode"] != "OFF" || p["issuer"] == nil {
		t.Fatalf("summary %v", p)
	}
}
