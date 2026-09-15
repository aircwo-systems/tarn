package mcp

import (
	"context"
	"errors"
	"strconv"

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

	ECS *ECSInfo `json:"ecs,omitempty" jsonschema:"ECS state, present only when the instance has any clusters."`
}

// FunctionInfo is the per-function summary carried in the status payload.
type FunctionInfo struct {
	Name    string `json:"name"`
	Runtime string `json:"runtime,omitempty"`
	State   string `json:"state,omitempty" jsonschema:"Active, Pending, or Failed. Only Active functions can be invoked."`
}

// ECSInfo is the ECS state carried in the status payload: clusters, the
// services running in them, and the tasks those services (or a direct
// RunTask) started.
type ECSInfo struct {
	Clusters []ECSClusterInfo `json:"clusters,omitempty"`
	Services []ECSServiceInfo `json:"services,omitempty"`
	Tasks    []ECSTaskInfo    `json:"tasks,omitempty"`
}

// ECSClusterInfo is one cluster's task counts.
type ECSClusterInfo struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	RunningTasks int    `json:"runningTasks"`
	PendingTasks int    `json:"pendingTasks"`
}

// ECSServiceInfo is one service's desired-vs-actual task counts.
type ECSServiceInfo struct {
	Name         string `json:"name"`
	Cluster      string `json:"cluster"`
	Status       string `json:"status"`
	DesiredCount int    `json:"desiredCount"`
	RunningCount int    `json:"runningCount"`
	PendingCount int    `json:"pendingCount"`
	// ProxyURL is a stable URL that reaches this service's running tasks
	// through Tarn's /_ecs/ reverse proxy, round-robining across replicas.
	// Unlike a container's hostUrl, it doesn't change when tasks restart.
	ProxyURL string `json:"proxyUrl,omitempty"`
}

// ECSTaskInfo is one task: its identity, lifecycle status, and the containers
// it ran, including where each container's ports landed on the host.
type ECSTaskInfo struct {
	TaskArn       string             `json:"taskArn"`
	Cluster       string             `json:"cluster"`
	Group         string             `json:"group,omitempty" jsonschema:"The service or standalone run that started this task, for example service:worker-service."`
	LastStatus    string             `json:"lastStatus"`
	DesiredStatus string             `json:"desiredStatus"`
	StoppedReason string             `json:"stoppedReason,omitempty"`
	Containers    []ECSContainerInfo `json:"containers,omitempty"`
}

// ECSContainerInfo is one container within a task.
type ECSContainerInfo struct {
	Name       string   `json:"name"`
	LastStatus string   `json:"lastStatus"`
	ExitCode   *int64   `json:"exitCode,omitempty"`
	HostURLs   []string `json:"hostUrls,omitempty" jsonschema:"http://127.0.0.1:<hostPort> for each port this container published. Dial these directly to reach the container."`
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
	ECS *struct {
		Clusters []struct {
			Name         string `json:"name"`
			Arn          string `json:"arn"`
			Status       string `json:"status"`
			RunningTasks int    `json:"runningTasks"`
			PendingTasks int    `json:"pendingTasks"`
		} `json:"clusters"`
		Services []struct {
			Name         string `json:"name"`
			ClusterArn   string `json:"clusterArn"`
			Status       string `json:"status"`
			DesiredCount int    `json:"desiredCount"`
			RunningCount int    `json:"runningCount"`
			PendingCount int    `json:"pendingCount"`
		} `json:"services"`
		Tasks []struct {
			Arn           string `json:"arn"`
			ClusterArn    string `json:"clusterArn"`
			Group         string `json:"group"`
			LastStatus    string `json:"lastStatus"`
			DesiredStatus string `json:"desiredStatus"`
			StoppedReason string `json:"stoppedReason"`
			Containers    []struct {
				Name            string `json:"name"`
				LastStatus      string `json:"lastStatus"`
				ExitCode        *int64 `json:"exitCode"`
				NetworkBindings []struct {
					HostPort int `json:"hostPort"`
				} `json:"networkBindings"`
			} `json:"containers"`
		} `json:"tasks"`
	} `json:"ecs"`
}

const statusDescription = `Report whether the local Tarn instance is running and what is provisioned on it.

Call this first. Tarn is an AWS emulator that runs on this machine, so every
function, queue, bucket, and secret it reports exists only here. The real AWS
CLI and console will not see them, and nothing here costs money or touches a
real AWS account.

Returns the endpoint, the emulated region and account, the services available,
resource counts, and the names of what is provisioned. Use those names as
arguments to the other tarn tools.

When ECS is provisioned, also returns its clusters, services, and tasks. A
task's containers publish their ports on 127.0.0.1 at an ephemeral host port;
this reports each as a ready-to-dial hostUrl. A service also reports a stable
proxyUrl (/_ecs/<account>/<cluster>/<service>/) that round-robins across its
running tasks and survives task restarts, unlike a container's hostUrl. A task's logs live in its
awslogs group, by convention /ecs/<family>, readable with tarn_get_logs using
logGroup.

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
		if ov.ECS != nil {
			ecs := &ECSInfo{}

			clusterNames := make(map[string]string, len(ov.ECS.Clusters))
			for _, cl := range ov.ECS.Clusters {
				clusterNames[cl.Arn] = cl.Name
				ecs.Clusters = append(ecs.Clusters, ECSClusterInfo{
					Name:         cl.Name,
					Status:       cl.Status,
					RunningTasks: cl.RunningTasks,
					PendingTasks: cl.PendingTasks,
				})
			}
			// clusterName resolves an ARN to the name reported above, falling
			// back to the ARN itself if the cluster list did not include it
			// (should not happen, but a task or service is more useful with a
			// raw ARN than with nothing).
			clusterName := func(arn string) string {
				if name, ok := clusterNames[arn]; ok {
					return name
				}
				return arn
			}

			for _, svc := range ov.ECS.Services {
				info := ECSServiceInfo{
					Name:         svc.Name,
					Cluster:      clusterName(svc.ClusterArn),
					Status:       svc.Status,
					DesiredCount: svc.DesiredCount,
					RunningCount: svc.RunningCount,
					PendingCount: svc.PendingCount,
				}
				// Only set ProxyURL when the cluster ARN actually resolved to a
				// name: clusterName falls back to the raw ARN on a miss, and an
				// ARN embedded in a URL path would be garbage, not a usable link.
				if resolvedName, ok := clusterNames[svc.ClusterArn]; ok && c.endpoint != "" && ov.Config.AccountID != "" {
					info.ProxyURL = c.endpoint + "/_ecs/" + ov.Config.AccountID + "/" +
						resolvedName + "/" + svc.Name + "/"
				}
				ecs.Services = append(ecs.Services, info)
			}

			for _, task := range ov.ECS.Tasks {
				info := ECSTaskInfo{
					TaskArn:       task.Arn,
					Cluster:       clusterName(task.ClusterArn),
					Group:         task.Group,
					LastStatus:    task.LastStatus,
					DesiredStatus: task.DesiredStatus,
					StoppedReason: task.StoppedReason,
				}
				for _, c := range task.Containers {
					container := ECSContainerInfo{
						Name:       c.Name,
						LastStatus: c.LastStatus,
						ExitCode:   c.ExitCode,
					}
					for _, nb := range c.NetworkBindings {
						container.HostURLs = append(container.HostURLs,
							"http://127.0.0.1:"+strconv.Itoa(nb.HostPort))
					}
					info.Containers = append(info.Containers, container)
				}
				ecs.Tasks = append(ecs.Tasks, info)
			}

			out.ECS = ecs
		}
		return nil, out, nil
	}

	mcp.AddTool(s, tool, handler)
}
