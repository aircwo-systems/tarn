package cognito

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"strings"
	"time"
)

// TriggerInvoker runs a Lambda function synchronously and returns its
// response payload. A function error is returned as an error carrying the
// function's errorMessage.
type TriggerInvoker func(ctx context.Context, functionArn string, payload []byte) ([]byte, error)

// SetTriggerInvoker lets the pool's LambdaConfig triggers run. Without one,
// configured triggers are skipped with a log line.
func (s *Service) SetTriggerInvoker(fn TriggerInvoker) { s.invoker = fn }

// triggerTimeout bounds one trigger call. AWS allows 5 seconds; a local cold
// start can take longer, so Tarn is more patient.
const triggerTimeout = 30 * time.Second

// triggerAttributes returns the user's attributes as Cognito passes them to
// triggers, including the cognito:user_status pseudo-attribute.
func (u *user) triggerAttributes() map[string]string {
	attrs := make(map[string]string, len(u.Attributes)+1)
	for k, v := range u.Attributes {
		attrs[k] = v
	}
	if u.Status != "" {
		attrs["cognito:user_status"] = u.Status
	}
	return attrs
}

// triggerCall is one trigger invocation, built under the lock and run
// without it.
type triggerCall struct {
	name     string // LambdaConfig member, used in error messages
	arn      string
	version  string
	source   string
	poolID   string
	username string
	clientID string
	request  map[string]any
	response map[string]any
}

// invoke runs the trigger and returns the response object the function sent
// back. A nil call or a missing invoker is a no-op.
func (s *Service) invoke(call *triggerCall) (map[string]any, error) {
	if call == nil || call.arn == "" {
		return nil, nil
	}
	if s.invoker == nil {
		log.Printf("[cognito] %s trigger %s skipped: no Lambda invoker configured", call.name, call.arn)
		return nil, nil
	}
	version := call.version
	if version == "" {
		version = "1"
	}
	response := call.response
	if response == nil {
		response = map[string]any{}
	}
	event := map[string]any{
		"version":       version,
		"region":        s.cfg.Region,
		"userPoolId":    call.poolID,
		"userName":      call.username,
		"callerContext": map[string]any{"awsSdkVersion": "aws-sdk-unknown-unknown", "clientId": call.clientID},
		"triggerSource": call.source,
		"request":       call.request,
		"response":      response,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), triggerTimeout)
	defer cancel()
	out, err := s.invoker(ctx, call.arn, payload)
	if err != nil {
		return nil, newError("UserLambdaValidationException", "%s failed with error %s.", call.name, err.Error())
	}
	var result struct {
		Response map[string]any `json:"response"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, newError("InvalidLambdaResponseException", "Invalid lambda response from %s.", call.name)
	}
	return result.Response, nil
}

func clientMetadata(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// preSignUpCall builds the PreSignUp trigger call for a new user.
func preSignUpCall(p *pool, source, username, clientID string, attrs map[string]string, validation []AttributeType, meta map[string]string) *triggerCall {
	arn := p.lambdaConfig().PreSignUp
	if arn == "" {
		return nil
	}
	userAttrs := make(map[string]string, len(attrs))
	for k, v := range attrs {
		userAttrs[k] = v
	}
	return &triggerCall{
		name: "PreSignUp", arn: arn, source: source, poolID: p.Config.Id,
		username: username, clientID: clientID,
		request: map[string]any{
			"userAttributes": userAttrs,
			"validationData": attrMap(validation),
			"clientMetadata": clientMetadata(meta),
		},
		response: map[string]any{"autoConfirmUser": false, "autoVerifyEmail": false, "autoVerifyPhone": false},
	}
}

// postConfirmationCall builds the PostConfirmation trigger call. Callers hold s.mu.
func postConfirmationCall(p *pool, u *user, source, clientID string, meta map[string]string) *triggerCall {
	arn := p.lambdaConfig().PostConfirmation
	if arn == "" {
		return nil
	}
	return &triggerCall{
		name: "PostConfirmation", arn: arn, source: source, poolID: p.Config.Id,
		username: u.Username, clientID: clientID,
		request: map[string]any{"userAttributes": u.triggerAttributes(), "clientMetadata": clientMetadata(meta)},
	}
}

// preAuthenticationCall builds the PreAuthentication trigger call. Callers
// hold s.mu.
func preAuthenticationCall(p *pool, u *user, clientID string, meta map[string]string) *triggerCall {
	arn := p.lambdaConfig().PreAuthentication
	if arn == "" {
		return nil
	}
	return &triggerCall{
		name: "PreAuthentication", arn: arn, source: "PreAuthentication_Authentication", poolID: p.Config.Id,
		username: u.Username, clientID: clientID,
		request: map[string]any{
			"userAttributes": u.triggerAttributes(), "validationData": map[string]string{},
			"userNotFound": false, "clientMetadata": clientMetadata(meta),
		},
	}
}

// reservedClaims cannot be added, overridden or suppressed by the pre token
// generation trigger.
var reservedClaims = []string{
	"acr", "amr", "at_hash", "aud", "auth_time", "azp", "client_id", "cognito:username",
	"exp", "iat", "identities", "iss", "jti", "nbf", "nonce", "origin_jti", "scope",
	"sub", "token_use", "username", "event_id",
}

func applyClaimOverrides(claims map[string]any, details map[string]any) {
	if details == nil {
		return
	}
	if add, ok := details["claimsToAddOrOverride"].(map[string]any); ok {
		for k, v := range add {
			if slices.Contains(reservedClaims, k) || strings.HasPrefix(k, "cognito:") {
				log.Printf("[cognito] PreTokenGeneration cannot override reserved claim %q; ignored", k)
				continue
			}
			claims[k] = v
		}
	}
	if suppress, ok := details["claimsToSuppress"].([]any); ok {
		for _, v := range suppress {
			if k, ok := v.(string); ok && !slices.Contains(reservedClaims, k) {
				delete(claims, k)
			}
		}
	}
}

func applyGroupOverride(job *tokenJob, details map[string]any) {
	if details == nil {
		return
	}
	raw, ok := details["groupsToOverride"].([]any)
	if !ok {
		return
	}
	groups := make([]string, 0, len(raw))
	for _, g := range raw {
		if name, ok := g.(string); ok {
			groups = append(groups, name)
		}
	}
	for _, claims := range []map[string]any{job.id, job.access} {
		if len(groups) == 0 {
			delete(claims, "cognito:groups")
		} else {
			claims["cognito:groups"] = groups
		}
	}
}

// runPreTokenGeneration applies the pre token generation trigger to job's
// claims. Version 1 events can only change the ID token; version 2 (the pool's
// PreTokenGenerationConfig.LambdaVersion V2_0 or later) can change both and
// the access token's scopes, as in AWS.
func (s *Service) runPreTokenGeneration(job *tokenJob) error {
	arn, version := job.lambda.PreTokenGeneration, "1"
	if cfg := job.lambda.PreTokenGenerationConfig; cfg != nil && cfg.LambdaArn != "" {
		arn = cfg.LambdaArn
		if cfg.LambdaVersion != "" && cfg.LambdaVersion != "V1_0" {
			version = "2"
		}
	}
	if arn == "" {
		return nil
	}
	scope, _ := job.access["scope"].(string)
	request := map[string]any{
		"userAttributes": job.userAttrs,
		"groupConfiguration": map[string]any{
			"groupsToOverride": job.groups, "iamRolesToOverride": []string{}, "preferredRole": nil,
		},
		"clientMetadata": map[string]string{},
	}
	if version == "2" {
		request["scopes"] = strings.Fields(scope)
	}
	resp, err := s.invoke(&triggerCall{
		name: "PreTokenGeneration", arn: arn, version: version, source: "TokenGeneration_" + job.source,
		poolID: job.poolID, username: job.username, clientID: job.clientID, request: request,
	})
	if err != nil || resp == nil {
		return err
	}

	if version == "1" {
		details, _ := resp["claimsOverrideDetails"].(map[string]any)
		applyClaimOverrides(job.id, details)
		if details != nil {
			groups, _ := details["groupOverrideDetails"].(map[string]any)
			applyGroupOverride(job, groups)
		}
		return nil
	}

	details, _ := resp["claimsAndScopeOverrideDetails"].(map[string]any)
	if details == nil {
		return nil
	}
	idGen, _ := details["idTokenGeneration"].(map[string]any)
	applyClaimOverrides(job.id, idGen)
	accessGen, _ := details["accessTokenGeneration"].(map[string]any)
	applyClaimOverrides(job.access, accessGen)
	if accessGen != nil {
		scopes := strings.Fields(scope)
		if add, ok := accessGen["scopesToAdd"].([]any); ok {
			for _, v := range add {
				if sc, ok := v.(string); ok && !slices.Contains(scopes, sc) {
					scopes = append(scopes, sc)
				}
			}
		}
		if drop, ok := accessGen["scopesToSuppress"].([]any); ok {
			scopes = slices.DeleteFunc(scopes, func(sc string) bool {
				return slices.ContainsFunc(drop, func(v any) bool { return v == sc })
			})
		}
		job.access["scope"] = strings.Join(scopes, " ")
	}
	groups, _ := details["groupOverrideDetails"].(map[string]any)
	applyGroupOverride(job, groups)
	return nil
}
