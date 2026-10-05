package common

import (
	"os"
	"strings"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/spf13/cobra"
)

// ApplyAddressFlags copies --host and --port onto cfg only when the user
// passed them. Their defaults must not replace TARN_HOST and TARN_PORT, which
// cfg.LoadFromEnv has already applied.
func ApplyAddressFlags(cmd *cobra.Command, cfg *config.Config) {
	flags := cmd.Flags()
	if flags.Changed("host") {
		if v, err := flags.GetString("host"); err == nil && strings.TrimSpace(v) != "" {
			cfg.Host = strings.TrimSpace(v)
		}
	}
	if flags.Changed("port") {
		if v, err := flags.GetInt("port"); err == nil && v != 0 {
			cfg.Port = v
		}
	}
}

// Endpoint resolves the Tarn base URL for CLI client commands. TARN_ENDPOINT
// wins; otherwise --host and --port apply when passed, then TARN_HOST and
// TARN_PORT, then the defaults.
func Endpoint(cmd *cobra.Command) string {
	if v := os.Getenv("TARN_ENDPOINT"); v != "" {
		return v
	}
	cfg := config.Default()
	cfg.LoadFromEnv()
	ApplyAddressFlags(cmd, cfg)
	return cfg.Endpoint()
}
