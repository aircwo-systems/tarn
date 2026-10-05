# Cognito User Pools PRD

Status: Proposed  
Target: Post-MVP service addition  
Primary area: New `internal/cognito` service module, API Gateway authorizers, dashboard

## 1. Summary

Tarn should emulate Amazon Cognito User Pools well enough that a developer can deploy pools, clients, groups and users with Terraform or the SDK, sign users up and in, receive real signed JWTs, and have those tokens accepted by API Gateway routes and by their own Lambda code, all without an AWS account.

The service follows Tarn's existing rule: **IAM is not enforced**. Any caller can run administrative and control-plane operations. Cognito's own authentication rules are a different matter. They are the behavior the developer is trying to test, so Tarn enforces them: passwords, confirmation state, disabled users, token signatures, expiry, refresh token revocation, groups and claims all behave the way AWS does.

Identity Pools (`cognito-identity`), the hosted UI, federation and real message delivery are out of scope for the first release.

## 2. Problem

Tarn has no identity service. Teams building authenticated APIs currently have three options, none of them good:

- Point local code at a real Cognito pool. This needs AWS credentials, network access and shared state, and it mixes real users with test data.
- Stub authentication out in local builds. The auth path is then never exercised until a shared environment, which is where token, claim and group bugs surface.
- Run a separate emulator. Its tokens are not trusted by Tarn's API Gateway, and its resources are invisible to the Tarn dashboard and Terraform flows.

There is a second gap. Tarn's API Gateway v1 and v2 implementations store `AuthorizationType` but do not run authorizers. No route in Tarn can currently reject an unauthenticated request. Adding Cognito without authorizers would only serve half of the "test auth locally" use case.

## 3. Product intent

The primary user is a developer building an application whose API sits behind Cognito. They want answers to questions like:

- Does sign-up, confirmation and sign-in work end to end against my Terraform-defined pool and client?
- Does my API Gateway route return `401` without a token and pass the request through with one?
- Does my Lambda read the right claims (`sub`, `email`, `cognito:groups`) from the request context?
- Does my frontend refresh tokens correctly, and handle an expired or revoked token?
- Does my `PreSignUp` or `PreTokenGeneration` trigger behave correctly?
- Does a user who was created by an admin get forced through `NEW_PASSWORD_REQUIRED`?

The workflow should need no special cases beyond pointing the SDK or Terraform at Tarn. Where Tarn has to differ from AWS, such as the token issuer URL or message delivery, the difference should be explicit, documented and visible in the dashboard.

## 4. Principles: where the balance sits

| Area | Tarn behavior | Reason |
|---|---|---|
| IAM on control-plane and `Admin*` calls | Not enforced. Any signed or unsigned request is accepted. | Matches every other Tarn service. See `docs/services/iam.md`. |
| Cognito authentication (passwords, user status, challenges) | Enforced exactly. | This is the behavior under test. |
| Tokens | Real RS256 JWTs, AWS-shaped claims, real expiry. | Downstream verification code must run unchanged, or as close to unchanged as possible. |
| Password policy and attribute validation | Enforced from pool settings. | Cheap to implement, and frontends show these errors to users. |
| Email and SMS delivery | Not sent. Codes are exposed locally. | No external side effects from a local tool. |
| Rate limits, advanced security, risk scoring | Not emulated. | Low value locally, high complexity. |
| Unsupported operations | Return `NotImplementedException` or a compatibility stub, logged as `[cognito] unhandled action`. | Keeps Terraform moving and makes gaps visible. |

## 5. Goals

- Implement the Cognito User Pools control plane that Terraform uses for `aws_cognito_user_pool`, `aws_cognito_user_pool_client`, `aws_cognito_user_pool_domain`, `aws_cognito_user_group`, `aws_cognito_user`, `aws_cognito_user_in_group` and `aws_cognito_resource_server`.
- Implement public sign-up, confirmation, sign-in, refresh, sign-out and password-reset flows used by the AWS SDKs and Amplify.
- Issue RS256-signed ID and access tokens with AWS-shaped claims, and serve JWKS and OpenID discovery documents.
- Add JWT authorizers to API Gateway v2 and `COGNITO_USER_POOLS` authorizers to API Gateway v1, with claims injected into the Lambda event.
- Invoke Cognito Lambda triggers through the existing Lambda service.
- Keep pools account-local, persist them with the existing snapshot model, and show them in the dashboard.

## 6. Non-goals

- Cognito Identity Pools, `GetId`, `GetCredentialsForIdentity` and temporary AWS credentials. Tarn does not enforce IAM, so the credentials would have no effect.
- The hosted UI, managed login branding and the `/oauth2/authorize` and `/login` browser flows.
- Federation with SAML, OIDC or social identity providers.
- Real email or SMS delivery, including SES and SNS integration.
- TOTP and SMS MFA in the first release. `GetUserPoolMfaConfig` and `SetUserPoolMfaConfig` are stored so Terraform works, but MFA is not challenged.
- Advanced security features, compromised credential checks, risk configuration and adaptive authentication.
- Passkeys, WebAuthn and passwordless `USER_AUTH` choice-based flows.
- Import jobs, user migration triggers and CSV imports.
- Custom domains with TLS.

### 6.1 Success measures

- A Terraform configuration using the MVP resources applies, plans with no drift, and destroys cleanly against Tarn.
- The AWS SDK v3 `CognitoIdentityProviderClient` can run sign-up, confirm, sign-in, refresh and sign-out against Tarn with only the endpoint changed.
- An API Gateway v2 route with a JWT authorizer rejects missing, malformed, expired and wrong-audience tokens with `401`, and passes claims to Lambda for valid tokens.
- Tokens issued before a Tarn restart still verify after the restart when persistence is enabled.
- A developer can find the confirmation code for a new user in the dashboard in under 10 seconds.

## 7. Terminology

| Term | Meaning |
|---|---|
| User pool | A user directory with its own settings, keypair, users and groups. ID format `<region>_<9 alphanumerics>`, for example `us-east-1_Ab12Cd34E`. |
| App client | A client registered on a pool. Has a client ID, an optional secret, allowed auth flows and token validity settings. |
| Public operation | A Cognito operation AWS allows without SigV4, such as `InitiateAuth`, `SignUp` and `GetUser`. |
| Admin operation | An `Admin*` operation that AWS requires IAM for. Tarn does not check IAM. |
| Issuer | The `iss` claim in tokens and the base URL for JWKS and discovery. |
| Trigger | A Lambda function configured in the pool's `LambdaConfig`. |
| Code | A confirmation, attribute verification or password reset code that AWS would email or text. |

## 8. Protocol and dispatch

User Pools use the AWS JSON 1.1 protocol:

- `POST /`
- `Content-Type: application/x-amz-json-1.1`
- `X-Amz-Target: AWSCognitoIdentityProviderService.<Action>`
- Errors are `{"__type": "<Code>", "message": "..."}` with the AWS status code, usually `400`.

Add an `IsCognitoRequest` check on the target prefix. It must run in both `postRootDispatch` and `dispatchProtocolRequest` in `internal/api/server.go`, before the SNS form parse and the SQS fallback. Add `cognito-idp` to the `/_tarn/health` services list.

The Terraform provider endpoint key is `cognitoidp`:

```hcl
provider "aws" {
  endpoints {
    cognitoidp = "http://localhost:4566"
  }
}
```

The `AWS_ENDPOINT_URL` variable that Tarn already injects into Lambda and ECS containers covers SDK calls made from inside them.

## 9. Account resolution

This is the main structural constraint. Tarn resolves the account from the SigV4 access key ID (`internal/account/account.go`). Cognito public operations are **unsigned**, so the resolver falls back to the default account. Each public operation carries a different identifier instead:

| Operations | Identifier in request | Resolution |
|---|---|---|
| `SignUp`, `ConfirmSignUp`, `InitiateAuth`, `RespondToAuthChallenge`, `ForgotPassword`, `ConfirmForgotPassword`, `ResendConfirmationCode` | `ClientId` | client ID to account and pool |
| `GetUser`, `UpdateUserAttributes`, `ChangePassword`, `GlobalSignOut`, `DeleteUser`, `VerifyUserAttribute`, `GetUserAttributeVerificationCode` | `AccessToken` | decode `iss` to get pool ID, then pool to account |
| `RevokeToken` | `Token` and `ClientId` | client ID to account |
| `Admin*` and control-plane operations | `UserPoolId`, usually signed | SigV4 account when present, otherwise pool ID to account |
| JWKS and discovery HTTP routes | pool ID in the path | pool ID to account |

Tarn therefore needs a process-wide index from pool ID and client ID to account ID. `HandlerRegistry` builds accounts lazily, which means a pool persisted in a non-default account is not loaded after a restart until something touches that account. The index must be built at startup by scanning each account's persisted Cognito state, or kept in its own small persisted file under `DataDir`. The scan is preferred because it cannot drift from the source of truth.

Pool and client IDs are random, so collisions across accounts are not a practical concern. The index lookup must happen before the per-account handler is chosen. Implement it as a Cognito-specific resolver in the dispatch path, not as a change to `account.FromRequest`.

## 10. Resource model

Stored per account, in `internal/cognito`:

- **UserPool**: ID, name, ARN, status, creation and modification dates, policies (password policy, sign-in policy), schema attributes (standard and `custom:`), `UsernameAttributes`, `AliasAttributes`, `AutoVerifiedAttributes`, `UsernameConfiguration.CaseSensitive`, `AdminCreateUserConfig`, `LambdaConfig`, MFA configuration, `DeletionProtection`, tags, account recovery settings, the RSA signing keypair and `kid`, and estimated user count.
- **UserPoolClient**: client ID, name, optional secret, `ExplicitAuthFlows`, token validity and units, `ReadAttributes`, `WriteAttributes`, `AllowedOAuthFlows`, `AllowedOAuthScopes`, callback and logout URLs, `PreventUserExistenceErrors`, `EnableTokenRevocation`, and `SupportedIdentityProviders`.
- **Domain**: prefix and owning pool. Stored for Terraform only.
- **ResourceServer**: identifier, name and scopes. Used for access token `scope` values.
- **Group**: name, description, precedence and `RoleArn`, which is stored but has no effect.
- **User**: username, `sub` (UUID), attributes, bcrypt password hash, `UserStatus`, `Enabled`, created and modified dates, group memberships, pending codes and token generation counter.
- **RefreshToken**: opaque random value stored as a hash, with client ID, user `sub`, expiry and a revoked flag.

User status values follow AWS: `UNCONFIRMED`, `CONFIRMED`, `FORCE_CHANGE_PASSWORD`, `RESET_REQUIRED`, `ARCHIVED` and `UNKNOWN`. Tarn only needs the first four.

Persistence uses a store with `persist.Flusher`, in the same way as `internal/secrets/store.go`. Passwords are hashed with bcrypt even though Tarn is local, because pool snapshots may be committed or shared by mistake. The signing private key is stored in the snapshot. When the shared vault is configured, it should be encrypted with the same vault that Secrets Manager uses.

## 11. Operations

### 11.1 Phase 1: control plane (Terraform)

These operations are taken from the provider's CRUD paths:

| Terraform resource | Operations |
|---|---|
| `aws_cognito_user_pool` | `CreateUserPool`, `DescribeUserPool`, `UpdateUserPool`, `DeleteUserPool`, `GetUserPoolMfaConfig`, `SetUserPoolMfaConfig`, `AddCustomAttributes`, `ListTagsForResource`, `TagResource`, `UntagResource` |
| `aws_cognito_user_pool_client` | `CreateUserPoolClient`, `DescribeUserPoolClient`, `UpdateUserPoolClient`, `DeleteUserPoolClient` |
| `aws_cognito_user_pool_domain` | `CreateUserPoolDomain`, `DescribeUserPoolDomain`, `UpdateUserPoolDomain`, `DeleteUserPoolDomain` |
| `aws_cognito_user_group` | `CreateGroup`, `GetGroup`, `UpdateGroup`, `DeleteGroup` |
| `aws_cognito_user` | `AdminCreateUser`, `AdminGetUser`, `AdminUpdateUserAttributes`, `AdminDeleteUserAttributes`, `AdminSetUserPassword`, `AdminEnableUser`, `AdminDisableUser`, `AdminDeleteUser` |
| `aws_cognito_user_in_group` | `AdminAddUserToGroup`, `AdminListGroupsForUser`, `AdminRemoveUserFromGroup` |
| `aws_cognito_resource_server` | `CreateResourceServer`, `DescribeResourceServer`, `UpdateResourceServer`, `DeleteResourceServer` |

`DescribeUserPool` must return every field Terraform reads, including defaults Tarn does not act on, so that plans do not drift. Verify each resource against the provider source during implementation. The lists above were checked for pools and users but should be confirmed for the rest.

Listing operations used by the SDK and the dashboard: `ListUserPools`, `ListUserPoolClients`, `ListUsers` (with the `Filter` expression subset `attr = "v"` and `attr ^= "v"`), `ListGroups`, `ListUsersInGroup` and `ListResourceServers`. All support `MaxResults` or `Limit`, and `NextToken` or `PaginationToken`.

### 11.2 Phase 1: authentication

Public operations: `SignUp`, `ConfirmSignUp`, `ResendConfirmationCode`, `InitiateAuth`, `RespondToAuthChallenge`, `ForgotPassword`, `ConfirmForgotPassword`, `GetUser`, `UpdateUserAttributes`, `DeleteUser`, `ChangePassword`, `GlobalSignOut`, `RevokeToken`, `GetUserAttributeVerificationCode` and `VerifyUserAttribute`.

Admin operations: `AdminInitiateAuth`, `AdminRespondToAuthChallenge`, `AdminConfirmSignUp`, `AdminResetUserPassword`, `AdminUserGlobalSignOut` and `AdminListUserAuthEvents` (returns an empty list).

Auth flows:

| Flow | Phase | Notes |
|---|---|---|
| `USER_PASSWORD_AUTH` | 1 | Requires `ALLOW_USER_PASSWORD_AUTH` on the client. |
| `ADMIN_USER_PASSWORD_AUTH` | 1 | Requires `ALLOW_ADMIN_USER_PASSWORD_AUTH`. |
| `REFRESH_TOKEN_AUTH` / `REFRESH_TOKEN` | 1 | Refresh tokens are not rotated unless client refresh token rotation is configured. |
| `USER_SRP_AUTH` | See open question 1 | This is the default flow in Amplify and in most frontend Cognito libraries. |
| `CUSTOM_AUTH` | 2 | Needs the Define, Create and Verify Auth Challenge triggers. |
| `USER_AUTH` (choice-based) | Out of scope | |

Challenges: `NEW_PASSWORD_REQUIRED` for users in `FORCE_CHANGE_PASSWORD`. `PASSWORD_VERIFIER` is added if SRP is included. MFA challenges are out of scope.

Behavior that must match AWS:

- `ClientId` must exist, and the flow must be listed in `ExplicitAuthFlows`. Otherwise Tarn returns `InvalidParameterException` with the AWS message.
- When the client has a secret, `SecretHash` (HMAC-SHA256 of username plus client ID) is required and checked.
- A wrong password returns `NotAuthorizedException` with `Incorrect username or password.`
- An unknown user returns `UserNotFoundException`, or `NotAuthorizedException` when `PreventUserExistenceErrors` is `ENABLED`.
- An unconfirmed user returns `UserNotConfirmedException`.
- A disabled user returns `NotAuthorizedException` with `User is disabled.`
- Password policy violations return `InvalidPasswordException`.
- Duplicate usernames or aliases return `UsernameExistsException` or `AliasExistsException`.
- Usernames are case-insensitive by default, as with AWS pools created with default settings.
- Sign-in by `email` or `phone_number` works when they are configured as username or alias attributes.

## 12. Tokens

Each pool has its own RSA-2048 keypair, generated at `CreateUserPool` and persisted. Tokens are RS256 JWTs with a `kid` header.

**ID token claims**: `sub`, `iss`, `aud` (client ID), `token_use: "id"`, `auth_time`, `iat`, `exp`, `cognito:username`, `cognito:groups` (omitted when empty), `email`, `email_verified`, `phone_number`, `phone_number_verified`, other readable standard and `custom:` attributes, `event_id`, `origin_jti` and `jti`.

**Access token claims**: `sub`, `iss`, `client_id`, `token_use: "access"`, `scope` (`aws.cognito.signin.user.admin` plus any granted resource server scopes), `auth_time`, `iat`, `exp`, `username`, `cognito:groups`, `event_id`, `origin_jti` and `jti`.

**Refresh token**: an opaque random string. It is not a JWE as in AWS, which is fine because clients treat it as opaque.

Validity uses the client's `AccessTokenValidity`, `IdTokenValidity` and `RefreshTokenValidity`, with AWS defaults of 60 minutes, 60 minutes and 30 days. Tarn also accepts an optional global override, `TARN_COGNITO_TOKEN_TTL`, so developers can test expiry without waiting an hour.

`GlobalSignOut`, `AdminUserGlobalSignOut` and `RevokeToken` revoke refresh tokens. They also increment a per-user generation counter that is embedded in access tokens, so `GetUser` and the other access-token operations reject revoked tokens with `NotAuthorizedException: Access Token has been revoked`. This matches AWS, where revocation affects Cognito's own APIs but not offline JWT verification.

HTTP routes, resolved by pool ID:

- `GET /{poolId}/.well-known/jwks.json`
- `GET /{poolId}/.well-known/openid-configuration`

## 13. Issuer and token verification

AWS sets `iss` to `https://cognito-idp.<region>.amazonaws.com/<poolId>`. Tarn cannot serve that host, and Tarn is reachable at different addresses depending on the caller:

- From the host: `http://localhost:4566`.
- From Lambda and ECS containers: `http://host.docker.internal:4566`, which is what `AWS_ENDPOINT_URL` is set to.

A token's `iss` is a single string, so one caller always sees an issuer URL it cannot fetch directly.

The popular `aws-jwt-verify` library affects the choice. Its `CognitoJwtVerifier` builds the issuer and JWKS URI from the pool ID as `https://cognito-idp.<region>.amazonaws.com/<poolId>` and provides no override. Code that uses it unchanged will reject any token with a local `iss`. The library supports two ways round this:

- `JwtRsaVerifier.create({ issuer, audience, jwksUri })` accepts any issuer and JWKS URI, but loses the Cognito-specific checks for `token_use` and `client_id`.
- `CognitoJwtVerifier` with `verifier.cacheJwks(jwks)` hydrates the key set locally, so no JWKS fetch happens. This only works if `iss` matches the AWS format.

Recommended design: an issuer mode setting, `TARN_COGNITO_ISSUER`.

| Mode | `iss` value | Works with |
|---|---|---|
| `local` (default) | `http://localhost:4566/<poolId>`, with host and port from config | Standard OIDC libraries, API Gateway authorizers in Tarn, browsers |
| `aws` | `https://cognito-idp.<region>.amazonaws.com/<poolId>` | Unmodified `CognitoJwtVerifier`, if the app hydrates JWKS with `cacheJwks` from Tarn's JWKS route |
| explicit URL | a user-supplied base URL | Custom setups, for example a hostname mapped in both host and container |

Tarn's own API Gateway authorizers look up the pool by the pool ID in the token, not by fetching `iss`, so they work in every mode.

`docs/services/cognito.md` must include copy-paste verification snippets for `aws-jwt-verify`, `jose` and Python's `PyJWT` for both host and in-container use.

## 14. Code delivery

Tarn never sends email or SMS. Wherever AWS would send a code or a temporary password:

- Tarn generates a 6-digit code, as AWS does, and stores it on the user with an expiry of 24 hours for sign-up and 1 hour for password reset.
- The code is logged as `[cognito] code pool=<id> user=<name> purpose=<signup|reset|verify> code=<code>`.
- The code appears in the dashboard's pending codes panel.
- The code can be read from `GET /_tarn/admin/cognito/pools/{poolId}/users/{username}/codes` for scripted tests.
- `CodeDeliveryDetails` in responses is AWS-shaped, with a masked destination such as `a***@e***.com`.

Opt-in fixed-code mode (`TARN_COGNITO_FIXED_CODE=123456`) makes every code the given value, so end-to-end tests do not need to fetch codes.

`AdminCreateUser` temporary passwords follow the same path. When `MessageAction` is `SUPPRESS`, nothing is logged, which matches AWS.

The `CustomMessage` trigger, if configured, is invoked and its output is logged in place of the default message.

## 15. Lambda triggers

Triggers are invoked through `lambdaSvc.Invoke`, which the Cognito service receives at construction in the same way SNS and EventBridge do in `internal/cli/server.go`. Events use the AWS trigger event shape (`version`, `triggerSource`, `region`, `userPoolId`, `userName`, `callerContext`, `request`, `response`).

| Trigger | Phase | Trigger sources |
|---|---|---|
| Pre sign-up | 1 | `PreSignUp_SignUp`, `PreSignUp_AdminCreateUser`. Honors `autoConfirmUser`, `autoVerifyEmail` and `autoVerifyPhone`. |
| Post confirmation | 1 | `PostConfirmation_ConfirmSignUp`, `PostConfirmation_ConfirmForgotPassword` |
| Pre token generation (V1 and V2) | 1 | `TokenGeneration_*`. Honors claim add, override and suppress, and group overrides. |
| Pre authentication, post authentication | 1 | |
| Custom message | 1 | |
| Define, create and verify auth challenge | 2 | Needed for `CUSTOM_AUTH`. |
| User migration | Out of scope | |

A trigger error fails the operation with `UserLambdaValidationException` and the function's error message, as in AWS. Trigger invocations appear in the existing trace view as child spans of the Cognito call.

## 16. API Gateway authorizers

This is a separate workstream that Cognito depends on in order to be useful.

**API Gateway v2 (HTTP APIs)**

- Add `CreateAuthorizer`, `GetAuthorizer`, `GetAuthorizers`, `UpdateAuthorizer` and `DeleteAuthorizer` for `AuthorizerType: JWT`, with `IdentitySource` (default `$request.header.Authorization`) and `JwtConfiguration.Issuer` and `Audience`.
- Honor `AuthorizationType`, `AuthorizerId` and `AuthorizationScopes` on routes.
- Validation covers the signature, `exp`, `nbf`, `iss` (matched against the configured issuer after mapping it to a Tarn pool), and `aud` or `client_id` against `Audience`. When the route has `AuthorizationScopes`, at least one must appear in `scope`.
- Failures return `401 {"message":"Unauthorized"}`. A scope mismatch returns `403 {"message":"Forbidden"}`.
- The event gets `requestContext.authorizer.jwt.claims` and `requestContext.authorizer.jwt.scopes`.
- Terraform: `aws_apigatewayv2_authorizer`, plus `authorization_type` and `authorizer_id` on `aws_apigatewayv2_route`.

The configured `Issuer` in real Terraform is the AWS URL. Tarn must accept an AWS-format issuer on the authorizer and treat it as equivalent to the same pool's local issuer, so unmodified Terraform works.

**API Gateway v1 (REST APIs)**

- Add `CreateAuthorizer`, `GetAuthorizer`, `GetAuthorizers`, `UpdateAuthorizer` and `DeleteAuthorizer` for `Type: COGNITO_USER_POOLS`, with `ProviderARNs` and `IdentitySource`.
- Honor `AuthorizationType: COGNITO_USER_POOLS`, `AuthorizerId` and `AuthorizationScopes` on methods.
- ID tokens are accepted when the method has no scopes. Access tokens are required when scopes are set, as in AWS.
- The event gets `requestContext.authorizer.claims`.

**Later**: Lambda `REQUEST` and `TOKEN` authorizers for both versions. They use the same seam and are a common follow-up request.

## 17. OAuth token endpoint

Phase 1.5, optional. `POST /{domainPrefix}/oauth2/token` (or a path-based equivalent) with `grant_type=client_credentials` and `refresh_token`, using the client secret and resource server scopes. This lets developers test machine-to-machine APIs behind scope-checked authorizers. It is small once resource servers and token minting exist. `authorization_code` requires the hosted UI and stays out of scope.

## 18. Dashboard

A new **Identity** section (`cognito-section.svelte`) following the Rack Console section style:

- A pool list with user, client and group counts, and the issuer URL with a copy button.
- Pool detail with tabs for users, groups, clients and settings.
- The users table shows username, `sub`, status, enabled state, email and verification state, groups and creation date. Row actions: confirm, enable or disable, reset password, set password, sign out everywhere and delete.
- A **Pending codes** panel listing unexpired codes, with copy buttons.
- A **Mint token** action that issues tokens for a chosen user and client, bypassing the password, for quick `curl` testing. This is a Tarn-only convenience and is clearly labeled as such.
- A token decoder where pasting a JWT shows its header, claims, expiry and whether Tarn's key verifies it.

Admin routes live under `/_tarn/admin/cognito/...` and are added to the overview payload in the same way as the other services.

## 19. Error model

All errors use AWS codes and messages. The minimum set is `InvalidParameterException`, `ResourceNotFoundException`, `NotAuthorizedException`, `UserNotFoundException`, `UserNotConfirmedException`, `UsernameExistsException`, `AliasExistsException`, `InvalidPasswordException`, `CodeMismatchException`, `ExpiredCodeException`, `LimitExceededException` (after 5 wrong codes), `PasswordResetRequiredException`, `UserLambdaValidationException`, `InvalidLambdaResponseException`, `GroupExistsException`, `InvalidUserPoolConfigurationException` and `NotImplementedException`.

The Cognito service should expose its operations at method seams so the planned infrastructure disruption feature (`docs/infrastructure-disruptions-prd.md`) can add Cognito rules later without restructuring.

## 20. Delivery plan

| Phase | Scope | Exit criteria |
|---|---|---|
| 1a | Dispatch, account index, store, control plane, Terraform resources | Example Terraform applies, plans clean and destroys. |
| 1b | Sign-up, confirm, sign-in (password flows), refresh, sign-out, forgot password, tokens, JWKS, codes | SDK v3 end-to-end test passes. Tokens verify with `jose`. |
| 1c | Lambda triggers (pre sign-up, post confirmation, pre token generation, pre and post auth, custom message) | Trigger tests pass. Traces show spans. |
| 1d | API Gateway v2 JWT authorizer and v1 `COGNITO_USER_POOLS` authorizer | `401`, `403` and pass-through tests pass for both versions. |
| 1e | Dashboard section, docs page, `api-coverage.md` update, example in `examples/terraform/cognito-apigw-lambda` | Manual verification in the UI. |
| 1.5 | `client_credentials` token endpoint, SRP if deferred | |
| 2 | `CUSTOM_AUTH`, Lambda authorizers, TOTP MFA | |

Every phase ships with Go tests in the style of the existing service tests, plus a Node compatibility test under `compatibility-test/node` that uses the real AWS SDK.

## 21. Open questions

1. **Include `USER_SRP_AUTH` in phase 1?** Amplify's `signIn` uses SRP by default, as does `amazon-cognito-identity-js`. Without SRP, frontends must switch to `USER_PASSWORD_AUTH` in local builds, which is a code difference between local and production. SRP is well specified: 3072-bit group, `PASSWORD_VERIFIER` challenge, and HKDF-derived signature over pool name, username, secret block and timestamp. It is roughly 300 lines plus tests. **Recommendation:** include it in phase 1b if the team uses Amplify or a browser SDK. Otherwise defer it to 1.5.
2. **Default issuer mode?** `local` works with standard OIDC libraries and browsers. `aws` works with unmodified `CognitoJwtVerifier` as long as JWKS is hydrated. Which libraries does the target application use to verify tokens?
3. **Authorizers in the same milestone?** API Gateway authorizers are needed to test protected routes end to end, but they are a separate code change in two services. Should Cognito ship without them first, or should they land together?
4. **Password hashing cost.** Bcrypt at cost 10 adds about 50 ms to each sign-in. Is that acceptable under the `tarn-load` harness, or should Tarn use a lower cost locally?
5. **Pool ID stability.** Should pool and client IDs be derived deterministically from name and account, so they survive a `tarn flush` plus re-apply and frontend `.env` files stay valid? AWS generates random IDs, but determinism is a convenient local deviation.
