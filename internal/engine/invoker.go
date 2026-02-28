package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/openstack-project/openstack/pkg/types"
)

// rieInvokePath is the endpoint the AWS RIE exposes for invoking functions.
const rieInvokePath = "/2015-03-31/functions/function/invocations"

// Invoker handles sending payloads to Lambda containers via the RIE.
type Invoker struct {
	client *http.Client
}

// NewInvoker creates an Invoker with appropriate timeout settings.
func NewInvoker() *Invoker {
	return &Invoker{
		client: &http.Client{
			// Per-request timeouts are set via context, not here.
			// This transport timeout covers connection setup only.
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: 5 * time.Second,
				}).DialContext,
				ResponseHeaderTimeout: 0, // no limit — Lambda can run up to 15 min
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConnsPerHost:   10,
			},
		},
	}
}

// WaitForReady polls the container's RIE until it accepts TCP connections.
// The AWS RIE queues invocation requests until the runtime is initialized,
// so TCP readiness is sufficient — the first real invoke will block inside
// the RIE until the handler is ready.
func (inv *Invoker) WaitForReady(ctx context.Context, hostPort string) error {
	addr := fmt.Sprintf("127.0.0.1:%s", hostPort)

	deadline := time.After(60 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			if lastErr != nil {
				return fmt.Errorf("container RIE not ready after 60s on port %s: %w", hostPort, lastErr)
			}
			return fmt.Errorf("container RIE not ready after 60s on port %s", hostPort)
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			if err != nil {
				lastErr = err
				continue
			}
			conn.Close()
			log.Printf("[invoker] RIE accepting connections on port %s", hostPort)
			return nil
		}
	}
}

// Invoke sends an event payload to the container's RIE and returns the response.
// The context should carry the function timeout deadline.
func (inv *Invoker) Invoke(ctx context.Context, hostPort string, input *types.InvokeInput) (*types.InvokeOutput, error) {
	url := fmt.Sprintf("http://127.0.0.1:%s%s", hostPort, rieInvokePath)

	payload := input.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	log.Printf("[invoker] invoking %s via RIE on port %s (%d bytes payload)", input.FunctionName, hostPort, len(payload))

	resp, err := inv.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &types.InvokeOutput{
				StatusCode:    200,
				Payload:       []byte(fmt.Sprintf(`{"errorMessage":"Task timed out after %s","errorType":"TimeoutError"}`, ctx.Err())),
				FunctionError: "Unhandled",
			}, nil
		}
		return nil, fmt.Errorf("RIE invocation failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read RIE response: %w", err)
	}

	output := &types.InvokeOutput{
		StatusCode: 200,
		Payload:    body,
	}

	// The RIE sets this header when the function returns an error
	if fnErr := resp.Header.Get("X-Amz-Function-Error"); fnErr != "" {
		output.FunctionError = fnErr
	}

	return output, nil
}

// InvokeWithLogs invokes the function and also captures logs if requested.
func (inv *Invoker) InvokeWithLogs(ctx context.Context, eng *Engine, info *ContainerInfo, input *types.InvokeInput) (*types.InvokeOutput, error) {
	output, err := inv.Invoke(ctx, info.HostPort, input)
	if err != nil {
		return nil, err
	}

	// Capture logs if LogType is Tail
	if input.LogType == "Tail" {
		logCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		logs, logErr := eng.ContainerLogs(logCtx, info.ID)
		if logErr == nil && logs != "" {
			// AWS returns last 4KB base64-encoded
			logBytes := []byte(logs)
			if len(logBytes) > 4096 {
				logBytes = logBytes[len(logBytes)-4096:]
			}
			output.LogResult = base64.StdEncoding.EncodeToString(logBytes)
		}
	}

	return output, nil
}
