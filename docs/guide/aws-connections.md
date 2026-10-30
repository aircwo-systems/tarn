# AWS credential vault

Tarn can save named AWS connections using an existing AWS profile or manual credentials. Profiles are the default. Manual credentials are encrypted on your machine. Tarn verifies the AWS account and identity through STS. This release does not browse AWS resources or pass saved credentials to local functions and tasks.

## Start and unlock

Build the dashboard and binary from the repository:

```bash
make ui-build
make build
./build/tarn start --host 127.0.0.1 --ui
```

In another terminal, run:

```bash
./build/tarn vault unlock
```

Open the single-use dashboard link it prints within five minutes. The link establishes a local owner session lasting eight hours and removes its unlock token from the address bar. In **Settings → AWS connections**, choose **Add AWS connection**.

Use the same data directory in both terminals if you set a custom one:

```bash
./build/tarn --data-dir /path/to/tarn-data vault unlock
```

The default server bind address is `0.0.0.0`. AWS connection management is disabled on that address; explicitly use `--host 127.0.0.1`. Manual credentials require Vault encryption. Profile-only connections also work with `--vault-key ""`. The ordinary emulator APIs continue to work on their existing bind configuration.

## Save and manage a connection

Enter a name and a commercial AWS region, then choose the credential source.

**Existing AWS profile** is the default. Select or enter a profile from the AWS configuration on the Tarn server. Tarn saves its name and region, and resolves credentials when you validate or test the connection. It does not copy profile keys or SSO tokens into Tarn's store. Supported profiles use IAM Identity Center SSO, access keys in shared AWS files, or `credential_process` helpers already configured on the machine.

The SDK refreshes SSO credentials where the configured session permits it. When company policy requires a new login, Tarn shows **Sign in again** with an `aws sso login --profile 'profile-name'` command. Run it in your terminal, then choose **Test connection**. Tarn does not launch a login or override provider expiry policies. A new assumed-role session for the same account and role is accepted during testing; a different account or role still requires confirmation through a source change. Key-based profiles are read again on each test, so rotating their keys does not require editing the Tarn connection.

**Manual credentials in Vault** accepts an access key ID and secret access key, plus a session token for temporary credentials. You can paste the entire credential block copied from AWS into **Paste AWS credentials**. Tarn fills each key into its own field and selects the credential type automatically. The same bulk paste works inside the individual key fields. Supported formats include shell exports, PowerShell, Windows `set` assignments, shared-credentials INI, and AWS credential JSON. Incomplete or duplicate credential sets are rejected. The pasted block is parsed locally as data, never executed, and is not retained in the paste box.

Choose **Validate and save** after entering or pasting credentials. Tarn ignores ambient emulator credentials and endpoint overrides; it uses the selected profile or the exact manual credentials. It does not make AWS requests while you type or paste.

After validation, Tarn shows the AWS account ID, identity ARN and test time. **Identity verified** means STS recognised the credentials. AWS service permissions have not been checked, and the credentials may have write permissions.

You can test a saved connection, edit its details, change its credential source or delete it locally. Switching between profiles and manual credentials is supported. A source change that changes the AWS account or identity requires confirmation. Failed validation leaves the previous source intact. Manually pasted temporary credentials require replacement after expiry.

**Lock** ends the browser's owner session. Unlocking again also replaces any prior owner session. Deleting a connection removes its local record but does not revoke its AWS keys or delete AWS resources.

## Storage and recovery

Connections are stored separately from emulated Secrets Manager resources under `<data-dir>/aws-connections/connections.json`. Profile records contain only the reference and connection metadata. Manual credential payloads use Vault's AES-256-GCM encryption. The directory and files are owner-only. Saved connections persist independently of the emulator's `--persist` setting.

The key defaults to `~/.tarn/vault.key`. Keep the original key if you move or back up manual connections. Existing encrypted records are preserved if the key is wrong or data cannot be loaded. If the key file is missing while manual records exist, startup stops with recovery guidance rather than creating a replacement key. Restore the original key before restarting. A mixed store containing manual records still requires their original key; a profile-only store has no such requirement.

Encryption protects the stored credential payload. Someone who can read both the key and credential files can decrypt it. This feature does not add a user password or protection from other processes running as your operating-system user.

The process-lifetime `owner.json` file is a private capability used by `tarn vault unlock`. It is removed on normal shutdown. Do not share it. Use one running Tarn process per data directory.

Credentials cannot be revealed or exported through the dashboard, generic secret APIs, the secrets proxy or MCP. Local account deletion, resource resets and emulated service snapshots do not include connection records.

## Frontend development

When running the frontend separately, explicitly allow its loopback origin on the backend and point Vite's proxy at the backend:

```bash
TARN_AWS_UI_ORIGIN=http://127.0.0.1:4173 ./build/tarn start --host 127.0.0.1
```

```bash
cd ui
TARN_UI_PROXY_TARGET=http://127.0.0.1:4566 bun run dev -- --host 127.0.0.1
```

Then run `./build/tarn vault unlock` from the repository root. Its link uses the configured frontend origin. Use that exact hostname and port. Cross-origin API calls, non-loopback peers and forwarded ownership headers are rejected; use the same-origin development proxy.

## Current limits

- Local, single-user management only.
- Commercial AWS regions only.
- Existing SSO, shared-key and credential-process profiles, or manual long-lived keys and temporary sessions.
- SSO renewal follows the SDK and provider session policy. Interactive sign-in stays in the AWS CLI.
- Assume-role, web-identity and instance/container credential-source profiles are not supported in this release.
- No AWS resource operations or credential injection into workloads.

AWS documents the identity response in [GetCallerIdentity](https://docs.aws.amazon.com/STS/latest/APIReference/API_GetCallerIdentity.html). Profiles and IAM Identity Center are described in the [AWS SDK for Go configuration guide](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html).
