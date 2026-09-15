// Package ecs implements the ECS control plane: clusters, task definition
// registration and revisioning, task records, and service records. It has no
// Docker involvement whatsoever — actually running containers is owned by the
// task runner behind types.TaskRunner (see docs/design/ecs-support.md, T7).
package ecs

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const defaultClusterName = "default"

var resourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

const maxTaskDefinitionListResults = 100

// ServiceError is returned for AWS-compatible API failures, following the
// per-service pattern in internal/eventbridge/service.go.
type ServiceError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *ServiceError) Error() string { return e.Message }

func (e *ServiceError) StatusCode() int {
	if e == nil || e.HTTPStatus == 0 {
		return 400
	}
	return e.HTTPStatus
}

func clientError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "ClientException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func invalidParameterError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "InvalidParameterException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func clusterNotFoundError(ref string) *ServiceError {
	return &ServiceError{
		Code:       "ClusterNotFoundException",
		Message:    fmt.Sprintf("Cluster not found: %s", ref),
		HTTPStatus: 400,
	}
}

// serviceNotActiveError mirrors AWS's ServiceNotActiveException, which real
// ECS returns for a mutation (UpdateService, and DeleteService a second time)
// against a service that is no longer ACTIVE. AWS's own message points the
// caller at re-creating the service, so this does too.
func serviceNotActiveError(name string) *ServiceError {
	return &ServiceError{
		Code:       "ServiceNotActiveException",
		Message:    fmt.Sprintf("The service %s is not active. You cannot delete an inactive service. If you have previously deleted this service, you can re-create it with CreateService.", name),
		HTTPStatus: 400,
	}
}

// Service manages the ECS control plane: clusters, task definitions,
// services, and task records. It never touches Docker; RunTask/StopTask
// themselves are implemented by the task runner (T7), which uses the
// exported record-management methods here to create and update Task rows.
type Service struct {
	cfg   *config.Config
	store *Store

	// taskDrainer is supplied by the runner. Keeping it as a callback avoids a
	// control-plane -> Docker dependency while still letting Force delete wait
	// for the actual containers to stop before removing the service record.
	taskDrainer func(context.Context, *types.Cluster, string) error

	// mu serialises every read-modify-write against the store: NextRevision
	// followed by SaveTaskDefinition, or GetTask followed by SaveTask, is
	// otherwise two separate store lock acquisitions and a lost update under
	// concurrent callers (T7's reconcile loop and log/wait goroutines all
	// mutate Task records). Lock ordering is always Service.mu -> store's
	// internal mutex, never the reverse, so this cannot deadlock.
	mu sync.Mutex
}

func NewService(cfg *config.Config, store *Store) *Service {
	if store == nil {
		store = NewStore(cfg)
	}
	return &Service{cfg: cfg, store: store}
}

// SetTaskDrainer connects the service control plane to the account's ECS
// runner. It is optional for tests and for callers that only use the control
// plane; Force delete refuses to remove active tasks when it is absent.
func (s *Service) SetTaskDrainer(drainer func(context.Context, *types.Cluster, string) error) {
	s.mu.Lock()
	s.taskDrainer = drainer
	s.mu.Unlock()
}

func (s *Service) Init() error {
	return s.store.Init()
}

// --- Clusters ----------------------------------------------------------------

func (s *Service) CreateCluster(in *types.CreateClusterInput) (*types.CreateClusterOutput, error) {
	name := defaultClusterName
	if in != nil && strings.TrimSpace(in.ClusterName) != "" {
		name = strings.TrimSpace(in.ClusterName)
	}
	if !resourceNamePattern.MatchString(name) {
		return nil, invalidParameterError("Invalid cluster name: %s", name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var tags []types.Tag
	if in != nil {
		if err := validateTags(in.Tags); err != nil {
			return nil, err
		}
		tags = cloneTags(in.Tags)
	}

	if existing, err := s.store.GetCluster(name); err == nil {
		s.hydrateClusterCounts(existing)
		return &types.CreateClusterOutput{Cluster: existing}, nil
	}

	cluster := &types.Cluster{
		ClusterName: name,
		ClusterArn:  clusterARN(s.cfg, name),
		Status:      types.ClusterStatusActive,
		Tags:        tags,
	}
	if err := s.store.SaveCluster(cluster); err != nil {
		return nil, err
	}
	return &types.CreateClusterOutput{Cluster: cluster}, nil
}

func (s *Service) ListClusters(in *types.ListClustersInput) (*types.ListClustersOutput, error) {
	// Ensure "default" shows up even if nothing has explicitly created it yet,
	// matching AWS's implicit-default-cluster behaviour.
	if _, err := s.ensureDefaultCluster(); err != nil {
		return nil, err
	}
	clusters := s.store.ListClusters()
	arns := make([]string, 0, len(clusters))
	for _, c := range clusters {
		arns = append(arns, c.ClusterArn)
	}
	return &types.ListClustersOutput{ClusterArns: arns}, nil
}

func (s *Service) DescribeClusters(in *types.DescribeClustersInput) (*types.DescribeClustersOutput, error) {
	var refs []string
	var include []string
	if in != nil {
		refs = in.Clusters
		include = in.Include
	}
	if len(refs) == 0 {
		refs = []string{defaultClusterName}
	}
	withTags := includesTag(include)

	out := &types.DescribeClustersOutput{}
	for _, ref := range refs {
		name := clusterNameFromRef(ref)
		var (
			cluster *types.Cluster
			err     error
		)
		if name == defaultClusterName {
			cluster, err = s.ensureDefaultCluster()
		} else {
			cluster, err = s.store.GetCluster(name)
		}
		if err != nil {
			out.Failures = append(out.Failures, types.Failure{
				Arn:    ref,
				Reason: "MISSING",
				Detail: fmt.Sprintf("cluster %s does not exist", name),
			})
			continue
		}
		s.hydrateClusterCounts(cluster)
		clusterCopy := *cluster
		if !withTags {
			clusterCopy.Tags = nil
		}
		out.Clusters = append(out.Clusters, clusterCopy)
	}
	return out, nil
}

// DeleteCluster deletes a cluster by name or ARN. There is no
// DeleteClusterInput/Output pair in pkg/types/ecs.go (T1 didn't define one,
// unlike every other action here), so this takes and returns plain values.
func (s *Service) DeleteCluster(ref string) (*types.Cluster, error) {
	name := clusterNameFromRef(ref)
	if name == "" {
		return nil, invalidParameterError("Cluster is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, err := s.store.GetCluster(name)
	if err != nil {
		return nil, clusterNotFoundError(ref)
	}
	s.hydrateClusterCounts(cluster)
	if cluster.RunningTasksCount > 0 || cluster.PendingTasksCount > 0 {
		return nil, clientError("The Cluster cannot be deleted while Tasks are active.")
	}
	if cluster.ActiveServicesCount > 0 {
		return nil, clientError("The Cluster cannot be deleted while Services are active.")
	}
	if err := s.store.DeleteCluster(name); err != nil {
		return nil, clusterNotFoundError(ref)
	}
	cluster.Status = types.ClusterStatusInactive
	return cluster, nil
}

// ResolveCluster resolves a cluster reference (bare name or ARN, "" meaning
// "default") to its record, creating the implicit default cluster on first
// use the way AWS behaves.
func (s *Service) ResolveCluster(ref string) (*types.Cluster, error) {
	name := clusterNameFromRef(ref)
	if name == "" {
		name = defaultClusterName
	}
	if name == defaultClusterName {
		return s.ensureDefaultCluster()
	}
	cluster, err := s.store.GetCluster(name)
	if err != nil {
		return nil, clusterNotFoundError(ref)
	}
	return cluster, nil
}

func (s *Service) ensureDefaultCluster() (*types.Cluster, error) {
	if cluster, err := s.store.GetCluster(defaultClusterName); err == nil {
		return cluster, nil
	}
	cluster := &types.Cluster{
		ClusterName: defaultClusterName,
		ClusterArn:  clusterARN(s.cfg, defaultClusterName),
		Status:      types.ClusterStatusActive,
	}
	if err := s.store.SaveCluster(cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}

// hydrateClusterCounts recomputes the derived task/service counts on cluster
// from current store state, rather than tracking them as persisted state that
// could drift from the tasks/services actually recorded.
func (s *Service) hydrateClusterCounts(cluster *types.Cluster) {
	cluster.RunningTasksCount = 0
	cluster.PendingTasksCount = 0
	cluster.ActiveServicesCount = 0
	cluster.RegisteredContainerInstancesCount = 0

	for _, t := range s.store.ListTasks(cluster.ClusterArn) {
		switch t.LastStatus {
		case types.TaskStatusRunning:
			cluster.RunningTasksCount++
		case types.TaskStatusProvisioning, types.TaskStatusPending:
			cluster.PendingTasksCount++
		}
	}
	for _, svc := range s.store.ListServices(cluster.ClusterArn) {
		if svc.Status == types.ServiceStatusActive {
			cluster.ActiveServicesCount++
		}
	}
}

// --- Task definitions ----------------------------------------------------------

func (s *Service) RegisterTaskDefinition(in *types.RegisterTaskDefinitionInput) (*types.RegisterTaskDefinitionOutput, error) {
	if in == nil {
		return nil, invalidParameterError("RegisterTaskDefinition input is required")
	}
	family := strings.TrimSpace(in.Family)
	if family == "" {
		return nil, invalidParameterError("Family is required")
	}
	if !resourceNamePattern.MatchString(family) {
		return nil, invalidParameterError("Invalid family name: %s", family)
	}
	if len(in.ContainerDefinitions) == 0 {
		return nil, invalidParameterError("ContainerDefinitions must contain at least one entry")
	}
	names := make(map[string]bool, len(in.ContainerDefinitions))
	for _, cd := range in.ContainerDefinitions {
		if strings.TrimSpace(cd.Name) == "" {
			return nil, invalidParameterError("Container definition Name is required")
		}
		if strings.TrimSpace(cd.Image) == "" {
			return nil, invalidParameterError("Container definition %s: Image is required", cd.Name)
		}
		if names[cd.Name] {
			return nil, invalidParameterError("Container definition names must be unique, duplicate: %s", cd.Name)
		}
		names[cd.Name] = true
	}
	if err := validateDependsOn(in.ContainerDefinitions, names); err != nil {
		return nil, err
	}
	if err := validateTags(in.Tags); err != nil {
		return nil, err
	}

	networkMode, err := normalizeNetworkMode(in.NetworkMode)
	if err != nil {
		return nil, invalidParameterError("%v", err)
	}
	if err := validateNetworkMode(networkMode, in.ContainerDefinitions); err != nil {
		return nil, invalidParameterError("%v", err)
	}
	resourceDefinition := &types.TaskDefinition{
		Cpu:                  in.Cpu,
		Memory:               in.Memory,
		ContainerDefinitions: in.ContainerDefinitions,
	}
	if _, err := resolveTaskContainerResources(resourceDefinition); err != nil {
		return nil, invalidParameterError("%v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	revision := s.store.NextRevision(family)
	containerDefs := make([]types.ContainerDefinition, len(in.ContainerDefinitions))
	copy(containerDefs, in.ContainerDefinitions)

	td := &types.TaskDefinition{
		TaskDefinitionArn:       taskDefinitionARN(s.cfg, family, revision),
		Family:                  family,
		Revision:                revision,
		ContainerDefinitions:    containerDefs,
		Cpu:                     in.Cpu,
		Memory:                  in.Memory,
		NetworkMode:             networkMode,
		Status:                  types.TaskDefinitionStatusActive,
		RequiresCompatibilities: append([]string(nil), in.RequiresCompatibilities...),
		RegisteredAt:            time.Now().UTC(),
		TaskRoleArn:             in.TaskRoleArn,
		ExecutionRoleArn:        in.ExecutionRoleArn,
		PidMode:                 in.PidMode,
		IpcMode:                 in.IpcMode,
		RuntimePlatform:         in.RuntimePlatform,
		EphemeralStorage:        in.EphemeralStorage,
		Volumes:                 append([]types.Volume(nil), in.Volumes...),
		PlacementConstraints:    append([]types.PlacementConstraint(nil), in.PlacementConstraints...),
		Tags:                    cloneTags(in.Tags),
	}
	if err := s.store.SaveTaskDefinition(td); err != nil {
		return nil, err
	}
	return &types.RegisterTaskDefinitionOutput{TaskDefinition: td, Tags: cloneTags(td.Tags)}, nil
}

// DescribeTaskDefinition resolves ref to a TaskDefinition record. Tags is
// only populated when include contains "TAGS", matching real ECS (tags sit
// at the top level of the output, never inside TaskDefinition itself).
func (s *Service) DescribeTaskDefinition(ref string, include []string) (*types.DescribeTaskDefinitionOutput, error) {
	td, err := s.resolveTaskDefinition(ref)
	if err != nil {
		return nil, err
	}
	out := &types.DescribeTaskDefinitionOutput{TaskDefinition: td}
	if includesTag(include) {
		out.Tags = cloneTags(td.Tags)
	}
	return out, nil
}

// ListTaskDefinitions returns task-definition ARNs in family/revision order.
// AWS defaults this operation to ACTIVE definitions and ascending order.
func (s *Service) ListTaskDefinitions(in *types.ListTaskDefinitionsInput) (*types.ListTaskDefinitionsOutput, error) {
	familyPrefix := ""
	maxResults := maxTaskDefinitionListResults
	nextToken := ""
	sortOrder := types.TaskDefinitionSortAscending
	status := types.TaskDefinitionStatusActive
	if in != nil {
		familyPrefix = strings.TrimSpace(in.FamilyPrefix)
		if in.MaxResults != 0 {
			maxResults = in.MaxResults
		}
		nextToken = in.NextToken
		if strings.TrimSpace(in.Sort) != "" {
			sortOrder = strings.ToUpper(strings.TrimSpace(in.Sort))
		}
		if strings.TrimSpace(in.Status) != "" {
			status = strings.ToUpper(strings.TrimSpace(in.Status))
		}
	}

	if maxResults < 1 || maxResults > maxTaskDefinitionListResults {
		return nil, invalidParameterError("MaxResults must be between 1 and %d", maxTaskDefinitionListResults)
	}
	if sortOrder != types.TaskDefinitionSortAscending && sortOrder != types.TaskDefinitionSortDescending {
		return nil, invalidParameterError("Sort must be ASC or DESC")
	}
	switch status {
	case types.TaskDefinitionStatusActive, types.TaskDefinitionStatusInactive, types.TaskDefinitionStatusDeleteInProgress:
	default:
		return nil, invalidParameterError("Status must be ACTIVE, INACTIVE, or DELETE_IN_PROGRESS")
	}

	offset, err := parseTaskDefinitionNextToken(nextToken)
	if err != nil {
		return nil, invalidParameterError("NextToken is invalid")
	}

	families := s.store.ListTaskDefinitionFamilies()
	arns := make([]string, 0)
	appendFamily := func(family string, revisions []*types.TaskDefinition) {
		if familyPrefix != "" && !strings.HasPrefix(family, familyPrefix) {
			return
		}
		for _, td := range revisions {
			if td.Status == status {
				arns = append(arns, td.TaskDefinitionArn)
			}
		}
	}
	if sortOrder == types.TaskDefinitionSortDescending {
		for i := len(families) - 1; i >= 0; i-- {
			family := families[i]
			revisions := s.store.ListTaskDefinitionRevisions(family)
			for left, right := 0, len(revisions)-1; left < right; left, right = left+1, right-1 {
				revisions[left], revisions[right] = revisions[right], revisions[left]
			}
			appendFamily(family, revisions)
		}
	} else {
		for _, family := range families {
			appendFamily(family, s.store.ListTaskDefinitionRevisions(family))
		}
	}

	if offset >= len(arns) {
		return &types.ListTaskDefinitionsOutput{TaskDefinitionArns: []string{}}, nil
	}
	end := offset + maxResults
	if end > len(arns) {
		end = len(arns)
	}
	next := ""
	if end < len(arns) {
		next = strconv.Itoa(end)
	}
	return &types.ListTaskDefinitionsOutput{
		TaskDefinitionArns: arns[offset:end],
		NextToken:          next,
	}, nil
}

func parseTaskDefinitionNextToken(token string) (int, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(token)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid token")
	}
	return offset, nil
}

// resolveTaskDefinition resolves a bare family (latest ACTIVE revision),
// "family:revision", or full ARN to a TaskDefinition record.
func (s *Service) resolveTaskDefinition(ref string) (*types.TaskDefinition, error) {
	family, revision, hasRevision, err := parseTaskDefinitionRef(ref)
	if err != nil {
		return nil, clientError("Unable to describe task definition: %v", err)
	}
	if hasRevision {
		td, err := s.store.GetTaskDefinition(family, revision)
		if err != nil {
			return nil, clientError("Unable to describe task definition: %s:%d does not exist.", family, revision)
		}
		return td, nil
	}
	td, err := s.store.LatestActiveTaskDefinition(family)
	if err != nil {
		return nil, clientError("Unable to describe task definition: family %s has no ACTIVE revision.", family)
	}
	return td, nil
}

// DeregisterTaskDefinition marks one specific revision INACTIVE. AWS requires
// an explicit revision here (a bare family name is not accepted), since
// deregistering "the latest" would be ambiguous once more get registered.
func (s *Service) DeregisterTaskDefinition(ref string) (*types.DescribeTaskDefinitionOutput, error) {
	family, revision, hasRevision, err := parseTaskDefinitionRef(ref)
	if err != nil {
		return nil, clientError("Unable to deregister task definition: %v", err)
	}
	if !hasRevision {
		return nil, invalidParameterError("TaskDefinition must include a revision, e.g. %s:1", family)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	td, err := s.store.GetTaskDefinition(family, revision)
	if err != nil {
		return nil, clientError("Unable to deregister task definition: %s:%d does not exist.", family, revision)
	}
	td.Status = types.TaskDefinitionStatusInactive
	if err := s.store.SaveTaskDefinition(td); err != nil {
		return nil, err
	}
	return &types.DescribeTaskDefinitionOutput{TaskDefinition: td}, nil
}

// --- Task records (created and driven by T7; T4 owns the record only) --------

// NewTaskRecord creates a task record in PROVISIONING for one instantiation
// of taskDef within cluster. The caller (T7's runner) drives the container
// and calls the Set* methods below as the container's real lifecycle
// progresses.
func (s *Service) NewTaskRecord(cluster *types.Cluster, taskDef *types.TaskDefinition, overrides *types.TaskOverride, launchType, group string) (*types.Task, error) {
	if cluster == nil {
		return nil, invalidParameterError("cluster is required")
	}
	if taskDef == nil {
		return nil, invalidParameterError("task definition is required")
	}
	if launchType == "" {
		launchType = types.LaunchTypeFargate
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	taskID := strings.ReplaceAll(uuid.NewString(), "-", "")
	arn := taskARN(s.cfg, cluster.ClusterName, taskID)

	containers := make([]types.TaskContainer, len(taskDef.ContainerDefinitions))
	for i, cd := range taskDef.ContainerDefinitions {
		containerID := strings.ReplaceAll(uuid.NewString(), "-", "")
		containers[i] = types.TaskContainer{
			Name:         cd.Name,
			ContainerArn: containerARN(s.cfg, cluster.ClusterName, taskID, containerID),
			LastStatus:   types.TaskStatusProvisioning,
		}
	}

	task := &types.Task{
		TaskArn:           arn,
		ClusterArn:        cluster.ClusterArn,
		TaskDefinitionArn: taskDef.TaskDefinitionArn,
		Group:             group,
		LaunchType:        launchType,
		LastStatus:        types.TaskStatusProvisioning,
		DesiredStatus:     types.TaskDesiredStatusRunning,
		Containers:        containers,
		CreatedAt:         time.Now().UTC(),
	}
	// Overrides is stored so DescribeTasks can echo back taskRoleArn /
	// executionRoleArn / cpu / memory overrides (real ECS does the same);
	// the runner still consumes overrides directly off the RunTask input to
	// build the container rather than reading them back off the task.
	task.Overrides = overrides

	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// SetTaskStatus transitions a task's LastStatus, recording StartedAt/StoppedAt
// as appropriate. It is a pure record update — it does not touch Docker.
func (s *Service) SetTaskStatus(taskArn, status string) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	task.LastStatus = status
	now := time.Now().UTC()
	if status == types.TaskStatusRunning && task.StartedAt == nil {
		task.StartedAt = &now
	}
	if status == types.TaskStatusStopped && task.StoppedAt == nil {
		task.StoppedAt = &now
	}
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// SetTaskRunningIfDesired transitions a task to RUNNING only while its desired
// status is still RUNNING. Launch and StopTask can overlap, so doing the check
// and write under the service lock prevents a stop request from being erased by
// the final step of a launch.
func (s *Service) SetTaskRunningIfDesired(taskArn string) (*types.Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, false, err
	}
	if task.DesiredStatus == types.TaskDesiredStatusStopped {
		return task, false, nil
	}
	task.LastStatus = types.TaskStatusRunning
	now := time.Now().UTC()
	if task.StartedAt == nil {
		task.StartedAt = &now
	}
	if err := s.store.SaveTask(task); err != nil {
		return nil, false, err
	}
	return task, true, nil
}

// SetTaskDesiredStatus records the desired status a caller (or the
// reconcile loop) wants the task to reach.
func (s *Service) SetTaskDesiredStatus(taskArn, status string) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	task.DesiredStatus = status
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// SetTaskCorrelationID records the trace correlation ID assigned to a task
// at launch (see types.Task.CorrelationID / types.RunTaskInput.CorrelationID),
// so DescribeTasks/overview callers can tie a task back to its trace. Not
// part of the AWS wire shape — internal/api/ecs/wire.go keeps it off
// wireTask deliberately.
func (s *Service) SetTaskCorrelationID(taskArn, correlationID string) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	task.CorrelationID = correlationID
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// StopTaskRecord marks a task's desired status STOPPED and records the
// reason, for use by the runner's StopTask before it stops the container.
func (s *Service) StopTaskRecord(taskArn, reason string) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	task.DesiredStatus = types.TaskDesiredStatusStopped
	task.StoppedReason = reason
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// SetContainerStatus updates one container's LastStatus within a task.
func (s *Service) SetContainerStatus(taskArn, containerName, status string) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	if !s.mutateContainer(task, containerName, func(c *types.TaskContainer) {
		c.LastStatus = status
	}) {
		return nil, clientError("container %s not found on task %s", containerName, taskArn)
	}
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// SetContainerID records the Docker runtime ID for one task container. The
// ID is persisted with the task so a new runner can reconnect its in-memory
// lifecycle bookkeeping after a Tarn restart.
func (s *Service) SetContainerID(taskArn, containerName, containerID string) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	if !s.mutateContainer(task, containerName, func(c *types.TaskContainer) {
		c.ContainerID = containerID
	}) {
		return nil, clientError("container %s not found on task %s", containerName, taskArn)
	}
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// SetContainerNetworkBindings records the published host ports for one
// container, the runtime counterpart to the task definition's PortMappings.
func (s *Service) SetContainerNetworkBindings(taskArn, containerName string, bindings []types.NetworkBinding) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	if !s.mutateContainer(task, containerName, func(c *types.TaskContainer) {
		c.NetworkBindings = append([]types.NetworkBinding(nil), bindings...)
	}) {
		return nil, clientError("container %s not found on task %s", containerName, taskArn)
	}
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// SetContainerExitCode records a container's exit code and reason. exitCode
// is a pointer straight through to types.TaskContainer.ExitCode: nil must
// never be collapsed to 0, since nil means "has not exited".
func (s *Service) SetContainerExitCode(taskArn, containerName string, exitCode *int64, reason string) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	if !s.mutateContainer(task, containerName, func(c *types.TaskContainer) {
		if exitCode != nil {
			v := *exitCode
			c.ExitCode = &v
		} else {
			c.ExitCode = nil
		}
		c.Reason = reason
	}) {
		return nil, clientError("container %s not found on task %s", containerName, taskArn)
	}
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// SetContainerHealthStatus records one container's Docker HEALTHCHECK result
// and recomputes the task's aggregate HealthStatus (UNHEALTHY if any
// container is unhealthy, else UNKNOWN if any is unknown, else HEALTHY —
// counting only containers that report a health check at all).
func (s *Service) SetContainerHealthStatus(taskArn, containerName, status string) (*types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	if !s.mutateContainer(task, containerName, func(c *types.TaskContainer) {
		c.HealthStatus = status
	}) {
		return nil, clientError("container %s not found on task %s", containerName, taskArn)
	}
	task.HealthStatus = aggregateTaskHealthStatus(task.Containers)
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// aggregateTaskHealthStatus rolls up every container's HealthStatus into the
// task-level value DescribeTasks reports, mirroring AWS: UNHEALTHY wins over
// UNKNOWN wins over HEALTHY. Containers with no health check (empty
// HealthStatus) are not counted; the result is "" when none report one.
func aggregateTaskHealthStatus(containers []types.TaskContainer) string {
	seen := false
	unhealthy := false
	unknown := false
	for _, c := range containers {
		switch c.HealthStatus {
		case "":
			continue
		case types.HealthStatusUnhealthy:
			seen = true
			unhealthy = true
		case types.HealthStatusUnknown:
			seen = true
			unknown = true
		case types.HealthStatusHealthy:
			seen = true
		}
	}
	if !seen {
		return ""
	}
	if unhealthy {
		return types.HealthStatusUnhealthy
	}
	if unknown {
		return types.HealthStatusUnknown
	}
	return types.HealthStatusHealthy
}

func (s *Service) mutateContainer(task *types.Task, containerName string, fn func(*types.TaskContainer)) bool {
	for i := range task.Containers {
		if task.Containers[i].Name == containerName {
			fn(&task.Containers[i])
			return true
		}
	}
	return false
}

// GetTask resolves a bare task ID or full ARN to its record.
func (s *Service) GetTask(taskRef string) (*types.Task, error) {
	return s.getTaskByRef(taskRef)
}

func (s *Service) getTaskByRef(ref string) (*types.Task, error) {
	if task, err := s.store.GetTask(ref); err == nil {
		return task, nil
	}
	id := taskIDFromRef(ref)
	for _, task := range s.store.ListTasks("") {
		if taskIDFromRef(task.TaskArn) == id {
			return task, nil
		}
	}
	return nil, clientError("task %s not found", ref)
}

// serviceGroup is the Task.Group convention a task launched on behalf of an
// ECSService is recorded under, matching AWS's own "service:<name>" Group
// value. T7's reconcile loop must set this Group when it calls NewTaskRecord
// for a service-owned task; ListTasks' ServiceName filter depends on it,
// since Task carries no direct service-ARN field.
func serviceGroup(serviceName string) string { return "service:" + serviceName }

func (s *Service) ListTasks(in *types.ListTasksInput) (*types.ListTasksOutput, error) {
	clusterRef := ""
	if in != nil {
		clusterRef = in.Cluster
	}
	cluster, err := s.ResolveCluster(clusterRef)
	if err != nil {
		return nil, err
	}

	tasks := s.store.ListTasks(cluster.ClusterArn)
	out := &types.ListTasksOutput{}
	for _, t := range tasks {
		if in != nil {
			if in.Family != "" {
				family, _, _, parseErr := parseTaskDefinitionRef(t.TaskDefinitionArn)
				if parseErr != nil || family != in.Family {
					continue
				}
			}
			if in.ServiceName != "" && t.Group != serviceGroup(in.ServiceName) {
				continue
			}
			if in.DesiredStatus != "" && !strings.EqualFold(t.DesiredStatus, in.DesiredStatus) {
				continue
			}
		}
		out.TaskArns = append(out.TaskArns, t.TaskArn)
	}
	return out, nil
}

func (s *Service) DescribeTasks(in *types.DescribeTasksInput) (*types.DescribeTasksOutput, error) {
	if in == nil || len(in.Tasks) == 0 {
		return nil, invalidParameterError("Tasks is required")
	}
	cluster, err := s.ResolveCluster(in.Cluster)
	if err != nil {
		return nil, err
	}

	withTags := includesTag(in.Include)
	out := &types.DescribeTasksOutput{}
	for _, ref := range in.Tasks {
		task, err := s.getTaskByRef(ref)
		if err != nil || task.ClusterArn != cluster.ClusterArn {
			out.Failures = append(out.Failures, types.Failure{
				Arn:    ref,
				Reason: "MISSING",
				Detail: fmt.Sprintf("task %s does not exist in cluster %s", ref, cluster.ClusterName),
			})
			continue
		}
		taskCopy := *task
		if !withTags {
			taskCopy.Tags = nil
		}
		out.Tasks = append(out.Tasks, taskCopy)
	}
	return out, nil
}

// --- Services ------------------------------------------------------------------

func (s *Service) CreateService(in *types.CreateServiceInput) (*types.CreateServiceOutput, error) {
	if in == nil {
		return nil, invalidParameterError("CreateService input is required")
	}
	name := strings.TrimSpace(in.ServiceName)
	if name == "" {
		return nil, invalidParameterError("serviceName is required")
	}
	if !resourceNamePattern.MatchString(name) {
		return nil, invalidParameterError("Invalid service name: %s", name)
	}
	if in.DesiredCount < 0 {
		return nil, invalidParameterError("desiredCount must be >= 0")
	}
	if err := validateTags(in.Tags); err != nil {
		return nil, err
	}

	cluster, err := s.ResolveCluster(in.Cluster)
	if err != nil {
		return nil, err
	}

	td, err := s.resolveTaskDefinition(in.TaskDefinition)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// An INACTIVE record is a tombstone left by a prior DeleteService (see
	// DeleteService below): CreateService is allowed to replace it, the way
	// AWS lets a new service reuse a deleted service's name.
	if existing, err := s.store.GetService(cluster.ClusterArn, name); err == nil && existing.Status != types.ServiceStatusInactive {
		return nil, clientError("Service already exists: %s", name)
	}

	launchType := in.LaunchType
	if launchType == "" {
		launchType = types.LaunchTypeFargate
	}

	schedulingStrategy := strings.ToUpper(strings.TrimSpace(in.SchedulingStrategy))
	switch schedulingStrategy {
	case "":
		schedulingStrategy = "REPLICA"
	case "REPLICA", "DAEMON":
	default:
		return nil, invalidParameterError("schedulingStrategy must be REPLICA or DAEMON")
	}

	platformVersion := in.PlatformVersion
	if platformVersion == "" && launchType == types.LaunchTypeFargate {
		platformVersion = "LATEST"
	}

	deploymentConfig := cloneDeploymentConfiguration(in.DeploymentConfiguration)
	if deploymentConfig == nil {
		deploymentConfig = &types.DeploymentConfiguration{}
	}
	if deploymentConfig.MaximumPercent == 0 {
		deploymentConfig.MaximumPercent = 200
	}
	if deploymentConfig.MinimumHealthyPercent == 0 {
		deploymentConfig.MinimumHealthyPercent = 100
	}

	propagateTags := in.PropagateTags
	if propagateTags == "" {
		propagateTags = "NONE"
	}

	svc := &types.ECSService{
		ServiceArn:              serviceARN(s.cfg, cluster.ClusterName, name),
		ServiceName:             name,
		ClusterArn:              cluster.ClusterArn,
		TaskDefinitionArn:       td.TaskDefinitionArn,
		DesiredCount:            in.DesiredCount,
		LaunchType:              launchType,
		Status:                  types.ServiceStatusActive,
		CreatedAt:               time.Now().UTC(),
		SchedulingStrategy:      schedulingStrategy,
		NetworkConfiguration:    cloneNetworkConfiguration(in.NetworkConfiguration),
		PlatformVersion:         platformVersion,
		DeploymentConfiguration: deploymentConfig,
		EnableECSManagedTags:    in.EnableECSManagedTags,
		PropagateTags:           propagateTags,
		Tags:                    cloneTags(in.Tags),
	}
	if err := s.store.SaveService(svc); err != nil {
		return nil, err
	}
	return &types.CreateServiceOutput{Service: svc}, nil
}

func (s *Service) UpdateService(in *types.UpdateServiceInput) (*types.UpdateServiceOutput, error) {
	if in == nil || strings.TrimSpace(in.Service) == "" {
		return nil, invalidParameterError("service is required")
	}
	cluster, err := s.ResolveCluster(in.Cluster)
	if err != nil {
		return nil, err
	}

	var td *types.TaskDefinition
	if in.TaskDefinition != "" {
		td, err = s.resolveTaskDefinition(in.TaskDefinition)
		if err != nil {
			return nil, err
		}
	}
	if in.DesiredCount != nil && *in.DesiredCount < 0 {
		return nil, invalidParameterError("desiredCount must be >= 0")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	name := serviceNameFromRef(in.Service)
	svc, err := s.store.GetService(cluster.ClusterArn, name)
	if err != nil {
		return nil, clientError("Service not found: %s", in.Service)
	}
	if svc.Status == types.ServiceStatusInactive {
		return nil, serviceNotActiveError(name)
	}

	if td != nil {
		svc.TaskDefinitionArn = td.TaskDefinitionArn
	}
	if in.DesiredCount != nil {
		svc.DesiredCount = *in.DesiredCount
	}
	if in.NetworkConfiguration != nil {
		svc.NetworkConfiguration = cloneNetworkConfiguration(in.NetworkConfiguration)
	}
	if in.PlatformVersion != "" {
		svc.PlatformVersion = in.PlatformVersion
	}
	if in.DeploymentConfiguration != nil {
		svc.DeploymentConfiguration = cloneDeploymentConfiguration(in.DeploymentConfiguration)
	}

	if err := s.store.SaveService(svc); err != nil {
		return nil, err
	}
	return &types.UpdateServiceOutput{Service: svc}, nil
}

func (s *Service) DeleteService(in *types.DeleteServiceInput) (*types.DeleteServiceOutput, error) {
	if in == nil || strings.TrimSpace(in.Service) == "" {
		return nil, invalidParameterError("service is required")
	}
	cluster, err := s.ResolveCluster(in.Cluster)
	if err != nil {
		return nil, err
	}

	name := serviceNameFromRef(in.Service)

	s.mu.Lock()
	svc, err := s.store.GetService(cluster.ClusterArn, name)
	if err != nil {
		s.mu.Unlock()
		return nil, clientError("Service not found: %s", in.Service)
	}
	// A tombstoned (INACTIVE) service is kept around only so Terraform's
	// post-destroy DescribeServices poll sees it settle to INACTIVE instead
	// of hanging; deleting it again is not a no-op success the way AWS
	// itself refuses a second DeleteService against an inactive service.
	if svc.Status == types.ServiceStatusInactive {
		s.mu.Unlock()
		return nil, serviceNotActiveError(name)
	}
	// AWS only requires desiredCount to be 0 for a non-forced delete; tasks
	// still running at that point drain in the background. Terraform relies
	// on this: it scales to 0 and deletes immediately, before the reconcile
	// loop has observed the scale-down.
	if !in.Force && svc.DesiredCount > 0 {
		s.mu.Unlock()
		return nil, clientError("The service cannot be stopped while it is scaled above 0.")
	}

	active := s.serviceTasksToDrainLocked(cluster.ClusterArn, name)
	if len(active) == 0 && svc.RunningCount == 0 {
		if err := s.tombstoneServiceLocked(svc); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.mu.Unlock()
		return &types.DeleteServiceOutput{Service: svc}, nil
	}

	drainer := s.taskDrainer
	// Make the desired state durable before invoking the runner. A process
	// restart during the drain must recover these tasks as STOPPED rather than
	// treating them as orphaned service capacity.
	svc.DesiredCount = 0
	svc.Status = types.ServiceStatusDraining
	if err := s.store.SaveService(svc); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	for _, task := range active {
		task.DesiredStatus = types.TaskDesiredStatusStopped
		task.StoppedReason = "service force-deleted"
		if err := s.store.SaveTask(task); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	s.mu.Unlock()

	if (len(active) > 0 || svc.RunningCount > 0) && drainer == nil {
		return nil, clientError("The service has active tasks but no task drainer is configured")
	}
	if drainer != nil {
		drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		drainErr := drainer(drainCtx, cluster, name)
		cancel()
		if drainErr != nil {
			return nil, clientError("failed to drain service %s before deletion: %v", name, drainErr)
		}
	}

	s.mu.Lock()
	remaining := s.serviceTasksToDrainLocked(cluster.ClusterArn, name)
	if len(remaining) > 0 {
		s.mu.Unlock()
		return nil, clientError("service %s still has active tasks after drain", name)
	}
	if err := s.tombstoneServiceLocked(svc); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	return &types.DeleteServiceOutput{Service: svc}, nil
}

// tombstoneServiceLocked replaces svc's persisted record with an INACTIVE
// tombstone instead of removing it, and updates svc in place to match what
// was persisted. Callers must hold s.mu.
//
// Real AWS keeps a deleted service describable (status INACTIVE) for some
// time after DeleteService; Tarn used to remove the record outright, which
// made DescribeServices return a MISSING failure and left Terraform's
// destroy polling for status INACTIVE forever (it never sees one). Keeping
// the record also means CreateService can reuse the same service name
// immediately, and reconcileOnce/hydrateClusterCounts/DeleteCluster already
// treat any non-ACTIVE service as inert, so nothing else needs to special-
// case INACTIVE. PruneInactiveServices (called from the reconcile loop)
// removes tombstones after they've aged out.
func (s *Service) tombstoneServiceLocked(svc *types.ECSService) error {
	now := time.Now().UTC()
	svc.Status = types.ServiceStatusInactive
	svc.DesiredCount = 0
	svc.RunningCount = 0
	svc.PendingCount = 0
	svc.InactiveAt = &now
	return s.store.SaveService(svc)
}

func (s *Service) serviceTasksToDrainLocked(clusterArn, serviceName string) []*types.Task {
	var active []*types.Task
	for _, task := range s.store.ListTasks(clusterArn) {
		if task.Group != serviceGroup(serviceName) || task.LastStatus == types.TaskStatusStopped {
			continue
		}
		active = append(active, task)
	}
	return active
}

func (s *Service) ListServices(in *types.ListServicesInput) (*types.ListServicesOutput, error) {
	clusterRef := ""
	if in != nil {
		clusterRef = in.Cluster
	}
	cluster, err := s.ResolveCluster(clusterRef)
	if err != nil {
		return nil, err
	}
	services := s.store.ListServices(cluster.ClusterArn)
	out := &types.ListServicesOutput{}
	for _, svc := range services {
		// AWS's ListServices does not return services it considers gone;
		// DescribeServices is the only action that still answers for an
		// INACTIVE (tombstoned) service.
		if svc.Status == types.ServiceStatusInactive {
			continue
		}
		out.ServiceArns = append(out.ServiceArns, svc.ServiceArn)
	}
	return out, nil
}

func (s *Service) DescribeServices(in *types.DescribeServicesInput) (*types.DescribeServicesOutput, error) {
	if in == nil || len(in.Services) == 0 {
		return nil, invalidParameterError("services is required")
	}
	cluster, err := s.ResolveCluster(in.Cluster)
	if err != nil {
		return nil, err
	}

	withTags := includesTag(in.Include)
	out := &types.DescribeServicesOutput{}
	for _, ref := range in.Services {
		name := serviceNameFromRef(ref)
		svc, err := s.store.GetService(cluster.ClusterArn, name)
		if err != nil {
			out.Failures = append(out.Failures, types.Failure{
				Arn:    ref,
				Reason: "MISSING",
				Detail: fmt.Sprintf("service %s does not exist", name),
			})
			continue
		}
		svcCopy := *svc
		if !withTags {
			svcCopy.Tags = nil
		}
		out.Services = append(out.Services, svcCopy)
	}
	return out, nil
}

// SetServiceCounts records the running/pending task counts observed by the
// reconcile loop (T7). Desired count is caller-set state only; running and
// pending counts are the only fields this control plane does not originate.
func (s *Service) SetServiceCounts(cluster *types.Cluster, serviceName string, running, pending int) (*types.ECSService, error) {
	if cluster == nil {
		return nil, invalidParameterError("cluster is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	svc, err := s.store.GetService(cluster.ClusterArn, serviceName)
	if err != nil {
		return nil, clientError("Service not found: %s", serviceName)
	}
	// A DeleteService tombstone must stay at desiredCount 0 / running 0 /
	// pending 0. reconcileService still runs its bookkeeping for the tick
	// that observes a service going away (defer runs after the tombstone is
	// already persisted), so writing counts back here would resurrect
	// nonzero counts on an INACTIVE record.
	if svc.Status == types.ServiceStatusInactive {
		return svc, nil
	}
	if svc.RunningCount == running && svc.PendingCount == pending {
		return svc, nil
	}
	svc.RunningCount = running
	svc.PendingCount = pending
	if err := s.store.SaveService(svc); err != nil {
		return nil, err
	}
	return svc, nil
}

// SetTaskTags records the tags a task was launched with (explicit RunTask
// tags plus whatever propagateTags copied from the task definition or
// owning service). Called by the runner right after NewTaskRecord, the same
// way SetTaskCorrelationID is.
func (s *Service) SetTaskTags(taskArn string, tags []types.Tag) (*types.Task, error) {
	if len(tags) == 0 {
		return s.GetTask(taskArn)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getTaskByRef(taskArn)
	if err != nil {
		return nil, err
	}
	task.Tags = cloneTags(tags)
	if err := s.store.SaveTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// --- Tagging (TagResource / UntagResource / ListTagsForResource) -----------

const maxResourceTags = 50

// includesTag reports whether include (a DescribeClusters/DescribeServices/
// DescribeTasks/DescribeTaskDefinition Include list) asked for "TAGS",
// matching real ECS's opt-in tag reporting.
func includesTag(include []string) bool {
	for _, v := range include {
		if strings.EqualFold(v, "TAGS") {
			return true
		}
	}
	return false
}

// validateTags checks a caller-supplied tag list against real ECS's limits:
// at most 50 tags, key 1-128 chars, value 0-256 chars, no "aws:"-prefixed
// keys (reserved), and no duplicate keys within one request.
func validateTags(tags []types.Tag) error {
	if len(tags) > maxResourceTags {
		return invalidParameterError("tags: the maximum number of tags per resource is %d", maxResourceTags)
	}
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		if len(t.Key) < 1 || len(t.Key) > 128 {
			return invalidParameterError("Tag key must be between 1 and 128 characters: %q", t.Key)
		}
		if len(t.Value) > 256 {
			return invalidParameterError("Tag value must be 256 characters or fewer: %q", t.Value)
		}
		if strings.HasPrefix(strings.ToLower(t.Key), "aws:") {
			return invalidParameterError("Tag keys beginning with 'aws:' are reserved: %q", t.Key)
		}
		if seen[t.Key] {
			return invalidParameterError("Duplicate tag key in request: %q", t.Key)
		}
		seen[t.Key] = true
	}
	return nil
}

// cloneTags returns an independent copy of tags, or nil for an empty list.
func cloneTags(tags []types.Tag) []types.Tag {
	if len(tags) == 0 {
		return nil
	}
	return append([]types.Tag(nil), tags...)
}

// mergeTags applies updates onto existing, replacing any key already
// present and appending new keys, preserving existing's original order.
func mergeTags(existing, updates []types.Tag) []types.Tag {
	byKey := make(map[string]string, len(existing)+len(updates))
	order := make([]string, 0, len(existing)+len(updates))
	for _, t := range existing {
		if _, ok := byKey[t.Key]; !ok {
			order = append(order, t.Key)
		}
		byKey[t.Key] = t.Value
	}
	for _, t := range updates {
		if _, ok := byKey[t.Key]; !ok {
			order = append(order, t.Key)
		}
		byKey[t.Key] = t.Value
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]types.Tag, len(order))
	for i, k := range order {
		out[i] = types.Tag{Key: k, Value: byKey[k]}
	}
	return out
}

// removeTagKeys drops every tag in existing whose key appears in keys.
func removeTagKeys(existing []types.Tag, keys []string) []types.Tag {
	if len(existing) == 0 || len(keys) == 0 {
		return existing
	}
	drop := make(map[string]bool, len(keys))
	for _, k := range keys {
		drop[k] = true
	}
	out := make([]types.Tag, 0, len(existing))
	for _, t := range existing {
		if !drop[t.Key] {
			out = append(out, t)
		}
	}
	return cloneTags(out)
}

// taggableLocked resolves arn (a cluster, task definition, service, or task
// ARN) to get/set accessors for that resource's tag list. Callers must hold
// s.mu; set persists through the same store method the resource's other
// mutators use.
func (s *Service) taggableLocked(arn string) (get func() []types.Tag, set func([]types.Tag) error, err error) {
	switch {
	case strings.Contains(arn, ":cluster/"):
		c, gerr := s.store.GetCluster(clusterNameFromRef(arn))
		if gerr != nil {
			return nil, nil, clientError("Cluster not found: %s", arn)
		}
		return func() []types.Tag { return c.Tags },
			func(t []types.Tag) error { c.Tags = t; return s.store.SaveCluster(c) },
			nil
	case strings.Contains(arn, ":task-definition/"):
		td, terr := s.resolveTaskDefinition(arn)
		if terr != nil {
			return nil, nil, clientError("Task definition not found: %s", arn)
		}
		return func() []types.Tag { return td.Tags },
			func(t []types.Tag) error { td.Tags = t; return s.store.SaveTaskDefinition(td) },
			nil
	case strings.Contains(arn, ":service/"):
		clusterName, name, ok := parseServiceArn(arn)
		if !ok {
			return nil, nil, invalidParameterError("Invalid service ARN: %s", arn)
		}
		svc, serr := s.store.GetService(clusterARN(s.cfg, clusterName), name)
		if serr != nil {
			return nil, nil, clientError("Service not found: %s", arn)
		}
		return func() []types.Tag { return svc.Tags },
			func(t []types.Tag) error { svc.Tags = t; return s.store.SaveService(svc) },
			nil
	case strings.Contains(arn, ":task/"):
		task, terr := s.getTaskByRef(arn)
		if terr != nil {
			return nil, nil, clientError("Task not found: %s", arn)
		}
		return func() []types.Tag { return task.Tags },
			func(t []types.Tag) error { task.Tags = t; return s.store.SaveTask(task) },
			nil
	default:
		return nil, nil, invalidParameterError("Long arn format is not supported for this resource: %s", arn)
	}
}

// TagResource adds or replaces tags on a cluster, task definition, service,
// or task, resolved from ResourceArn.
func (s *Service) TagResource(in *types.TagResourceInput) (*types.TagResourceOutput, error) {
	if in == nil || strings.TrimSpace(in.ResourceArn) == "" {
		return nil, invalidParameterError("resourceArn is required")
	}
	if err := validateTags(in.Tags); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	get, set, err := s.taggableLocked(in.ResourceArn)
	if err != nil {
		return nil, err
	}
	merged := mergeTags(get(), in.Tags)
	if len(merged) > maxResourceTags {
		return nil, invalidParameterError("tags: the maximum number of tags per resource is %d", maxResourceTags)
	}
	if err := set(merged); err != nil {
		return nil, err
	}
	return &types.TagResourceOutput{}, nil
}

// UntagResource removes the given tag keys from a cluster, task definition,
// service, or task, resolved from ResourceArn.
func (s *Service) UntagResource(in *types.UntagResourceInput) (*types.UntagResourceOutput, error) {
	if in == nil || strings.TrimSpace(in.ResourceArn) == "" {
		return nil, invalidParameterError("resourceArn is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	get, set, err := s.taggableLocked(in.ResourceArn)
	if err != nil {
		return nil, err
	}
	if err := set(removeTagKeys(get(), in.TagKeys)); err != nil {
		return nil, err
	}
	return &types.UntagResourceOutput{}, nil
}

// ListTagsForResource returns the tags currently attached to a cluster, task
// definition, service, or task, resolved from ResourceArn.
func (s *Service) ListTagsForResource(in *types.ListTagsForResourceInput) (*types.ListTagsForResourceOutput, error) {
	if in == nil || strings.TrimSpace(in.ResourceArn) == "" {
		return nil, invalidParameterError("resourceArn is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	get, _, err := s.taggableLocked(in.ResourceArn)
	if err != nil {
		return nil, err
	}
	return &types.ListTagsForResourceOutput{Tags: cloneTags(get())}, nil
}
