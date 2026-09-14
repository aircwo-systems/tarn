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
)

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
