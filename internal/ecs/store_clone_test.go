package ecs

import (
	"fmt"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
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

// TestListTaskDefinitionRefsNamesEveryRevision covers the reason the refs exist.
// The dashboard lists every revision on a five-second poll, and doing that by
// listing and describing each one deep-cloned every container definition each
// time. The refs must name them all, in family/revision order, and must agree
// with what the AWS-shaped listing returns.
func TestListTaskDefinitionRefsNamesEveryRevision(t *testing.T) {
	svc := newTestService(t)

	// Register a new revision per call, across two families.
	for _, family := range []string{"beta", "alpha"} {
		for range 3 {
			if _, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
				Family: family,
				ContainerDefinitions: []types.ContainerDefinition{{
					Name:        "app",
					Image:       "example/app:latest",
					Environment: []types.KeyValuePair{{Name: "A", Value: "1"}},
				}},
			}); err != nil {
				t.Fatalf("register %s: %v", family, err)
			}
		}
	}

	// Unlike AWS, this store leaves every revision ACTIVE, so mark the older ones
	// INACTIVE directly to give the status filter something to exclude.
	store := svc.store
	store.mu.Lock()
	for _, revs := range store.taskDefs {
		newest := 0
		for _, td := range revs {
			if td.Revision > newest {
				newest = td.Revision
			}
		}
		for _, td := range revs {
			if td.Revision < newest {
				td.Status = types.TaskDefinitionStatusInactive
			}
		}
	}
	store.mu.Unlock()

	refs := svc.ListTaskDefinitionRefs()
	if len(refs) != 6 {
		t.Fatalf("refs = %d, want 6", len(refs))
	}
	// Families ascending, revisions ascending within each.
	for i, want := range []struct {
		family   string
		revision int
	}{
		{"alpha", 1}, {"alpha", 2}, {"alpha", 3},
		{"beta", 1}, {"beta", 2}, {"beta", 3},
	} {
		if refs[i].Family != want.family || refs[i].Revision != want.revision {
			t.Errorf("refs[%d] = %s:%d, want %s:%d", i, refs[i].Family, refs[i].Revision, want.family, want.revision)
		}
		if refs[i].Arn == "" {
			t.Errorf("refs[%d] has no ARN", i)
		}
	}
	// Only the newest revision of a family stays ACTIVE.
	if refs[0].Status != types.TaskDefinitionStatusInactive {
		t.Errorf("oldest alpha revision status = %q, want INACTIVE", refs[0].Status)
	}
	if refs[2].Status != types.TaskDefinitionStatusActive {
		t.Errorf("newest alpha revision status = %q, want ACTIVE", refs[2].Status)
	}

	// The listing the refs replaced must be unchanged: same ARNs, same order,
	// for both sort orders and for a status filter.
	for _, sortOrder := range []string{types.TaskDefinitionSortAscending, types.TaskDefinitionSortDescending} {
		out, err := svc.ListTaskDefinitions(&types.ListTaskDefinitionsInput{
			Sort:       sortOrder,
			Status:     types.TaskDefinitionStatusActive,
			MaxResults: 100,
		})
		if err != nil {
			t.Fatalf("list task definitions %s: %v", sortOrder, err)
		}
		if len(out.TaskDefinitionArns) != 2 {
			t.Fatalf("%s: active ARNs = %d, want 2 (%v)", sortOrder, len(out.TaskDefinitionArns), out.TaskDefinitionArns)
		}
		first := refs[2]
		last := refs[5]
		if sortOrder == types.TaskDefinitionSortAscending {
			if out.TaskDefinitionArns[0] != first.Arn || out.TaskDefinitionArns[1] != last.Arn {
				t.Errorf("ascending ARNs = %v, want [%s %s]", out.TaskDefinitionArns, first.Arn, last.Arn)
			}
		} else if out.TaskDefinitionArns[0] != last.Arn || out.TaskDefinitionArns[1] != first.Arn {
			t.Errorf("descending ARNs = %v, want [%s %s]", out.TaskDefinitionArns, last.Arn, first.Arn)
		}
	}

	// A family prefix filter still narrows the listing.
	out, err := svc.ListTaskDefinitions(&types.ListTaskDefinitionsInput{
		FamilyPrefix: "alph",
		Status:       types.TaskDefinitionStatusActive,
		MaxResults:   100,
	})
	if err != nil {
		t.Fatalf("list by family prefix: %v", err)
	}
	if len(out.TaskDefinitionArns) != 1 || out.TaskDefinitionArns[0] != refs[2].Arn {
		t.Errorf("family-prefixed ARNs = %v, want [%s]", out.TaskDefinitionArns, refs[2].Arn)
	}

	// Paging must still walk the whole set without repeating or dropping one.
	var paged []string
	token := ""
	for range 10 {
		page, err := svc.ListTaskDefinitions(&types.ListTaskDefinitionsInput{
			Status:     types.TaskDefinitionStatusInactive,
			MaxResults: 2,
			NextToken:  token,
		})
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		paged = append(paged, page.TaskDefinitionArns...)
		token = page.NextToken
		if token == "" {
			break
		}
	}
	if len(paged) != 4 {
		t.Fatalf("paged inactive ARNs = %d, want 4 (%v)", len(paged), paged)
	}
}

// BenchmarkListTaskDefinitions measures the listing the dashboard drove once
// per status per page. It used to deep-clone every registered revision — every
// container definition, environment entry and secret — on each page, purely to
// read two fields.
func BenchmarkListTaskDefinitions(b *testing.B) {
	cfg := config.Default()
	cfg.DataDir = b.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	svc := NewService(cfg, store)
	if err := svc.Init(); err != nil {
		b.Fatalf("init service: %v", err)
	}

	families, revisions := 10, 20
	for f := range families {
		for range revisions {
			if _, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
				Family: fmt.Sprintf("fam-%02d", f),
				ContainerDefinitions: []types.ContainerDefinition{{
					Name:  "app",
					Image: "example/app:latest",
					Environment: []types.KeyValuePair{
						{Name: "A", Value: "1"},
						{Name: "B", Value: "2"},
					},
					Command: []string{"serve"},
				}},
			}); err != nil {
				b.Fatalf("register: %v", err)
			}
		}
	}

	b.ResetTimer()
	for b.Loop() {
		for _, status := range []string{
			types.TaskDefinitionStatusActive,
			types.TaskDefinitionStatusInactive,
		} {
			token := ""
			for {
				page, err := svc.ListTaskDefinitions(&types.ListTaskDefinitionsInput{
					MaxResults: 100,
					NextToken:  token,
					Status:     status,
				})
				if err != nil {
					b.Fatal(err)
				}
				token = page.NextToken
				if token == "" {
					break
				}
			}
		}
	}
}
