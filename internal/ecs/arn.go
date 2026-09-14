package ecs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aircwo-systems/tarn/internal/config"
)

// clusterARN builds the ARN for a cluster name.
func clusterARN(cfg *config.Config, name string) string {
	return fmt.Sprintf("arn:aws:ecs:%s:%s:cluster/%s", cfg.Region, cfg.AccountID, name)
}

// taskDefinitionARN builds the ARN for one family:revision.
func taskDefinitionARN(cfg *config.Config, family string, revision int) string {
	return fmt.Sprintf("arn:aws:ecs:%s:%s:task-definition/%s:%d", cfg.Region, cfg.AccountID, family, revision)
}

// serviceARN builds the ARN for a service within a cluster.
func serviceARN(cfg *config.Config, clusterName, serviceName string) string {
	return fmt.Sprintf("arn:aws:ecs:%s:%s:service/%s/%s", cfg.Region, cfg.AccountID, clusterName, serviceName)
}

// taskARN builds the ARN for a task within a cluster.
func taskARN(cfg *config.Config, clusterName, taskID string) string {
	return fmt.Sprintf("arn:aws:ecs:%s:%s:task/%s/%s", cfg.Region, cfg.AccountID, clusterName, taskID)
}

// containerARN builds the ARN for a container within a task.
func containerARN(cfg *config.Config, clusterName, taskID, containerID string) string {
	return fmt.Sprintf("arn:aws:ecs:%s:%s:container/%s/%s/%s", cfg.Region, cfg.AccountID, clusterName, taskID, containerID)
}

// clusterNameFromRef extracts a cluster's short name from either a bare name
// or a full "arn:aws:ecs:...:cluster/<name>" ARN.
func clusterNameFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if idx := strings.Index(ref, ":cluster/"); idx >= 0 {
		return ref[idx+len(":cluster/"):]
	}
	return ref
}

// serviceNameFromRef extracts a service's short name from either a bare name
// or a full "arn:aws:ecs:...:service/<cluster>/<name>" ARN.
func serviceNameFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if idx := strings.Index(ref, ":service/"); idx >= 0 {
		rest := ref[idx+len(":service/"):]
		if slash := strings.LastIndex(rest, "/"); slash >= 0 {
			return rest[slash+1:]
		}
		return rest
	}
	return ref
}

// parseTaskDefinitionRef parses a family, "family:revision", or full
// "arn:aws:ecs:...:task-definition/family:revision" reference. hasRevision is
// false when the caller gave a bare family name, meaning "latest ACTIVE".
func parseTaskDefinitionRef(ref string) (family string, revision int, hasRevision bool, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", 0, false, fmt.Errorf("task definition is required")
	}

	if idx := strings.Index(ref, ":task-definition/"); idx >= 0 {
		ref = ref[idx+len(":task-definition/"):]
	}

	if idx := strings.LastIndex(ref, ":"); idx >= 0 {
		family = ref[:idx]
		revStr := ref[idx+1:]
		rev, convErr := strconv.Atoi(revStr)
		if convErr != nil || rev <= 0 {
			return "", 0, false, fmt.Errorf("invalid task definition revision %q", revStr)
		}
		if family == "" {
			return "", 0, false, fmt.Errorf("task definition family is required")
		}
		return family, rev, true, nil
	}

	if ref == "" {
		return "", 0, false, fmt.Errorf("task definition family is required")
	}
	return ref, 0, false, nil
}

// taskIDFromRef extracts a task's ID/ARN suffix used to key store lookups.
// Accepts a full task ARN or a bare task ID.
func taskIDFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if idx := strings.LastIndex(ref, "/"); idx >= 0 {
		return ref[idx+1:]
	}
	return ref
}
