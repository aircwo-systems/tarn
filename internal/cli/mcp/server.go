package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructions is sent to the client on initialize. Treat it as the README a
// model gets when the project has no AGENTS.md: it is the only place that can
// explain what Tarn is before any tool is called.
//
// Client support for this field is uneven, so nothing load-bearing lives only
// here. Every tool description repeats the facts a caller needs in isolation.
const instructions = `Tarn is an AWS emulator running on this machine. It answers the AWS APIs
locally, so Lambda functions, SQS queues, SNS topics, S3 buckets, DynamoDB
tables, ECS clusters/services/tasks, and Secrets Manager secrets reported by
these tools exist only on this machine.

Nothing here reaches a real AWS account, costs money, or appears in the AWS
console. Conversely, real AWS resources are not visible through these tools.

Start with tarn_status. It reports whether the instance is up and lists what is
provisioned, including ECS clusters, services, and tasks; the names it returns
are the arguments the other tools expect. A task's containers publish their
ports on 127.0.0.1 at an ephemeral host port, which tarn_status reports as a
ready-to-dial URL per container. A task's logs live in its awslogs group, by
convention /ecs/<family>, readable with tarn_get_logs using logGroup.

To diagnose a failing function, invoke it with tarn_invoke_lambda and read the
result. An unhandled exception comes back with its type, message, and stack
frames, which usually names the failing file and line. Reach for tarn_get_logs
when that is not enough: when the handler caught its own error, returned wrong
data, or printed something you need to see.

Asynchronous paths have no caller to receive a result. When a queue, topic, or
schedule triggers a function, logs are the only record of what happened. Use
tarn_send_message, tarn_publish, or tarn_fire_rule to set one off, then read the
consumer's logs. tarn_get_traces gives the same paths a structural view: one
record per request with a span per hop (gateway, lambda, queue, ecs, and so
on), so a queue -> Lambda -> ECS pipeline can be checked end to end by
correlation ID instead of piecing it together from separate log groups.

Tarn isolates resources per account. Every tool takes an optional twelve-digit
account argument, and omitting it addresses the default account. Traces are
the one exception: they are instance-wide, since a single request path can
cross accounts.`

// serverName identifies this server to clients.
const serverName = "tarn"

// newServer builds the MCP server for the instance at endpoint.
//
// It does not connect to anything. Transports are attached by the caller, which
// keeps the tool surface testable over an in-memory transport.
func newServer(endpoint, version string) *mcp.Server {
	c := newClient(endpoint)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "Tarn local AWS emulator",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: instructions,
	})

	addTools(server, c)

	return server
}

// addTools registers the tool surface. Keeping it in one place makes the size
// of the surface visible: clients cap how many tools can be active across all
// configured servers, so this list is a budget.
func addTools(server *mcp.Server, c *client) {
	addStatusTool(server, c)
	addDeployTool(server, c)
	addInvokeTool(server, c)
	addLogsTool(server, c)
	addPeekQueueTool(server, c)
	addSendMessageTool(server, c)
	addPublishTool(server, c)
	addListObjectsTool(server, c)
	addGetObjectTool(server, c)
	addFireRuleTool(server, c)
	addTracesTool(server, c)
}
