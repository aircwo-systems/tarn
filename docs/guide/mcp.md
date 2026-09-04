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

Every tool takes an optional twelve-digit `account` argument. Tarn isolates
resources per account, and omitting it addresses the default account
(`000000000000`).

### `tarn_status`

Reports whether an instance is running and what is provisioned on it: the
endpoint, emulated region and account, available services, resource counts, and
the names of functions, queues, topics, buckets, and secrets.

Secrets are reported by name only. Values are never returned.

If no instance is listening, the tool returns `running: false` along with the
endpoint it tried and the command to start Tarn, rather than failing. An
assistant that receives a connection error has nothing to act on and will
usually retry blindly; one that receives a remediation can tell you what to do.

### `tarn_deploy_lambda`

Creates or replaces a function. Code comes either from `files`, a map of path to
source text that is zipped in memory, or from `zipPath`, an absolute path to an
existing package. The `files` form lets an assistant deploy code it just wrote
without building a zip on disk.

Reports whether an existing function of the same name was replaced.

### `tarn_invoke_lambda`

Invokes a function and fuses the outcome into one result: a `succeeded` flag,
the return value on success, and the error type, message, and stack frames on
failure.

A failed invocation still answers HTTP 200, so `succeeded` is the signal to
read. For an unhandled exception the stack usually names the failing file and
line, which is often enough to diagnose the fault without opening logs.

### `tarn_get_logs`

Reads what a function logged. Returns the newest events, ordered oldest first so
the sequence reads in the order it happened.

Container runtime chatter is withheld by default. Each invocation emits START,
END, REPORT, init markers, and extension banners, which on a measured run were
22 of 25 events. Set `includeRuntime` to see them, for example when diagnosing
cold starts or timeouts. Narrow further with `pattern` or `level`.

### `tarn_peek_queue`

Inspects messages in an SQS queue without receiving them, changing their
visibility, or deleting anything, so it is safe to call while a consumer runs.

A message with a climbing `receiveCount` means its consumer keeps failing.

### `tarn_send_message`

Sends a message to a queue. Use it to exercise an event-driven path end to end,
then read the consumer's logs. FIFO queues require `groupId`.

### `tarn_publish`

Publishes to an SNS topic. Subscribers receive it as they would on AWS, so
subscribed queues can then be inspected with `tarn_peek_queue`.

### `tarn_list_objects` and `tarn_get_object`

List keys in a bucket, then read one. Text objects are returned inline and
truncated at 64 KB. Binary objects report size and content type but no contents.

### `tarn_fire_rule`

Triggers a scheduled EventBridge rule immediately instead of waiting for its
next interval. Reports how many targets succeeded and failed, but not why; read
the target function's logs for that.

## What an assistant sees

Tools are self-describing, so an assistant needs no project documentation to
use them. On connect it receives a description of what Tarn is, and each tool
carries its own description and typed argument schema.

The description makes one thing explicit that models otherwise get wrong: these
resources are local. The real AWS CLI and console cannot see them, nothing here
costs money, and real AWS resources are not visible through these tools.
