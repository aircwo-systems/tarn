package sqs

import (
	"fmt"

	"github.com/aircwo-systems/tarn/internal/cli/common"
	"github.com/spf13/cobra"
)

func getEndpoint(cmd *cobra.Command) string {
	return common.Endpoint(cmd)
}

func queueURL(endpoint, queueName string) string {
	return fmt.Sprintf("%s/%s/%s", endpoint, common.AccountID(), queueName)
}
