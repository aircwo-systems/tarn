// Package cognito serves the Cognito User Pools JSON 1.1 API, the per-pool
// JWKS and OpenID discovery documents, and Tarn's admin routes for pending
// codes.
package cognito

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	cognitosvc "github.com/aircwo-systems/tarn/internal/cognito"
)

const targetPrefix = "AWSCognitoIdentityProviderService."

// IsCognitoRequest reports whether r targets the Cognito User Pools API.
func IsCognitoRequest(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("X-Amz-Target"), targetPrefix)
}

// Handler serves one account's Cognito API.
type Handler struct {
	svc *cognitosvc.Service
}

// NewHandler creates a Cognito API handler.
func NewHandler(svc *cognitosvc.Service) *Handler { return &Handler{svc: svc} }

// Service returns the underlying Cognito service.
func (h *Handler) Service() *cognitosvc.Service { return h.svc }

type operation func(svc *cognitosvc.Service, body []byte) (any, error)

// op adapts a typed service method to the dispatch table.
func op[I, O any](fn func(*cognitosvc.Service, *I) (O, error)) operation {
	return func(svc *cognitosvc.Service, body []byte) (any, error) {
		var in I
		if len(body) > 0 {
			if err := json.Unmarshal(body, &in); err != nil {
				return nil, &cognitosvc.Error{Code: "SerializationException", Message: err.Error()}
			}
		}
		return fn(svc, &in)
	}
}

type service = cognitosvc.Service

var operations = map[string]operation{
	// User pools
	"CreateUserPool":       op((*service).CreateUserPool),
	"DescribeUserPool":     op((*service).DescribeUserPool),
	"UpdateUserPool":       op((*service).UpdateUserPool),
	"DeleteUserPool":       op((*service).DeleteUserPool),
	"ListUserPools":        op((*service).ListUserPools),
	"AddCustomAttributes":  op((*service).AddCustomAttributes),
	"GetUserPoolMfaConfig": op((*service).GetUserPoolMfaConfig),
	"SetUserPoolMfaConfig": op((*service).SetUserPoolMfaConfig),
	"ListTagsForResource":  op((*service).ListTagsForResource),
	"TagResource":          op((*service).TagResource),
	"UntagResource":        op((*service).UntagResource),

	// App clients
	"CreateUserPoolClient":   op((*service).CreateUserPoolClient),
	"DescribeUserPoolClient": op((*service).DescribeUserPoolClient),
	"UpdateUserPoolClient":   op((*service).UpdateUserPoolClient),
	"DeleteUserPoolClient":   op((*service).DeleteUserPoolClient),
	"ListUserPoolClients":    op((*service).ListUserPoolClients),

	// Domains and resource servers
	"CreateUserPoolDomain":   op((*service).CreateUserPoolDomain),
	"DescribeUserPoolDomain": op((*service).DescribeUserPoolDomain),
	"UpdateUserPoolDomain":   op((*service).UpdateUserPoolDomain),
	"DeleteUserPoolDomain":   op((*service).DeleteUserPoolDomain),
	"CreateResourceServer":   op((*service).CreateResourceServer),
	"DescribeResourceServer": op((*service).DescribeResourceServer),
	"UpdateResourceServer":   op((*service).UpdateResourceServer),
	"DeleteResourceServer":   op((*service).DeleteResourceServer),
	"ListResourceServers":    op((*service).ListResourceServers),

	// Groups
	"CreateGroup":              op((*service).CreateGroup),
	"GetGroup":                 op((*service).GetGroup),
	"UpdateGroup":              op((*service).UpdateGroup),
	"DeleteGroup":              op((*service).DeleteGroup),
	"ListGroups":               op((*service).ListGroups),
	"ListUsersInGroup":         op((*service).ListUsersInGroup),
	"AdminAddUserToGroup":      op((*service).AdminAddUserToGroup),
	"AdminRemoveUserFromGroup": op((*service).AdminRemoveUserFromGroup),
	"AdminListGroupsForUser":   op((*service).AdminListGroupsForUser),

	// Admin user management
	"AdminCreateUser":             op((*service).AdminCreateUser),
	"AdminGetUser":                op((*service).AdminGetUser),
	"AdminUpdateUserAttributes":   op((*service).AdminUpdateUserAttributes),
	"AdminDeleteUserAttributes":   op((*service).AdminDeleteUserAttributes),
	"AdminSetUserPassword":        op((*service).AdminSetUserPassword),
	"AdminEnableUser":             op((*service).AdminEnableUser),
	"AdminDisableUser":            op((*service).AdminDisableUser),
	"AdminDeleteUser":             op((*service).AdminDeleteUser),
	"AdminConfirmSignUp":          op((*service).AdminConfirmSignUp),
	"AdminResetUserPassword":      op((*service).AdminResetUserPassword),
	"AdminUserGlobalSignOut":      op((*service).AdminUserGlobalSignOut),
	"AdminListUserAuthEvents":     op((*service).AdminListUserAuthEvents),
	"AdminInitiateAuth":           op((*service).AdminInitiateAuth),
	"AdminRespondToAuthChallenge": op((*service).AdminRespondToAuthChallenge),
	"ListUsers":                   op((*service).ListUsers),

	// Public sign-up and sign-in
	"SignUp":                           op((*service).SignUp),
	"ConfirmSignUp":                    op((*service).ConfirmSignUp),
	"ResendConfirmationCode":           op((*service).ResendConfirmationCode),
	"InitiateAuth":                     op((*service).InitiateAuth),
	"RespondToAuthChallenge":           op((*service).RespondToAuthChallenge),
	"ForgotPassword":                   op((*service).ForgotPassword),
	"ConfirmForgotPassword":            op((*service).ConfirmForgotPassword),
	"GetUser":                          op((*service).GetUser),
	"UpdateUserAttributes":             op((*service).UpdateUserAttributes),
	"DeleteUser":                       op((*service).DeleteUser),
	"ChangePassword":                   op((*service).ChangePassword),
	"GlobalSignOut":                    op((*service).GlobalSignOut),
	"RevokeToken":                      op((*service).RevokeToken),
	"GetUserAttributeVerificationCode": op((*service).GetUserAttributeVerificationCode),
	"VerifyUserAttribute":              op((*service).VerifyUserAttribute),
}

// Dispatch routes a JSON 1.1 request by its X-Amz-Target action.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), targetPrefix)
	fn, ok := operations[action]
	if !ok {
		log.Printf("[cognito] unhandled action %s", action)
		writeError(w, cognitosvc.NotImplemented(action))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, &cognitosvc.Error{Code: "SerializationException", Message: err.Error()})
		return
	}
	out, err := fn(h.svc, body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ServeWellKnown answers the pool's JWKS and OpenID discovery routes. It
// returns false when path is neither, so the caller can fall through to S3.
func (h *Handler) ServeWellKnown(w http.ResponseWriter, poolID, path string) bool {
	var (
		out any
		err error
	)
	switch path {
	case ".well-known/jwks.json":
		out, err = h.svc.JWKS(poolID)
	case ".well-known/openid-configuration":
		out, err = h.svc.OpenIDConfiguration(poolID)
	default:
		return false
	}
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(out)
	return true
}

// PendingCodes serves GET /_tarn/admin/cognito/pools/{poolId}/codes and
// /_tarn/admin/cognito/pools/{poolId}/users/{username}/codes, so scripted
// tests can read the codes Tarn would have emailed.
func (h *Handler) PendingCodes(w http.ResponseWriter, r *http.Request) {
	codes, err := h.svc.PendingCodes(r.PathValue("poolId"), r.PathValue("username"))
	if err != nil {
		var ce *cognitosvc.Error
		status := http.StatusInternalServerError
		if errors.As(err, &ce) {
			status = http.StatusNotFound
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"codes": codes})
}

// Identifiers are the request fields that name the pool a call is for.
type Identifiers struct {
	UserPoolId  string `json:"UserPoolId"`
	ClientId    string `json:"ClientId"`
	AccessToken string `json:"AccessToken"`
}

// ReadIdentifiers extracts the pool, client and access token from a request
// body for account resolution.
func ReadIdentifiers(body []byte) Identifiers {
	var ids Identifiers
	_ = json.Unmarshal(body, &ids)
	return ids
}

// ResolveAccount finds the account a Cognito request belongs to. A ClientId
// or access token is authoritative, since public operations are unsigned.
// For pool-scoped admin calls a SigV4 account wins when present; signed
// reports whether the request carried one.
func ResolveAccount(index *cognitosvc.Index, body []byte, signed bool) (string, bool) {
	ids := ReadIdentifiers(body)
	if ids.ClientId != "" {
		if acct, ok := index.AccountForClient(ids.ClientId); ok {
			return acct, true
		}
	}
	if ids.AccessToken != "" {
		if acct, ok := index.AccountForPool(cognitosvc.PoolIDFromToken(ids.AccessToken)); ok {
			return acct, true
		}
	}
	if ids.UserPoolId != "" && !signed {
		return index.AccountForPool(ids.UserPoolId)
	}
	return "", false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var ce *cognitosvc.Error
	if !errors.As(err, &ce) {
		log.Printf("[cognito] internal error: %v", err)
		ce = &cognitosvc.Error{Code: "InternalErrorException", Message: err.Error()}
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.Header().Set("X-Amzn-ErrorType", ce.Code)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"__type": ce.Code, "message": ce.Message})
}
