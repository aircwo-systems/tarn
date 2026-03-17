package secrets

import (
	"fmt"
	"os"

	"github.com/openstack-project/openstack/internal/secretsproxy"
	"github.com/spf13/cobra"
)

func newProxyCmd() *cobra.Command {
	var (
		listenHost   string
		port         int
		upstream     string
		sessionToken string
		requireToken bool
	)

	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run a local Secrets Cache Extension-compatible proxy",
		Long: `Starts a local HTTP endpoint compatible with the AWS Parameters and Secrets
Lambda Extension API so non-OpenStack local lambdas can fetch secrets through
localhost:2773.`,
		Example: `  openstack secrets proxy
  openstack secrets proxy --port 2773 --session-token local-dev-token
  openstack secrets proxy --upstream http://localhost:4566`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if upstream == "" {
				upstream = getEndpoint(cmd)
			}
			if sessionToken == "" {
				sessionToken = os.Getenv("AWS_SESSION_TOKEN")
			}
			if sessionToken == "" {
				sessionToken = "local-dev-token"
			}

			addr := fmt.Sprintf("%s:%d", listenHost, port)
			fmt.Fprintf(cmd.OutOrStdout(), "Starting secrets extension proxy on http://%s\n", addr)
			fmt.Fprintf(cmd.OutOrStdout(), "Upstream OpenStack endpoint: %s\n", upstream)
			fmt.Fprintf(cmd.OutOrStdout(), "Set local lambda env: PARAMETERS_SECRETS_EXTENSION_HTTP_PORT=%d AWS_SESSION_TOKEN=%s\n", port, sessionToken)

			return secretsproxy.ListenAndServe(addr, secretsproxy.Options{
				UpstreamURL:  upstream,
				SessionToken: sessionToken,
				RequireToken: requireToken,
			})
		},
	}

	cmd.Flags().StringVar(&listenHost, "listen-host", "127.0.0.1", "Host interface to bind (default localhost-only)")
	cmd.Flags().IntVar(&port, "port", 2773, "Port for the extension-compatible endpoint")
	cmd.Flags().StringVar(&upstream, "upstream", "", "Upstream OpenStack endpoint (default derived from OPENSTACK_ENDPOINT or root host/port)")
	cmd.Flags().StringVar(&sessionToken, "session-token", "", "Expected value for X-Aws-Parameters-Secrets-Token (default AWS_SESSION_TOKEN or local-dev-token)")
	cmd.Flags().BoolVar(&requireToken, "require-token", true, "Require X-Aws-Parameters-Secrets-Token validation")

	return cmd
}
