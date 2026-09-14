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
	ClusterName                       string `json:"clusterName"`
	ClusterArn                        string `json:"clusterArn"`
	Status                            string `json:"status"`
	RegisteredContainerInstancesCount int    `json:"registeredContainerInstancesCount"`
	RunningTasksCount                 int    `json:"runningTasksCount"`
	PendingTasksCount                 int    `json:"pendingTasksCount"`
	ActiveServicesCount               int    `json:"activeServicesCount"`
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
}

type wireContainerDefinition struct {
	Name              string                `json:"name"`
	Image             string                `json:"image"`
	Command           []string              `json:"command,omitempty"`
	EntryPoint        []string              `json:"entryPoint,omitempty"`
	Environment       []wireKeyValuePair    `json:"environment,omitempty"`
	PortMappings      []wirePortMapping     `json:"portMappings,omitempty"`
	LogConfiguration  *wireLogConfiguration `json:"logConfiguration,omitempty"`
	Essential         *bool                 `json:"essential,omitempty"`
	Cpu               int                   `json:"cpu,omitempty"`
	Memory            int                   `json:"memory,omitempty"`
	MemoryReservation int                   `json:"memoryReservation,omitempty"`
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
	CreatedAt         *float64            `json:"createdAt,omitempty"`
}

type wireTaskContainer struct {
	Name            string               `json:"name"`
	ContainerArn    string               `json:"containerArn"`
	ContainerID     string               `json:"runtimeId,omitempty"`
	LastStatus      string               `json:"lastStatus"`
	ExitCode        *int64               `json:"exitCode,omitempty"`
	Reason          string               `json:"reason,omitempty"`
	NetworkBindings []wireNetworkBinding `json:"networkBindings,omitempty"`
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

type wireRegisterTaskDefinitionOutput struct {
	TaskDefinition *wireTaskDefinition `json:"taskDefinition"`
}

type wireDescribeTaskDefinitionOutput struct {
	TaskDefinition *wireTaskDefinition `json:"taskDefinition"`
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
		return wireRegisterTaskDefinitionOutput{TaskDefinition: wireTaskDefinitionPtr(value.TaskDefinition)}
	case *types.RegisterTaskDefinitionOutput:
		if value == nil {
			return (*wireRegisterTaskDefinitionOutput)(nil)
		}
		return wireRegisterTaskDefinitionOutput{TaskDefinition: wireTaskDefinitionPtr(value.TaskDefinition)}
	case types.DescribeTaskDefinitionOutput:
		return wireDescribeTaskDefinitionOutput{TaskDefinition: wireTaskDefinitionPtr(value.TaskDefinition)}
	case *types.DescribeTaskDefinitionOutput:
		if value == nil {
			return (*wireDescribeTaskDefinitionOutput)(nil)
		}
		return wireDescribeTaskDefinitionOutput{TaskDefinition: wireTaskDefinitionPtr(value.TaskDefinition)}
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
	}
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
	}
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
	}
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
		CreatedAt:         epochSecondsOrNil(value.CreatedAt),
	}
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
