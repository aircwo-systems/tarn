package ecs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// taskContainerResources is expressed in ECS units. The engine converts these
// values to Docker's CPU and byte-based resource fields at container create
// time.
type taskContainerResources struct {
	cpu               int64
	memory            int64
	memoryReservation int64
}

func parseTaskResource(raw, name string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer, got %q", name, raw)
	}
	return value, nil
}

// resolveTaskContainerResources maps task-level CPU and memory to the
// per-container values Docker requires. Explicit container values win. When
// a task-level value must fill multiple unspecified containers, the remaining
// allocation is divided deterministically, with the remainder assigned to
// the first containers.
func resolveTaskContainerResources(td *types.TaskDefinition) (map[string]taskContainerResources, error) {
	if td == nil {
		return nil, fmt.Errorf("task definition is required")
	}

	taskCPU, err := parseTaskResource(td.Cpu, "task CPU")
	if err != nil {
		return nil, err
	}
	taskMemory, err := parseTaskResource(td.Memory, "task memory")
	if err != nil {
		return nil, err
	}

	resources := make(map[string]taskContainerResources, len(td.ContainerDefinitions))
	var unspecifiedCPU []string
	var unspecifiedMemory []string
	var usedCPU, usedMemory int64
	for _, cd := range td.ContainerDefinitions {
		if _, exists := resources[cd.Name]; exists {
			return nil, fmt.Errorf("container definition names must be unique, duplicate: %s", cd.Name)
		}
		if cd.Cpu < 0 {
			return nil, fmt.Errorf("container %s CPU must not be negative", cd.Name)
		}
		if cd.Memory < 0 {
			return nil, fmt.Errorf("container %s memory must not be negative", cd.Name)
		}
		if cd.MemoryReservation < 0 {
			return nil, fmt.Errorf("container %s memory reservation must not be negative", cd.Name)
		}
		if cd.Memory > 0 && cd.MemoryReservation > cd.Memory {
			return nil, fmt.Errorf("container %s memory reservation cannot exceed memory", cd.Name)
		}

		resource := taskContainerResources{
			cpu:               int64(cd.Cpu),
			memory:            int64(cd.Memory),
			memoryReservation: int64(cd.MemoryReservation),
		}
		resources[cd.Name] = resource

		if cd.Cpu == 0 {
			unspecifiedCPU = append(unspecifiedCPU, cd.Name)
		} else {
			usedCPU += int64(cd.Cpu)
		}
		if cd.Memory == 0 {
			unspecifiedMemory = append(unspecifiedMemory, cd.Name)
		} else {
			usedMemory += int64(cd.Memory)
		}
	}

	distributeRemaining := func(total, used int64, names []string, set func(*taskContainerResources, int64)) {
		if total <= used || len(names) == 0 {
			return
		}
		remaining := total - used
		base := remaining / int64(len(names))
		remainder := remaining % int64(len(names))
		for i, name := range names {
			allocation := base
			if int64(i) < remainder {
				allocation++
			}
			resource := resources[name]
			set(&resource, allocation)
			resources[name] = resource
		}
	}

	distributeRemaining(taskCPU, usedCPU, unspecifiedCPU, func(resource *taskContainerResources, value int64) {
		resource.cpu = value
	})
	distributeRemaining(taskMemory, usedMemory, unspecifiedMemory, func(resource *taskContainerResources, value int64) {
		resource.memory = value
	})
	for name, resource := range resources {
		if resource.memory > 0 && resource.memoryReservation > resource.memory {
			return nil, fmt.Errorf("container %s memory reservation cannot exceed memory", name)
		}
	}

	return resources, nil
}

// applyResourceOverrides returns a shallow copy of td with task-level
// Cpu/Memory and per-container Cpu/Memory/MemoryReservation replaced by
// whatever RunTask's overrides supplied, so resolveTaskContainerResources
// sees the overridden values without mutating the stored task definition.
// A blank override field leaves the task definition's own value in place.
func applyResourceOverrides(td *types.TaskDefinition, overrides *types.TaskOverride) *types.TaskDefinition {
	if td == nil {
		return td
	}
	if overrides == nil || (overrides.Cpu == "" && overrides.Memory == "" && !anyContainerResourceOverride(overrides)) {
		return td
	}

	out := *td
	if overrides.Cpu != "" {
		out.Cpu = overrides.Cpu
	}
	if overrides.Memory != "" {
		out.Memory = overrides.Memory
	}
	out.ContainerDefinitions = make([]types.ContainerDefinition, len(td.ContainerDefinitions))
	copy(out.ContainerDefinitions, td.ContainerDefinitions)
	for i, cd := range out.ContainerDefinitions {
		co := containerOverrideFor(overrides, cd.Name)
		if co == nil {
			continue
		}
		if co.Cpu != 0 {
			cd.Cpu = co.Cpu
		}
		if co.Memory != 0 {
			cd.Memory = co.Memory
		}
		if co.MemoryReservation != 0 {
			cd.MemoryReservation = co.MemoryReservation
		}
		out.ContainerDefinitions[i] = cd
	}
	return &out
}

func anyContainerResourceOverride(overrides *types.TaskOverride) bool {
	for _, co := range overrides.ContainerOverrides {
		if co.Cpu != 0 || co.Memory != 0 || co.MemoryReservation != 0 {
			return true
		}
	}
	return false
}

func validateTaskDefinition(td *types.TaskDefinition) error {
	if td == nil {
		return fmt.Errorf("task definition is required")
	}
	if err := validateNetworkMode(td.NetworkMode, td.ContainerDefinitions); err != nil {
		return err
	}
	_, err := resolveTaskContainerResources(td)
	return err
}
