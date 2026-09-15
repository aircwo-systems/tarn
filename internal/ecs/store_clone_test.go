package ecs

import (
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// TestCloneContainerDefinitionDeepCopiesSecretsHealthCheckDependsOn guards
// the store clone against slice/pointer aliasing: mutating a described
// copy must never leak back into stored state.
func TestCloneContainerDefinitionDeepCopiesSecretsHealthCheckDependsOn(t *testing.T) {
	interval, timeout, retries, startPeriod := 10, 5, 3, 0
	src := types.ContainerDefinition{
		Name:  "app",
		Image: "example/app:latest",
		Secrets: []types.ContainerSecret{
			{Name: "DB_PASSWORD", ValueFrom: "arn:aws:secretsmanager:us-east-1:000000000000:secret:db"},
		},
		HealthCheck: &types.ContainerHealthCheck{
			Command:     []string{"CMD-SHELL", "true"},
			Interval:    &interval,
			Timeout:     &timeout,
			Retries:     &retries,
			StartPeriod: &startPeriod,
		},
		DependsOn: []types.ContainerDependency{
			{ContainerName: "db", Condition: types.ContainerConditionHealthy},
		},
	}

	dst := cloneContainerDefinition(src)

	dst.Secrets[0].Name = "MUTATED"
	dst.HealthCheck.Command[0] = "MUTATED"
	*dst.HealthCheck.Interval = 99
	dst.HealthCheck.Timeout = nil
	dst.DependsOn[0].Condition = "MUTATED"

	if src.Secrets[0].Name != "DB_PASSWORD" {
		t.Errorf("Secrets aliased: src secret name = %q", src.Secrets[0].Name)
	}
	if src.HealthCheck.Command[0] != "CMD-SHELL" {
		t.Errorf("HealthCheck.Command aliased: %q", src.HealthCheck.Command)
	}
	if *src.HealthCheck.Interval != 10 {
		t.Errorf("HealthCheck.Interval aliased: %d", *src.HealthCheck.Interval)
	}
	if src.HealthCheck.Timeout == nil || *src.HealthCheck.Timeout != 5 {
		t.Errorf("HealthCheck.Timeout aliased or lost")
	}
	if src.DependsOn[0].Condition != types.ContainerConditionHealthy {
		t.Errorf("DependsOn aliased: %q", src.DependsOn[0].Condition)
	}
}
