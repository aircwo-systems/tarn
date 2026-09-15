package ecs

import (
	"context"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	svc := NewService(cfg, store)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}
	return svc
}

func webTaskDefInput() *types.RegisterTaskDefinitionInput {
	return &types.RegisterTaskDefinitionInput{
		Family: "web",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "web", Image: "nginx:latest"},
		},
	}
}

// --- Clusters -----------------------------------------------------------------

func TestImplicitDefaultCluster(t *testing.T) {
	svc := newTestService(t)

	// Nothing has explicitly created "default" yet.
	out, err := svc.DescribeClusters(&types.DescribeClustersInput{})
	if err != nil {
		t.Fatalf("DescribeClusters: %v", err)
	}
	if len(out.Clusters) != 1 || out.Clusters[0].ClusterName != "default" {
		t.Fatalf("expected implicit default cluster, got %+v", out.Clusters)
	}
	if out.Clusters[0].Status != types.ClusterStatusActive {
		t.Fatalf("expected ACTIVE default cluster, got %s", out.Clusters[0].Status)
	}

	// RunTask-equivalent: resolving "" or "default" must both work with no
	// explicit CreateCluster call.
	c1, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster(\"\"): %v", err)
	}
	c2, err := svc.ResolveCluster("default")
	if err != nil {
		t.Fatalf("ResolveCluster(default): %v", err)
	}
	if c1.ClusterArn != c2.ClusterArn {
		t.Fatalf("expected same cluster for \"\" and \"default\": %+v vs %+v", c1, c2)
	}

	list, err := svc.ListClusters(&types.ListClustersInput{})
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(list.ClusterArns) != 1 {
		t.Fatalf("expected 1 cluster ARN, got %v", list.ClusterArns)
	}
}

func TestCreateAndDescribeCluster(t *testing.T) {
	svc := newTestService(t)

	out, err := svc.CreateCluster(&types.CreateClusterInput{ClusterName: "staging"})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	if out.Cluster.ClusterName != "staging" || out.Cluster.Status != types.ClusterStatusActive {
		t.Fatalf("unexpected cluster: %+v", out.Cluster)
	}

	// Idempotent create.
	out2, err := svc.CreateCluster(&types.CreateClusterInput{ClusterName: "staging"})
	if err != nil {
		t.Fatalf("CreateCluster (again): %v", err)
	}
	if out2.Cluster.ClusterArn != out.Cluster.ClusterArn {
		t.Fatalf("expected same ARN on repeat create")
	}

	desc, err := svc.DescribeClusters(&types.DescribeClustersInput{Clusters: []string{"staging", out.Cluster.ClusterArn, "does-not-exist"}})
	if err != nil {
		t.Fatalf("DescribeClusters: %v", err)
	}
	if len(desc.Clusters) != 2 {
		t.Fatalf("expected 2 described clusters, got %d: %+v", len(desc.Clusters), desc.Clusters)
	}
	if len(desc.Failures) != 1 || desc.Failures[0].Arn != "does-not-exist" {
		t.Fatalf("expected 1 failure for unknown cluster, got %+v", desc.Failures)
	}
}

func TestDeleteCluster(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.CreateCluster(&types.CreateClusterInput{ClusterName: "temp"}); err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	cluster, err := svc.DeleteCluster("temp")
	if err != nil {
		t.Fatalf("DeleteCluster: %v", err)
	}
	if cluster.Status != types.ClusterStatusInactive {
		t.Fatalf("expected INACTIVE cluster returned, got %s", cluster.Status)
	}

	if _, err := svc.DeleteCluster("temp"); err == nil {
		t.Fatalf("expected error deleting already-deleted cluster")
	} else if se, ok := err.(*ServiceError); !ok || se.Code != "ClusterNotFoundException" {
		t.Fatalf("expected ClusterNotFoundException, got %v", err)
	}
}

func TestDeleteClusterUnknownReturnsClusterNotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.DeleteCluster("nope")
	se, ok := err.(*ServiceError)
	if !ok || se.Code != "ClusterNotFoundException" {
		t.Fatalf("expected ClusterNotFoundException, got %v", err)
	}
}

// --- Task definitions -----------------------------------------------------------

func TestRegisterTaskDefinitionRevisioning(t *testing.T) {
	svc := newTestService(t)

	out1, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition (1): %v", err)
	}
	if out1.TaskDefinition.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", out1.TaskDefinition.Revision)
	}

	out2, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition (2): %v", err)
	}
	if out2.TaskDefinition.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", out2.TaskDefinition.Revision)
	}

	// Both revisions must stay independently describable.
	d1, err := svc.DescribeTaskDefinition("web:1", nil)
	if err != nil {
		t.Fatalf("DescribeTaskDefinition web:1: %v", err)
	}
	if d1.TaskDefinition.Revision != 1 {
		t.Fatalf("expected web:1 to describe revision 1, got %d", d1.TaskDefinition.Revision)
	}

	d2, err := svc.DescribeTaskDefinition("web:2", nil)
	if err != nil {
		t.Fatalf("DescribeTaskDefinition web:2: %v", err)
	}
	if d2.TaskDefinition.Revision != 2 {
		t.Fatalf("expected web:2 to describe revision 2, got %d", d2.TaskDefinition.Revision)
	}

	// A bare family name resolves to the latest ACTIVE revision.
	latest, err := svc.DescribeTaskDefinition("web", nil)
	if err != nil {
		t.Fatalf("DescribeTaskDefinition web: %v", err)
	}
	if latest.TaskDefinition.Revision != 2 {
		t.Fatalf("expected bare family to resolve to revision 2, got %d", latest.TaskDefinition.Revision)
	}

	// Full ARN form also resolves.
	byARN, err := svc.DescribeTaskDefinition(out1.TaskDefinition.TaskDefinitionArn, nil)
	if err != nil {
		t.Fatalf("DescribeTaskDefinition by ARN: %v", err)
	}
	if byARN.TaskDefinition.Revision != 1 {
		t.Fatalf("expected ARN lookup to resolve revision 1, got %d", byARN.TaskDefinition.Revision)
	}
}

func TestDeregisterTaskDefinitionMarksInactiveButDescribable(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	deregistered, err := svc.DeregisterTaskDefinition("web:1")
	if err != nil {
		t.Fatalf("DeregisterTaskDefinition: %v", err)
	}
	if deregistered.TaskDefinition.Status != types.TaskDefinitionStatusInactive {
		t.Fatalf("expected INACTIVE, got %s", deregistered.TaskDefinition.Status)
	}

	// Still describable directly.
	d, err := svc.DescribeTaskDefinition("web:1", nil)
	if err != nil {
		t.Fatalf("DescribeTaskDefinition web:1 after deregister: %v", err)
	}
	if d.TaskDefinition.Status != types.TaskDefinitionStatusInactive {
		t.Fatalf("expected INACTIVE task definition still describable, got %s", d.TaskDefinition.Status)
	}

	// Bare family still resolves to the latest ACTIVE revision (2), skipping
	// the now-INACTIVE revision 1.
	latest, err := svc.DescribeTaskDefinition("web", nil)
	if err != nil {
		t.Fatalf("DescribeTaskDefinition web: %v", err)
	}
	if latest.TaskDefinition.Revision != 2 {
		t.Fatalf("expected latest ACTIVE revision 2, got %d", latest.TaskDefinition.Revision)
	}

	// Deregistering without a revision is rejected.
	if _, err := svc.DeregisterTaskDefinition("web"); err == nil {
		t.Fatalf("expected error deregistering bare family without revision")
	}
}

func TestListTaskDefinitionsFiltersSortsAndPaginates(t *testing.T) {
	svc := newTestService(t)

	register := func(family string) *types.TaskDefinition {
		td, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
			Family:               family,
			ContainerDefinitions: []types.ContainerDefinition{{Name: "app", Image: "busybox"}},
		})
		if err != nil {
			t.Fatalf("RegisterTaskDefinition %s: %v", family, err)
		}
		return td.TaskDefinition
	}

	web1 := register("web")
	web2 := register("web")
	api1 := register("api")
	if _, err := svc.DeregisterTaskDefinition(web1.TaskDefinitionArn); err != nil {
		t.Fatalf("DeregisterTaskDefinition: %v", err)
	}

	active, err := svc.ListTaskDefinitions(nil)
	if err != nil {
		t.Fatalf("ListTaskDefinitions default: %v", err)
	}
	wantActive := []string{api1.TaskDefinitionArn, web2.TaskDefinitionArn}
	if len(active.TaskDefinitionArns) != len(wantActive) {
		t.Fatalf("active definitions = %v, want %v", active.TaskDefinitionArns, wantActive)
	}
	for i := range wantActive {
		if active.TaskDefinitionArns[i] != wantActive[i] {
			t.Fatalf("active definitions = %v, want %v", active.TaskDefinitionArns, wantActive)
		}
	}

	inactive, err := svc.ListTaskDefinitions(&types.ListTaskDefinitionsInput{
		FamilyPrefix: "web",
		Status:       types.TaskDefinitionStatusInactive,
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions inactive: %v", err)
	}
	if len(inactive.TaskDefinitionArns) != 1 || inactive.TaskDefinitionArns[0] != web1.TaskDefinitionArn {
		t.Fatalf("inactive definitions = %v, want [%s]", inactive.TaskDefinitionArns, web1.TaskDefinitionArn)
	}

	page, err := svc.ListTaskDefinitions(&types.ListTaskDefinitionsInput{MaxResults: 1})
	if err != nil {
		t.Fatalf("ListTaskDefinitions page 1: %v", err)
	}
	if len(page.TaskDefinitionArns) != 1 || page.TaskDefinitionArns[0] != api1.TaskDefinitionArn || page.NextToken != "1" {
		t.Fatalf("page 1 = %+v, want first ARN and token 1", page)
	}
	page2, err := svc.ListTaskDefinitions(&types.ListTaskDefinitionsInput{MaxResults: 1, NextToken: page.NextToken})
	if err != nil {
		t.Fatalf("ListTaskDefinitions page 2: %v", err)
	}
	if len(page2.TaskDefinitionArns) != 1 || page2.TaskDefinitionArns[0] != web2.TaskDefinitionArn || page2.NextToken != "" {
		t.Fatalf("page 2 = %+v, want second ARN and no token", page2)
	}

	desc, err := svc.ListTaskDefinitions(&types.ListTaskDefinitionsInput{Sort: types.TaskDefinitionSortDescending})
	if err != nil {
		t.Fatalf("ListTaskDefinitions descending: %v", err)
	}
	if len(desc.TaskDefinitionArns) != 2 || desc.TaskDefinitionArns[0] != web2.TaskDefinitionArn || desc.TaskDefinitionArns[1] != api1.TaskDefinitionArn {
		t.Fatalf("descending definitions = %v", desc.TaskDefinitionArns)
	}
}

func TestListTaskDefinitionsRejectsInvalidParameters(t *testing.T) {
	svc := newTestService(t)
	for name, in := range map[string]*types.ListTaskDefinitionsInput{
		"max results": {MaxResults: 101},
		"sort":        {Sort: "MIDDLE"},
		"status":      {Status: "UNKNOWN"},
		"token":       {NextToken: "not-a-token"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.ListTaskDefinitions(in); err == nil {
				t.Fatalf("expected invalid parameter error")
			} else if se, ok := err.(*ServiceError); !ok || se.Code != "InvalidParameterException" {
				t.Fatalf("expected InvalidParameterException, got %v", err)
			}
		})
	}
}

func TestRegisterTaskDefinitionValidation(t *testing.T) {
	svc := newTestService(t)

	cases := []struct {
		name string
		in   *types.RegisterTaskDefinitionInput
	}{
		{"missing family", &types.RegisterTaskDefinitionInput{ContainerDefinitions: []types.ContainerDefinition{{Name: "a", Image: "x"}}}},
		{"no containers", &types.RegisterTaskDefinitionInput{Family: "f"}},
		{"missing image", &types.RegisterTaskDefinitionInput{Family: "f", ContainerDefinitions: []types.ContainerDefinition{{Name: "a"}}}},
		{"duplicate container names", &types.RegisterTaskDefinitionInput{
			Family: "f",
			ContainerDefinitions: []types.ContainerDefinition{
				{Name: "a", Image: "x"},
				{Name: "a", Image: "y"},
			},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.RegisterTaskDefinition(tc.in); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

// --- Task records ----------------------------------------------------------------

func TestTaskRecordLifecycle(t *testing.T) {
	svc := newTestService(t)

	tdOut, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}

	task, err := svc.NewTaskRecord(cluster, tdOut.TaskDefinition, nil, "", "")
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}
	if task.LastStatus != types.TaskStatusProvisioning {
		t.Fatalf("expected PROVISIONING, got %s", task.LastStatus)
	}
	if len(task.Containers) != 1 || task.Containers[0].Name != "web" {
		t.Fatalf("unexpected containers: %+v", task.Containers)
	}
	if task.Containers[0].ExitCode != nil {
		t.Fatalf("expected nil ExitCode before exit, got %v", *task.Containers[0].ExitCode)
	}

	if _, err := svc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
		t.Fatalf("SetTaskStatus RUNNING: %v", err)
	}

	bindings := []types.NetworkBinding{{ContainerPort: 80, HostPort: 32768, Protocol: "tcp"}}
	if _, err := svc.SetContainerNetworkBindings(task.TaskArn, "web", bindings); err != nil {
		t.Fatalf("SetContainerNetworkBindings: %v", err)
	}

	var exitCode int64 = 0
	if _, err := svc.SetContainerExitCode(task.TaskArn, "web", &exitCode, "Essential container exited"); err != nil {
		t.Fatalf("SetContainerExitCode: %v", err)
	}

	updated, err := svc.SetTaskStatus(task.TaskArn, types.TaskStatusStopped)
	if err != nil {
		t.Fatalf("SetTaskStatus STOPPED: %v", err)
	}
	if updated.StoppedAt == nil {
		t.Fatalf("expected StoppedAt to be set")
	}
	if updated.Containers[0].ExitCode == nil || *updated.Containers[0].ExitCode != 0 {
		t.Fatalf("expected exit code 0 preserved, got %+v", updated.Containers[0].ExitCode)
	}
	if len(updated.Containers[0].NetworkBindings) != 1 || updated.Containers[0].NetworkBindings[0].HostPort != 32768 {
		t.Fatalf("expected network binding preserved, got %+v", updated.Containers[0].NetworkBindings)
	}

	// Describable by bare task ID too, not just the full ARN.
	shortID := taskIDFromRef(task.TaskArn)
	byID, err := svc.GetTask(shortID)
	if err != nil {
		t.Fatalf("GetTask by short ID: %v", err)
	}
	if byID.TaskArn != task.TaskArn {
		t.Fatalf("expected same task, got %+v", byID)
	}
}

func TestTaskContainerExitCodeNilIsNotZero(t *testing.T) {
	svc := newTestService(t)

	tdOut, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	task, err := svc.NewTaskRecord(cluster, tdOut.TaskDefinition, nil, "", "")
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}
	if task.Containers[0].ExitCode != nil {
		t.Fatalf("expected nil exit code before any exit is recorded")
	}
}

func TestDescribeTasksReturnsFailuresForUnknownARNs(t *testing.T) {
	svc := newTestService(t)

	tdOut, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	task, err := svc.NewTaskRecord(cluster, tdOut.TaskDefinition, nil, "", "")
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}

	out, err := svc.DescribeTasks(&types.DescribeTasksInput{Tasks: []string{task.TaskArn, "arn:aws:ecs:us-east-1:000000000000:task/default/does-not-exist"}})
	if err != nil {
		t.Fatalf("DescribeTasks: %v", err)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].TaskArn != task.TaskArn {
		t.Fatalf("expected 1 described task, got %+v", out.Tasks)
	}
	if len(out.Failures) != 1 || out.Failures[0].Reason != "MISSING" {
		t.Fatalf("expected 1 MISSING failure, got %+v", out.Failures)
	}
}

func TestListTasksFiltersByCluster(t *testing.T) {
	svc := newTestService(t)

	tdOut, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	defaultCluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster default: %v", err)
	}
	otherOut, err := svc.CreateCluster(&types.CreateClusterInput{ClusterName: "other"})
	if err != nil {
		t.Fatalf("CreateCluster other: %v", err)
	}

	taskA, err := svc.NewTaskRecord(defaultCluster, tdOut.TaskDefinition, nil, "", "")
	if err != nil {
		t.Fatalf("NewTaskRecord A: %v", err)
	}
	if _, err := svc.NewTaskRecord(otherOut.Cluster, tdOut.TaskDefinition, nil, "", ""); err != nil {
		t.Fatalf("NewTaskRecord B: %v", err)
	}

	out, err := svc.ListTasks(&types.ListTasksInput{Cluster: "default"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(out.TaskArns) != 1 || out.TaskArns[0] != taskA.TaskArn {
		t.Fatalf("expected only task A in default cluster, got %v", out.TaskArns)
	}
}

// --- Services --------------------------------------------------------------------

func TestServiceCRUD(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	createOut, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "web-svc",
		TaskDefinition: "web",
		DesiredCount:   3,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if createOut.Service.DesiredCount != 3 {
		t.Fatalf("expected desired count 3, got %d", createOut.Service.DesiredCount)
	}
	if createOut.Service.Status != types.ServiceStatusActive {
		t.Fatalf("expected ACTIVE service, got %s", createOut.Service.Status)
	}

	// Duplicate create is rejected.
	if _, err := svc.CreateService(&types.CreateServiceInput{ServiceName: "web-svc", TaskDefinition: "web", DesiredCount: 1}); err == nil {
		t.Fatalf("expected error creating duplicate service")
	}

	desiredCount := 5
	updateOut, err := svc.UpdateService(&types.UpdateServiceInput{Service: "web-svc", DesiredCount: &desiredCount})
	if err != nil {
		t.Fatalf("UpdateService: %v", err)
	}
	if updateOut.Service.DesiredCount != 5 {
		t.Fatalf("expected desired count 5 after update, got %d", updateOut.Service.DesiredCount)
	}

	listOut, err := svc.ListServices(&types.ListServicesInput{})
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(listOut.ServiceArns) != 1 {
		t.Fatalf("expected 1 service ARN, got %v", listOut.ServiceArns)
	}

	descOut, err := svc.DescribeServices(&types.DescribeServicesInput{Services: []string{"web-svc", "missing-svc"}})
	if err != nil {
		t.Fatalf("DescribeServices: %v", err)
	}
	if len(descOut.Services) != 1 {
		t.Fatalf("expected 1 described service, got %+v", descOut.Services)
	}
	if len(descOut.Failures) != 1 {
		t.Fatalf("expected 1 failure for missing service, got %+v", descOut.Failures)
	}

	// Delete without Force fails while DesiredCount > 0.
	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: "web-svc"}); err == nil {
		t.Fatalf("expected error deleting service with nonzero desired count")
	}

	deleteOut, err := svc.DeleteService(&types.DeleteServiceInput{Service: "web-svc", Force: true})
	if err != nil {
		t.Fatalf("DeleteService (forced): %v", err)
	}
	if deleteOut.Service.Status != types.ServiceStatusInactive {
		t.Fatalf("expected INACTIVE service returned, got %s", deleteOut.Service.Status)
	}
}

func TestForceDeleteMarksTasksStoppedBeforeRemovingService(t *testing.T) {
	svc := newTestService(t)
	tdOut, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	created, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "drain-svc",
		TaskDefinition: tdOut.TaskDefinition.Family,
		DesiredCount:   1,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	task, err := svc.NewTaskRecord(cluster, tdOut.TaskDefinition, nil, types.LaunchTypeFargate, serviceGroup(created.Service.ServiceName))
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}
	if _, err := svc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
		t.Fatalf("SetTaskStatus: %v", err)
	}

	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: created.Service.ServiceName, Force: true}); err == nil {
		t.Fatal("expected force delete without a task drainer to refuse removing active tasks")
	}
	service, err := svc.store.GetService(cluster.ClusterArn, created.Service.ServiceName)
	if err != nil {
		t.Fatalf("service should remain while draining: %v", err)
	}
	if service.DesiredCount != 0 || service.Status != types.ServiceStatusDraining {
		t.Fatalf("service was not moved to draining state: %+v", service)
	}
	stopped, err := svc.GetTask(task.TaskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stopped.DesiredStatus != types.TaskDesiredStatusStopped {
		t.Fatalf("task desired status = %q, want STOPPED", stopped.DesiredStatus)
	}
}

// TestDeleteServiceScaledToZeroDrainsRunningTasks mirrors Terraform's destroy:
// UpdateService to desiredCount 0, then an immediate non-forced DeleteService
// while tasks are still running. AWS accepts this and drains the tasks.
func TestDeleteServiceScaledToZeroDrainsRunningTasks(t *testing.T) {
	svc := newTestService(t)
	tdOut, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	created, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "tf-svc",
		TaskDefinition: tdOut.TaskDefinition.Family,
		DesiredCount:   1,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	task, err := svc.NewTaskRecord(cluster, tdOut.TaskDefinition, nil, types.LaunchTypeFargate, serviceGroup(created.Service.ServiceName))
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}
	if _, err := svc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
		t.Fatalf("SetTaskStatus: %v", err)
	}
	zero := 0
	if _, err := svc.UpdateService(&types.UpdateServiceInput{Service: "tf-svc", DesiredCount: &zero}); err != nil {
		t.Fatalf("UpdateService: %v", err)
	}

	var drained string
	svc.SetTaskDrainer(func(_ context.Context, _ *types.Cluster, name string) error {
		drained = name
		_, err := svc.SetTaskStatus(task.TaskArn, types.TaskStatusStopped)
		return err
	})

	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: "tf-svc"}); err != nil {
		t.Fatalf("non-forced DeleteService at desiredCount 0: %v", err)
	}
	if drained != "tf-svc" {
		t.Fatalf("drainer called for %q, want tf-svc", drained)
	}
	// The service record is kept as an INACTIVE tombstone (not removed), so a
	// Terraform destroy's post-DeleteService DescribeServices poll for status
	// INACTIVE settles instead of hanging waiting for a record that no
	// longer exists.
	tombstone, err := svc.store.GetService(cluster.ClusterArn, "tf-svc")
	if err != nil {
		t.Fatalf("service record should remain as an INACTIVE tombstone after drain: %v", err)
	}
	if tombstone.Status != types.ServiceStatusInactive {
		t.Fatalf("tombstoned service status = %q, want INACTIVE", tombstone.Status)
	}
	if tombstone.DesiredCount != 0 || tombstone.RunningCount != 0 || tombstone.PendingCount != 0 {
		t.Fatalf("tombstoned service counts not zeroed: %+v", tombstone)
	}
	if tombstone.InactiveAt == nil {
		t.Fatal("tombstoned service should record InactiveAt")
	}
}

func TestCreateServiceRequiresKnownTaskDefinition(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.CreateService(&types.CreateServiceInput{ServiceName: "x", TaskDefinition: "nonexistent", DesiredCount: 1})
	if err == nil {
		t.Fatalf("expected error for unknown task definition")
	}
}

func TestConcurrentTaskMutatorsDoNotLoseUpdates(t *testing.T) {
	svc := newTestService(t)

	tdOut, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	task, err := svc.NewTaskRecord(cluster, tdOut.TaskDefinition, nil, "", "")
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}

	// Two independent updates to two different fields of the same container,
	// fired concurrently. If a read-modify-write races another, one field's
	// update gets clobbered by a save built from a stale clone.
	done := make(chan struct{}, 2)
	go func() {
		bindings := []types.NetworkBinding{{ContainerPort: 80, HostPort: 40000, Protocol: "tcp"}}
		_, _ = svc.SetContainerNetworkBindings(task.TaskArn, "web", bindings)
		done <- struct{}{}
	}()
	go func() {
		var code int64 = 137
		_, _ = svc.SetContainerExitCode(task.TaskArn, "web", &code, "OOMKilled")
		done <- struct{}{}
	}()
	<-done
	<-done

	final, err := svc.GetTask(task.TaskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	c := final.Containers[0]
	if len(c.NetworkBindings) != 1 || c.NetworkBindings[0].HostPort != 40000 {
		t.Fatalf("expected network binding to survive concurrent update, got %+v", c.NetworkBindings)
	}
	if c.ExitCode == nil || *c.ExitCode != 137 {
		t.Fatalf("expected exit code to survive concurrent update, got %+v", c.ExitCode)
	}
}

// --- Persistence -----------------------------------------------------------------

func TestPersistenceSurvivesStoreReload(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true

	store1 := NewStore(cfg)
	svc1 := NewService(cfg, store1)
	if err := svc1.Init(); err != nil {
		t.Fatalf("init service 1: %v", err)
	}

	if _, err := svc1.CreateCluster(&types.CreateClusterInput{ClusterName: "persisted"}); err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	tdOut, err := svc1.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	if _, err := svc1.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition (2): %v", err)
	}
	if _, err := svc1.CreateService(&types.CreateServiceInput{ServiceName: "svc", TaskDefinition: "web", DesiredCount: 2}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	cluster, err := svc1.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	task, err := svc1.NewTaskRecord(cluster, tdOut.TaskDefinition, nil, "", "")
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}

	// Force a synchronous flush instead of waiting on the ticker.
	store1.flushToDisk()

	store2 := NewStore(cfg)
	svc2 := NewService(cfg, store2)
	if err := svc2.Init(); err != nil {
		t.Fatalf("init service 2: %v", err)
	}

	desc, err := svc2.DescribeClusters(&types.DescribeClustersInput{Clusters: []string{"persisted"}})
	if err != nil {
		t.Fatalf("DescribeClusters after reload: %v", err)
	}
	if len(desc.Clusters) != 1 {
		t.Fatalf("expected persisted cluster to survive reload, got %+v", desc.Clusters)
	}

	tdDesc, err := svc2.DescribeTaskDefinition("web:2", nil)
	if err != nil {
		t.Fatalf("DescribeTaskDefinition after reload: %v", err)
	}
	if tdDesc.TaskDefinition.Revision != 2 {
		t.Fatalf("expected revision 2 to survive reload, got %d", tdDesc.TaskDefinition.Revision)
	}

	svcDesc, err := svc2.DescribeServices(&types.DescribeServicesInput{Services: []string{"svc"}})
	if err != nil {
		t.Fatalf("DescribeServices after reload: %v", err)
	}
	if len(svcDesc.Services) != 1 || svcDesc.Services[0].DesiredCount != 2 {
		t.Fatalf("expected service to survive reload, got %+v", svcDesc.Services)
	}

	taskDesc, err := svc2.DescribeTasks(&types.DescribeTasksInput{Tasks: []string{task.TaskArn}})
	if err != nil {
		t.Fatalf("DescribeTasks after reload: %v", err)
	}
	if len(taskDesc.Tasks) != 1 {
		t.Fatalf("expected task to survive reload, got %+v", taskDesc.Tasks)
	}
}

// --- DeleteService tombstone behaviour --------------------------------------

// TestDeleteServiceTombstoneKeepsDescribableAndBlocksListServices guards the
// Terraform destroy hang: DeleteService must leave the record describable as
// INACTIVE (not remove it), while ListServices and the cluster's
// ActiveServicesCount stop counting it.
func TestDeleteServiceTombstoneKeepsDescribableAndBlocksListServices(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "tombstone-svc",
		TaskDefinition: "web",
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: "tombstone-svc"}); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}

	// DescribeServices still finds it, as INACTIVE, not a MISSING failure.
	descOut, err := svc.DescribeServices(&types.DescribeServicesInput{Services: []string{"tombstone-svc"}})
	if err != nil {
		t.Fatalf("DescribeServices: %v", err)
	}
	if len(descOut.Failures) != 0 {
		t.Fatalf("expected no MISSING failures for a tombstoned service, got %+v", descOut.Failures)
	}
	if len(descOut.Services) != 1 || descOut.Services[0].Status != types.ServiceStatusInactive {
		t.Fatalf("expected 1 INACTIVE service, got %+v", descOut.Services)
	}

	// ListServices excludes it.
	listOut, err := svc.ListServices(&types.ListServicesInput{})
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(listOut.ServiceArns) != 0 {
		t.Fatalf("expected ListServices to exclude the tombstoned service, got %v", listOut.ServiceArns)
	}

	// The cluster's ActiveServicesCount excludes it too, so DeleteCluster is
	// not blocked by a tombstone.
	descCluster, err := svc.DescribeClusters(&types.DescribeClustersInput{Clusters: []string{cluster.ClusterArn}})
	if err != nil {
		t.Fatalf("DescribeClusters: %v", err)
	}
	if len(descCluster.Clusters) != 1 || descCluster.Clusters[0].ActiveServicesCount != 0 {
		t.Fatalf("expected ActiveServicesCount 0, got %+v", descCluster.Clusters)
	}
	if _, err := svc.DeleteCluster(cluster.ClusterName); err != nil {
		t.Fatalf("DeleteCluster should ignore an INACTIVE service: %v", err)
	}
}

// TestCreateServiceReplacesInactiveTombstone guards the other half of the
// tombstone design: a service recreated under the same name after a
// DeleteService must succeed, the way AWS lets a new service reuse a deleted
// service's name.
func TestCreateServiceReplacesInactiveTombstone(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "recreate-svc",
		TaskDefinition: "web",
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: "recreate-svc"}); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}

	recreated, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "recreate-svc",
		TaskDefinition: "web",
		DesiredCount:   2,
	})
	if err != nil {
		t.Fatalf("CreateService over a tombstone should succeed: %v", err)
	}
	if recreated.Service.Status != types.ServiceStatusActive || recreated.Service.DesiredCount != 2 {
		t.Fatalf("expected fresh ACTIVE service, got %+v", recreated.Service)
	}

	listOut, err := svc.ListServices(&types.ListServicesInput{})
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(listOut.ServiceArns) != 1 {
		t.Fatalf("expected 1 service after recreate, got %v", listOut.ServiceArns)
	}
}

// TestDeleteServiceOnInactiveServiceIsRejected guards AWS's behaviour of
// refusing a second DeleteService against an already-inactive service,
// instead of silently succeeding twice.
func TestDeleteServiceOnInactiveServiceIsRejected(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "double-delete-svc",
		TaskDefinition: "web",
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: "double-delete-svc"}); err != nil {
		t.Fatalf("first DeleteService: %v", err)
	}

	_, err := svc.DeleteService(&types.DeleteServiceInput{Service: "double-delete-svc"})
	if err == nil {
		t.Fatal("expected second DeleteService against an INACTIVE service to fail")
	}
	svcErr, ok := err.(*ServiceError)
	if !ok {
		t.Fatalf("expected *ServiceError, got %T (%v)", err, err)
	}
	if svcErr.Code != "ServiceNotActiveException" {
		t.Fatalf("expected ServiceNotActiveException, got %s", svcErr.Code)
	}

	// UpdateService against the same tombstone must be rejected too.
	desired := 1
	if _, err := svc.UpdateService(&types.UpdateServiceInput{Service: "double-delete-svc", DesiredCount: &desired}); err == nil {
		t.Fatal("expected UpdateService against an INACTIVE service to fail")
	}
}

// TestPruneInactiveServicesRemovesAgedTombstones exercises the store-level
// housekeeping directly: a tombstone older than cutoff is removed, a fresh
// one is kept.
func TestPruneInactiveServicesRemovesAgedTombstones(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "old-tombstone",
		TaskDefinition: "web",
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: "old-tombstone"}); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "fresh-tombstone",
		TaskDefinition: "web",
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: "fresh-tombstone"}); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}

	// Back-date the "old" tombstone directly in the store, past a cutoff that
	// the "fresh" one is still inside.
	old, err := svc.store.GetService(cluster.ClusterArn, "old-tombstone")
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	past := time.Now().UTC().Add(-2 * time.Hour)
	old.InactiveAt = &past
	if err := svc.store.SaveService(old); err != nil {
		t.Fatalf("SaveService: %v", err)
	}

	removed, err := svc.store.PruneInactiveServices(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("PruneInactiveServices: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 pruned tombstone, got %d", removed)
	}
	if _, err := svc.store.GetService(cluster.ClusterArn, "old-tombstone"); err == nil {
		t.Fatal("expected old tombstone to be pruned")
	}
	if _, err := svc.store.GetService(cluster.ClusterArn, "fresh-tombstone"); err != nil {
		t.Fatalf("expected fresh tombstone to survive prune: %v", err)
	}
}

// --- CreateService defaults / drift-preventing echo -------------------------

// TestCreateServiceEchoesDriftPreventingDefaults guards the second Terraform
// bug: DescribeServices must echo schedulingStrategy, networkConfiguration,
// launchType, platformVersion, and deploymentConfiguration back exactly as
// given (or with AWS's documented defaults filled in), or every subsequent
// `terraform plan` sees drift on these ForceNew attributes and replaces the
// service.
func TestCreateServiceEchoesDriftPreventingDefaults(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	netCfg := &types.NetworkConfiguration{
		AwsvpcConfiguration: &types.AwsVpcConfiguration{
			Subnets:        []string{"subnet-1", "subnet-2"},
			SecurityGroups: []string{"sg-1"},
			AssignPublicIp: "ENABLED",
		},
	}
	out, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:          "drift-svc",
		TaskDefinition:       "web",
		DesiredCount:         1,
		NetworkConfiguration: netCfg,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	got := out.Service
	if got.LaunchType != types.LaunchTypeFargate {
		t.Fatalf("expected default launchType FARGATE, got %q", got.LaunchType)
	}
	if got.SchedulingStrategy != "REPLICA" {
		t.Fatalf("expected default schedulingStrategy REPLICA, got %q", got.SchedulingStrategy)
	}
	if got.PlatformVersion != "LATEST" {
		t.Fatalf("expected default platformVersion LATEST for FARGATE, got %q", got.PlatformVersion)
	}
	if got.DeploymentConfiguration == nil || got.DeploymentConfiguration.MaximumPercent != 200 || got.DeploymentConfiguration.MinimumHealthyPercent != 100 {
		t.Fatalf("expected default deploymentConfiguration 200/100, got %+v", got.DeploymentConfiguration)
	}
	if got.NetworkConfiguration == nil || got.NetworkConfiguration.AwsvpcConfiguration == nil {
		t.Fatal("expected networkConfiguration to be echoed back")
	}
	avc := got.NetworkConfiguration.AwsvpcConfiguration
	if len(avc.Subnets) != 2 || avc.Subnets[0] != "subnet-1" || avc.SecurityGroups[0] != "sg-1" || avc.AssignPublicIp != "ENABLED" {
		t.Fatalf("networkConfiguration not echoed exactly, got %+v", avc)
	}

	// DescribeServices returns the same values, not just CreateService's
	// response.
	descOut, err := svc.DescribeServices(&types.DescribeServicesInput{Services: []string{"drift-svc"}})
	if err != nil {
		t.Fatalf("DescribeServices: %v", err)
	}
	if len(descOut.Services) != 1 {
		t.Fatalf("expected 1 described service, got %+v", descOut.Services)
	}
	desc := descOut.Services[0]
	if desc.SchedulingStrategy != "REPLICA" || desc.PlatformVersion != "LATEST" {
		t.Fatalf("DescribeServices dropped defaults: %+v", desc)
	}
	if desc.NetworkConfiguration == nil || desc.NetworkConfiguration.AwsvpcConfiguration == nil ||
		len(desc.NetworkConfiguration.AwsvpcConfiguration.Subnets) != 2 {
		t.Fatalf("DescribeServices dropped networkConfiguration: %+v", desc)
	}

	// Mutating the input after the call must not retroactively change the
	// stored record (store.SaveService must deep-copy pointer fields).
	netCfg.AwsvpcConfiguration.Subnets[0] = "mutated"
	descAgain, err := svc.DescribeServices(&types.DescribeServicesInput{Services: []string{"drift-svc"}})
	if err != nil {
		t.Fatalf("DescribeServices: %v", err)
	}
	if descAgain.Services[0].NetworkConfiguration.AwsvpcConfiguration.Subnets[0] != "subnet-1" {
		t.Fatal("service record aliases the caller's NetworkConfiguration slice instead of cloning it")
	}
}

// TestCreateServiceAcceptsDaemonSchedulingStrategy guards that DAEMON is
// accepted and stored (not just REPLICA), even though Tarn's reconcile loop
// treats every service identically.
func TestCreateServiceAcceptsDaemonSchedulingStrategy(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	out, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:        "daemon-svc",
		TaskDefinition:     "web",
		DesiredCount:       1,
		SchedulingStrategy: "daemon",
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if out.Service.SchedulingStrategy != "DAEMON" {
		t.Fatalf("expected schedulingStrategy DAEMON, got %q", out.Service.SchedulingStrategy)
	}

	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:        "bad-strategy-svc",
		TaskDefinition:     "web",
		SchedulingStrategy: "NOT_A_STRATEGY",
	}); err == nil {
		t.Fatal("expected an invalid schedulingStrategy to be rejected")
	}
}

// --- Tagging --------------------------------------------------------------

// TestCreateClusterTagsAndListTagsForResource guards that tags supplied on
// CreateCluster are stored and retrievable via ListTagsForResource, and are
// gated behind Include=["TAGS"] on DescribeClusters (matching real ECS,
// which omits tags unless asked for).
func TestCreateClusterTagsAndListTagsForResource(t *testing.T) {
	svc := newTestService(t)

	out, err := svc.CreateCluster(&types.CreateClusterInput{
		ClusterName: "tagged-cluster",
		Tags:        []types.Tag{{Key: "env", Value: "prod"}, {Key: "team", Value: "platform"}},
	})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	if len(out.Cluster.Tags) != 2 {
		t.Fatalf("expected 2 tags on created cluster, got %+v", out.Cluster.Tags)
	}

	listed, err := svc.ListTagsForResource(&types.ListTagsForResourceInput{ResourceArn: out.Cluster.ClusterArn})
	if err != nil {
		t.Fatalf("ListTagsForResource: %v", err)
	}
	if len(listed.Tags) != 2 {
		t.Fatalf("expected 2 tags from ListTagsForResource, got %+v", listed.Tags)
	}

	// DescribeClusters without Include=["TAGS"] must not echo tags.
	withoutInclude, err := svc.DescribeClusters(&types.DescribeClustersInput{Clusters: []string{"tagged-cluster"}})
	if err != nil {
		t.Fatalf("DescribeClusters: %v", err)
	}
	if len(withoutInclude.Clusters[0].Tags) != 0 {
		t.Fatalf("expected no tags without Include=TAGS, got %+v", withoutInclude.Clusters[0].Tags)
	}

	// DescribeClusters with Include=["TAGS"] must echo them.
	withInclude, err := svc.DescribeClusters(&types.DescribeClustersInput{
		Clusters: []string{"tagged-cluster"},
		Include:  []string{"TAGS"},
	})
	if err != nil {
		t.Fatalf("DescribeClusters with Include=TAGS: %v", err)
	}
	if len(withInclude.Clusters[0].Tags) != 2 {
		t.Fatalf("expected 2 tags with Include=TAGS, got %+v", withInclude.Clusters[0].Tags)
	}
}

// TestTagResourceUntagResourceRoundTrip covers add/replace/remove across all
// four taggable resource kinds (cluster, task definition, service, task).
func TestTagResourceUntagResourceRoundTrip(t *testing.T) {
	svc := newTestService(t)

	clusterOut, err := svc.CreateCluster(&types.CreateClusterInput{ClusterName: "rt-cluster"})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	tdOut, err := svc.RegisterTaskDefinition(webTaskDefInput())
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	svcOut, err := svc.CreateService(&types.CreateServiceInput{
		Cluster:        "rt-cluster",
		ServiceName:    "rt-service",
		TaskDefinition: "web",
		DesiredCount:   0,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	task, err := svc.NewTaskRecord(clusterOut.Cluster, tdOut.TaskDefinition, nil, "", "")
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}

	for _, arn := range []string{clusterOut.Cluster.ClusterArn, tdOut.TaskDefinition.TaskDefinitionArn, svcOut.Service.ServiceArn, task.TaskArn} {
		if _, err := svc.TagResource(&types.TagResourceInput{
			ResourceArn: arn,
			Tags:        []types.Tag{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}},
		}); err != nil {
			t.Fatalf("TagResource(%s): %v", arn, err)
		}
		listed, err := svc.ListTagsForResource(&types.ListTagsForResourceInput{ResourceArn: arn})
		if err != nil {
			t.Fatalf("ListTagsForResource(%s): %v", arn, err)
		}
		if len(listed.Tags) != 2 {
			t.Fatalf("ListTagsForResource(%s): expected 2 tags, got %+v", arn, listed.Tags)
		}

		// TagResource replaces an existing key's value rather than duplicating it.
		if _, err := svc.TagResource(&types.TagResourceInput{
			ResourceArn: arn,
			Tags:        []types.Tag{{Key: "a", Value: "updated"}},
		}); err != nil {
			t.Fatalf("TagResource replace(%s): %v", arn, err)
		}
		listed, err = svc.ListTagsForResource(&types.ListTagsForResourceInput{ResourceArn: arn})
		if err != nil {
			t.Fatalf("ListTagsForResource(%s): %v", arn, err)
		}
		if len(listed.Tags) != 2 {
			t.Fatalf("ListTagsForResource(%s): expected key replace not append, got %+v", arn, listed.Tags)
		}
		for _, tag := range listed.Tags {
			if tag.Key == "a" && tag.Value != "updated" {
				t.Fatalf("ListTagsForResource(%s): key a not replaced, got %+v", arn, listed.Tags)
			}
		}

		if _, err := svc.UntagResource(&types.UntagResourceInput{ResourceArn: arn, TagKeys: []string{"a"}}); err != nil {
			t.Fatalf("UntagResource(%s): %v", arn, err)
		}
		listed, err = svc.ListTagsForResource(&types.ListTagsForResourceInput{ResourceArn: arn})
		if err != nil {
			t.Fatalf("ListTagsForResource(%s): %v", arn, err)
		}
		if len(listed.Tags) != 1 || listed.Tags[0].Key != "b" {
			t.Fatalf("UntagResource(%s) did not remove key a, got %+v", arn, listed.Tags)
		}
	}
}

// TestTagResourceUnknownArnFails guards that tagging a nonexistent resource
// reports an error, matching real ECS, rather than silently succeeding.
func TestTagResourceUnknownArnFails(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.TagResource(&types.TagResourceInput{
		ResourceArn: "arn:aws:ecs:us-east-1:000000000000:cluster/does-not-exist",
		Tags:        []types.Tag{{Key: "a", Value: "1"}},
	}); err == nil {
		t.Fatal("expected TagResource against an unknown cluster ARN to fail")
	}
	if _, err := svc.ListTagsForResource(&types.ListTagsForResourceInput{
		ResourceArn: "arn:aws:ecs:us-east-1:000000000000:task/default/does-not-exist",
	}); err == nil {
		t.Fatal("expected ListTagsForResource against an unknown task ARN to fail")
	}
	if _, err := svc.TagResource(&types.TagResourceInput{
		ResourceArn: "not-an-arn-at-all",
		Tags:        []types.Tag{{Key: "a", Value: "1"}},
	}); err == nil {
		t.Fatal("expected TagResource against an unresolvable ARN shape to fail")
	}
}

// TestTagValidation guards the limits real ECS enforces: max 50 tags, key
// 1-128 chars, value <=256 chars, no "aws:"-prefixed keys, no duplicate keys
// within one request.
func TestTagValidation(t *testing.T) {
	svc := newTestService(t)
	clusterOut, err := svc.CreateCluster(&types.CreateClusterInput{ClusterName: "validate-cluster"})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}

	cases := []struct {
		name string
		tags []types.Tag
	}{
		{"empty key", []types.Tag{{Key: "", Value: "x"}}},
		{"key too long", []types.Tag{{Key: string(make([]byte, 129)), Value: "x"}}},
		{"value too long", []types.Tag{{Key: "k", Value: string(make([]byte, 257))}}},
		{"reserved aws prefix", []types.Tag{{Key: "aws:reserved", Value: "x"}}},
		{"duplicate key", []types.Tag{{Key: "dup", Value: "1"}, {Key: "dup", Value: "2"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.TagResource(&types.TagResourceInput{
				ResourceArn: clusterOut.Cluster.ClusterArn,
				Tags:        tc.tags,
			}); err == nil {
				t.Fatalf("expected tag validation to reject %s", tc.name)
			}
		})
	}

	// Exceeding 50 tags total (existing + new) is also rejected.
	var many []types.Tag
	for i := 0; i < 51; i++ {
		many = append(many, types.Tag{Key: string(rune('a'+i%26)) + string(rune(i)), Value: "v"})
	}
	if _, err := svc.CreateCluster(&types.CreateClusterInput{
		ClusterName: "too-many-tags",
		Tags:        many,
	}); err == nil {
		t.Fatal("expected CreateCluster to reject more than 50 tags")
	}
}

// TestRegisterTaskDefinitionTagsAtTopLevel guards that tags never appear
// inside the TaskDefinition wire/domain object itself: they're a sibling
// field on Register/DescribeTaskDefinitionOutput, and DescribeTaskDefinition
// only populates them when include contains "TAGS".
func TestRegisterTaskDefinitionTagsAtTopLevel(t *testing.T) {
	svc := newTestService(t)

	in := webTaskDefInput()
	in.Tags = []types.Tag{{Key: "env", Value: "test"}}
	out, err := svc.RegisterTaskDefinition(in)
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	if len(out.Tags) != 1 {
		t.Fatalf("expected RegisterTaskDefinitionOutput.Tags to carry the tag, got %+v", out.Tags)
	}

	withoutInclude, err := svc.DescribeTaskDefinition("web", nil)
	if err != nil {
		t.Fatalf("DescribeTaskDefinition: %v", err)
	}
	if len(withoutInclude.Tags) != 0 {
		t.Fatalf("expected no tags without Include=TAGS, got %+v", withoutInclude.Tags)
	}

	withInclude, err := svc.DescribeTaskDefinition("web", []string{"TAGS"})
	if err != nil {
		t.Fatalf("DescribeTaskDefinition with Include=TAGS: %v", err)
	}
	if len(withInclude.Tags) != 1 {
		t.Fatalf("expected 1 tag with Include=TAGS, got %+v", withInclude.Tags)
	}
}

// TestCreateServiceTagsAndDescribeGating mirrors the cluster test for
// services: CreateService.Tags is stored, and DescribeServices only echoes
// it back when Include contains "TAGS".
func TestCreateServiceTagsAndDescribeGating(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.RegisterTaskDefinition(webTaskDefInput()); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "tagged-service",
		TaskDefinition: "web",
		DesiredCount:   0,
		Tags:           []types.Tag{{Key: "env", Value: "prod"}},
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	withoutInclude, err := svc.DescribeServices(&types.DescribeServicesInput{Services: []string{"tagged-service"}})
	if err != nil {
		t.Fatalf("DescribeServices: %v", err)
	}
	if len(withoutInclude.Services[0].Tags) != 0 {
		t.Fatalf("expected no tags without Include=TAGS, got %+v", withoutInclude.Services[0].Tags)
	}

	withInclude, err := svc.DescribeServices(&types.DescribeServicesInput{
		Services: []string{"tagged-service"},
		Include:  []string{"TAGS"},
	})
	if err != nil {
		t.Fatalf("DescribeServices with Include=TAGS: %v", err)
	}
	if len(withInclude.Services[0].Tags) != 1 || withInclude.Services[0].Tags[0].Key != "env" {
		t.Fatalf("expected 1 tag with Include=TAGS, got %+v", withInclude.Services[0].Tags)
	}
}
