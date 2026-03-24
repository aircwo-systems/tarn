// secrets-proxy is a lightweight HTTP proxy that runs inside Lambda containers.
// It implements the AWS Parameters and Secrets Lambda Extension HTTP API on port 2773,
// forwarding requests to the OpenStack Secrets Manager API via host.docker.internal.
package main

import (
	"log"
	"os"
	"strings"

	"github.com/openstack-project/openstack/internal/secretsproxy"
)

func main() {
	port := os.Getenv("PARAMETERS_SECRETS_EXTENSION_HTTP_PORT")
	if port == "" {
		port = "2773"
	}

	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if endpoint == "" {
		endpoint = "http://host.docker.internal:4566"
	}

	sessionToken := os.Getenv("AWS_SESSION_TOKEN")
	if sessionToken == "" {
		sessionToken = "local-dev-token"
	}
	requireToken := true
	if isInternalLambdaRuntime(os.Getenv("OPENSTACK_INTERNAL_LAMBDA")) {
		requireToken = false
	}

	opts := secretsproxy.Options{
		UpstreamURL:  endpoint,
		SessionToken: sessionToken,
		RequireToken: requireToken,
	}
	addr := ":" + port
	log.Printf("[secrets-proxy] listening on %s, upstream: %s, requireToken=%t", addr, endpoint, requireToken)
	if err := secretsproxy.ListenAndServe(addr, opts); err != nil {
		log.Fatalf("[secrets-proxy] failed to start: %v", err)
	}
}

func isInternalLambdaRuntime(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
