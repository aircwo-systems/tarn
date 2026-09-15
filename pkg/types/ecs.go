package types

import (
	"context"
	"time"
)

// Task lifecycle statuses, following the AWS ECS state machine:
// PROVISIONING -> PENDING -> RUNNING -> DEACTIVATING -> STOPPING ->
// DEPROVISIONING -> STOPPED.
const (
	TaskStatusProvisioning   = "PROVISIONING"
	TaskStatusPending        = "PENDING"
	TaskStatusRunning        = "RUNNING"
	TaskStatusDeactivating   = "DEACTIVATING"
	TaskStatusStopping       = "STOPPING"
	TaskStatusDeprovisioning = "DEPROVISIONING"
	TaskStatusStopped        = "STOPPED"
)

// Desired status values a caller may request for a task.
const (
	TaskDesiredStatusRunning = "RUNNING"
	TaskDesiredStatusStopped = "STOPPED"
)

// Cluster lifecycle statuses.
const (
	ClusterStatusActive   = "ACTIVE"
	ClusterStatusInactive = "INACTIVE"
)

// Task definition lifecycle statuses.
const (
	TaskDefinitionStatusActive           = "ACTIVE"
	TaskDefinitionStatusInactive         = "INACTIVE"
	TaskDefinitionStatusDeleteInProgress = "DELETE_IN_PROGRESS"
)

// Task definition list ordering values.
const (
	TaskDefinitionSortAscending  = "ASC"
	TaskDefinitionSortDescending = "DESC"
)

// Launch types. Tarn runs everything locally via Docker; FARGATE vs EC2 is a
// reported distinction only (see docs/design/ecs-support.md, "Open questions").
const (
	LaunchTypeFargate = "FARGATE"
	LaunchTypeEC2     = "EC2"
)

// Network modes recognised on a TaskDefinition.
const (
	NetworkModeAwsVPC = "awsvpc"
	NetworkModeBridge = "bridge"
	NetworkModeHost   = "host"
	NetworkModeNone   = "none"
)

// ECSService lifecycle statuses.
const (
	ServiceStatusActive   = "ACTIVE"
	ServiceStatusDraining = "DRAINING"
	ServiceStatusInactive = "INACTIVE"
)

// TaskEventPayloadEnvMaxBytes leaves room for the EVENT_PAYLOAD= prefix under
// Linux's MAX_ARG_STRLEN (131072 bytes for one "NAME=value" environment
// string), and for the terminating NUL that MAX_ARG_STRLEN counts against
// the value but that len() does not. Larger EventBridge payloads are mounted
// into the task instead.
const TaskEventPayloadEnvMaxBytes = 128*1024 - len("EVENT_PAYLOAD=") - 1

// Cluster models an ECS cluster: a named grouping that tasks and services
// belong to. Tarn has no real capacity model, so RegisteredContainerInstancesCount
// is always 0 and is kept only for wire compatibility.
type Cluster struct {
	ClusterName                       string `json:"ClusterName"`
	ClusterArn                        string `json:"ClusterArn"`
	Status                            string `json:"Status"`
	RegisteredContainerInstancesCount int    `json:"registeredContainerInstancesCount"`
	RunningTasksCount                 int    `json:"runningTasksCount"`
	PendingTasksCount                 int    `json:"pendingTasksCount"`
	ActiveServicesCount               int    `json:"activeServicesCount"`
	// Tags is only echoed on DescribeClusters when the request's Include
	// contains "TAGS", matching real ECS. See internal/ecs/service.go's
	// includesTag / applyTagInclusion.
	Tags []Tag `json:"Tags,omitempty"`
}

// Tag mirrors the AWS ECS tag shape. Unlike most ECS wire fields (which are
// camelCase), ECS's Tag member names are lowercase "key"/"value" on the
// wire; see internal/api/ecs/wire.go's wireTag.
type Tag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

// TaskDefinition models one revision of a task definition family. Family plus
// Revision together form the identity that ARNs like
// arn:aws:ecs:...:task-definition/<family>:<revision> encode.
type TaskDefinition struct {
	TaskDefinitionArn       string                `json:"TaskDefinitionArn"`
	Family                  string                `json:"Family"`
	Revision                int                   `json:"Revision"`
	ContainerDefinitions    []ContainerDefinition `json:"ContainerDefinitions"`
	Cpu                     string                `json:"Cpu,omitempty"`
	Memory                  string                `json:"Memory,omitempty"`
	NetworkMode             string                `json:"NetworkMode,omitempty"`
	Status                  string                `json:"Status"`
	RequiresCompatibilities []string              `json:"RequiresCompatibilities,omitempty"`
	RegisteredAt            time.Time             `json:"RegisteredAt"`
	// Tags is never part of the taskDefinition wire shape itself (real ECS
	// reports tags at the top level of Register/DescribeTaskDefinition's
	// output, only when Include contains "TAGS"); wireTaskDefinitionValue in
	// internal/api/ecs/wire.go deliberately does not map this field onto
	// wireTaskDefinition. The JSON tag here only matters for on-disk
	// persistence (internal/ecs/store.go marshals types.TaskDefinition
	// directly).
	Tags []Tag `json:"Tags,omitempty"`

	// TaskRoleArn / ExecutionRoleArn are stored and echoed for Terraform
	// drift-avoidance only; Tarn does not vend credentials from either role
	// locally.
	TaskRoleArn      string `json:"TaskRoleArn,omitempty"`
	ExecutionRoleArn string `json:"ExecutionRoleArn,omitempty"`
	// PidMode and IpcMode are stored/echoed only; the namespace sharing they
	// describe is not applied locally.
	PidMode string `json:"PidMode,omitempty"`
	IpcMode string `json:"IpcMode,omitempty"`

	RuntimePlatform      *RuntimePlatform      `json:"RuntimePlatform,omitempty"`
	EphemeralStorage     *EphemeralStorage     `json:"EphemeralStorage,omitempty"`
	Volumes              []Volume              `json:"Volumes,omitempty"`
	PlacementConstraints []PlacementConstraint `json:"PlacementConstraints,omitempty"`
}

// RuntimePlatform mirrors the AWS ECS shape. Store/echo only.
type RuntimePlatform struct {
	CpuArchitecture       string `json:"CpuArchitecture,omitempty"`
	OperatingSystemFamily string `json:"OperatingSystemFamily,omitempty"`
}

// EphemeralStorage mirrors the AWS ECS shape. Store/echo only.
type EphemeralStorage struct {
	SizeInGiB int `json:"SizeInGiB,omitempty"`
}

// PlacementConstraint mirrors the AWS ECS shape. Store/echo only.
type PlacementConstraint struct {
	Type       string `json:"Type,omitempty"`
	Expression string `json:"Expression,omitempty"`
}

// Volume mirrors the AWS ECS task-definition volume shape. Host and
// DockerVolumeConfiguration are applied locally (see internal/engine/task.go
// volume resolution); EfsVolumeConfiguration is store/echo only.
type Volume struct {
	Name                      string                     `json:"Name"`
	Host                      *HostVolumeProperties      `json:"Host,omitempty"`
	DockerVolumeConfiguration *DockerVolumeConfiguration `json:"DockerVolumeConfiguration,omitempty"`
	EfsVolumeConfiguration    *EFSVolumeConfiguration    `json:"EfsVolumeConfiguration,omitempty"`
}

// HostVolumeProperties mirrors the AWS ECS shape.
type HostVolumeProperties struct {
	SourcePath string `json:"SourcePath,omitempty"`
}

// DockerVolumeConfiguration mirrors the AWS ECS shape.
type DockerVolumeConfiguration struct {
	Scope         string            `json:"Scope,omitempty"`
	Autoprovision bool              `json:"Autoprovision,omitempty"`
	Driver        string            `json:"Driver,omitempty"`
	DriverOpts    map[string]string `json:"DriverOpts,omitempty"`
	Labels        map[string]string `json:"Labels,omitempty"`
}

// EFSVolumeConfiguration mirrors the AWS ECS shape. Store/echo only: Tarn
// has no EFS emulation.
type EFSVolumeConfiguration struct {
	FileSystemId          string                  `json:"FileSystemId,omitempty"`
	RootDirectory         string                  `json:"RootDirectory,omitempty"`
	TransitEncryption     string                  `json:"TransitEncryption,omitempty"`
	TransitEncryptionPort int                     `json:"TransitEncryptionPort,omitempty"`
	AuthorizationConfig   *EFSAuthorizationConfig `json:"AuthorizationConfig,omitempty"`
}

// EFSAuthorizationConfig mirrors the AWS ECS shape. Store/echo only.
type EFSAuthorizationConfig struct {
	AccessPointId string `json:"AccessPointId,omitempty"`
	IAM           string `json:"IAM,omitempty"`
}

// ContainerDefinition models a single container within a TaskDefinition.
type ContainerDefinition struct {
	Name              string            `json:"Name"`
	Image             string            `json:"Image"`
	Command           []string          `json:"Command,omitempty"`
	EntryPoint        []string          `json:"EntryPoint,omitempty"`
	Environment       []KeyValuePair    `json:"Environment,omitempty"`
	PortMappings      []PortMapping     `json:"PortMappings,omitempty"`
	LogConfiguration  *LogConfiguration `json:"LogConfiguration,omitempty"`
	Essential         *bool             `json:"Essential,omitempty"`
	Cpu               int               `json:"Cpu,omitempty"`
	Memory            int               `json:"Memory,omitempty"`
	MemoryReservation int               `json:"MemoryReservation,omitempty"`
	// Secrets are resolved at launch time (Secrets Manager only — Tarn has no
	// SSM service) and injected as environment variables named Name. Not
	// echoed anywhere except back through this same field: values are never
	// logged, traced, or placed in Docker labels. If an Environment entry
	// shares the same Name, the secret wins (AWS's own precedence here is
	// unusual/undocumented; this is the documented Tarn choice).
	Secrets []ContainerSecret `json:"Secrets,omitempty"`
	// HealthCheck is stored/echoed exactly as given — nil sub-fields must stay
	// nil so DescribeTaskDefinition doesn't introduce drift against
	// Terraform's container_definitions plan. Defaults (interval 30s, timeout
	// 5s, retries 3, startPeriod 0) are applied only at Docker HEALTHCHECK
	// creation time, never written back here.
	HealthCheck *ContainerHealthCheck `json:"HealthCheck,omitempty"`
	// DependsOn orders this container's start relative to others in the same
	// task definition. Validated for unknown container names and cycles at
	// RegisterTaskDefinition time.
	DependsOn []ContainerDependency `json:"DependsOn,omitempty"`

	// Fields below are additional Terraform-drift-avoidance fields. See the
	// per-field comments for which are applied locally vs. store/echo only.
	WorkingDirectory string            `json:"WorkingDirectory,omitempty"`
	User             string            `json:"User,omitempty"`
	StopTimeout      int               `json:"StopTimeout,omitempty"`
	StartTimeout     int               `json:"StartTimeout,omitempty"`
	Ulimits          []Ulimit          `json:"Ulimits,omitempty"`
	DockerLabels     map[string]string `json:"DockerLabels,omitempty"`
	MountPoints      []MountPoint      `json:"MountPoints,omitempty"`
	VolumesFrom      []VolumeFrom      `json:"VolumesFrom,omitempty"`

	ReadonlyRootFilesystem *bool            `json:"ReadonlyRootFilesystem,omitempty"`
	Privileged             *bool            `json:"Privileged,omitempty"`
	LinuxParameters        *LinuxParameters `json:"LinuxParameters,omitempty"`

	Hostname       string          `json:"Hostname,omitempty"`
	DnsServers     []string        `json:"DnsServers,omitempty"`
	ExtraHosts     []HostEntry     `json:"ExtraHosts,omitempty"`
	Interactive    *bool           `json:"Interactive,omitempty"`
	PseudoTerminal *bool           `json:"PseudoTerminal,omitempty"`
	SystemControls []SystemControl `json:"SystemControls,omitempty"`

	// EnvironmentFiles, RepositoryCredentials and FirelensConfiguration are
	// store/echo only: Tarn does not fetch S3 env files, pull credentials
	// from Secrets Manager for image pulls, or run a log router locally.
	EnvironmentFiles      []EnvironmentFile      `json:"EnvironmentFiles,omitempty"`
	RepositoryCredentials *RepositoryCredentials `json:"RepositoryCredentials,omitempty"`
	FirelensConfiguration *FirelensConfiguration `json:"FirelensConfiguration,omitempty"`
}

// Ulimit mirrors the AWS ECS shape. Applied to HostConfig.Ulimits.
type Ulimit struct {
	Name      string `json:"Name"`
	SoftLimit int    `json:"SoftLimit"`
	HardLimit int    `json:"HardLimit"`
}

// MountPoint mirrors the AWS ECS shape: SourceVolume names an entry in the
// TaskDefinition's Volumes list, resolved to a Docker mount at ContainerPath.
type MountPoint struct {
	SourceVolume  string `json:"SourceVolume,omitempty"`
	ContainerPath string `json:"ContainerPath,omitempty"`
	ReadOnly      bool   `json:"ReadOnly,omitempty"`
}

// VolumeFrom mirrors the AWS ECS shape: SourceContainer names another
// container in the same task definition to inherit mounts from.
type VolumeFrom struct {
	SourceContainer string `json:"SourceContainer,omitempty"`
	ReadOnly        bool   `json:"ReadOnly,omitempty"`
}

// LinuxParameters mirrors the (partial) AWS ECS shape. InitProcessEnabled,
// Capabilities and SharedMemorySize/Tmpfs are applied locally; other real
// AWS sub-fields (Devices, MaxSwap, Swappiness) are not modeled.
type LinuxParameters struct {
	InitProcessEnabled *bool               `json:"InitProcessEnabled,omitempty"`
	Capabilities       *KernelCapabilities `json:"Capabilities,omitempty"`
	SharedMemorySize   int                 `json:"SharedMemorySize,omitempty"`
	Tmpfs              []Tmpfs             `json:"Tmpfs,omitempty"`
}

// KernelCapabilities mirrors the AWS ECS shape.
type KernelCapabilities struct {
	Add  []string `json:"Add,omitempty"`
	Drop []string `json:"Drop,omitempty"`
}

// Tmpfs mirrors the AWS ECS shape.
type Tmpfs struct {
	ContainerPath string   `json:"ContainerPath,omitempty"`
	Size          int      `json:"Size,omitempty"`
	MountOptions  []string `json:"MountOptions,omitempty"`
}

// HostEntry mirrors the AWS ECS extraHosts shape.
type HostEntry struct {
	Hostname  string `json:"Hostname,omitempty"`
	IpAddress string `json:"IpAddress,omitempty"`
}

// SystemControl mirrors the AWS ECS shape. Store/echo only.
type SystemControl struct {
	Namespace string `json:"Namespace,omitempty"`
	Value     string `json:"Value,omitempty"`
}

// EnvironmentFile mirrors the AWS ECS shape. Store/echo only.
type EnvironmentFile struct {
	Value string `json:"Value,omitempty"`
	Type  string `json:"Type,omitempty"`
}

// RepositoryCredentials mirrors the AWS ECS shape. Store/echo only.
type RepositoryCredentials struct {
	CredentialsParameter string `json:"CredentialsParameter,omitempty"`
}

// FirelensConfiguration mirrors the AWS ECS shape. Store/echo only.
type FirelensConfiguration struct {
	Type    string            `json:"Type,omitempty"`
	Options map[string]string `json:"Options,omitempty"`
}

// ContainerSecret mirrors the AWS ECS Secret shape: an environment variable
// Name whose value is resolved from ValueFrom at launch time.
type ContainerSecret struct {
	Name      string `json:"Name"`
	ValueFrom string `json:"ValueFrom"`
}

// ContainerHealthCheck mirrors the AWS ECS HealthCheck shape. Interval,
// Timeout, Retries and StartPeriod are pointers so an unset field is
// distinguishable from an explicit 0, matching AWS's echo-only-what-was-set
// behaviour on DescribeTaskDefinition.
type ContainerHealthCheck struct {
	Command     []string `json:"Command,omitempty"`
	Interval    *int     `json:"Interval,omitempty"`
	Timeout     *int     `json:"Timeout,omitempty"`
	Retries     *int     `json:"Retries,omitempty"`
	StartPeriod *int     `json:"StartPeriod,omitempty"`
}

// Container health check defaults, applied only at Docker HEALTHCHECK
// creation time (never persisted back onto a ContainerHealthCheck).
const (
	DefaultHealthCheckIntervalSeconds    = 30
	DefaultHealthCheckTimeoutSeconds     = 5
	DefaultHealthCheckRetries            = 3
	DefaultHealthCheckStartPeriodSeconds = 0
)

// ContainerDependency mirrors the AWS ECS ContainerDependency shape.
type ContainerDependency struct {
	ContainerName string `json:"ContainerName"`
	Condition     string `json:"Condition"`
}

// Container dependency conditions, per AWS ECS semantics.
const (
	ContainerConditionStart    = "START"
	ContainerConditionComplete = "COMPLETE"
	ContainerConditionSuccess  = "SUCCESS"
	ContainerConditionHealthy  = "HEALTHY"
)

// Container/task health status values, mirroring AWS ECS's HealthStatus.
const (
	HealthStatusHealthy   = "HEALTHY"
	HealthStatusUnhealthy = "UNHEALTHY"
	HealthStatusUnknown   = "UNKNOWN"
)

// PortMapping maps a container port to a host port. HostPort is 0 in the
// task definition (meaning "assign an ephemeral port at run time"); the
// value actually bound is recorded on TaskContainer.NetworkBindings instead.
type PortMapping struct {
	ContainerPort int    `json:"ContainerPort"`
	HostPort      int    `json:"HostPort,omitempty"`
	Protocol      string `json:"Protocol,omitempty"`
}

// KeyValuePair mirrors the AWS shape used for container environment
// variables and Docker labels.
type KeyValuePair struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

// LogConfiguration mirrors the AWS awslogs driver configuration. Only the
// awslogs driver is meaningful locally; Options carries
// "awslogs-group" / "awslogs-stream-prefix" when the task definition sets them.
type LogConfiguration struct {
	LogDriver string            `json:"LogDriver"`
	Options   map[string]string `json:"Options,omitempty"`
}

// ECSService models a running ECS service: a desired-count of tasks from one
// task definition, kept alive by a reconcile loop. Named ECSService (not
// Service) to avoid colliding with the Go service type every internal/*
// package defines.
type ECSService struct {
	ServiceArn        string    `json:"ServiceArn"`
	ServiceName       string    `json:"ServiceName"`
	ClusterArn        string    `json:"ClusterArn"`
	TaskDefinitionArn string    `json:"TaskDefinition"`
	DesiredCount      int       `json:"DesiredCount"`
	RunningCount      int       `json:"RunningCount"`
	PendingCount      int       `json:"PendingCount"`
	LaunchType        string    `json:"LaunchType,omitempty"`
	Status            string    `json:"Status"`
	CreatedAt         time.Time `json:"CreatedAt"`

	// SchedulingStrategy is "REPLICA" (default) or "DAEMON". Tarn has no
	// per-host placement model, so DAEMON is accepted and echoed back but
	// behaves identically to REPLICA in the reconcile loop.
	SchedulingStrategy string `json:"SchedulingStrategy,omitempty"`
	// NetworkConfiguration, PlatformVersion, and DeploymentConfiguration are
	// all ForceNew attributes on the Terraform aws_ecs_service resource
	// (terraform-provider-aws v6's ecs service read function treats them as
	// immutable). Failing to echo back what CreateService/UpdateService were
	// given makes every subsequent plan see drift and replace the service.
	NetworkConfiguration    *NetworkConfiguration    `json:"NetworkConfiguration,omitempty"`
	PlatformVersion         string                   `json:"PlatformVersion,omitempty"`
	DeploymentConfiguration *DeploymentConfiguration `json:"DeploymentConfiguration,omitempty"`
	EnableECSManagedTags    bool                     `json:"EnableECSManagedTags,omitempty"`
	PropagateTags           string                   `json:"PropagateTags,omitempty"`
	// Tags is only echoed on DescribeServices when the request's Include
	// contains "TAGS", matching real ECS.
	Tags []Tag `json:"Tags,omitempty"`

	// InactiveAt records when DeleteService tombstoned this record (kept
	// INACTIVE rather than removed, so Terraform's post-destroy
	// DescribeServices poll for status INACTIVE doesn't hang forever waiting
	// for a record that no longer exists). Nil for a service that has never
	// been deleted. Not part of the AWS wire shape — internal/api/ecs/wire.go
	// builds wireService field-by-field and never reads this. Housekeeping
	// prunes tombstones some time after InactiveAt (see
	// internal/ecs/runner.go's reconcileOnce / PruneInactiveServices).
	InactiveAt *time.Time `json:"InactiveAt,omitempty"`
}

// NetworkConfiguration mirrors the AWS ECS shape used by CreateService /
// UpdateService / DescribeServices for awsvpc-mode task networking.
type NetworkConfiguration struct {
	AwsvpcConfiguration *AwsVpcConfiguration `json:"AwsvpcConfiguration,omitempty"`
}

// AwsVpcConfiguration mirrors the AWS ECS awsvpcConfiguration shape.
type AwsVpcConfiguration struct {
	Subnets        []string `json:"Subnets,omitempty"`
	SecurityGroups []string `json:"SecurityGroups,omitempty"`
	AssignPublicIp string   `json:"AssignPublicIp,omitempty"`
}

// DeploymentConfiguration mirrors the AWS ECS shape returned on
// DescribeServices. Tarn has no real rolling-deployment model, so these
// values are stored and echoed back only to keep Terraform's plan stable.
type DeploymentConfiguration struct {
	MaximumPercent        int `json:"MaximumPercent,omitempty"`
	MinimumHealthyPercent int `json:"MinimumHealthyPercent,omitempty"`
}

// Task models one running or completed ECS task: a single instantiation of a
// TaskDefinition, one Docker container per ContainerDefinition.
type Task struct {
	TaskArn           string          `json:"TaskArn"`
	ClusterArn        string          `json:"ClusterArn"`
	TaskDefinitionArn string          `json:"TaskDefinitionArn"`
	Group             string          `json:"Group,omitempty"`
	LaunchType        string          `json:"LaunchType,omitempty"`
	LastStatus        string          `json:"LastStatus"`
	DesiredStatus     string          `json:"DesiredStatus"`
	Containers        []TaskContainer `json:"Containers"`
	StartedAt         *time.Time      `json:"StartedAt,omitempty"`
	StoppedAt         *time.Time      `json:"StoppedAt,omitempty"`
	StoppedReason     string          `json:"StoppedReason,omitempty"`
	CreatedAt         time.Time       `json:"CreatedAt"`
	// CorrelationID is the trace correlation ID assigned to this task at
	// launch (see RunTaskInput.CorrelationID). Not an AWS field — kept off
	// the wire shape in internal/api/ecs/wire.go the same way EventPayload is
	// kept off RunTaskInput's.
	CorrelationID string `json:"-"`
	// Tags is only echoed on DescribeTasks when the request's Include
	// contains "TAGS", matching real ECS.
	Tags []Tag `json:"Tags,omitempty"`
	// HealthStatus aggregates every container's HealthStatus: UNHEALTHY if
	// any is unhealthy, else UNKNOWN if any is unknown, else HEALTHY. Empty
	// (omitted) when the task definition defines no container health checks
	// at all, matching AWS leaving the field unset in that case.
	HealthStatus string `json:"HealthStatus,omitempty"`
	// Overrides records the TaskOverride this task was launched with (RunTask
	// input), echoed back on DescribeTasks so a caller can see the
	// taskRoleArn/executionRoleArn/cpu/memory overrides that were actually
	// applied. Never mutated after launch.
	Overrides *TaskOverride `json:"Overrides,omitempty"`
}

// TaskContainer models one container within a running Task. ExitCode is a
// pointer so "has not exited yet" is distinguishable from "exited 0": a nil
// ExitCode marshals to an absent field rather than a misleading 0.
type TaskContainer struct {
	Name            string           `json:"Name"`
	ContainerArn    string           `json:"ContainerArn"`
	ContainerID     string           `json:"RuntimeId,omitempty"`
	LastStatus      string           `json:"LastStatus"`
	ExitCode        *int64           `json:"ExitCode,omitempty"`
	Reason          string           `json:"Reason,omitempty"`
	NetworkBindings []NetworkBinding `json:"NetworkBindings,omitempty"`
	// HealthStatus reflects the container's Docker HEALTHCHECK state
	// (HEALTHY/UNHEALTHY/UNKNOWN). Empty when the container defines no
	// HealthCheck.
	HealthStatus string `json:"HealthStatus,omitempty"`
}

// NetworkBinding records one published host port for a TaskContainer, the
// runtime counterpart to a TaskDefinition's PortMapping.
type NetworkBinding struct {
	ContainerPort int    `json:"ContainerPort"`
	HostPort      int    `json:"HostPort"`
	Protocol      string `json:"Protocol,omitempty"`
	BindIP        string `json:"BindIP,omitempty"`
}

// TaskOverride mirrors the AWS RunTask override shape. This is how T9
// delivers the EventBridge event payload into a task's container(s), since
// ECS targets have no payload channel of their own (see the design doc's
// "Payload delivery" section).
type TaskOverride struct {
	ContainerOverrides []ContainerOverride `json:"ContainerOverrides,omitempty"`
	// Cpu / Memory override the task definition's own values for this run
	// only, feeding resolveTaskContainerResources the same way the task
	// definition's fields do.
	Cpu    string `json:"Cpu,omitempty"`
	Memory string `json:"Memory,omitempty"`
	// TaskRoleArn / ExecutionRoleArn are stored on the task and echoed back
	// on DescribeTasks; Tarn does not vend credentials from either role.
	TaskRoleArn      string `json:"TaskRoleArn,omitempty"`
	ExecutionRoleArn string `json:"ExecutionRoleArn,omitempty"`
}

// ContainerOverride overrides fields of a single named container for one
// RunTask invocation, without mutating the underlying TaskDefinition.
type ContainerOverride struct {
	Name        string         `json:"Name"`
	Command     []string       `json:"Command,omitempty"`
	Environment []KeyValuePair `json:"Environment,omitempty"`
	// Cpu / Memory / MemoryReservation override the container definition's
	// own resource values for this run only.
	Cpu               int `json:"Cpu,omitempty"`
	Memory            int `json:"Memory,omitempty"`
	MemoryReservation int `json:"MemoryReservation,omitempty"`
	// EnvironmentFiles is store/echo only, mirroring
	// ContainerDefinition.EnvironmentFiles.
	EnvironmentFiles []EnvironmentFile `json:"EnvironmentFiles,omitempty"`
}

// --- Phase-one request/response shapes -------------------------------------

// CreateClusterInput is the input to CreateCluster.
type CreateClusterInput struct {
	ClusterName string `json:"ClusterName"`
	Tags        []Tag  `json:"Tags,omitempty"`
}

// CreateClusterOutput is the output of CreateCluster.
type CreateClusterOutput struct {
	Cluster *Cluster `json:"Cluster"`
}

// ListClustersInput is the input to ListClusters.
type ListClustersInput struct {
	MaxResults int    `json:"MaxResults,omitempty"`
	NextToken  string `json:"NextToken,omitempty"`
}

// ListClustersOutput is the output of ListClusters.
type ListClustersOutput struct {
	ClusterArns []string `json:"ClusterArns"`
	NextToken   string   `json:"NextToken,omitempty"`
}

// DescribeClustersInput is the input to DescribeClusters.
type DescribeClustersInput struct {
	Clusters []string `json:"Clusters,omitempty"`
	// Include may contain "TAGS", which is required for DescribeClusters to
	// echo back the cluster's Tags, matching real ECS.
	Include []string `json:"Include,omitempty"`
}

// DescribeClustersOutput is the output of DescribeClusters.
type DescribeClustersOutput struct {
	Clusters []Cluster `json:"Clusters"`
	Failures []Failure `json:"Failures,omitempty"`
}

// Failure mirrors the AWS shape used to report per-item errors in batched
// Describe* responses (e.g. an unknown cluster or task ARN).
type Failure struct {
	Arn    string `json:"Arn,omitempty"`
	Reason string `json:"Reason,omitempty"`
	Detail string `json:"Detail,omitempty"`
}

// RegisterTaskDefinitionInput is the input to RegisterTaskDefinition.
type RegisterTaskDefinitionInput struct {
	Family                  string                `json:"Family"`
	ContainerDefinitions    []ContainerDefinition `json:"ContainerDefinitions"`
	Cpu                     string                `json:"Cpu,omitempty"`
	Memory                  string                `json:"Memory,omitempty"`
	NetworkMode             string                `json:"NetworkMode,omitempty"`
	RequiresCompatibilities []string              `json:"RequiresCompatibilities,omitempty"`
	Tags                    []Tag                 `json:"Tags,omitempty"`

	TaskRoleArn          string                `json:"TaskRoleArn,omitempty"`
	ExecutionRoleArn     string                `json:"ExecutionRoleArn,omitempty"`
	PidMode              string                `json:"PidMode,omitempty"`
	IpcMode              string                `json:"IpcMode,omitempty"`
	RuntimePlatform      *RuntimePlatform      `json:"RuntimePlatform,omitempty"`
	EphemeralStorage     *EphemeralStorage     `json:"EphemeralStorage,omitempty"`
	Volumes              []Volume              `json:"Volumes,omitempty"`
	PlacementConstraints []PlacementConstraint `json:"PlacementConstraints,omitempty"`
}

// RegisterTaskDefinitionOutput is the output of RegisterTaskDefinition.
// Tags is populated (from the TaskDefinition record) at the top level of the
// output, never inside TaskDefinition, matching real ECS.
type RegisterTaskDefinitionOutput struct {
	TaskDefinition *TaskDefinition `json:"TaskDefinition"`
	Tags           []Tag           `json:"Tags,omitempty"`
}

// DescribeTaskDefinitionInput is the input to DescribeTaskDefinition.
type DescribeTaskDefinitionInput struct {
	TaskDefinition string `json:"TaskDefinition"`
	// Include may contain "TAGS", which is required for DescribeTaskDefinition
	// to echo back the task definition's Tags, matching real ECS.
	Include []string `json:"Include,omitempty"`
}

// DescribeTaskDefinitionOutput is the output of DescribeTaskDefinition. Tags
// is only populated when the request's Include contains "TAGS", and always
// sits at the top level of the output, never inside TaskDefinition.
type DescribeTaskDefinitionOutput struct {
	TaskDefinition *TaskDefinition `json:"TaskDefinition"`
	Tags           []Tag           `json:"Tags,omitempty"`
}

// ListTaskDefinitionsInput is the input to ListTaskDefinitions.
type ListTaskDefinitionsInput struct {
	FamilyPrefix string `json:"FamilyPrefix,omitempty"`
	MaxResults   int    `json:"MaxResults,omitempty"`
	NextToken    string `json:"NextToken,omitempty"`
	Sort         string `json:"Sort,omitempty"`
	Status       string `json:"Status,omitempty"`
}

// ListTaskDefinitionsOutput is the output of ListTaskDefinitions.
type ListTaskDefinitionsOutput struct {
	TaskDefinitionArns []string `json:"TaskDefinitionArns"`
	NextToken          string   `json:"NextToken,omitempty"`
}

// RunTaskInput is the input to RunTask. Overrides carries the per-run
// container overrides (command/environment) applied on top of the
// TaskDefinition without mutating it.
type RunTaskInput struct {
	Cluster        string        `json:"Cluster"`
	TaskDefinition string        `json:"TaskDefinition"`
	Count          int           `json:"Count,omitempty"`
	Group          string        `json:"Group,omitempty"`
	LaunchType     string        `json:"LaunchType,omitempty"`
	Overrides      *TaskOverride `json:"Overrides,omitempty"`
	Tags           []Tag         `json:"Tags,omitempty"`
	// PropagateTags is "TASK_DEFINITION" or "NONE" ("" defaults to "NONE"),
	// matching real RunTask (which, unlike CreateService, has no "SERVICE"
	// option).
	PropagateTags string `json:"PropagateTags,omitempty"`
	// EventPayload is an internal delivery hint used by the local
	// EventBridge-to-ECS adapter. It is never part of the AWS RunTask wire
	// request.
	EventPayload []byte `json:"-"`
	// CorrelationID lets an upstream dispatcher (EventBridge, Step Functions)
	// hand the runner a trace correlation ID to reuse instead of minting a
	// fresh one, so an ECS task's trace can be tied back to the trace that
	// triggered it. Never part of the AWS RunTask wire request; empty means
	// "mint a new one".
	CorrelationID string `json:"-"`
}

// RunTaskOutput is the output of RunTask.
type RunTaskOutput struct {
	Tasks    []Task    `json:"Tasks"`
	Failures []Failure `json:"Failures,omitempty"`
}

// StopTaskInput is the input to StopTask.
type StopTaskInput struct {
	Cluster string `json:"Cluster,omitempty"`
	Task    string `json:"Task"`
	Reason  string `json:"Reason,omitempty"`
}

// StopTaskOutput is the output of StopTask.
type StopTaskOutput struct {
	Task *Task `json:"Task"`
}

// ListTasksInput is the input to ListTasks.
type ListTasksInput struct {
	Cluster       string `json:"Cluster,omitempty"`
	Family        string `json:"Family,omitempty"`
	ServiceName   string `json:"ServiceName,omitempty"`
	DesiredStatus string `json:"DesiredStatus,omitempty"`
	MaxResults    int    `json:"MaxResults,omitempty"`
	NextToken     string `json:"NextToken,omitempty"`
}

// ListTasksOutput is the output of ListTasks.
type ListTasksOutput struct {
	TaskArns  []string `json:"TaskArns"`
	NextToken string   `json:"NextToken,omitempty"`
}

// DescribeTasksInput is the input to DescribeTasks.
type DescribeTasksInput struct {
	Cluster string   `json:"Cluster,omitempty"`
	Tasks   []string `json:"Tasks"`
	// Include may contain "TAGS", which is required for DescribeTasks to
	// echo back each task's Tags, matching real ECS.
	Include []string `json:"Include,omitempty"`
}

// DescribeTasksOutput is the output of DescribeTasks.
type DescribeTasksOutput struct {
	Tasks    []Task    `json:"Tasks"`
	Failures []Failure `json:"Failures,omitempty"`
}

// --- Phase-two (Services) request/response shapes ---------------------------

// CreateServiceInput is the input to CreateService.
type CreateServiceInput struct {
	Cluster                 string                   `json:"Cluster,omitempty"`
	ServiceName             string                   `json:"ServiceName"`
	TaskDefinition          string                   `json:"TaskDefinition"`
	DesiredCount            int                      `json:"DesiredCount"`
	LaunchType              string                   `json:"LaunchType,omitempty"`
	SchedulingStrategy      string                   `json:"SchedulingStrategy,omitempty"`
	NetworkConfiguration    *NetworkConfiguration    `json:"NetworkConfiguration,omitempty"`
	PlatformVersion         string                   `json:"PlatformVersion,omitempty"`
	DeploymentConfiguration *DeploymentConfiguration `json:"DeploymentConfiguration,omitempty"`
	EnableECSManagedTags    bool                     `json:"EnableECSManagedTags,omitempty"`
	PropagateTags           string                   `json:"PropagateTags,omitempty"`
	Tags                    []Tag                    `json:"Tags,omitempty"`
}

// CreateServiceOutput is the output of CreateService.
type CreateServiceOutput struct {
	Service *ECSService `json:"Service"`
}

// UpdateServiceInput is the input to UpdateService.
type UpdateServiceInput struct {
	Cluster                 string                   `json:"Cluster,omitempty"`
	Service                 string                   `json:"Service"`
	TaskDefinition          string                   `json:"TaskDefinition,omitempty"`
	DesiredCount            *int                     `json:"DesiredCount,omitempty"`
	NetworkConfiguration    *NetworkConfiguration    `json:"NetworkConfiguration,omitempty"`
	PlatformVersion         string                   `json:"PlatformVersion,omitempty"`
	DeploymentConfiguration *DeploymentConfiguration `json:"DeploymentConfiguration,omitempty"`
}

// UpdateServiceOutput is the output of UpdateService.
type UpdateServiceOutput struct {
	Service *ECSService `json:"Service"`
}

// DeleteServiceInput is the input to DeleteService.
type DeleteServiceInput struct {
	Cluster string `json:"Cluster,omitempty"`
	Service string `json:"Service"`
	Force   bool   `json:"Force,omitempty"`
}

// DeleteServiceOutput is the output of DeleteService.
type DeleteServiceOutput struct {
	Service *ECSService `json:"Service"`
}

// ListServicesInput is the input to ListServices.
type ListServicesInput struct {
	Cluster    string `json:"Cluster,omitempty"`
	MaxResults int    `json:"MaxResults,omitempty"`
	NextToken  string `json:"NextToken,omitempty"`
}

// ListServicesOutput is the output of ListServices.
type ListServicesOutput struct {
	ServiceArns []string `json:"ServiceArns"`
	NextToken   string   `json:"NextToken,omitempty"`
}

// DescribeServicesInput is the input to DescribeServices.
type DescribeServicesInput struct {
	Cluster  string   `json:"Cluster,omitempty"`
	Services []string `json:"Services"`
	// Include may contain "TAGS", which is required for DescribeServices to
	// echo back each service's Tags, matching real ECS.
	Include []string `json:"Include,omitempty"`
}

// DescribeServicesOutput is the output of DescribeServices.
type DescribeServicesOutput struct {
	Services []ECSService `json:"Services"`
	Failures []Failure    `json:"Failures,omitempty"`
}

// --- Tagging request/response shapes ----------------------------------------

// TagResourceInput is the input to TagResource. ResourceArn identifies a
// cluster, task definition, service, or task; see
// internal/ecs/service.go's taggableLocked for ARN resolution.
type TagResourceInput struct {
	ResourceArn string `json:"ResourceArn"`
	Tags        []Tag  `json:"Tags"`
}

// TagResourceOutput is the (empty) output of TagResource.
type TagResourceOutput struct{}

// UntagResourceInput is the input to UntagResource.
type UntagResourceInput struct {
	ResourceArn string   `json:"ResourceArn"`
	TagKeys     []string `json:"TagKeys"`
}

// UntagResourceOutput is the (empty) output of UntagResource.
type UntagResourceOutput struct{}

// ListTagsForResourceInput is the input to ListTagsForResource.
type ListTagsForResourceInput struct {
	ResourceArn string `json:"ResourceArn"`
}

// ListTagsForResourceOutput is the output of ListTagsForResource.
type ListTagsForResourceOutput struct {
	Tags []Tag `json:"Tags"`
}

// TaskRunner defines the ECS behaviour required by callers (EventBridge, Step
// Functions), mirroring LambdaInterface at
// internal/eventbridge/service.go:33. Consumers depend on this interface,
// never on a concrete runner type.
type TaskRunner interface {
	RunTask(ctx context.Context, in *RunTaskInput) (*RunTaskOutput, error)
	StopTask(ctx context.Context, cluster, taskArn, reason string) error
}
