package ecs

import (
	"fmt"
	"strings"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// normalizeNetworkMode applies the ECS default while retaining the canonical
// spelling used by the Docker-backed runner. An empty mode is the historical
// bridge behavior and is normalized only at the control-plane boundary.
func normalizeNetworkMode(mode string) (string, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return types.NetworkModeBridge, nil
	}

	switch mode {
	case types.NetworkModeAwsVPC, types.NetworkModeBridge, types.NetworkModeHost, types.NetworkModeNone:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported ECS network mode %q", mode)
	}
}

func validateNetworkMode(mode string, containers []types.ContainerDefinition) error {
	normalized, err := normalizeNetworkMode(mode)
	if err != nil {
		return err
	}
	if normalized != types.NetworkModeHost && normalized != types.NetworkModeNone {
		return nil
	}

	for _, cd := range containers {
		if len(cd.PortMappings) > 0 {
			return fmt.Errorf("ECS network mode %q does not support port mappings", normalized)
		}
	}
	return nil
}
