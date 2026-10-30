package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/internal/api"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/spf13/cobra"
)

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "vault", Short: "Manage access to saved real AWS connections"}
	cmd.AddCommand(&cobra.Command{
		Use: "unlock", Args: cobra.NoArgs,
		Short: "Print a single-use local dashboard link for AWS connection management",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.Default()
			cfg.LoadFromEnv()
			if dir, _ := cmd.Flags().GetString("data-dir"); dir != "" {
				cfg.DataDir = dir
			}
			link, err := ownerUnlockLink(cmd.Context(), cfg.DataDir)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Open this local link within five minutes. It can be used once:")
			fmt.Fprintln(cmd.OutOrStdout(), link)
			return nil
		},
	})
	return cmd
}

func ownerUnlockLink(ctx context.Context, dataDir string) (string, error) {
	data, err := os.ReadFile(api.OwnerControlPath(dataDir))
	if err != nil {
		return "", errors.New("AWS connections are unavailable. Start Tarn with --host 127.0.0.1 using the same --data-dir; manual credentials also require Vault encryption")
	}
	var control api.OwnerControl
	if json.Unmarshal(data, &control) != nil || control.Token == "" {
		return "", errors.New("the local AWS owner capability is invalid; restart Tarn")
	}
	u, err := url.Parse(control.Endpoint)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback()) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("the AWS owner endpoint must be a local HTTP origin")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", control.Endpoint+"/_tarn/owner/bootstrap", nil)
	if err != nil {
		return "", errors.New("invalid AWS owner endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+control.Token)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("cannot reach the local AWS owner API; check that Tarn is running")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("the local AWS owner API refused the unlock request; restart Tarn and try again")
	}
	var result struct {
		URL string `json:"url"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result) != nil || !strings.Contains(result.URL, "#settings?aws-unlock=") {
		return "", errors.New("the local AWS owner API returned an invalid unlock link")
	}
	return result.URL, nil
}
