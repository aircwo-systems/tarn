package eventbridge

import (
	"testing"
	"time"
)

func TestValidateScheduleExpressionRatePlurality(t *testing.T) {
	if err := validateScheduleExpression("rate(1 minute)"); err != nil {
		t.Fatalf("expected valid rate(1 minute), got %v", err)
	}
	if err := validateScheduleExpression("rate(2 minutes)"); err != nil {
		t.Fatalf("expected valid rate(2 minutes), got %v", err)
	}
	if err := validateScheduleExpression("rate(1 minutes)"); err == nil {
		t.Fatalf("expected invalid singular/plural mismatch")
	}
	if err := validateScheduleExpression("rate(2 minute)"); err == nil {
		t.Fatalf("expected invalid singular/plural mismatch")
	}
}

func TestValidateScheduleExpressionCronWildcardCoverage(t *testing.T) {
	valid := []string{
		"cron(0/5 8-18 ? * MON-FRI 2026)",
		"cron(0 12 L * ? 2026)",
		"cron(0 12 15W * ? 2026)",
		"cron(0 12 ? * 2#1 2026)",
		"cron(0 12 ? * 6L 2026)",
	}
	for _, expr := range valid {
		if err := validateScheduleExpression(expr); err != nil {
			t.Fatalf("expected valid %q, got %v", expr, err)
		}
	}

	invalid := []string{
		"cron(0 12 * * * 2026)",
		"cron(0 12 ? * ? 2026)",
		"cron(0 12 ? * 2#6 2026)",
		"cron(0 12 LW * 2#1 2026)",
	}
	for _, expr := range invalid {
		if err := validateScheduleExpression(expr); err == nil {
			t.Fatalf("expected invalid %q", expr)
		}
	}
}

func TestComputeNextRunRate(t *testing.T) {
	anchor := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)
	after := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)
	next, err := computeNextRun("rate(1 minute)", anchor, after)
	if err != nil {
		t.Fatalf("computeNextRun: %v", err)
	}
	want := time.Date(2026, 3, 23, 10, 1, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
}

func TestComputeNextRunCron(t *testing.T) {
	anchor := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 3, 23, 9, 58, 0, 0, time.UTC)
	next, err := computeNextRun("cron(0/5 8-18 ? * MON-FRI 2026)", anchor, after)
	if err != nil {
		t.Fatalf("computeNextRun: %v", err)
	}
	want := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
}
