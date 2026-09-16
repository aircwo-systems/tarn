package ecs

import (
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// The ECS control-plane types are also used for persistence, where their
// historical PascalCase JSON names are part of the on-disk format. Keep that
// representation private to the store and translate responses at the HTTP
// boundary to the camelCase, epoch-seconds shape used by ECS clients.

type wireCluster struct {
	ClusterName                       string    `json:"clusterName"`
	ClusterArn                        string    `json:"clusterArn"`
	Status                            string    `json:"status"`
	RegisteredContainerInstancesCount int       `json:"registeredContainerInstancesCount"`
	RunningTasksCount                 int       `json:"runningTasksCount"`
	PendingTasksCount                 int       `json:"pendingTasksCount"`
	ActiveServicesCount               int       `json:"activeServicesCount"`
	Tags                              []wireTag `json:"tags,omitempty"`
}

// wireTag mirrors the AWS ECS Tag shape. Unlike most ECS wire fields, its
// member names are lowercase "key"/"value" even before the general
// PascalCase->camelCase translation this file otherwise applies.
type wireTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type wireTaskDefinition struct {
	TaskDefinitionArn       string                    `json:"taskDefinitionArn"`
	Family                  string                    `json:"family"`
	Revision                int                       `json:"revision"`
	ContainerDefinitions    []wireContainerDefinition `json:"containerDefinitions"`
	Cpu                     string                    `json:"cpu,omitempty"`
	Memory                  string                    `json:"memory,omitempty"`
	NetworkMode             string                    `json:"networkMode,omitempty"`
	Status                  string                    `json:"status"`
	RequiresCompatibilities []string                  `json:"requiresCompatibilities,omitempty"`
	RegisteredAt            *float64                  `json:"registeredAt,omitempty"`

	TaskRoleArn          string                    `json:"taskRoleArn,omitempty"`
	ExecutionRoleArn     string                    `json:"executionRoleArn,omitempty"`
	PidMode              string                    `json:"pidMode,omitempty"`
	IpcMode              string                    `json:"ipcMode,omitempty"`
	RuntimePlatform      *wireRuntimePlatform      `json:"runtimePlatform,omitempty"`
	EphemeralStorage     *wireEphemeralStorage     `json:"ephemeralStorage,omitempty"`
	Volumes              []wireVolume              `json:"volumes,omitempty"`
	PlacementConstraints []wirePlacementConstraint `json:"placementConstraints,omitempty"`
}

type wireRuntimePlatform struct {
	CpuArchitecture       string `json:"cpuArchitecture,omitempty"`
	OperatingSystemFamily string `json:"operatingSystemFamily,omitempty"`
}

type wireEphemeralStorage struct {
	SizeInGiB int `json:"sizeInGiB,omitempty"`
}

type wirePlacementConstraint struct {
	Type       string `json:"type,omitempty"`
	Expression string `json:"expression,omitempty"`
}

type wireVolume struct {
	Name                      string                      `json:"name"`
	Host                      *wireHostVolumeProperties   `json:"host,omitempty"`
	DockerVolumeConfiguration *wireDockerVolumeConfig     `json:"dockerVolumeConfiguration,omitempty"`
	EfsVolumeConfiguration    *wireEFSVolumeConfiguration `json:"efsVolumeConfiguration,omitempty"`
}

type wireHostVolumeProperties struct {
	SourcePath string `json:"sourcePath,omitempty"`
}

type wireDockerVolumeConfig struct {
	Scope         string            `json:"scope,omitempty"`
	Autoprovision bool              `json:"autoprovision,omitempty"`
	Driver        string            `json:"driver,omitempty"`
	DriverOpts    map[string]string `json:"driverOpts,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type wireEFSVolumeConfiguration struct {
	FileSystemId          string                      `json:"fileSystemId,omitempty"`
	RootDirectory         string                      `json:"rootDirectory,omitempty"`
	TransitEncryption     string                      `json:"transitEncryption,omitempty"`
	TransitEncryptionPort int                         `json:"transitEncryptionPort,omitempty"`
	AuthorizationConfig   *wireEFSAuthorizationConfig `json:"authorizationConfig,omitempty"`
}

type wireEFSAuthorizationConfig struct {
	AccessPointId string `json:"accessPointId,omitempty"`
	IAM           string `json:"iam,omitempty"`
}

type wireContainerDefinition struct {
	Name              string                    `json:"name"`
	Image             string                    `json:"image"`
	Command           []string                  `json:"command,omitempty"`
	EntryPoint        []string                  `json:"entryPoint,omitempty"`
	Environment       []wireKeyValuePair        `json:"environment,omitempty"`
	PortMappings      []wirePortMapping         `json:"portMappings,omitempty"`
	LogConfiguration  *wireLogConfiguration     `json:"logConfiguration,omitempty"`
	Essential         *bool                     `json:"essential,omitempty"`
	Cpu               int                       `json:"cpu,omitempty"`
	Memory            int                       `json:"memory,omitempty"`
	MemoryReservation int                       `json:"memoryReservation,omitempty"`
	Secrets           []wireContainerSecret     `json:"secrets,omitempty"`
	HealthCheck       *wireContainerHealthCheck `json:"healthCheck,omitempty"`
	DependsOn         []wireContainerDependency `json:"dependsOn,omitempty"`

	WorkingDirectory string            `json:"workingDirectory,omitempty"`
	User             string            `json:"user,omitempty"`
	StopTimeout      int               `json:"stopTimeout,omitempty"`
	StartTimeout     int               `json:"startTimeout,omitempty"`
	Ulimits          []wireUlimit      `json:"ulimits,omitempty"`
	DockerLabels     map[string]string `json:"dockerLabels,omitempty"`
	MountPoints      []wireMountPoint  `json:"mountPoints,omitempty"`
	VolumesFrom      []wireVolumeFrom  `json:"volumesFrom,omitempty"`

	ReadonlyRootFilesystem *bool                `json:"readonlyRootFilesystem,omitempty"`
	Privileged             *bool                `json:"privileged,omitempty"`
	LinuxParameters        *wireLinuxParameters `json:"linuxParameters,omitempty"`

	Hostname       string              `json:"hostname,omitempty"`
	DnsServers     []string            `json:"dnsServers,omitempty"`
	ExtraHosts     []wireHostEntry     `json:"extraHosts,omitempty"`
	Interactive    *bool               `json:"interactive,omitempty"`
	PseudoTerminal *bool               `json:"pseudoTerminal,omitempty"`
	SystemControls []wireSystemControl `json:"systemControls,omitempty"`

	EnvironmentFiles      []wireEnvironmentFile      `json:"environmentFiles,omitempty"`
	RepositoryCredentials *wireRepositoryCredentials `json:"repositoryCredentials,omitempty"`
	FirelensConfiguration *wireFirelensConfiguration `json:"firelensConfiguration,omitempty"`
}

type wireUlimit struct {
	Name      string `json:"name"`
	SoftLimit int    `json:"softLimit"`
	HardLimit int    `json:"hardLimit"`
}

type wireMountPoint struct {
	SourceVolume  string `json:"sourceVolume,omitempty"`
	ContainerPath string `json:"containerPath,omitempty"`
	ReadOnly      bool   `json:"readOnly,omitempty"`
}

type wireVolumeFrom struct {
	SourceContainer string `json:"sourceContainer,omitempty"`
	ReadOnly        bool   `json:"readOnly,omitempty"`
}

type wireLinuxParameters struct {
	InitProcessEnabled *bool                   `json:"initProcessEnabled,omitempty"`
	Capabilities       *wireKernelCapabilities `json:"capabilities,omitempty"`
	SharedMemorySize   int                     `json:"sharedMemorySize,omitempty"`
	Tmpfs              []wireTmpfs             `json:"tmpfs,omitempty"`
}

type wireKernelCapabilities struct {
	Add  []string `json:"add,omitempty"`
	Drop []string `json:"drop,omitempty"`
}

type wireTmpfs struct {
	ContainerPath string   `json:"containerPath,omitempty"`
	Size          int      `json:"size,omitempty"`
	MountOptions  []string `json:"mountOptions,omitempty"`
}

type wireHostEntry struct {
	Hostname  string `json:"hostname,omitempty"`
	IpAddress string `json:"ipAddress,omitempty"`
}

type wireSystemControl struct {
	Namespace string `json:"namespace,omitempty"`
	Value     string `json:"value,omitempty"`
}

type wireEnvironmentFile struct {
	Value string `json:"value,omitempty"`
	Type  string `json:"type,omitempty"`
}

type wireRepositoryCredentials struct {
	CredentialsParameter string `json:"credentialsParameter,omitempty"`
}

type wireFirelensConfiguration struct {
	Type    string            `json:"type,omitempty"`
	Options map[string]string `json:"options,omitempty"`
}

// wireContainerSecret echoes ValueFrom verbatim (it is a reference, not a
// value) — never the resolved secret value.
type wireContainerSecret struct {
	Name      string `json:"name"`
	ValueFrom string `json:"valueFrom"`
}

type wireContainerHealthCheck struct {
	Command     []string `json:"command,omitempty"`
	Interval    *int     `json:"interval,omitempty"`
	Timeout     *int     `json:"timeout,omitempty"`
	Retries     *int     `json:"retries,omitempty"`
	StartPeriod *int     `json:"startPeriod,omitempty"`
}

type wireContainerDependency struct {
	ContainerName string `json:"containerName"`
	Condition     string `json:"condition"`
}

type wirePortMapping struct {
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

type wireKeyValuePair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type wireLogConfiguration struct {
	LogDriver string            `json:"logDriver"`
	Options   map[string]string `json:"options,omitempty"`
}

type wireService struct {
	ServiceArn              string                       `json:"serviceArn"`
	ServiceName             string                       `json:"serviceName"`
	ClusterArn              string                       `json:"clusterArn"`
	TaskDefinitionArn       string                       `json:"taskDefinition"`
	DesiredCount            int                          `json:"desiredCount"`
	RunningCount            int                          `json:"runningCount"`
	PendingCount            int                          `json:"pendingCount"`
	LaunchType              string                       `json:"launchType,omitempty"`
	Status                  string                       `json:"status"`
	Deployments             []wireDeployment             `json:"deployments"`
	CreatedAt               *float64                     `json:"createdAt,omitempty"`
	SchedulingStrategy      string                       `json:"schedulingStrategy,omitempty"`
	NetworkConfiguration    *wireNetworkConfiguration    `json:"networkConfiguration,omitempty"`
	PlatformVersion         string                       `json:"platformVersion,omitempty"`
	DeploymentConfiguration *wireDeploymentConfiguration `json:"deploymentConfiguration,omitempty"`
	EnableECSManagedTags    bool                         `json:"enableECSManagedTags,omitempty"`
	PropagateTags           string                       `json:"propagateTags,omitempty"`
	Tags                    []wireTag                    `json:"tags,omitempty"`
}

type wireNetworkConfiguration struct {
	AwsvpcConfiguration *wireAwsVpcConfiguration `json:"awsvpcConfiguration,omitempty"`
}

type wireAwsVpcConfiguration struct {
	Subnets        []string `json:"subnets,omitempty"`
	SecurityGroups []string `json:"securityGroups,omitempty"`
	AssignPublicIp string   `json:"assignPublicIp,omitempty"`
}

type wireDeploymentConfiguration struct {
	MaximumPercent        int `json:"maximumPercent,omitempty"`
	MinimumHealthyPercent int `json:"minimumHealthyPercent,omitempty"`
}

// wireDeployment is ECS's per-service rollout record. Tarn has no real
// rolling-deployment model, so DescribeServices always reports the single
// PRIMARY deployment implied by the service's current counts. This is
// enough for `aws ecs wait services-stable` and Terraform's
// wait_for_steady_state, both of which key off deployments having length 1
// alongside runningCount == desiredCount.
type wireDeployment struct {
	ID             string   `json:"id"`
	Status         string   `json:"status"`
	TaskDefinition string   `json:"taskDefinition"`
	DesiredCount   int      `json:"desiredCount"`
	RunningCount   int      `json:"runningCount"`
	PendingCount   int      `json:"pendingCount"`
	LaunchType     string   `json:"launchType,omitempty"`
	RolloutState   string   `json:"rolloutState"`
	CreatedAt      *float64 `json:"createdAt,omitempty"`
	UpdatedAt      *float64 `json:"updatedAt,omitempty"`
}

type wireTask struct {
	TaskArn           string              `json:"taskArn"`
	ClusterArn        string              `json:"clusterArn"`
	TaskDefinitionArn string              `json:"taskDefinitionArn"`
	Group             string              `json:"group,omitempty"`
	LaunchType        string              `json:"launchType,omitempty"`
	LastStatus        string              `json:"lastStatus"`
	DesiredStatus     string              `json:"desiredStatus"`
	Containers        []wireTaskContainer `json:"containers"`
	StartedAt         *float64            `json:"startedAt,omitempty"`
	StoppedAt         *float64            `json:"stoppedAt,omitempty"`
	StoppedReason     string              `json:"stoppedReason,omitempty"`
	StopCode          string              `json:"stopCode,omitempty"`
	CreatedAt         *float64            `json:"createdAt,omitempty"`
	Tags              []wireTag           `json:"tags,omitempty"`
	HealthStatus      string              `json:"healthStatus,omitempty"`
	Overrides         *wireTaskOverride   `json:"overrides,omitempty"`
}

type wireTaskOverride struct {
	ContainerOverrides []wireContainerOverride `json:"containerOverrides,omitempty"`
	Cpu                string                  `json:"cpu,omitempty"`
	Memory             string                  `json:"memory,omitempty"`
	TaskRoleArn        string                  `json:"taskRoleArn,omitempty"`
	ExecutionRoleArn   string                  `json:"executionRoleArn,omitempty"`
}

type wireContainerOverride struct {
	Name              string                `json:"name"`
	Command           []string              `json:"command,omitempty"`
	Environment       []wireKeyValuePair    `json:"environment,omitempty"`
	Cpu               int                   `json:"cpu,omitempty"`
	Memory            int                   `json:"memory,omitempty"`
	MemoryReservation int                   `json:"memoryReservation,omitempty"`
	EnvironmentFiles  []wireEnvironmentFile `json:"environmentFiles,omitempty"`
}

type wireTaskContainer struct {
	Name            string               `json:"name"`
	ContainerArn    string               `json:"containerArn"`
	ContainerID     string               `json:"runtimeId,omitempty"`
	LastStatus      string               `json:"lastStatus"`
	ExitCode        *int64               `json:"exitCode,omitempty"`
	Reason          string               `json:"reason,omitempty"`
	NetworkBindings []wireNetworkBinding `json:"networkBindings,omitempty"`
	HealthStatus    string               `json:"healthStatus,omitempty"`
}

type wireNetworkBinding struct {
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort"`
	Protocol      string `json:"protocol,omitempty"`
	BindIP        string `json:"bindIP,omitempty"`
}

type wireFailure struct {
	Arn    string `json:"arn,omitempty"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type wireCreateClusterOutput struct {
	Cluster *wireCluster `json:"cluster"`
}

type wireDescribeClustersOutput struct {
	Clusters []wireCluster `json:"clusters"`
	Failures []wireFailure `json:"failures,omitempty"`
}

// wireRegisterTaskDefinitionOutput carries Tags at the top level, alongside
// TaskDefinition, never inside it — matching real ECS's
// RegisterTaskDefinitionResponse shape.
type wireRegisterTaskDefinitionOutput struct {
	TaskDefinition *wireTaskDefinition `json:"taskDefinition"`
	Tags           []wireTag           `json:"tags,omitempty"`
}

// wireDescribeTaskDefinitionOutput carries Tags at the top level (only when
// the request's Include contained "TAGS"), never inside TaskDefinition —
// matching real ECS's DescribeTaskDefinitionResponse shape.
type wireDescribeTaskDefinitionOutput struct {
	TaskDefinition *wireTaskDefinition `json:"taskDefinition"`
	Tags           []wireTag           `json:"tags,omitempty"`
}

type wireListTagsForResourceOutput struct {
	Tags []wireTag `json:"tags"`
}

type wireRunTaskOutput struct {
	Tasks    []wireTask    `json:"tasks"`
	Failures []wireFailure `json:"failures,omitempty"`
}

type wireStopTaskOutput struct {
	Task *wireTask `json:"task"`
}

type wireDescribeTasksOutput struct {
	Tasks    []wireTask    `json:"tasks"`
	Failures []wireFailure `json:"failures,omitempty"`
}

type wireCreateServiceOutput struct {
	Service *wireService `json:"service"`
}

type wireUpdateServiceOutput struct {
	Service *wireService `json:"service"`
}

type wireDeleteServiceOutput struct {
	Service *wireService `json:"service"`
}

type wireDescribeServicesOutput struct {
	Services []wireService `json:"services"`
	Failures []wireFailure `json:"failures,omitempty"`
}

func ecsWireBody(body any) any {
	switch value := body.(type) {
	case types.CreateClusterOutput:
		return wireCreateClusterOutput{Cluster: wireClusterPtr(value.Cluster)}
	case *types.CreateClusterOutput:
		if value == nil {
			return (*wireCreateClusterOutput)(nil)
		}
		return wireCreateClusterOutput{Cluster: wireClusterPtr(value.Cluster)}
	case types.ListClustersOutput:
		return wireStringList(value.ClusterArns, "clusterArns", value.NextToken)
	case *types.ListClustersOutput:
		if value == nil {
			return nil
		}
		return wireStringList(value.ClusterArns, "clusterArns", value.NextToken)
	case types.DescribeClustersOutput:
		return wireDescribeClusters(value)
	case *types.DescribeClustersOutput:
		if value == nil {
			return (*wireDescribeClustersOutput)(nil)
		}
		return wireDescribeClusters(*value)
	case deleteClusterOutput:
		return wireCreateClusterOutput{Cluster: wireClusterPtr(value.Cluster)}
	case types.RegisterTaskDefinitionOutput:
		return wireRegisterTaskDefinitionOutput{TaskDefinition: wireTaskDefinitionPtr(value.TaskDefinition), Tags: wireTags(value.Tags)}
	case *types.RegisterTaskDefinitionOutput:
		if value == nil {
			return (*wireRegisterTaskDefinitionOutput)(nil)
		}
		return wireRegisterTaskDefinitionOutput{TaskDefinition: wireTaskDefinitionPtr(value.TaskDefinition), Tags: wireTags(value.Tags)}
	case types.DescribeTaskDefinitionOutput:
		return wireDescribeTaskDefinitionOutput{TaskDefinition: wireTaskDefinitionPtr(value.TaskDefinition), Tags: wireTags(value.Tags)}
	case *types.DescribeTaskDefinitionOutput:
		if value == nil {
			return (*wireDescribeTaskDefinitionOutput)(nil)
		}
		return wireDescribeTaskDefinitionOutput{TaskDefinition: wireTaskDefinitionPtr(value.TaskDefinition), Tags: wireTags(value.Tags)}
	case types.ListTagsForResourceOutput:
		return wireListTagsForResourceOutput{Tags: wireTagsOrEmpty(value.Tags)}
	case *types.ListTagsForResourceOutput:
		if value == nil {
			return (*wireListTagsForResourceOutput)(nil)
		}
		return wireListTagsForResourceOutput{Tags: wireTagsOrEmpty(value.Tags)}
	case types.ListTaskDefinitionsOutput:
		return wireStringList(value.TaskDefinitionArns, "taskDefinitionArns", value.NextToken)
	case *types.ListTaskDefinitionsOutput:
		if value == nil {
			return nil
		}
		return wireStringList(value.TaskDefinitionArns, "taskDefinitionArns", value.NextToken)
	case types.RunTaskOutput:
		return wireRunTask(value)
	case *types.RunTaskOutput:
		if value == nil {
			return (*wireRunTaskOutput)(nil)
		}
		return wireRunTask(*value)
	case types.StopTaskOutput:
		return wireStopTaskOutput{Task: wireTaskPtr(value.Task)}
	case *types.StopTaskOutput:
		if value == nil {
			return (*wireStopTaskOutput)(nil)
		}
		return wireStopTaskOutput{Task: wireTaskPtr(value.Task)}
	case types.ListTasksOutput:
		return wireStringList(value.TaskArns, "taskArns", value.NextToken)
	case *types.ListTasksOutput:
		if value == nil {
			return nil
		}
		return wireStringList(value.TaskArns, "taskArns", value.NextToken)
	case types.DescribeTasksOutput:
		return wireDescribeTasks(value)
	case *types.DescribeTasksOutput:
		if value == nil {
			return (*wireDescribeTasksOutput)(nil)
		}
		return wireDescribeTasks(*value)
	case types.CreateServiceOutput:
		return wireCreateServiceOutput{Service: wireServicePtr(value.Service)}
	case *types.CreateServiceOutput:
		if value == nil {
			return (*wireCreateServiceOutput)(nil)
		}
		return wireCreateServiceOutput{Service: wireServicePtr(value.Service)}
	case types.UpdateServiceOutput:
		return wireUpdateServiceOutput{Service: wireServicePtr(value.Service)}
	case *types.UpdateServiceOutput:
		if value == nil {
			return (*wireUpdateServiceOutput)(nil)
		}
		return wireUpdateServiceOutput{Service: wireServicePtr(value.Service)}
	case types.DeleteServiceOutput:
		return wireDeleteServiceOutput{Service: wireServicePtr(value.Service)}
	case *types.DeleteServiceOutput:
		if value == nil {
			return (*wireDeleteServiceOutput)(nil)
		}
		return wireDeleteServiceOutput{Service: wireServicePtr(value.Service)}
	case types.ListServicesOutput:
		return wireStringList(value.ServiceArns, "serviceArns", value.NextToken)
	case *types.ListServicesOutput:
		if value == nil {
			return nil
		}
		return wireStringList(value.ServiceArns, "serviceArns", value.NextToken)
	case types.DescribeServicesOutput:
		return wireDescribeServices(value)
	case *types.DescribeServicesOutput:
		if value == nil {
			return (*wireDescribeServicesOutput)(nil)
		}
		return wireDescribeServices(*value)
	default:
		return body
	}
}

func wireStringList(values []string, key, nextToken string) map[string]any {
	if values == nil {
		values = []string{}
	}
	out := map[string]any{key: values}
	if nextToken != "" {
		out["nextToken"] = nextToken
	}
	return out
}

func wireDescribeClusters(value types.DescribeClustersOutput) wireDescribeClustersOutput {
	if value.Clusters == nil {
		value.Clusters = []types.Cluster{}
	}
	clusters := make([]wireCluster, len(value.Clusters))
	for i := range value.Clusters {
		clusters[i] = wireClusterValue(value.Clusters[i])
	}
	return wireDescribeClustersOutput{Clusters: clusters, Failures: wireFailures(value.Failures)}
}

func wireRunTask(value types.RunTaskOutput) wireRunTaskOutput {
	if value.Tasks == nil {
		value.Tasks = []types.Task{}
	}
	tasks := make([]wireTask, len(value.Tasks))
	for i := range value.Tasks {
		tasks[i] = wireTaskValue(value.Tasks[i])
	}
	return wireRunTaskOutput{Tasks: tasks, Failures: wireFailures(value.Failures)}
}

func wireDescribeTasks(value types.DescribeTasksOutput) wireDescribeTasksOutput {
	if value.Tasks == nil {
		value.Tasks = []types.Task{}
	}
	tasks := make([]wireTask, len(value.Tasks))
	for i := range value.Tasks {
		tasks[i] = wireTaskValue(value.Tasks[i])
	}
	return wireDescribeTasksOutput{Tasks: tasks, Failures: wireFailures(value.Failures)}
}

func wireDescribeServices(value types.DescribeServicesOutput) wireDescribeServicesOutput {
	if value.Services == nil {
		value.Services = []types.ECSService{}
	}
	services := make([]wireService, len(value.Services))
	for i := range value.Services {
		services[i] = wireServiceValue(value.Services[i])
	}
	return wireDescribeServicesOutput{Services: services, Failures: wireFailures(value.Failures)}
}

func wireFailures(values []types.Failure) []wireFailure {
	if values == nil {
		return nil
	}
	out := make([]wireFailure, len(values))
	for i, value := range values {
		out[i] = wireFailure{Arn: value.Arn, Reason: value.Reason, Detail: value.Detail}
	}
	return out
}

func wireClusterPtr(value *types.Cluster) *wireCluster {
	if value == nil {
		return nil
	}
	out := wireClusterValue(*value)
	return &out
}

func wireClusterValue(value types.Cluster) wireCluster {
	return wireCluster{
		ClusterName:                       value.ClusterName,
		ClusterArn:                        value.ClusterArn,
		Status:                            value.Status,
		RegisteredContainerInstancesCount: value.RegisteredContainerInstancesCount,
		RunningTasksCount:                 value.RunningTasksCount,
		PendingTasksCount:                 value.PendingTasksCount,
		ActiveServicesCount:               value.ActiveServicesCount,
		Tags:                              wireTags(value.Tags),
	}
}

// wireTags converts a domain tag list to the wire shape, or nil for an empty
// list (so "tags" is omitted rather than emitted as []).
func wireTags(values []types.Tag) []wireTag {
	if len(values) == 0 {
		return nil
	}
	out := make([]wireTag, len(values))
	for i, t := range values {
		out[i] = wireTag{Key: t.Key, Value: t.Value}
	}
	return out
}

// wireTagsOrEmpty is like wireTags but returns [] rather than nil for an
// empty list, matching ListTagsForResource's always-present "tags" array.
func wireTagsOrEmpty(values []types.Tag) []wireTag {
	if tags := wireTags(values); tags != nil {
		return tags
	}
	return []wireTag{}
}

func wireTaskDefinitionPtr(value *types.TaskDefinition) *wireTaskDefinition {
	if value == nil {
		return nil
	}
	out := wireTaskDefinitionValue(*value)
	return &out
}

func wireTaskDefinitionValue(value types.TaskDefinition) wireTaskDefinition {
	containers := make([]wireContainerDefinition, len(value.ContainerDefinitions))
	for i, container := range value.ContainerDefinitions {
		containers[i] = wireContainerDefinitionValue(container)
	}
	return wireTaskDefinition{
		TaskDefinitionArn:       value.TaskDefinitionArn,
		Family:                  value.Family,
		Revision:                value.Revision,
		ContainerDefinitions:    containers,
		Cpu:                     value.Cpu,
		Memory:                  value.Memory,
		NetworkMode:             value.NetworkMode,
		Status:                  value.Status,
		RequiresCompatibilities: value.RequiresCompatibilities,
		RegisteredAt:            epochSecondsOrNil(value.RegisteredAt),

		TaskRoleArn:          value.TaskRoleArn,
		ExecutionRoleArn:     value.ExecutionRoleArn,
		PidMode:              value.PidMode,
		IpcMode:              value.IpcMode,
		RuntimePlatform:      wireRuntimePlatformPtr(value.RuntimePlatform),
		EphemeralStorage:     wireEphemeralStoragePtr(value.EphemeralStorage),
		Volumes:              wireVolumes(value.Volumes),
		PlacementConstraints: wirePlacementConstraints(value.PlacementConstraints),
	}
}

func wireRuntimePlatformPtr(value *types.RuntimePlatform) *wireRuntimePlatform {
	if value == nil {
		return nil
	}
	return &wireRuntimePlatform{CpuArchitecture: value.CpuArchitecture, OperatingSystemFamily: value.OperatingSystemFamily}
}

func wireEphemeralStoragePtr(value *types.EphemeralStorage) *wireEphemeralStorage {
	if value == nil {
		return nil
	}
	return &wireEphemeralStorage{SizeInGiB: value.SizeInGiB}
}

func wirePlacementConstraints(values []types.PlacementConstraint) []wirePlacementConstraint {
	if values == nil {
		return nil
	}
	out := make([]wirePlacementConstraint, len(values))
	for i, v := range values {
		out[i] = wirePlacementConstraint{Type: v.Type, Expression: v.Expression}
	}
	return out
}

func wireVolumes(values []types.Volume) []wireVolume {
	if values == nil {
		return nil
	}
	out := make([]wireVolume, len(values))
	for i, v := range values {
		wv := wireVolume{Name: v.Name}
		if v.Host != nil {
			wv.Host = &wireHostVolumeProperties{SourcePath: v.Host.SourcePath}
		}
		if v.DockerVolumeConfiguration != nil {
			wv.DockerVolumeConfiguration = &wireDockerVolumeConfig{
				Scope:         v.DockerVolumeConfiguration.Scope,
				Autoprovision: v.DockerVolumeConfiguration.Autoprovision,
				Driver:        v.DockerVolumeConfiguration.Driver,
				DriverOpts:    v.DockerVolumeConfiguration.DriverOpts,
				Labels:        v.DockerVolumeConfiguration.Labels,
			}
		}
		if v.EfsVolumeConfiguration != nil {
			wv.EfsVolumeConfiguration = &wireEFSVolumeConfiguration{
				FileSystemId:          v.EfsVolumeConfiguration.FileSystemId,
				RootDirectory:         v.EfsVolumeConfiguration.RootDirectory,
				TransitEncryption:     v.EfsVolumeConfiguration.TransitEncryption,
				TransitEncryptionPort: v.EfsVolumeConfiguration.TransitEncryptionPort,
			}
			if v.EfsVolumeConfiguration.AuthorizationConfig != nil {
				wv.EfsVolumeConfiguration.AuthorizationConfig = &wireEFSAuthorizationConfig{
					AccessPointId: v.EfsVolumeConfiguration.AuthorizationConfig.AccessPointId,
					IAM:           v.EfsVolumeConfiguration.AuthorizationConfig.IAM,
				}
			}
		}
		out[i] = wv
	}
	return out
}

func wireContainerDefinitionValue(value types.ContainerDefinition) wireContainerDefinition {
	environment := make([]wireKeyValuePair, len(value.Environment))
	for i, pair := range value.Environment {
		environment[i] = wireKeyValuePair{Name: pair.Name, Value: pair.Value}
	}
	ports := make([]wirePortMapping, len(value.PortMappings))
	for i, port := range value.PortMappings {
		ports[i] = wirePortMapping{ContainerPort: port.ContainerPort, HostPort: port.HostPort, Protocol: port.Protocol}
	}
	var logConfiguration *wireLogConfiguration
	if value.LogConfiguration != nil {
		logConfiguration = &wireLogConfiguration{
			LogDriver: value.LogConfiguration.LogDriver,
			Options:   value.LogConfiguration.Options,
		}
	}
	var secrets []wireContainerSecret
	if value.Secrets != nil {
		secrets = make([]wireContainerSecret, len(value.Secrets))
		for i, secret := range value.Secrets {
			secrets[i] = wireContainerSecret{Name: secret.Name, ValueFrom: secret.ValueFrom}
		}
	}
	var healthCheck *wireContainerHealthCheck
	if value.HealthCheck != nil {
		healthCheck = &wireContainerHealthCheck{
			Command:     value.HealthCheck.Command,
			Interval:    value.HealthCheck.Interval,
			Timeout:     value.HealthCheck.Timeout,
			Retries:     value.HealthCheck.Retries,
			StartPeriod: value.HealthCheck.StartPeriod,
		}
	}
	var dependsOn []wireContainerDependency
	if value.DependsOn != nil {
		dependsOn = make([]wireContainerDependency, len(value.DependsOn))
		for i, dep := range value.DependsOn {
			dependsOn[i] = wireContainerDependency{ContainerName: dep.ContainerName, Condition: dep.Condition}
		}
	}
	return wireContainerDefinition{
		Name:              value.Name,
		Image:             value.Image,
		Command:           value.Command,
		EntryPoint:        value.EntryPoint,
		Environment:       environment,
		PortMappings:      ports,
		LogConfiguration:  logConfiguration,
		Essential:         value.Essential,
		Cpu:               value.Cpu,
		Memory:            value.Memory,
		MemoryReservation: value.MemoryReservation,
		Secrets:           secrets,
		HealthCheck:       healthCheck,
		DependsOn:         dependsOn,

		WorkingDirectory: value.WorkingDirectory,
		User:             value.User,
		StopTimeout:      value.StopTimeout,
		StartTimeout:     value.StartTimeout,
		Ulimits:          wireUlimits(value.Ulimits),
		DockerLabels:     value.DockerLabels,
		MountPoints:      wireMountPoints(value.MountPoints),
		VolumesFrom:      wireVolumesFrom(value.VolumesFrom),

		ReadonlyRootFilesystem: value.ReadonlyRootFilesystem,
		Privileged:             value.Privileged,
		LinuxParameters:        wireLinuxParametersPtr(value.LinuxParameters),

		Hostname:       value.Hostname,
		DnsServers:     value.DnsServers,
		ExtraHosts:     wireHostEntries(value.ExtraHosts),
		Interactive:    value.Interactive,
		PseudoTerminal: value.PseudoTerminal,
		SystemControls: wireSystemControls(value.SystemControls),

		EnvironmentFiles:      wireEnvironmentFiles(value.EnvironmentFiles),
		RepositoryCredentials: wireRepositoryCredentialsPtr(value.RepositoryCredentials),
		FirelensConfiguration: wireFirelensConfigurationPtr(value.FirelensConfiguration),
	}
}

func wireUlimits(values []types.Ulimit) []wireUlimit {
	if values == nil {
		return nil
	}
	out := make([]wireUlimit, len(values))
	for i, v := range values {
		out[i] = wireUlimit{Name: v.Name, SoftLimit: v.SoftLimit, HardLimit: v.HardLimit}
	}
	return out
}

func wireMountPoints(values []types.MountPoint) []wireMountPoint {
	if values == nil {
		return nil
	}
	out := make([]wireMountPoint, len(values))
	for i, v := range values {
		out[i] = wireMountPoint{SourceVolume: v.SourceVolume, ContainerPath: v.ContainerPath, ReadOnly: v.ReadOnly}
	}
	return out
}

func wireVolumesFrom(values []types.VolumeFrom) []wireVolumeFrom {
	if values == nil {
		return nil
	}
	out := make([]wireVolumeFrom, len(values))
	for i, v := range values {
		out[i] = wireVolumeFrom{SourceContainer: v.SourceContainer, ReadOnly: v.ReadOnly}
	}
	return out
}

func wireHostEntries(values []types.HostEntry) []wireHostEntry {
	if values == nil {
		return nil
	}
	out := make([]wireHostEntry, len(values))
	for i, v := range values {
		out[i] = wireHostEntry{Hostname: v.Hostname, IpAddress: v.IpAddress}
	}
	return out
}

func wireSystemControls(values []types.SystemControl) []wireSystemControl {
	if values == nil {
		return nil
	}
	out := make([]wireSystemControl, len(values))
	for i, v := range values {
		out[i] = wireSystemControl{Namespace: v.Namespace, Value: v.Value}
	}
	return out
}

func wireEnvironmentFiles(values []types.EnvironmentFile) []wireEnvironmentFile {
	if values == nil {
		return nil
	}
	out := make([]wireEnvironmentFile, len(values))
	for i, v := range values {
		out[i] = wireEnvironmentFile{Value: v.Value, Type: v.Type}
	}
	return out
}

func wireRepositoryCredentialsPtr(value *types.RepositoryCredentials) *wireRepositoryCredentials {
	if value == nil {
		return nil
	}
	return &wireRepositoryCredentials{CredentialsParameter: value.CredentialsParameter}
}

func wireFirelensConfigurationPtr(value *types.FirelensConfiguration) *wireFirelensConfiguration {
	if value == nil {
		return nil
	}
	return &wireFirelensConfiguration{Type: value.Type, Options: value.Options}
}

func wireLinuxParametersPtr(value *types.LinuxParameters) *wireLinuxParameters {
	if value == nil {
		return nil
	}
	out := &wireLinuxParameters{
		InitProcessEnabled: value.InitProcessEnabled,
		SharedMemorySize:   value.SharedMemorySize,
	}
	if value.Capabilities != nil {
		out.Capabilities = &wireKernelCapabilities{Add: value.Capabilities.Add, Drop: value.Capabilities.Drop}
	}
	if value.Tmpfs != nil {
		out.Tmpfs = make([]wireTmpfs, len(value.Tmpfs))
		for i, tf := range value.Tmpfs {
			out.Tmpfs[i] = wireTmpfs{ContainerPath: tf.ContainerPath, Size: tf.Size, MountOptions: tf.MountOptions}
		}
	}
	return out
}

func wireServicePtr(value *types.ECSService) *wireService {
	if value == nil {
		return nil
	}
	out := wireServiceValue(*value)
	return &out
}

func wireServiceValue(value types.ECSService) wireService {
	createdAt := epochSecondsOrNil(value.CreatedAt)
	return wireService{
		ServiceArn:              value.ServiceArn,
		ServiceName:             value.ServiceName,
		ClusterArn:              value.ClusterArn,
		TaskDefinitionArn:       value.TaskDefinitionArn,
		DesiredCount:            value.DesiredCount,
		RunningCount:            value.RunningCount,
		PendingCount:            value.PendingCount,
		LaunchType:              value.LaunchType,
		Status:                  value.Status,
		Deployments:             []wireDeployment{wireServiceDeployment(value, createdAt)},
		CreatedAt:               createdAt,
		SchedulingStrategy:      value.SchedulingStrategy,
		NetworkConfiguration:    wireNetworkConfigurationPtr(value.NetworkConfiguration),
		PlatformVersion:         value.PlatformVersion,
		DeploymentConfiguration: wireDeploymentConfigurationPtr(value.DeploymentConfiguration),
		EnableECSManagedTags:    value.EnableECSManagedTags,
		PropagateTags:           value.PropagateTags,
		Tags:                    wireTags(value.Tags),
	}
}

func wireNetworkConfigurationPtr(value *types.NetworkConfiguration) *wireNetworkConfiguration {
	if value == nil {
		return nil
	}
	out := wireNetworkConfiguration{}
	if value.AwsvpcConfiguration != nil {
		out.AwsvpcConfiguration = &wireAwsVpcConfiguration{
			Subnets:        value.AwsvpcConfiguration.Subnets,
			SecurityGroups: value.AwsvpcConfiguration.SecurityGroups,
			AssignPublicIp: value.AwsvpcConfiguration.AssignPublicIp,
		}
	}
	return &out
}

func wireDeploymentConfigurationPtr(value *types.DeploymentConfiguration) *wireDeploymentConfiguration {
	if value == nil {
		return nil
	}
	return &wireDeploymentConfiguration{
		MaximumPercent:        value.MaximumPercent,
		MinimumHealthyPercent: value.MinimumHealthyPercent,
	}
}

// wireServiceDeployment builds the single PRIMARY deployment DescribeServices
// reports for a service. id is derived from the service ARN so repeated
// describes of the same service return the same deployment id, the way AWS
// callers (aws ecs wait services-stable, Terraform) expect a stable
// identity to poll against.
func wireServiceDeployment(value types.ECSService, createdAt *float64) wireDeployment {
	rolloutState := "IN_PROGRESS"
	if value.RunningCount == value.DesiredCount {
		rolloutState = "COMPLETED"
	}
	return wireDeployment{
		ID:             deploymentID(value.ServiceArn),
		Status:         "PRIMARY",
		TaskDefinition: value.TaskDefinitionArn,
		DesiredCount:   value.DesiredCount,
		RunningCount:   value.RunningCount,
		PendingCount:   value.PendingCount,
		LaunchType:     value.LaunchType,
		RolloutState:   rolloutState,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
}

// deploymentID derives a stable ecs-svc/<id> deployment identifier from a
// service ARN, the same "ecs-svc/" prefix real ECS deployment ids use.
func deploymentID(serviceArn string) string {
	suffix := serviceArn
	if idx := strings.LastIndex(serviceArn, "/"); idx >= 0 {
		suffix = serviceArn[idx+1:]
	}
	if suffix == "" {
		suffix = "unknown"
	}
	return "ecs-svc/" + suffix
}

func wireTaskPtr(value *types.Task) *wireTask {
	if value == nil {
		return nil
	}
	out := wireTaskValue(*value)
	return &out
}

func wireTaskValue(value types.Task) wireTask {
	containers := make([]wireTaskContainer, len(value.Containers))
	for i, container := range value.Containers {
		containers[i] = wireTaskContainerValue(container)
	}
	return wireTask{
		TaskArn:           value.TaskArn,
		ClusterArn:        value.ClusterArn,
		TaskDefinitionArn: value.TaskDefinitionArn,
		Group:             value.Group,
		LaunchType:        value.LaunchType,
		LastStatus:        value.LastStatus,
		DesiredStatus:     value.DesiredStatus,
		Containers:        containers,
		StartedAt:         epochSecondsPtr(value.StartedAt),
		StoppedAt:         epochSecondsPtr(value.StoppedAt),
		StoppedReason:     value.StoppedReason,
		StopCode:          value.StopCode,
		CreatedAt:         epochSecondsOrNil(value.CreatedAt),
		Tags:              wireTags(value.Tags),
		HealthStatus:      value.HealthStatus,
		Overrides:         wireTaskOverridePtr(value.Overrides),
	}
}

func wireTaskOverridePtr(value *types.TaskOverride) *wireTaskOverride {
	if value == nil {
		return nil
	}
	out := &wireTaskOverride{
		Cpu:              value.Cpu,
		Memory:           value.Memory,
		TaskRoleArn:      value.TaskRoleArn,
		ExecutionRoleArn: value.ExecutionRoleArn,
	}
	if value.ContainerOverrides != nil {
		out.ContainerOverrides = make([]wireContainerOverride, len(value.ContainerOverrides))
		for i, co := range value.ContainerOverrides {
			environment := make([]wireKeyValuePair, len(co.Environment))
			for j, kv := range co.Environment {
				environment[j] = wireKeyValuePair{Name: kv.Name, Value: kv.Value}
			}
			out.ContainerOverrides[i] = wireContainerOverride{
				Name:              co.Name,
				Command:           co.Command,
				Environment:       environment,
				Cpu:               co.Cpu,
				Memory:            co.Memory,
				MemoryReservation: co.MemoryReservation,
				EnvironmentFiles:  wireEnvironmentFiles(co.EnvironmentFiles),
			}
		}
	}
	return out
}

func wireTaskContainerValue(value types.TaskContainer) wireTaskContainer {
	bindings := make([]wireNetworkBinding, len(value.NetworkBindings))
	for i, binding := range value.NetworkBindings {
		bindings[i] = wireNetworkBinding{
			ContainerPort: binding.ContainerPort,
			HostPort:      binding.HostPort,
			Protocol:      binding.Protocol,
			BindIP:        binding.BindIP,
		}
	}
	return wireTaskContainer{
		Name:            value.Name,
		ContainerArn:    value.ContainerArn,
		ContainerID:     value.ContainerID,
		LastStatus:      value.LastStatus,
		ExitCode:        value.ExitCode,
		Reason:          value.Reason,
		NetworkBindings: bindings,
		HealthStatus:    value.HealthStatus,
	}
}

func epochSecondsPtr(value *time.Time) *float64 {
	if value == nil {
		return nil
	}
	return epochSecondsOrNil(*value)
}

// epochSecondsOrNil converts value to epoch seconds, or nil for the zero
// time.Time. Legacy on-disk state (or a domain field that was simply never
// set) can carry a zero timestamp; serializing that as epoch second 0 would
// read as a real, wrong-looking year-0001 date to a client, so it is
// omitted instead.
func epochSecondsOrNil(value time.Time) *float64 {
	if value.IsZero() {
		return nil
	}
	seconds := float64(value.UnixMilli()) / 1000
	return &seconds
}
