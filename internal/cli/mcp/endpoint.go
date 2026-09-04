package mcp

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Endpoint resolves the Tarn base URL for cmd, matching the precedence the
// other CLI commands use: TARN_ENDPOINT first, then the root --host/--port
// flags.
func Endpoint(cmd *cobra.Command) string {
	if v := os.Getenv("TARN_ENDPOINT"); v != "" {
		return v
	}

	host, _ := cmd.Root().Flags().GetString("host")
	port, _ := cmd.Root().Flags().GetInt("port")

	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	if port == 0 {
		port = 4566
	}

	return fmt.Sprintf("http://%s:%d", host, port)
}
