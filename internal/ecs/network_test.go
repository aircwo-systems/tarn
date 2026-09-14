package ecs

import (
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestNormalizeNetworkModeDefaultsToBridge(t *testing.T) {
	got, err := normalizeNetworkMode("")
	if err != nil {
		t.Fatalf("normalizeNetworkMode: %v", err)
	}
	if got != types.NetworkModeBridge {
		t.Fatalf("normalized empty mode = %q, want %q", got, types.NetworkModeBridge)
	}
}

func TestValidateNetworkModeSupportsPortMappingsForBridgeAndAwsvpc(t *testing.T) {
	containers := []types.ContainerDefinition{{
		Name:         "app",
		PortMappings: []types.PortMapping{{ContainerPort: 8080}},
	}}
	for _, mode := range []string{types.NetworkModeBridge, types.NetworkModeAwsVPC} {
		if err := validateNetworkMode(mode, containers); err != nil {
			t.Errorf("validateNetworkMode(%q): %v", mode, err)
		}
	}
}

func TestValidateNetworkModeRejectsPortsForHostAndNone(t *testing.T) {
	containers := []types.ContainerDefinition{{
		Name:         "app",
		PortMappings: []types.PortMapping{{ContainerPort: 8080}},
	}}
	for _, mode := range []string{types.NetworkModeHost, types.NetworkModeNone} {
		if err := validateNetworkMode(mode, containers); err == nil {
			t.Errorf("validateNetworkMode(%q) accepted port mappings", mode)
		}
	}
}

func TestValidateNetworkModeRejectsUnsupportedMode(t *testing.T) {
	if err := validateNetworkMode("overlay", nil); err == nil {
		t.Fatal("validateNetworkMode accepted unsupported mode")
	}
}

func TestRegisterTaskDefinitionRejectsUnsupportedNetworkCombinations(t *testing.T) {
	tests := []struct {
		name string
		in   *types.RegisterTaskDefinitionInput
	}{
		{
			name: "unsupported mode",
			in: &types.RegisterTaskDefinitionInput{
				Family:               "unsupported-mode",
				NetworkMode:          "overlay",
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app", Image: "example/app:latest"}},
			},
		},
		{
			name: "host mode ports",
			in: &types.RegisterTaskDefinitionInput{
				Family:               "host-ports",
				NetworkMode:          types.NetworkModeHost,
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app", Image: "example/app:latest", PortMappings: []types.PortMapping{{ContainerPort: 8080}}}},
			},
		},
		{
			name: "none mode ports",
			in: &types.RegisterTaskDefinitionInput{
				Family:               "none-ports",
				NetworkMode:          types.NetworkModeNone,
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app", Image: "example/app:latest", PortMappings: []types.PortMapping{{ContainerPort: 8080}}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t)
			if _, err := svc.RegisterTaskDefinition(tt.in); err == nil {
				t.Fatal("expected task definition validation error")
			}
		})
	}
}
