# MCP

Tarn ships a [Model Context Protocol](https://modelcontextprotocol.io) server, so
an AI coding assistant can inspect a running instance directly instead of being
told what is on it.

The server is a client of Tarn's HTTP API. It reads the same endpoints the
dashboard uses, so it reports on whatever instance the CLI can reach and holds
no lock on the data directory.

## Running it

```bash
tarn mcp
```

The server speaks MCP over stdin and stdout. It is meant to be launched by an
editor rather than run by hand — started in a terminal it will simply wait for
a client handshake.

It targets the same endpoint as every other CLI command, so the global flags
and `TARN_ENDPOINT` point it at a specific instance:

```bash
tarn mcp --port 4599
TARN_ENDPOINT=http://127.0.0.1:4599 tarn mcp
```

## VS Code and GitHub Copilot

Create `.vscode/mcp.json` in your project:

```json
{
  "servers": {
    "tarn": {
      "type": "stdio",
      "command": "tarn",
      "args": ["mcp"]
    }
  }
}
```

## Claude Code

Create `.mcp.json` in your project root:

```json
{
  "mcpServers": {
    "tarn": {
      "command": "tarn",
      "args": ["mcp"]
    }
  }
}
```

## Tools

### `tarn_status`

Reports whether an instance is running and what is provisioned on it: the
endpoint, emulated region and account, available services, resource counts, and
the names of functions, queues, topics, buckets, and secrets.

Takes an optional twelve-digit `account` argument. Tarn isolates resources per
account, and omitting the argument addresses the default account
(`000000000000`).

Secrets are reported by name only. Values are never returned.

If no instance is listening, the tool returns `running: false` along with the
endpoint it tried and the command to start Tarn, rather than failing. An
assistant that receives a connection error has nothing to act on and will
usually retry blindly; one that receives a remediation can tell you what to do.

## What an assistant sees

Tools are self-describing, so an assistant needs no project documentation to
use them. On connect it receives a description of what Tarn is, and each tool
carries its own description and typed argument schema.

The description makes one thing explicit that models otherwise get wrong: these
resources are local. The real AWS CLI and console cannot see them, nothing here
costs money, and real AWS resources are not visible through these tools.
