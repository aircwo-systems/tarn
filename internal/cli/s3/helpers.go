package s3

import (
	"fmt"

	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

func getEndpoint(cmd *cobra.Command) string {
	return common.Endpoint(cmd)
}

func s3URL(endpoint, path string) string {
	return fmt.Sprintf("%s/_s3/%s", endpoint, path)
}
