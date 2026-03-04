package s3

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func getEndpoint(cmd *cobra.Command) string {
	if v := os.Getenv("OPENSTACK_ENDPOINT"); v != "" {
		return v
	}

	host, _ := cmd.Root().Flags().GetString("host")
	port, _ := cmd.Root().Flags().GetInt("port")

	if host == "0.0.0.0" {
		host = "localhost"
	}

	return fmt.Sprintf("http://%s:%d", host, port)
}

func s3URL(endpoint, path string) string {
	return fmt.Sprintf("%s/_s3/%s", endpoint, path)
}
