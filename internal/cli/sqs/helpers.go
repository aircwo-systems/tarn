package sqs

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func getEndpoint(cmd *cobra.Command) string {
	if v := os.Getenv("TARN_ENDPOINT"); v != "" {
		return v
	}

	host, _ := cmd.Root().Flags().GetString("host")
	port, _ := cmd.Root().Flags().GetInt("port")

	if host == "0.0.0.0" {
		host = "localhost"
	}

	return fmt.Sprintf("http://%s:%d", host, port)
}

func getAccountID() string {
	if v := os.Getenv("TARN_ACCOUNT_ID"); v != "" {
		return v
	}
	return "000000000000"
}

func queueURL(endpoint, queueName string) string {
	return fmt.Sprintf("%s/%s/%s", endpoint, getAccountID(), queueName)
}
