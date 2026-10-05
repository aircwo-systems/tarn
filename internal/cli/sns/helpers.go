package sns

import (
	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

func getEndpoint(cmd *cobra.Command) string {
	return common.Endpoint(cmd)
}
