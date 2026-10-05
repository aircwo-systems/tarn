package mcp

import (
	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

// Endpoint resolves the Tarn base URL for cmd, matching the precedence the
// other CLI commands use.
func Endpoint(cmd *cobra.Command) string {
	return common.Endpoint(cmd)
}
