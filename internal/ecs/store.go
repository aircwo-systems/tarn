package ecs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Store is an in-memory ECS control-plane store with optional JSON
// persistence, following the same layout and locking discipline as
// internal/eventbridge/store.go.
type Store struct {
	mu    sync.RWMutex
	dirty atomic.Bool
	cfg   *config.Config

	clusters map[string]*types.Cluster // key: cluster name

	// taskDefs holds every registered revision, keyed by family, then by
	// revision number. Deregistering a revision marks it INACTIVE in place;
	// it is never removed, so DescribeTaskDefinition keeps answering for it.
	taskDefs map[string]map[int]*types.TaskDefinition

	services map[string]*types.ECSService // key: cluster name + "/" + service name
	tasks    map[string]*types.Task       // key: task ARN
}

func NewStore(cfg *config.Config) *Store {
	return &Store{
		cfg:      cfg,
		clusters: make(map[string]*types.Cluster),
		taskDefs: make(map[string]map[int]*types.TaskDefinition),
		services: make(map[string]*types.ECSService),
		tasks:    make(map[string]*types.Task),
	}
}

func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}
	go s.startFlusher()

	if err := os.MkdirAll(s.cfg.ECSDir(), 0o755); err != nil {
		return fmt.Errorf("create ecs dir: %w", err)
	}

	data, err := os.ReadFile(s.cfg.ECSStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read ecs state: %w", err)
	}
	if len(data) == 0 {
		return nil
	}

	var snapshot struct {
		Clusters        []*types.Cluster        `json:"clusters"`
		TaskDefinitions []*types.TaskDefinition `json:"taskDefinitions"`
		Services        []*types.ECSService     `json:"services"`
		Tasks           []*types.Task           `json:"tasks"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&snapshot); err != nil {
		return fmt.Errorf("decode ecs state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.clusters = make(map[string]*types.Cluster, len(snapshot.Clusters))
	for _, c := range snapshot.Clusters {
		if c == nil || c.ClusterName == "" {
			continue
		}
		s.clusters[c.ClusterName] = cloneCluster(c)
	}

	s.taskDefs = make(map[string]map[int]*types.TaskDefinition)
	for _, td := range snapshot.TaskDefinitions {
		if td == nil || td.Family == "" {
			continue
		}
		revs, ok := s.taskDefs[td.Family]
		if !ok {
			revs = make(map[int]*types.TaskDefinition)
			s.taskDefs[td.Family] = revs
		}
		revs[td.Revision] = cloneTaskDefinition(td)
	}

	s.services = make(map[string]*types.ECSService, len(snapshot.Services))
	for _, svc := range snapshot.Services {
		if svc == nil || svc.ServiceName == "" {
			continue
		}
		s.services[serviceKey(svc.ClusterArn, svc.ServiceName)] = cloneService(svc)
	}

	s.tasks = make(map[string]*types.Task, len(snapshot.Tasks))
	for _, t := range snapshot.Tasks {
		if t == nil || t.TaskArn == "" {
			continue
		}
		s.tasks[t.TaskArn] = cloneTask(t)
	}

	return nil
}

// --- Clusters ---------------------------------------------------------------

func (s *Store) SaveCluster(c *types.Cluster) error {
	if c == nil || c.ClusterName == "" {
		return fmt.Errorf("cluster name is required")
	}
	s.mu.Lock()
	s.clusters[c.ClusterName] = cloneCluster(c)
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) GetCluster(name string) (*types.Cluster, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clusters[name]
	if !ok {
		return nil, fmt.Errorf("cluster %s not found", name)
	}
	return cloneCluster(c), nil
}

func (s *Store) DeleteCluster(name string) error {
	s.mu.Lock()
	if _, ok := s.clusters[name]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("cluster %s not found", name)
	}
	delete(s.clusters, name)
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) ListClusters() []*types.Cluster {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.Cluster, 0, len(s.clusters))
	for _, c := range s.clusters {
		out = append(out, cloneCluster(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClusterName < out[j].ClusterName })
	return out
}

// --- Task definitions --------------------------------------------------------

// NextRevision returns the revision number the next RegisterTaskDefinition
// call for family should use (1 if the family has never been registered).
func (s *Store) NextRevision(family string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	revs, ok := s.taskDefs[family]
	if !ok || len(revs) == 0 {
		return 1
	}
	max := 0
	for r := range revs {
		if r > max {
			max = r
		}
	}
	return max + 1
}

func (s *Store) SaveTaskDefinition(td *types.TaskDefinition) error {
	if td == nil || td.Family == "" || td.Revision <= 0 {
		return fmt.Errorf("task definition family and revision are required")
	}
	s.mu.Lock()
	revs, ok := s.taskDefs[td.Family]
	if !ok {
		revs = make(map[int]*types.TaskDefinition)
		s.taskDefs[td.Family] = revs
	}
	revs[td.Revision] = cloneTaskDefinition(td)
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) GetTaskDefinition(family string, revision int) (*types.TaskDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	revs, ok := s.taskDefs[family]
	if !ok {
		return nil, fmt.Errorf("task definition family %s not found", family)
	}
	td, ok := revs[revision]
	if !ok {
		return nil, fmt.Errorf("task definition %s:%d not found", family, revision)
	}
	return cloneTaskDefinition(td), nil
}

// LatestActiveTaskDefinition returns the highest-revision ACTIVE task
// definition in family, i.e. what a bare family name resolves to.
func (s *Store) LatestActiveTaskDefinition(family string) (*types.TaskDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	revs, ok := s.taskDefs[family]
	if !ok {
		return nil, fmt.Errorf("task definition family %s not found", family)
	}
	var best *types.TaskDefinition
	for _, td := range revs {
		if td.Status != types.TaskDefinitionStatusActive {
			continue
		}
		if best == nil || td.Revision > best.Revision {
			best = td
		}
	}
	if best == nil {
		return nil, fmt.Errorf("task definition family %s has no ACTIVE revision", family)
	}
	return cloneTaskDefinition(best), nil
}

// ListTaskDefinitionRevisions returns every revision registered for family,
// sorted ascending, including INACTIVE ones.
func (s *Store) ListTaskDefinitionRevisions(family string) []*types.TaskDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	revs, ok := s.taskDefs[family]
	if !ok {
		return nil
	}
	out := make([]*types.TaskDefinition, 0, len(revs))
	for _, td := range revs {
		out = append(out, cloneTaskDefinition(td))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out
}

// ListTaskDefinitionFamilies returns every family that has at least one
// registered revision, sorted.
func (s *Store) ListTaskDefinitionFamilies() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.taskDefs))
	for family := range s.taskDefs {
		out = append(out, family)
	}
	sort.Strings(out)
	return out
}

// --- Services -----------------------------------------------------------------

func serviceKey(clusterArn, serviceName string) string {
	return clusterArn + "/" + serviceName
}

func (s *Store) SaveService(svc *types.ECSService) error {
	if svc == nil || svc.ServiceName == "" || svc.ClusterArn == "" {
		return fmt.Errorf("service name and cluster are required")
	}
	s.mu.Lock()
	s.services[serviceKey(svc.ClusterArn, svc.ServiceName)] = cloneService(svc)
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) GetService(clusterArn, serviceName string) (*types.ECSService, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[serviceKey(clusterArn, serviceName)]
	if !ok {
		return nil, fmt.Errorf("service %s not found", serviceName)
	}
	return cloneService(svc), nil
}

func (s *Store) DeleteService(clusterArn, serviceName string) error {
	key := serviceKey(clusterArn, serviceName)
	s.mu.Lock()
	if _, ok := s.services[key]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("service %s not found", serviceName)
	}
	delete(s.services, key)
	s.mu.Unlock()
	return s.persist()
}

// PruneInactiveServices deletes INACTIVE (tombstoned) service records whose
// DeleteService call happened before cutoff, in one persistence operation.
// Keeping tombstones around briefly lets DescribeServices settle on INACTIVE
// for Terraform's post-destroy poll; keeping them forever would grow
// state.json without bound.
func (s *Store) PruneInactiveServices(cutoff time.Time) (int, error) {
	s.mu.Lock()
	removed := 0
	for key, svc := range s.services {
		if svc.Status != types.ServiceStatusInactive || svc.InactiveAt == nil || svc.InactiveAt.After(cutoff) {
			continue
		}
		delete(s.services, key)
		removed++
	}
	s.mu.Unlock()
	if removed == 0 {
		return 0, nil
	}
	return removed, s.persist()
}

// ListServices returns every service, optionally filtered to those in
// clusterArn (all services if clusterArn is empty).
func (s *Store) ListServices(clusterArn string) []*types.ECSService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.ECSService, 0, len(s.services))
	for _, svc := range s.services {
		if clusterArn != "" && svc.ClusterArn != clusterArn {
			continue
		}
		out = append(out, cloneService(svc))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceName < out[j].ServiceName })
	return out
}

// --- Tasks ----------------------------------------------------------------

func (s *Store) SaveTask(t *types.Task) error {
	if t == nil || t.TaskArn == "" {
		return fmt.Errorf("task ARN is required")
	}
	s.mu.Lock()
	s.tasks[t.TaskArn] = cloneTask(t)
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) GetTask(taskArn string) (*types.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[taskArn]
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskArn)
	}
	return cloneTask(t), nil
}

func (s *Store) DeleteTask(taskArn string) error {
	s.mu.Lock()
	if _, ok := s.tasks[taskArn]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("task %s not found", taskArn)
	}
	delete(s.tasks, taskArn)
	s.mu.Unlock()
	return s.persist()
}

// PruneStoppedTasks deletes stopped task records older than cutoff in one
// persistence operation. Keeping a short history makes DescribeTasks useful
// while preventing a crash-looping service from growing state.json forever.
func (s *Store) PruneStoppedTasks(cutoff time.Time) (int, error) {
	s.mu.Lock()
	removed := 0
	for arn, task := range s.tasks {
		if task.LastStatus != types.TaskStatusStopped || task.StoppedAt == nil || task.StoppedAt.After(cutoff) {
			continue
		}
		delete(s.tasks, arn)
		removed++
	}
	s.mu.Unlock()
	if removed == 0 {
		return 0, nil
	}
	return removed, s.persist()
}

// ListTasks returns every task, optionally filtered to those in clusterArn
// (all tasks if clusterArn is empty).
func (s *Store) ListTasks(clusterArn string) []*types.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if clusterArn != "" && t.ClusterArn != clusterArn {
			continue
		}
		out = append(out, cloneTask(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskArn < out[j].TaskArn })
	return out
}

// --- persistence --------------------------------------------------------------

func (s *Store) persist() error {
	s.dirty.Store(true)
	return nil
}

func (s *Store) startFlusher() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		if s.dirty.Swap(false) {
			s.flushToDisk()
		}
	}
}

func (s *Store) flushToDisk() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	s.mu.RLock()
	clusters := make([]*types.Cluster, 0, len(s.clusters))
	for _, c := range s.clusters {
		clusters = append(clusters, cloneCluster(c))
	}
	var taskDefs []*types.TaskDefinition
	for _, revs := range s.taskDefs {
		for _, td := range revs {
			taskDefs = append(taskDefs, cloneTaskDefinition(td))
		}
	}
	services := make([]*types.ECSService, 0, len(s.services))
	for _, svc := range s.services {
		services = append(services, cloneService(svc))
	}
	tasks := make([]*types.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, cloneTask(t))
	}
	s.mu.RUnlock()

	sort.Slice(clusters, func(i, j int) bool { return clusters[i].ClusterName < clusters[j].ClusterName })
	sort.Slice(taskDefs, func(i, j int) bool {
		if taskDefs[i].Family != taskDefs[j].Family {
			return taskDefs[i].Family < taskDefs[j].Family
		}
		return taskDefs[i].Revision < taskDefs[j].Revision
	})
	sort.Slice(services, func(i, j int) bool { return services[i].ServiceName < services[j].ServiceName })
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskArn < tasks[j].TaskArn })

	payload := struct {
		Clusters        []*types.Cluster        `json:"clusters"`
		TaskDefinitions []*types.TaskDefinition `json:"taskDefinitions"`
		Services        []*types.ECSService     `json:"services"`
		Tasks           []*types.Task           `json:"tasks"`
	}{
		Clusters:        clusters,
		TaskDefinitions: taskDefs,
		Services:        services,
		Tasks:           tasks,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	if err := os.MkdirAll(s.cfg.ECSDir(), 0o755); err != nil {
		return
	}

	tmp, err := os.CreateTemp(s.cfg.ECSDir(), "state-*.json.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, s.cfg.ECSStatePath()); err != nil {
		_ = os.Remove(tmpPath)
	}
}

// --- cloning -------------------------------------------------------------------

func cloneCluster(src *types.Cluster) *types.Cluster {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func cloneTaskDefinition(src *types.TaskDefinition) *types.TaskDefinition {
	if src == nil {
		return nil
	}
	dst := *src
	if src.ContainerDefinitions != nil {
		dst.ContainerDefinitions = make([]types.ContainerDefinition, len(src.ContainerDefinitions))
		for i, cd := range src.ContainerDefinitions {
			dst.ContainerDefinitions[i] = cloneContainerDefinition(cd)
		}
	}
	if src.RequiresCompatibilities != nil {
		dst.RequiresCompatibilities = append([]string(nil), src.RequiresCompatibilities...)
	}
	return &dst
}

func cloneContainerDefinition(src types.ContainerDefinition) types.ContainerDefinition {
	dst := src
	if src.Command != nil {
		dst.Command = append([]string(nil), src.Command...)
	}
	if src.EntryPoint != nil {
		dst.EntryPoint = append([]string(nil), src.EntryPoint...)
	}
	if src.Environment != nil {
		dst.Environment = append([]types.KeyValuePair(nil), src.Environment...)
	}
	if src.PortMappings != nil {
		dst.PortMappings = append([]types.PortMapping(nil), src.PortMappings...)
	}
	if src.LogConfiguration != nil {
		lc := *src.LogConfiguration
		if src.LogConfiguration.Options != nil {
			lc.Options = cloneStringMap(src.LogConfiguration.Options)
		}
		dst.LogConfiguration = &lc
	}
	if src.Essential != nil {
		v := *src.Essential
		dst.Essential = &v
	}
	return dst
}

func cloneService(src *types.ECSService) *types.ECSService {
	if src == nil {
		return nil
	}
	dst := *src
	dst.NetworkConfiguration = cloneNetworkConfiguration(src.NetworkConfiguration)
	dst.DeploymentConfiguration = cloneDeploymentConfiguration(src.DeploymentConfiguration)
	if src.InactiveAt != nil {
		t := *src.InactiveAt
		dst.InactiveAt = &t
	}
	return &dst
}

func cloneNetworkConfiguration(src *types.NetworkConfiguration) *types.NetworkConfiguration {
	if src == nil {
		return nil
	}
	dst := *src
	if src.AwsvpcConfiguration != nil {
		vpc := *src.AwsvpcConfiguration
		vpc.Subnets = append([]string(nil), src.AwsvpcConfiguration.Subnets...)
		vpc.SecurityGroups = append([]string(nil), src.AwsvpcConfiguration.SecurityGroups...)
		dst.AwsvpcConfiguration = &vpc
	}
	return &dst
}

func cloneDeploymentConfiguration(src *types.DeploymentConfiguration) *types.DeploymentConfiguration {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func cloneTask(src *types.Task) *types.Task {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Containers != nil {
		dst.Containers = make([]types.TaskContainer, len(src.Containers))
		for i, c := range src.Containers {
			dst.Containers[i] = cloneTaskContainer(c)
		}
	}
	if src.StartedAt != nil {
		t := *src.StartedAt
		dst.StartedAt = &t
	}
	if src.StoppedAt != nil {
		t := *src.StoppedAt
		dst.StoppedAt = &t
	}
	return &dst
}

func cloneTaskContainer(src types.TaskContainer) types.TaskContainer {
	dst := src
	if src.ExitCode != nil {
		v := *src.ExitCode
		dst.ExitCode = &v
	}
	if src.NetworkBindings != nil {
		dst.NetworkBindings = append([]types.NetworkBinding(nil), src.NetworkBindings...)
	}
	return dst
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
