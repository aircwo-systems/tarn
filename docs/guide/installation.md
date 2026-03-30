# Installation

Get Tarn up and running on your system.

::: info Distribution Roadmap
The current release is <DocsVersionCode />.

Prebuilt Docker images, a Homebrew tap, and a one-line installer script are planned for future releases. For now, the primary install paths are release binaries or building Tarn from source.

See the [project roadmap](https://github.com/aircwo-systems/tarn/blob/develop-docs/ROADMAP.md) for the broader direction.
:::

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
