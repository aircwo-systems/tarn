package mcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// StatusInput is the argument set for tarn_status.
type StatusInput struct {
	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID to inspect. Tarn isolates resources per account; omit this to use the default account (000000000000)."`
}

// StatusOutput is the orientation payload a model reads before doing anything
// else. It answers three questions in one call: is Tarn up, what is it, and
// what is provisioned.
type StatusOutput struct {
	Running     bool   `json:"running" jsonschema:"Whether a Tarn instance answered at the endpoint."`
	Endpoint    string `json:"endpoint" jsonschema:"The base URL that was contacted."`
	Remediation string `json:"remediation,omitempty" jsonschema:"When running is false, the command to start Tarn."`

	Region    string   `json:"region,omitempty"`
	AccountID string   `json:"accountId,omitempty"`
	DataDir   string   `json:"dataDir,omitempty"`
	Services  []string `json:"services,omitempty" jsonschema:"AWS services this instance emulates."`

	Counts    map[string]int `json:"counts,omitempty" jsonschema:"Provisioned resource counts by type."`
	Functions []FunctionInfo `json:"functions,omitempty"`
	Queues    []string       `json:"queues,omitempty"`
	Topics    []string       `json:"topics,omitempty"`
	Buckets   []string       `json:"buckets,omitempty"`
	Secrets   []string       `json:"secrets,omitempty" jsonschema:"Secret names only. Values are never returned here."`
}

// FunctionInfo is the per-function summary carried in the status payload.
type FunctionInfo struct {
	Name    string `json:"name"`
	Runtime string `json:"runtime,omitempty"`
	State   string `json:"state,omitempty" jsonschema:"Active, Pending, or Failed. Only Active functions can be invoked."`
}

// overview mirrors the subset of GET /_tarn/admin/overview that tarn_status
// reports. The endpoint returns more (traces, infrastructure probes, per-object
// previews); those belong to the tools that drill down, not to orientation.
type overview struct {
	Services []string `json:"services"`
	Config   struct {
		Region    string `json:"region"`
		AccountID string `json:"accountId"`
		Endpoint  string `json:"endpoint"`
		DataDir   string `json:"dataDir"`
	} `json:"config"`
	Counts    map[string]int `json:"counts"`
	Functions []struct {
		Name    string `json:"name"`
		Runtime string `json:"runtime"`
		State   string `json:"state"`
	} `json:"functions"`
	Queues []struct {
		Name string `json:"name"`
	} `json:"queues"`
	Topics []struct {
		Name string `json:"name"`
	} `json:"topics"`
	Buckets []struct {
		Name string `json:"name"`
	} `json:"buckets"`
	Secrets []struct {
		Name string `json:"name"`
	} `json:"secrets"`
}

const statusDescription = `Report whether the local Tarn instance is running and what is provisioned on it.

Call this first. Tarn is an AWS emulator that runs on this machine, so every
function, queue, bucket, and secret it reports exists only here. The real AWS
CLI and console will not see them, and nothing here costs money or touches a
real AWS account.

Returns the endpoint, the emulated region and account, the services available,
resource counts, and the names of what is provisioned. Use those names as
arguments to the other tarn tools.

If Tarn is not running this returns running=false with the command to start it,
rather than failing.`

// newStatusTool wires tarn_status to an instance client.
func addStatusTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_status",
		Description: statusDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Tarn status",
			ReadOnlyHint: true,
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in StatusInput) (
		*mcp.CallToolResult, StatusOutput, error,
	) {
		var ov overview
		if err := c.get(ctx, "/_tarn/admin/overview", in.Account, nil, &ov); err != nil {
			var down *errNotRunning
			if errors.As(err, &down) {
				// Not an error result: a model that receives a transport
				// failure has nothing to act on and will retry blindly.
				return nil, StatusOutput{
					Running:     false,
					Endpoint:    c.endpoint,
					Remediation: "Start Tarn with `tarn start`, then call tarn_status again.",
				}, nil
			}
			return nil, StatusOutput{}, err
		}

		out := StatusOutput{
			Running:   true,
			Endpoint:  c.endpoint,
			Region:    ov.Config.Region,
			AccountID: ov.Config.AccountID,
			DataDir:   ov.Config.DataDir,
			Services:  ov.Services,
			Counts:    ov.Counts,
		}
		for _, fn := range ov.Functions {
			out.Functions = append(out.Functions, FunctionInfo{
				Name:    fn.Name,
				Runtime: fn.Runtime,
				State:   fn.State,
			})
		}
		for _, q := range ov.Queues {
			out.Queues = append(out.Queues, q.Name)
		}
		for _, t := range ov.Topics {
			out.Topics = append(out.Topics, t.Name)
		}
		for _, b := range ov.Buckets {
			out.Buckets = append(out.Buckets, b.Name)
		}
		for _, s := range ov.Secrets {
			out.Secrets = append(out.Secrets, s.Name)
		}
		return nil, out, nil
	}

	mcp.AddTool(s, tool, handler)
}
