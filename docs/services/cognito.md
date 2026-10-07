# Cognito User Pools

User directories with sign-up, sign-in and signed JWTs.

<span class="status-badge status-partial">Partial</span>

Tarn emulates Cognito User Pools (`cognito-idp`) so an application can sign users up and in, receive real RS256 tokens, and verify them against the pool's JWKS without an AWS account. Identity Pools (`cognito-identity`), the hosted UI and federation are not implemented.

## What is enforced

Tarn does not enforce IAM, so any caller can run control-plane and `Admin*` operations. Cognito's own authentication rules are the behavior under test, so Tarn enforces them:

- Password policy and required attributes, taken from the pool settings.
- User status: unconfirmed, disabled and `RESET_REQUIRED` users cannot sign in, and `FORCE_CHANGE_PASSWORD` users get a `NEW_PASSWORD_REQUIRED` challenge. A temporary password that has expired is rejected.
- Which auth flows each app client allows.
- Token signatures, expiry, and refresh token revocation.
- `PreventUserExistenceErrors` on app clients.

Email and SMS are never sent. Confirmation, verification and reset codes are logged and exposed locally instead. See [Reading codes](#reading-codes).

## Supported Operations

Requests use the AWS JSON 1.1 protocol: `POST /` with `X-Amz-Target: AWSCognitoIdentityProviderService.<Action>`. Public operations are unsigned, as in AWS. Tarn finds the owning account from the `ClientId`, access token or pool ID in the request.

| Area | Operations |
|------|------------|
| User pools | `CreateUserPool`, `DescribeUserPool`, `UpdateUserPool`, `DeleteUserPool`, `ListUserPools`, `AddCustomAttributes` |
| MFA configuration | `GetUserPoolMfaConfig`, `SetUserPoolMfaConfig` |
| Tags | `ListTagsForResource`, `TagResource`, `UntagResource` |
| App clients | `CreateUserPoolClient`, `DescribeUserPoolClient`, `UpdateUserPoolClient`, `DeleteUserPoolClient`, `ListUserPoolClients` |
| Domains | `CreateUserPoolDomain`, `DescribeUserPoolDomain`, `UpdateUserPoolDomain`, `DeleteUserPoolDomain` |
| Resource servers | `CreateResourceServer`, `DescribeResourceServer`, `UpdateResourceServer`, `DeleteResourceServer`, `ListResourceServers` |
| Groups | `CreateGroup`, `GetGroup`, `UpdateGroup`, `DeleteGroup`, `ListGroups`, `ListUsersInGroup`, `AdminAddUserToGroup`, `AdminRemoveUserFromGroup`, `AdminListGroupsForUser` |
| Admin users | `AdminCreateUser`, `AdminGetUser`, `AdminUpdateUserAttributes`, `AdminDeleteUserAttributes`, `AdminSetUserPassword`, `AdminEnableUser`, `AdminDisableUser`, `AdminDeleteUser`, `AdminConfirmSignUp`, `AdminResetUserPassword`, `AdminUserGlobalSignOut`, `AdminListUserAuthEvents`, `ListUsers` |
| Admin auth | `AdminInitiateAuth`, `AdminRespondToAuthChallenge` |
| Sign-up | `SignUp`, `ConfirmSignUp`, `ResendConfirmationCode` |
| Sign-in | `InitiateAuth`, `RespondToAuthChallenge`, `GlobalSignOut`, `RevokeToken` |
| Passwords | `ForgotPassword`, `ConfirmForgotPassword`, `ChangePassword` |
| Signed-in user | `GetUser`, `UpdateUserAttributes`, `DeleteUser`, `GetUserAttributeVerificationCode`, `VerifyUserAttribute` |
| MFA preference | `SetUserMFAPreference`, `AdminSetUserMFAPreference` |

Any other action returns `NotImplementedException` and logs `[cognito] unhandled action <name>`.

## Auth flows

| Flow | Notes |
|------|-------|
| `USER_PASSWORD_AUTH` | Requires `ALLOW_USER_PASSWORD_AUTH` on the app client |
| `ADMIN_USER_PASSWORD_AUTH` | Also accepted as `ADMIN_NO_SRP_AUTH`. Requires `ALLOW_ADMIN_USER_PASSWORD_AUTH` |
| `USER_SRP_AUTH` | Compatible with `amazon-cognito-identity-js` and Amplify |
| `REFRESH_TOKEN_AUTH` | Also accepted as `REFRESH_TOKEN` |

The challenges `NEW_PASSWORD_REQUIRED`, `PASSWORD_VERIFIER`, `SMS_MFA` and `EMAIL_OTP` are supported. `CUSTOM_AUTH` is accepted but returns an error, because the custom auth Lambda triggers are not implemented.

An app client that lists no `ALLOW_*` flows allows SRP and refresh, as the AWS defaults do.

## Tokens

Each pool has its own RSA key. ID and access tokens are RS256 JWTs with AWS-shaped claims (`sub`, `iss`, `aud` or `client_id`, `token_use`, `cognito:username`, `cognito:groups`, `origin_jti`, `jti`, `auth_time`, `exp`). Refresh tokens are tracked per sign-in, so `GlobalSignOut`, `AdminUserGlobalSignOut` and `RevokeToken` invalidate every token from that session.

Pool signing keys are sealed with the vault key (`--vault-key`) when they are persisted.

Verification endpoints follow the AWS paths on the Tarn port:

```text
GET /<poolId>/.well-known/jwks.json
GET /<poolId>/.well-known/openid-configuration
```

The `iss` claim defaults to `http://localhost:<port>/<poolId>`. Set `TARN_COGNITO_ISSUER=aws` to use the `https://cognito-idp.<region>.amazonaws.com/<poolId>` form that production verification code expects, or give a base URL. `TARN_COGNITO_TOKEN_TTL` shortens token validity for every app client so expiry handling can be tested in seconds. See [Configuration](/guide/configuration#cognito).

## Lambda triggers

Triggers in the pool's `LambdaConfig` run through Tarn's Lambda service:

| Trigger | Runs on |
|---------|---------|
| `PreSignUp` | Sign-up. Can auto-confirm and auto-verify the user |
| `PostConfirmation` | Sign-up confirmation, admin confirmation and forgot password |
| `PreAuthentication` | Before a sign-in succeeds |
| `PostAuthentication` | After a sign-in succeeds |
| `PreTokenGeneration` | Before tokens are issued. Version 1 events change the ID token only. `V2_0` events can also change the access token and its scopes |

A trigger that fails returns `UserLambdaValidationException`. Trigger and token signing calls run outside the service lock, so a cold start does not block other requests.

## Browser apps

Cognito responses carry CORS headers that expose `x-amzn-errortype`, and preflights that ask to send `X-Amz-Target` are answered before S3 sees them. Amplify and `amazon-cognito-identity-js` can call Tarn from any origin.

## Reading codes

Codes are written to the Tarn log and returned by two dashboard routes, so scripted tests can read them:

```bash
curl http://localhost:4566/_tarn/admin/cognito/pools/us-east-1_Ab12Cd34E/codes
curl http://localhost:4566/_tarn/admin/cognito/pools/us-east-1_Ab12Cd34E/users/alice/codes
```

Set `TARN_COGNITO_FIXED_CODE=123456` to use one known value for every code.

## Dashboard

The **Cognito** tab lists pools and shows each pool's settings, app clients, users and pending codes. Per user it can confirm, set a password, reset, enable or disable, sign out and delete, and it can mint tokens for a user without their password. A decoder shows the claims in a pasted token. These actions use `/_tarn/admin/cognito/...` routes, which are Tarn-only and not part of the AWS API.

## Examples

### AWS SDK for JavaScript

```javascript
import {
  CognitoIdentityProviderClient,
  SignUpCommand,
  ConfirmSignUpCommand,
  InitiateAuthCommand,
} from "@aws-sdk/client-cognito-identity-provider";

const cognito = new CognitoIdentityProviderClient({
  endpoint: "http://localhost:4566",
  region: "us-east-1",
});

await cognito.send(new SignUpCommand({
  ClientId: clientId,
  Username: "alice@example.com",
  Password: "Passw0rd!",
}));

// Read the code from the dashboard or the codes route
await cognito.send(new ConfirmSignUpCommand({
  ClientId: clientId,
  Username: "alice@example.com",
  ConfirmationCode: "123456",
}));

const { AuthenticationResult } = await cognito.send(new InitiateAuthCommand({
  ClientId: clientId,
  AuthFlow: "USER_PASSWORD_AUTH",
  AuthParameters: { USERNAME: "alice@example.com", PASSWORD: "Passw0rd!" },
}));
```

### Terraform

<div class="example-block">
<div class="lang">HCL (Terraform)</div>

```hcl
provider "aws" {
  endpoints {
    cognitoidp = "http://localhost:4566"
  }
}

resource "aws_cognito_user_pool" "main" {
  name                     = "app-users"
  auto_verified_attributes = ["email"]
}

resource "aws_cognito_user_pool_client" "web" {
  name            = "web"
  user_pool_id    = aws_cognito_user_pool.main.id
  generate_secret = false
  explicit_auth_flows = [
    "ALLOW_USER_PASSWORD_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]
}

resource "aws_cognito_user_group" "admins" {
  name         = "admins"
  user_pool_id = aws_cognito_user_pool.main.id
}
```

</div>

The resources `aws_cognito_user_pool`, `aws_cognito_user_pool_client`, `aws_cognito_user_pool_domain`, `aws_cognito_user_group`, `aws_cognito_user`, `aws_cognito_user_in_group` and `aws_cognito_resource_server` apply, plan without drift and destroy.

## Persistence

With `--persist`, pools, clients, users, groups and refresh sessions survive a restart, and tokens issued before the restart still verify. Passwords are stored as bcrypt hashes. In-progress auth challenges are never persisted.

## Known Limitations

- **No API Gateway authorizers.** API Gateway stores `AuthorizerId` and `AuthorizationType` but does not run JWT or `COGNITO_USER_POOLS` authorizers, so a route never rejects a request for a missing or invalid token. Verify tokens in your Lambda against the pool's JWKS.
- **Identity Pools** (`cognito-identity`) are not implemented.
- **Hosted UI and OAuth endpoints** (`/oauth2/authorize`, `/login`) are not implemented. `CreateUserPoolDomain` is stored so Terraform works.
- **Federation** with SAML, OIDC and social providers is not implemented.
- **TOTP MFA** is not implemented. SMS and email MFA run when the pool's MFA is `ON` or `OPTIONAL`, with codes exposed locally.
- **Custom auth** Lambda triggers are not implemented.
- **Advanced security**, risk scoring, import jobs and passwordless flows are not emulated.

## See Also

- [Configuration](/guide/configuration#cognito) for the Cognito environment variables
- [Terraform](/guide/terraform) for provider setup
- [Lambda](/services/lambda) for trigger functions
- [API coverage](/reference/api-coverage)
