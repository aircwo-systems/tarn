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
}

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
}

// ContainerOverride overrides fields of a single named container for one
// RunTask invocation, without mutating the underlying TaskDefinition.
type ContainerOverride struct {
	Name        string         `json:"Name"`
	Command     []string       `json:"Command,omitempty"`
	Environment []KeyValuePair `json:"Environment,omitempty"`
}

// --- Phase-one request/response shapes -------------------------------------

// CreateClusterInput is the input to CreateCluster.
type CreateClusterInput struct {
	ClusterName string `json:"ClusterName"`
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
}

// RegisterTaskDefinitionOutput is the output of RegisterTaskDefinition.
type RegisterTaskDefinitionOutput struct {
	TaskDefinition *TaskDefinition `json:"TaskDefinition"`
}

// DescribeTaskDefinitionInput is the input to DescribeTaskDefinition.
type DescribeTaskDefinitionInput struct {
	TaskDefinition string `json:"TaskDefinition"`
}

// DescribeTaskDefinitionOutput is the output of DescribeTaskDefinition.
type DescribeTaskDefinitionOutput struct {
	TaskDefinition *TaskDefinition `json:"TaskDefinition"`
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
}

// DescribeServicesOutput is the output of DescribeServices.
type DescribeServicesOutput struct {
	Services []ECSService `json:"Services"`
	Failures []Failure    `json:"Failures,omitempty"`
}

// TaskRunner defines the ECS behaviour required by callers (EventBridge, Step
// Functions), mirroring LambdaInterface at
// internal/eventbridge/service.go:33. Consumers depend on this interface,
// never on a concrete runner type.
type TaskRunner interface {
	RunTask(ctx context.Context, in *RunTaskInput) (*RunTaskOutput, error)
	StopTask(ctx context.Context, cluster, taskArn, reason string) error
}
