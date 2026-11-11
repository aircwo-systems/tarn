# Cognito passwordless scope

Research date: 2026-11-11. Originally a proposed scope. Native OTP implementation now accompanies this note; see implementation status below.

## Implementation status

Implemented native `USER_AUTH` email/SMS OTP, preferred and selected factors, password/SRP choices, public/admin calls, passwordless admin creation, sign-up/confirmation sessions and immediate sign-in, contact verification, first-password setup, ephemeral per-session codes and unknown-user simulation. Pool/client validation rejects incompatible OTP/MFA/tier settings. Existing JWT, refresh, revocation and sender infrastructure is reused.

Tests cover lifecycle, identity/contact/client/session binding, concurrent proof consumption, aliases, secret hashes, Lambda events and failures, optional MFA and restart behavior. A real AWS SDK v3 smoke fixture is in [compatibility-test/cognito-passwordless](../compatibility-test/cognito-passwordless/README.md). It runs against Tarn and does not establish live AWS parity.

Remaining compatibility work: measure AWS OTP format, lifetime, retry limits, concurrent-attempt invalidation, simulated-challenge randomization, temporary/reset status edge cases and invitation sender behavior. Current local behavior uses six-digit codes, auth-session expiry, retry until expiry and session-isolated codes. Passwordless admin creation does not deliver an invitation. These differences are explicit limitations, not verified AWS defaults. Passkeys, factor-inspection APIs and custom auth are deferred.

The repository-findings table and estimates below record the original assessment; consult [the service guide](services/cognito.md) for current behavior.

## Recommendation

Ship native Cognito `USER_AUTH` with email and SMS OTP first. Include both preferred-factor and challenge-selection paths, public and admin APIs, and passwordless onboarding. Reuse Tarn's token issuance, Lambda invocation, local code exposure and persistence infrastructure. Keep passkey verification and Lambda-driven `CUSTOM_AUTH` as separate workstreams.

AWS enables native choice-based sign-in with pool `Policies.SignInPolicy.AllowedFirstAuthFactors` and client `ALLOW_USER_AUTH`. OTP and passkeys are Essentials/Plus features. `ALLOW_USER_AUTH` also permits password and SRP choices without their separate client flow flags, and explicitly excludes `CUSTOM_AUTH`. The OTP milestone must support password/SRP selection through existing implementations when eligible, including credentials supplied with selection where AWS permits them. Do not advertise a factor whose challenge cannot complete. Sources: [AWS client configuration](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_UserPoolClientType.html), [AWS feature plans](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-sign-in-feature-plans.html).

## AWS compatibility requirement

AWS Cognito is the behavioral reference for every supported path. The same SDK requests and pool/client configuration must work against AWS and Tarn after changing endpoint and credentials. Match request and response fields, challenge transitions, factor eligibility, validation, errors, user status changes and trigger events. Sources: [InitiateAuth](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_InitiateAuth.html), [RespondToAuthChallenge](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_RespondToAuthChallenge.html).

Run the same contract scenarios against Tarn and an authorized disposable AWS pool. Compare stable fields and behavior while normalizing generated IDs, timestamps, opaque sessions, OTP values and token signatures. Check token claims and signatures independently. Capture AWS region, SDK version, configuration and observation date with fixtures. Where official documentation is ambiguous, verify AWS behavior before implementing a local rule. No live AWS comparison has been performed for this scope.

Keep Tarn's existing local testing exceptions explicit: no IAM enforcement, no real email/SMS delivery, local code inspection and opt-in fixed codes. These exceptions must not change factor eligibility or proof validation. Session storage can differ internally, but externally visible concurrency, retry and replay behavior must follow AWS. Passkeys remain deferred and prevent claiming complete AWS passwordless coverage; this milestone is native OTP compatibility.

## Repository findings

No `AGENTS.md` or `CLAUDE.md` was found at the repository root, its checked ancestors, or under `internal` and `docs`. [CONTRIBUTING.md](../CONTRIBUTING.md) asks for scoped changes, AWS compatibility, tests with code changes, and `make test`/`make lint` before a PR. This research changes only this document; existing unrelated working-tree changes are outside its scope.

| Area | Current evidence | Gap |
|---|---|---|
| Pool configuration | [model.go](../internal/cognito/model.go) defines `SignInPolicyType` and `UserPoolTier`; [pools.go](../internal/cognito/pools.go) defaults factors to `PASSWORD` and tier to `ESSENTIALS`. | Storing these settings is not runtime support. Add shared validation for create/update and MFA changes, including factor names, tier and incompatible configurations. |
| Passwordless sign-up | [public.go](../internal/cognito/public.go), `passwordlessSignUp`, allows omitted/blank password with a matching OTP contact. [service_test.go](../internal/cognito/service_test.go), `TestSignUpPasswordless`, checks sign-up, confirmation and later password assignment. | Existing tests never authenticate with an OTP first factor. The current flow can create a passwordless user who cannot sign in natively. |
| Admin creation | [users.go](../internal/cognito/users.go), `AdminCreateUser` and `setTemporaryPassword`, always create/generate a temporary password and use `FORCE_CHANGE_PASSWORD`. | Add passwordless admin creation when pool policy permits it; preserve temporary-password behavior when explicitly requested. |
| Auth entry | [auth.go](../internal/cognito/auth.go), both initiate methods, reject `USER_AUTH`. [clients.go](../internal/cognito/clients.go), `allowsFlow`, has no matching case. | Add flow dispatch, client gating and factor eligibility. |
| Wire/state model | [auth.go](../internal/cognito/auth.go), `AuthOutput`, lacks `AvailableChallenges`; `InitiateAuthInput` lacks `Session`. `authChallenge` tracks name, pool, client, user, SRP and expiry. | Add selection state, allowed factors, explicit first-factor versus MFA context, session-bound OTP data and confirmation handoff. |
| Existing OTP | [mfa.go](../internal/cognito/mfa.go) emits `SMS_MFA` or `EMAIL_OTP` after password proof. [auth.go](../internal/cognito/auth.go) consumes `purposeMFA` then calls `completeAuth`. | The shared `EMAIL_OTP` label must not determine authentication meaning. First-factor OTP requires different status/verification transitions and must not accidentally enter password MFA logic. |
| Codes | [codes.go](../internal/cognito/codes.go) keys persisted codes by user/purpose, applies one hour except sign-up, limits attempts to five, and exposes pending codes. [ids.go](../internal/cognito/ids.go) generates six digits. | Avoid a single user-wide `auth` slot that lets concurrent clients overwrite or consume each other's code. Match AWS expiry, retry and invalidation semantics using documented behavior and observed compatibility fixtures. Existing TTL/attempt/code-length choices are not evidence of AWS native OTP parity. |
| Confirmation/password lifecycle | [public.go](../internal/cognito/public.go), `ConfirmSignUp`, returns an empty result; `ChangePassword` always checks the prior password. | Add confirmation-session output/input and support setting a first password through an authenticated passwordless session. |
| Reusable infrastructure | [tokens.go](../internal/cognito/tokens.go), [triggers.go](../internal/cognito/triggers.go), [senders.go](../internal/cognito/senders.go), [admin.go](../internal/cognito/admin.go). | Reuse token/refresh/revocation and local code inspection; verify trigger ordering and metadata for the new paths. Keep Lambda/signing work outside the service lock. |
| Passkeys/custom auth | [model.go](../internal/cognito/model.go) stores raw `WebAuthnConfiguration` and three custom challenge Lambda ARNs. [auth.go](../internal/cognito/auth.go) rejects custom auth after gating. [handler.go](../internal/api/cognito/handler.go) has no passkey handlers. | Configuration round-trip does not mean registration, assertion verification or custom challenge execution exists. |

[docs/services/cognito.md](services/cognito.md) explicitly excludes passwordless and custom auth. [docs/cognito-prd.md](cognito-prd.md) is historical planning material that puts `USER_AUTH` out of scope; it is not a reliable statement of current implementation. Update both when the scope ships.

## Native OTP contract

Proposed minimum pool configuration uses `EMAIL_OTP`, `SMS_OTP`, or both in `AllowedFirstAuthFactors`, with `MfaConfiguration: OFF`. Use `ESSENTIALS` or `PLUS`, and an app client with `ALLOW_USER_AUTH` plus explicit refresh permission if refresh is needed.

1. `InitiateAuth` or `AdminInitiateAuth` with `AuthFlow: USER_AUTH`, `USERNAME`, and optional `PREFERRED_CHALLENGE`.
2. An eligible preferred OTP sends a code and returns `EMAIL_OTP` or `SMS_OTP`. With no preference, or an unavailable preference, return available choices and a session. `SELECT_CHALLENGE` accepts `ANSWER` naming an offered factor.
3. Respond with the latest session and `EMAIL_OTP_CODE` or `SMS_OTP_CODE`, username and required secret hash. Success verifies the destination, can confirm an unconfirmed user, and issues tokens without password proof.

These are AWS native semantics, including the important unconfirmed-user exception. Do not route OTP success through the existing unconditional `UserNotConfirmedException` in `completeAuth`. Source: [AWS authentication flows](https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-authentication-flow-methods.html). Response field names and secret-hash requirements are specified in [RespondToAuthChallenge](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_RespondToAuthChallenge.html).

Omitted passwords are already partly supported by Tarn and are allowed by [AWS SignUp](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_SignUp.html). Preserve required attributes, writable attributes, secret hashes and alias uniqueness. Do not require a preverified destination just to offer OTP: ownership is established by successful proof. Resolve alias collisions before committing verification or issuing tokens.

`ConfirmSignUp` can return a session consumed by `USER_AUTH` for immediate first sign-in without a second code. Bind this proof to the user, pool, client, delivery method and expiry; make replay fail. Source: [AWS ConfirmSignUp](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_ConfirmSignUp.html). Its exact error behavior and consumption timing need compatibility fixtures.

OTP first factors cannot coexist with required pool MFA. Under optional MFA, enrolled users must use password authentication; unenrolled users may use OTP. Enforce the configuration rule at every mutation entry point, including `SetUserPoolMfaConfig`. Source: [AWS MFA rules](https://docs.aws.amazon.com/cognito/latest/developerguide/user-pool-settings-mfa.html).

With `PreventUserExistenceErrors: ENABLED`, AWS can simulate challenges for unknown users. Returning Tarn's existing immediate wrong-password error would expose a different protocol. Simulated sessions must never create users, deliver messages or issue tokens. The AWS error guide contains inconsistent wording about choice-based suppression, but its detailed section explicitly describes simulated challenges. Capture observed fixtures before promising exact randomization or error text. Source: [AWS user-existence errors](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-user-pool-managing-errors.html).

Use `AuthSessionValidity` for challenge expiry, with AWS's three-minute default and supported 3–15 minute range. Keep auth challenges ephemeral across restart, consistent with Tarn today. Do not adopt the existing five-attempt limit or six-digit generator for native OTP without AWS evidence. Verify code format, code lifetime, retry limits and invalidation separately from session lifetime; do not copy the one-hour reset-code TTL into the new path. Sources: [AWS auth session duration](https://docs.aws.amazon.com/cognito/latest/developerguide/authentication-flow-things-to-know.html), [AWS client type](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_UserPoolClientType.html).

## Passkey and custom auth boundaries

Passkeys need real registration and assertion verification. A later phase should implement `StartWebAuthnRegistration`, `CompleteWebAuthnRegistration`, `ListWebAuthnCredentials` and `DeleteWebAuthnCredential`, persist public credentials, and handle `WEB_AUTHN` challenges. Registration is access-token authorized and must validate client, challenge, RP ID and origin. Start with an established Go WebAuthn implementation after dependency review. Do not accept arbitrary `CREDENTIAL` JSON as authentication. Source: [AWS CompleteWebAuthnRegistration](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_CompleteWebAuthnRegistration.html), [AWS authorization model](https://docs.aws.amazon.com/cognito/latest/developerguide/authentication-flows-public-server-side.html).

Decide localhost/RP-ID and secure-origin behavior before promising browser compatibility. Passkeys need another enabled factor to bootstrap enrollment. Current AWS supports passkey MFA through `FactorConfiguration: MULTI_FACTOR_WITH_USER_VERIFICATION`; avoid an outdated blanket claim that all passkeys are incompatible with MFA. Full passkey/MFA support deserves its own acceptance matrix. Source: [AWS passkey configuration and enrollment](https://docs.aws.amazon.com/en_en/cognito/latest/developerguide/amazon-cognito-user-pools-authentication-flow-methods.html).

`CUSTOM_AUTH` is a separate Lambda-controlled state machine: Define decides continuation/success/failure, Create supplies public/private parameters, Verify checks the answer, and the session carries challenge history. Support requires protected private state, bounded loops, failures, `UserNotFound`, and optional SRP/password-change transitions. Custom sender triggers merely deliver codes; they do not implement custom authentication. Magic links or application-specific proofs belong in that later workstream. AWS custom auth is API-driven and unavailable in managed login. Sources: [AWS custom challenge triggers](https://docs.aws.amazon.com/cognito/latest/developerguide/user-pool-lambda-challenge.html), [AWS authentication considerations](https://docs.aws.amazon.com/cognito/latest/developerguide/authentication-flow-things-to-know.html).

Hosted login, OAuth endpoints, federation, real SES/SNS delivery, identity pools, rate-limit fidelity, threat protection and API Gateway authorizers remain outside this proposal. Native SDK passwordless sign-in does not require implementing hosted login.

## Phases and rough effort

Estimates are engineering days for one developer familiar with Tarn, including review and meaningful tests. They are planning judgments, not measured delivery dates.

| Phase | Concrete files and work | Exit | Effort |
|---|---|---|---|
| 0 | Add official API fixtures and verify unresolved semantics against AWS. Use existing [service_test.go](../internal/cognito/service_test.go) fixtures; add proposed `internal/cognito/passwordless_test.go`. | AWS request/response contract captured, including status transitions, aliases and unknown users. Unverified behavior remains an explicit release blocker for the affected path. | 1–2 days |
| 1 | `model.go`, `pools.go`, `clients.go`, `mfa.go`: factor/tier/MFA validation; `auth.go` plus proposed `passwordless.go`: shared eligibility, selection, preferred OTP, password/SRP reuse, both admin/public paths. | Native OTP challenges complete with safe session binding. | 3–5 days |
| 2 | `codes.go`, `ids.go`, `service.go`, `senders.go`, `triggers.go`, `public.go`, `users.go`: isolated pending OTPs, local inspection, sender events, onboarding, admin creation, confirmation handoff and first password. | Passwordless user lifecycle and concurrent sessions work. | 3–5 days |
| 3 | `internal/api/cognito/handler.go` as needed for changed result types; [cognito_routes_test.go](../internal/api/cognito_routes_test.go); proposed SDK fixture under `compatibility-test/`; docs and examples. | Real AWS SDK round trip and regression checks pass. | 2–3 days |

Budget approximately 9–15 engineering days for the complete native OTP milestone. A deliberately narrower preferred-OTP-only slice is smaller but should not be called complete `USER_AUTH` support. Optional factor-inspection APIs such as `GetUserAuthFactors` and `AdminGetUserAuthFactors` can follow if an actual target client needs them.

Separate estimates: custom auth 5–10 days; passkeys 10–20 days including browser/authenticator tests and dependency review. Passkey MFA and platform-specific compatibility can extend that estimate. No frontend redesign is necessary for the API milestone; reuse the current pending-code display and document new purposes.

## Acceptance tests

- Configuration round-trip for email-only, SMS-only and combined factors; reject unsupported tier, unknown factor, OTP with required MFA, and inconsistent updates through either pool or MFA APIs. Preserve existing default password/SRP behavior.
- AWS SDK request serialization through Tarn's real JSON handler for both admin and public calls. Cover preferred OTP, omitted preference, unavailable preference, valid selection, unoffered selection, `PASSWORD` and `PASSWORD_SRP` choices where eligible. A new SDK fixture is needed; the current `compatibility-test/` tree contains Terraform smoke infrastructure, not an existing Node Cognito suite.
- Sign up with blank/omitted password, authenticate with each OTP, verify contact and confirm the user, validate signed tokens, refresh them, then revoke/sign out. Also test explicit `ConfirmSignUp` and one-use auto-sign-in handoff.
- Check matching versus missing contact, username attributes, verified aliases, alias collisions, case handling, required attributes and client write permissions. Assert failed alias claiming leaves no partly verified state.
- Check disabled/deleted users, changes made while a challenge is pending, passwordless admin-created users, and password-backed temporary/reset states. Specify these status combinations in fixtures rather than inheriting password-flow decisions silently.
- Reject missing/wrong secret hash, mismatched user/client/pool, wrong challenge name, expired/replayed session and code, altered contact after issuance, and invalid confirmation handoff. Wrong-code retry and invalidation behavior must match AWS fixtures, including concurrent attempts and any user-wide limits AWS applies.
- Two simultaneous clients and two simultaneous attempts for the same user cannot consume each other's OTP. Exactly one concurrent successful response wins. Test expiry with the service clock, without sleeps.
- With existence suppression enabled, unknown users get non-authenticating simulated challenges and no local code/sender side effect; legacy mode returns the appropriate missing-user error.
- Optional MFA excludes enrolled users from OTP choices; eligible OTP success never enters `purposeMFA`. Existing password-plus-email/SMS MFA tests continue to pass.
- Custom sender payload/encryption and metadata, pre/post authentication and token-generation events, confirmation behavior and Lambda failures match the chosen AWS fixtures. Use [triggers_test.go](../internal/cognito/triggers_test.go) and [senders_test.go](../internal/cognito/senders_test.go).
- Persist/restart retains passwordless user attributes and issued refresh sessions while invalidating in-progress OTP and confirmation auth sessions. Old snapshots lacking new fields load cleanly. Pending-code APIs show only live local OTPs.
- Run `go test ./internal/cognito ./internal/api/...`, then repository-required `make test` and `make lint` when implementation lands. This document-only research did not run implementation checks.

## Risks and remaining verification

The largest risk is reusing `completeAuth` too broadly: its status and MFA behavior assumes prior password proof. Factor eligibility and successful proof need explicit inputs. Another risk is persisted user-wide code state granting access beyond the originating challenge. Keep new auth proof ephemeral and isolate it by session.

AWS documentation evolves. The current MFA page includes new passkey settings, and different pages have uneven terminology for email MFA and choice-based errors. Verify exact selection response shape, code length, OTP lifetime, retry invalidation, sender trigger metadata, post-confirmation behavior after implicit confirmation, and forced-password/reset statuses against official API examples and, if available, a disposable authorized AWS pool before claiming full parity. This research used official docs and repository inspection, not live AWS execution.

Tarn deliberately exposes codes and bypasses IAM. Document this local testing behavior alongside passwordless examples. It cannot establish production email/SMS deliverability, sandbox eligibility, regional availability or production abuse resistance. Preserve existing password, SRP, refresh and MFA flows while adding native OTP; keep unsupported passkey/custom-auth calls explicit until their implementation is complete.
