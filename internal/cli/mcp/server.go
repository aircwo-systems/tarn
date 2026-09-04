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
tables, and Secrets Manager secrets reported by these tools exist only on this
machine.

Nothing here reaches a real AWS account, costs money, or appears in the AWS
console. Conversely, real AWS resources are not visible through these tools.

Start with tarn_status. It reports whether the instance is up and lists what is
provisioned; the names it returns are the arguments the other tools expect.

Tarn isolates resources per account. Every tool takes an optional twelve-digit
account argument, and omitting it addresses the default account.`

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

	statusTool, statusHandler := newStatusTool(c)
	mcp.AddTool(server, statusTool, statusHandler)

	return server
}
