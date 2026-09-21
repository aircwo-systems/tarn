# Installation

Get Tarn up and running on your system.

::: info Distribution Roadmap
The current release is <DocsVersionCode />.

Prebuilt Docker images and a Homebrew tap are planned for future releases. For now, use the install script, a release binary, or build Tarn from source.

See the [project roadmap](https://github.com/aircwo-systems/tarn/blob/develop-docs/ROADMAP.md) for the broader direction.
:::

## Install Script (Recommended)

Install the latest release for macOS or Linux (arm64 or amd64):

```bash
curl -fsSL https://aircwo-systems.github.io/tarn/install.sh | sh
```

The script:

- installs `tarn` to `~/.tarn/bin` without `sudo`
- checks the download against the release's `checksums.txt`
- adds `~/.tarn/bin` to your `PATH` in `~/.zshrc`, `~/.bashrc`/`~/.bash_profile`, or fish `conf.d`

Because `curl` downloads the binary rather than a browser, macOS doesn't quarantine it. You don't need `chmod` or `xattr`, and this works on managed Macs. Open a new terminal afterwards and run `tarn start`. Re-run the same command to upgrade.

| Variable | Default | Purpose |
|---|---|---|
| `TARN_VERSION` | `latest` | Install a specific tag, e.g. `v0.2.0` |
| `TARN_INSTALL_DIR` | `~/.tarn/bin` | Install location |
| `TARN_NO_MODIFY_PATH` | unset | Set to `1` to leave shell rc files untouched |

```bash
curl -fsSL https://aircwo-systems.github.io/tarn/install.sh | TARN_VERSION=v0.2.0 sh
```

To read the script before running it:

```bash
curl -fsSL -o install.sh https://aircwo-systems.github.io/tarn/install.sh
less install.sh && sh install.sh
```

To uninstall, run `rm -rf ~/.tarn/bin` and delete the `# tarn` line from your shell rc file. `~/.tarn/data` holds emulator state; remove it too if you want a clean slate.

## From Releases

Download the pre-built binary for your platform:

<ReleaseDownloadTabs />

## From Source

Build Tarn from the latest development version:

```bash
git clone https://github.com/aircwo-systems/tarn.git
cd tarn
make build
./build/tarn start
```

### Lite Build

The lite build strips the embedded dashboard UI and the `lambda`/`s3`/`sqs`/`sns`/`secrets` CLI sub-commands, producing a smaller binary suited for environments that use `awslocal` or another AWS CLI wrapper instead of the built-in commands:

```bash
make build-lite
./build/tarn-lite start
```

The lite binary runs the full API server — all emulated AWS services are available. Only the interactive CLI commands and the bundled dashboard are removed. Use `awslocal` or any AWS SDK to interact with the endpoint as normal.

### Requirements

- **Go 1.26+** (for building)
- **Docker** (required for Lambda execution)
- **make** and standard Unix tools

## Using Docker

Build a local Tarn image from source and run it in Docker:

```bash
# Build image
make docker-build

# Run container
docker run -p 4566:4566 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  tarn:0.1.0-dev
```

The `-v /var/run/docker.sock:/var/run/docker.sock` mount allows Tarn to create Lambda containers.

## With Dashboard

Include the built-in web dashboard:

```bash
# Build with UI
make docker-build-ui

# Run
docker run -p 4566:4566 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  tarn:0.1.0-dev-ui

# Open in browser
open http://127.0.0.1:4566
```

## Verify Installation

Check that Tarn is working:

```bash
./tarn version
```

Release binary output: `tarn <DocsVersionCode />`

Source builds may report the repository development version instead.

## Optional: Install `awslocal`

If you prefer AWS CLI-compatible commands without repeating `--endpoint-url`, install `awslocal`:

::: code-group

```bash [pipx]
pipx install awscli-local
```

```bash [pip]
pip install awscli-local
```

:::

Then point it at Tarn:

```bash
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1
export AWS_ENDPOINT_URL=http://127.0.0.1:4566

awslocal lambda list-functions
```

## Next Steps

- [Quick Start](/guide/getting-started)
- [Configuration](/guide/configuration)
- [Services Overview](/services/)
