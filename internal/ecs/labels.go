package ecs

// Docker label keys applied to every task container. The engine is shared
// across accounts (sharedDeps.eng), so these labels are the only thing
// preventing two accounts with an identically named cluster from colliding
// during reconcile or startup reaping.
const (
	labelAccount = "tarn.account"
	labelCluster = "tarn.cluster"
	labelService = "tarn.service"
	labelTaskArn = "tarn.task-arn"
	// labelVolumeScope marks a Docker named volume Tarn created for an ECS
	// task definition volume, distinguishing "task" (removed when the owning
	// task stops) from "shared" (never removed automatically).
	labelVolumeScope = "tarn.volume-scope"
)

const (
	volumeScopeTask   = "task"
	volumeScopeShared = "shared"
)

// taskVolumeLabels builds the label set for a Docker named volume Tarn
// creates on behalf of a task definition Volume entry. taskArn is empty for
// a shared-scope volume: it is not owned by any single task, so tagging it
// with one task's ARN would be misleading for recovery's orphan sweep.
func taskVolumeLabels(accountID, taskArn, scope string) map[string]string {
	labels := map[string]string{
		labelAccount:     accountID,
		labelVolumeScope: scope,
	}
	if taskArn != "" {
		labels[labelTaskArn] = taskArn
	}
	return labels
}

// selectorForAccountTaskVolumes matches every task-scoped Docker volume
// Tarn created for accountID, used by startup recovery to find volumes left
// behind by tasks no longer in the store.
func selectorForAccountTaskVolumes(accountID string) map[string]string {
	return map[string]string{
		labelAccount:     accountID,
		labelVolumeScope: volumeScopeTask,
	}
}

// taskLabels builds the full label set for a task's containers. serviceName
// is omitted entirely (not set to "") for standalone RunTask tasks: an
// empty-valued label would still match a label-equality filter that doesn't
// specify tarn.service, which is exactly the account/cluster-wide selector
// startup reaping uses. Omitting the key means selectorForService (which
// does specify a value) can never accidentally count a standalone task, and
// selectorForAccount still sees every container.
func taskLabels(accountID, clusterName, serviceName, taskArn string) map[string]string {
	labels := map[string]string{
		labelAccount: accountID,
		labelCluster: clusterName,
		labelTaskArn: taskArn,
	}
	if serviceName != "" {
		labels[labelService] = serviceName
	}
	return labels
}

// selectorForAccount matches every task container belonging to accountID,
// regardless of cluster or service. Used for whole-account teardown
// (tarn flush, account stop).
func selectorForAccount(accountID string) map[string]string {
	return map[string]string{labelAccount: accountID}
}

// selectorForService matches every task container launched on behalf of one
// ECS service. This is what the reconcile loop uses to count RUNNING tasks
// toward DesiredCount.
func selectorForService(accountID, clusterName, serviceName string) map[string]string {
	return map[string]string{
		labelAccount: accountID,
		labelCluster: clusterName,
		labelService: serviceName,
	}
}
