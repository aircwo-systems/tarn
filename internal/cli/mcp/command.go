package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// NewMCPCmd builds the `tarn mcp` command.
//
// The command speaks MCP over stdio, which is the transport every editor
// client supports. It targets the same endpoint as the other CLI commands, so
// --host/--port and TARN_ENDPOINT point it at a specific instance.
func NewMCPCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Expose a running Tarn instance to LLM tooling over MCP",
		Long: `Run a Model Context Protocol server that reports on a local Tarn instance.

The server speaks MCP over stdin/stdout and is intended to be launched by an
editor rather than run by hand. Configure it in VS Code (.vscode/mcp.json) or
Claude Code (.mcp.json) as:

  {"servers": {"tarn": {"command": "tarn", "args": ["mcp"]}}}

It talks to the instance over HTTP, so it does not need to run on the same
machine that started Tarn, and it does not hold the instance's data directory.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := Endpoint(cmd)

			// stdout carries the protocol, so anything human-readable has to
			// go to stderr or it corrupts the stream.
			fmt.Fprintf(os.Stderr, "tarn mcp: serving over stdio, endpoint %s\n", endpoint)

			server := newServer(endpoint, version)

			// Report the session to the instance for the dashboard until the
			// editor disconnects; Run returns when stdin closes.
			ctx, cancel := context.WithCancel(cmd.Context())
			done := make(chan struct{})
			go func() {
				defer close(done)
				newHeartbeat(endpoint, version, server).run(ctx)
			}()
			defer func() {
				cancel()
				<-done
			}()

			return server.Run(ctx, &mcp.StdioTransport{})
		},
	}
}
