# AWS credential vault PRD

Status: Proposed  
Date: 2026-10-17  
Target: Local AWS connection management  
Primary area: Vault, connection storage, dashboard settings, STS integration

Implementation: The initial workflow is available in the working tree. See [setup and current limits](guide/aws-connections.md). Live AWS validation remains opt-in; automated checks use a synthetic STS response and real encrypted storage.

## 1. Summary

A developer should be able to add a named AWS connection using an existing AWS profile or manual credentials, and validate which AWS account and identity it belongs to. Profiles are the default; manual credentials are encrypted on their machine. They can return later to test the connection, replace its credentials or delete it.

The first release provides credential management and identity validation. Saved connections prepare Tarn for future AWS integrations. Saving a connection does not redirect the dashboard, discover AWS resources or give local Lambda and ECS workloads access to the credentials.

Reuse Tarn's Vault encryption in a separate connection store. Connection credentials must never become ordinary emulated Secrets Manager resources.

## 2. Problem and user value

Tarn started with local emulated accounts and encrypted secret storage. Real AWS connections need their own credential sources and ownership boundary, separate from those resources.

A developer preparing to connect Tarn to AWS needs to know:

- Where can I save the credentials without putting them in source files or browser storage?
- Which AWS account and identity do these credentials actually belong to?
- Did my session expire, or is AWS temporarily unreachable?
- Can I replace or remove the credentials without affecting local resources?

The immediate value is a named, validated record of an AWS identity. Resource browsing and deployment remain separate features. The UI must make this scope clear before the developer enters credentials.

## 3. Goals and release boundary

The first release must:

- Create multiple named AWS connections with a configurable credential source. Default to an existing local AWS profile; offer manual access keys with an optional session token.
- Discover profile names without exposing credentials. Use SDK providers for SSO, shared-file keys and configured credential helpers.
- Accept the complete credential block copied from AWS and split each value into its field before validation, without executing or retaining the block.
- Store a default AWS region for each connection.
- Validate credentials through AWS STS `GetCallerIdentity` and display the returned account ID and identity ARN.
- Encrypt credentials at rest and retain connections across restarts.
- Support manual connection testing, metadata editing, credential replacement and deletion.
- Keep connection management separate from Tarn's emulated account selector and resource lifecycle.

The first release excludes:

- Browsing or changing AWS resources, including EC2 instances.
- Switching the existing dashboard to an AWS account.
- Credential export, secret reveal, shell environment generation or a credential vending endpoint.
- Injecting credentials into Lambda, ECS, the secrets proxy or MCP tools.
- Shared or remotely hosted credential management and multiple human users.
- An app-owned interactive IAM Identity Center login, AssumeRole, web-identity or instance/container credential-source profiles. Provider refresh uses SDK behavior within the permitted session; login and MFA stay in existing provider tools.
- A general-purpose credential manager or integration with HashiCorp Vault.

Profile connection records contain a profile reference and connection metadata. Never copy profile keys or cached SSO tokens into Tarn. Resolve the selected profile again when used, so file rotation is picked up. If SSO sign-in is required, show the appropriate AWS CLI command and allow another test after login.

## 4. User experience

### Entry point

Add an **AWS connections** area in Settings. Keep the existing local proxy and MCP connection information intact. Explain that AWS connections store credentials for future integrations and currently support identity testing only.

An empty state offers **Add AWS connection**. The form asks for a unique display name, region and credential source. Existing AWS profile is selected by default, with profile-name suggestions. Manual credentials ask for an access key ID, secret access key and optional session token. A bulk paste box accepts AWS export statements, PowerShell, Windows set assignments, shared-credentials INI and AWS credential JSON. Successful paste fills the individual masked fields, selects the matching credential kind and clears the paste box. Saving still requires Validate and save. Secret fields are masked. Do not request an AWS account ID; AWS supplies it during validation.

### Add a connection

1. The developer enters the connection details.
2. They choose **Validate and save**.
3. The backend validates the supplied credentials with STS.
4. On success, Tarn durably saves the profile reference or encrypted manual credential payload and shows its AWS account ID, identity ARN, region and validation time.
5. On failure, Tarn explains the failure and leaves the form available for correction. It does not create a saved connection.

The success message must say **Identity verified**, with supporting copy that AWS service permissions have not been checked. Do not describe a successful STS response as proof of read-only access or permission to deploy.

Never retain entered secrets in browser storage, URLs, analytics or client logs. Clear secret fields after a successful save or when the form closes. Do not echo them in the response.

### Manage a saved connection

Each connection shows its name, account ID, identity ARN, region, credential source and latest test result. Manual connections show a masked access key suffix; profile connections show the profile name. Available actions are **Test connection**, **Edit details**, **Change credential source** and **Delete**.

- Editing the name does not require re-entering credentials.
- Changing the region requires another identity test before saving.
- Changing credential source can switch between an existing profile and a complete manual credential set, and validates it before replacing the old source. An omitted session token means the new set has no session token.
- If replacement changes the AWS account or identity ARN, show both identities and require explicit acknowledgement before committing the replacement.
- A failed test or replacement preserves the existing saved credentials.
- Deletion removes the local connection and any cached credentials. Explain that deletion does not revoke the keys in AWS or delete AWS resources.

Adding a connection never selects an AWS environment or changes the current local account.

## 5. Validation and status

Use the official AWS SDK for Go v2 to sign STS requests. Supply the connection's credentials and region explicitly. Use only the selected profile or the supplied manual credentials. Do not fall back to ambient environment credentials or another profile when validation fails. Profile resolution uses the SDK's credential providers without inheriting emulator endpoint overrides, including in SSO refresh clients.

This distinction matters because Tarn users commonly have dummy AWS credentials and a local `AWS_ENDPOINT_URL` configured for emulation. Validation must use the SDK's real regional STS endpoint, ignoring endpoint overrides from the environment and shared configuration. Arbitrary endpoint URLs are outside this release. Support the standard commercial AWS partition first; reject unsupported partitions explicitly.

Set a 10-second overall validation deadline, including retries, and propagate cancellation. Test only on explicit user actions. Opening Settings or polling the dashboard must not make AWS requests.

Persist the last successful identity and validation time separately from the latest test result. Show these results distinctly:

- **Identity verified**: the last test succeeded. Always show when it ran.
- **Credentials rejected**: AWS rejected the credentials; replacement is available.
- **Session expired**: AWS reported expiry, or an explicitly supplied expiry has passed.
- **Sign in again**: the selected SSO profile needs interactive reauthentication. Show a safely quoted `aws sso login --profile ...` command; do not execute it from Tarn.
- **Could not verify**: a network, timeout or AWS service failure prevented verification. Keep the last successful identity visible as historical information.
- **Not tested this session**: a stored connection has been loaded after restart without a new test.

Do not infer a session expiry from an access key ID or from `GetCallerIdentity`. Expiry is unknown unless supplied by the credential source or reported by AWS. Manually entered temporary credentials require manual replacement when they expire.

Errors must be useful and sanitised. Never render raw signed requests, credentials or unfiltered SDK diagnostics.

## 6. Storage and access requirements

### Separate credential storage

Use an instance-wide store keyed by opaque connection IDs. It belongs to the local Tarn installation, independently of emulated AWS accounts.

Reuse `internal/secrets/Vault` encryption, with a separate versioned file under a private connection directory. Encrypt the complete manual credential payload, including the access key ID, secret key and session token. Profile references have no credential payload. Store only the masked suffix in returned metadata.

- Connection directories use owner-only permissions, and files use mode `0600`.
- Connection persistence is independent of optional emulated resource persistence. A successful save survives restart even when resource persistence is disabled.
- Save, replace and delete operations acknowledge success only after an atomic durable write. A failed write leaves the previous record usable.
- If Vault encryption is disabled, profile-only connections remain available and manual credentials are disabled. A mixed store with manual records still requires their original key to load. Never fall back to plaintext manual credentials.
- If an existing store cannot be decrypted, leave it intact and disable connection use with a recovery message. A missing key must not cause existing encrypted records to be overwritten with an empty store.
- Reuse of the current Vault key does not provide a user unlock password or protection against someone who can read both the key and credential files. Describe this accurately in documentation.

Account archive, account deletion, local resource resets and emulator snapshots must not remove or export AWS connection records. Deleting a connection affects only that record. Vault encryption-key rotation and cloud backup are outside the first release. Rotation of keys in an external AWS profile is handled by reloading the profile on use.

### Connection access boundary

The current emulator APIs do not authenticate a human owner. Vault encryption alone is insufficient for a browser feature holding real AWS credentials. A protected local owner session is a release requirement for all connection endpoints, including metadata and identity tests.

For the first release:

- Credential management is available only on a loopback-bound server with an authenticated local owner session. Tarn's existing non-loopback deployments continue to run the emulator with connection management disabled.
- Establish the owner session through a short-lived, single-use bootstrap issued by the local CLI. It must not be retrievable from a public dashboard or unauthenticated API, or appear in server access logs.
- Use a session cookie inaccessible to browser JavaScript, strict same-site handling and origin/host validation. Reject cross-origin access and mutation requests without CSRF protection. Loopback access alone is not authentication.
- Do not trust forwarded headers to establish local ownership. Development proxy support must preserve the same access checks with explicitly configured origins.
- Return metadata only. There is no endpoint to retrieve saved credential values.
- Emulated AWS APIs, generic secret reveal, secrets proxy, runtime containers and MCP cannot read or resolve connection credentials.
- Redact request bodies and credential values from logs and traces. Connection responses use `Cache-Control: no-store`.

This is a local single-user boundary. Remote hosting requires a later design for authenticated users and TLS. The existing Cognito emulator and placeholder SigV4 account headers do not establish owner identity.

## 7. Backend contract and implementation direction

Introduce a connection service that owns encrypted persistence, credential resolution and STS validation. Keep raw credentials inside this backend module except for the initial protected browser submission. Future AWS integrations should request a credential provider by connection ID through an internal interface.

Suggested protected API operations:

- `GET /_tarn/admin/aws-connections`: list metadata.
- `GET /_tarn/admin/aws-connections/profiles`: list local AWS profile names under the same owner-session checks.
- `POST /_tarn/admin/aws-connections`: validate and persist a new connection.
- `PATCH /_tarn/admin/aws-connections/{id}`: edit metadata or validate and replace credentials.
- `POST /_tarn/admin/aws-connections/{id}/test`: test saved credentials and update the latest result.
- `DELETE /_tarn/admin/aws-connections/{id}`: durably remove a connection.

The route prefix does not grant access. Every operation must pass the owner-session checks above, regardless of emulated account headers.

Metadata contains the connection ID, display name, credential kind, region, access key suffix, last successful account ID and ARN, creation/update times and validation history fields. Credentials are write-only fields, never part of metadata responses. Profile, temporary and long-lived sources must be distinct variants. Profile references cannot carry raw credentials, and temporary manual credentials require a session token.

Apply per-connection concurrency control so an older test cannot overwrite the state of newer credentials. Delete and replace invalidate cached providers. A test finishing after deletion must not recreate the record. Bound validation concurrency and reject repeated submissions while an operation is in progress.

Do not refactor the dashboard overview or local service implementations in this release.

## 8. Acceptance criteria

The release is complete when:

1. A developer can add two named connections, see their actual AWS identities, restart Tarn and find both records intact.
2. Both long-lived credentials and temporary credentials with a session token can be validated.
3. Invalid credentials, expired sessions and unreachable AWS services produce distinct, sanitised feedback.
4. Validation still reaches real AWS when Tarn is launched with dummy AWS credentials and a local endpoint override.
5. Failed creation leaves no saved record. Failed replacement or disk writes preserve the prior connection.
6. A changed identity is shown before a replacement is committed, and failed acknowledgement leaves the old connection intact.
7. Successful STS validation never implies that service permissions were checked or that the keys are read-only.
8. Credential files contain no plaintext credentials. Disabled encryption, a missing key and a wrong key never cause plaintext fallback or destructive recovery.
9. All connection endpoints reject unauthenticated requests. Cross-origin requests, invalid hosts and CSRF attempts cannot submit credentials or trigger identity tests.
10. Generic secret APIs, MCP, runtime containers, snapshots and local account operations cannot expose or delete connection credentials.
11. Replacement and deletion invalidate cached credentials, including during concurrent tests.
12. Browser storage, API responses, request logs and traces contain no secret keys or session tokens.
13. Opening Settings, dashboard polling and ordinary local workloads make no AWS calls using saved connections.
14. Delete survives restart and clearly tells the developer that AWS keys remain active until revoked in AWS.
15. New connections default to profiles, profile-only connections work without a Vault key, and profile credentials are never copied into the connection store.
16. A rotated shared-file profile key is picked up on the next test; SSO expiry produces a sign-in-again state without overriding the provider policy.
17. Bulk paste splits every supported AWS credential field, selects temporary or long-lived mode, rejects incomplete/duplicate sets and never executes copied text.

Use real encrypted storage in automated persistence tests and an injected STS client for deterministic error and concurrency tests. Keep production endpoint resolution separate from test injection. Verify the browser workflow with synthetic credentials against a test backend. An opt-in test with a developer-provided AWS identity should confirm signing and identity discovery; live AWS credentials are not required for normal CI.

## 9. Delivery sequence

1. Implement the protected local owner session and encrypted connection store. Verify isolation from ordinary secrets and resource resets.
2. Add explicit STS credential validation, metadata-only APIs and safe replacement/deletion behavior.
3. Add the Settings workflow and complete the acceptance checks.

These are implementation steps for one release. Do not ship a credential entry form before its access and storage requirements work.

Follow-up PRDs can add more profile provider types, an interactive sign-in flow, a read-only AWS resource adapter, or explicit use by local workloads. Each consumer must define its own AWS permissions and user consent. No consumer gets access merely because a connection exists.

## 10. Existing code and source references

The following are implementation facts at the time of writing:

- [Vault encryption](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/secrets/vault.go) provides AES-256-GCM encryption and a local key file.
- [Secret persistence](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/secrets/store.go) encrypts ordinary secrets when Vault is configured, and follows the emulator's persistence setting. The connection store needs mandatory encryption for manual credentials and independent persistence; profile references do not contain credentials.
- [Secret reveal](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/admin/handler.go) returns ordinary secret values to the dashboard. It must never resolve connection records.
- [Server routes](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/server.go) and [account resolution](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/account/account.go) route emulator requests without a human owner session.
- [Default configuration](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/config/config.go) binds the server to `0.0.0.0`. The local-only connection requirement therefore needs an explicit loopback setup and clear UI guidance.
- [Frontend API](../ui/src/lib/api.ts) uses a configured Tarn base URL and placeholder account signatures. Saved AWS credentials do not change that behavior.
- [AWS GetCallerIdentity](https://docs.aws.amazon.com/STS/latest/APIReference/API_GetCallerIdentity.html) returns account and identity information without establishing service permissions.
- [AWS SDK for Go v2 configuration](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html) documents explicit credential providers, session tokens, profiles and IAM Identity Center support.
