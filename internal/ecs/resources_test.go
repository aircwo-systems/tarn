package ecs

import (
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestResolveTaskContainerResourcesUsesContainerValuesAndDistributesTaskRemainders(t *testing.T) {
	td := &types.TaskDefinition{
		Cpu:    "1024",
		Memory: "512",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "explicit", Cpu: 256, Memory: 128, MemoryReservation: 64},
			{Name: "first-unset"},
			{Name: "second-unset"},
		},
	}

	got, err := resolveTaskContainerResources(td)
	if err != nil {
		t.Fatalf("resolveTaskContainerResources: %v", err)
	}

	want := map[string]taskContainerResources{
		"explicit":     {cpu: 256, memory: 128, memoryReservation: 64},
		"first-unset":  {cpu: 384, memory: 192},
		"second-unset": {cpu: 384, memory: 192},
	}
	if len(got) != len(want) {
		t.Fatalf("got resources for %d containers, want %d: %+v", len(got), len(want), got)
	}
	for name, wantResource := range want {
		if got[name] != wantResource {
			t.Errorf("resources[%q] = %+v, want %+v", name, got[name], wantResource)
		}
	}
}

func TestResolveTaskContainerResourcesAssignsRemainderDeterministically(t *testing.T) {
	got, err := resolveTaskContainerResources(&types.TaskDefinition{
		Cpu:    "5",
		Memory: "4",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "a"},
			{Name: "b"},
			{Name: "c"},
		},
	})
	if err != nil {
		t.Fatalf("resolveTaskContainerResources: %v", err)
	}

	if got["a"].cpu != 2 || got["b"].cpu != 2 || got["c"].cpu != 1 {
		t.Fatalf("unexpected CPU allocation: %+v", got)
	}
	if got["a"].memory != 2 || got["b"].memory != 1 || got["c"].memory != 1 {
		t.Fatalf("unexpected memory allocation: %+v", got)
	}
}

func TestResolveTaskContainerResourcesRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		td   *types.TaskDefinition
	}{
		{
			name: "invalid task CPU",
			td: &types.TaskDefinition{
				Cpu:                  "two",
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app"}},
			},
		},
		{
			name: "negative task memory",
			td: &types.TaskDefinition{
				Memory:               "-1",
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app"}},
			},
		},
		{
			name: "negative container CPU",
			td: &types.TaskDefinition{
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app", Cpu: -1}},
			},
		},
		{
			name: "reservation above hard limit",
			td: &types.TaskDefinition{
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app", Memory: 128, MemoryReservation: 256}},
			},
		},
		{
			name: "reservation above distributed task memory",
			td: &types.TaskDefinition{
				Memory:               "128",
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app", MemoryReservation: 256}},
			},
		},
		{
			name: "duplicate container names",
			td: &types.TaskDefinition{
				ContainerDefinitions: []types.ContainerDefinition{{Name: "app"}, {Name: "app"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolveTaskContainerResources(tt.td); err == nil {
				t.Fatal("expected invalid resource error")
			}
		})
	}
}
