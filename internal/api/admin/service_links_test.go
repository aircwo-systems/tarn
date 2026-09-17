package admin

import (
	"testing"

	infrasvc "github.com/aircwo-systems/tarn/internal/infrastructure"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestInferInfraConnectionsLinksUserServices(t *testing.T) {
	probes := []infrasvc.ProbeResult{
		{Name: "Payments API", Kind: "http", Host: "192.168.1.50", Port: 8080, Source: infrasvc.SourceUser},
		{Name: "Billing", Kind: "https", Host: "billing.lan", Port: 443, Source: infrasvc.SourceUser},
		{Name: "LAN Postgres", Kind: "tcp", Host: "10.0.0.7", Port: 5432, Source: infrasvc.SourceUser},
		{Name: "Unused", Kind: "http", Host: "localhost", Port: 9999, Source: infrasvc.SourceUser},
	}
	secrets := map[string]string{
		"arn:aws:secretsmanager:us-east-1:000000000000:secret:payments-abc": `{"baseUrl":"http://192.168.1.50:8080/v1","apiKey":"x"}`,
	}
	lookup := func(ref string) (string, string, bool) {
		v, ok := secrets[ref]
		return "payments", v, ok
	}
	functions := []*types.FunctionConfig{
		{FunctionName: "via-secret", Environment: map[string]string{"PAYMENTS_SECRET": "arn:aws:secretsmanager:us-east-1:000000000000:secret:payments-abc"}},
		{FunctionName: "via-env", Environment: map[string]string{"BILLING_URL": "https://billing.lan/api"}},
		{FunctionName: "via-db-url", Environment: map[string]string{"DATABASE_URL": "postgres://u:p@10.0.0.7:5432/app"}},
		{FunctionName: "unrelated", Environment: map[string]string{"OTHER": "http://example.com:8080"}},
	}

	got := inferInfraConnections(functions, probes, lookup)

	want := map[string]struct{ target, evidence, source string }{
		"via-secret": {"http-192.168.1.50-8080", "secret", "secret:payments"},
		"via-env":    {"https-billing.lan-443", "env", "BILLING_URL"},
		"via-db-url": {"tcp-10.0.0.7-5432", "env", "DATABASE_URL"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d connections, want %d: %+v", len(got), len(want), got)
	}
	for _, c := range got {
		w, ok := want[c.SourceFunction]
		if !ok {
			t.Fatalf("unexpected connection %+v", c)
		}
		if c.TargetID != w.target || c.Evidence != w.evidence || c.Source != w.source {
			t.Errorf("%s: got target=%s evidence=%s source=%s, want %+v", c.SourceFunction, c.TargetID, c.Evidence, c.Source, w)
		}
	}
}

func TestInferSecretConnectionHintsHostPort(t *testing.T) {
	hints := inferSecretConnectionHints("db", `{"engine":"postgres","host":"10.0.0.7","port":"6543"}`)
	if len(hints) != 1 || hints[0].host != "10.0.0.7" || hints[0].port != 6543 || hints[0].kind != "postgresql" {
		t.Fatalf("unexpected hints: %+v", hints)
	}
}

func TestURLHintsDoNotMatchDatabaseProbes(t *testing.T) {
	probe := infrasvc.ProbeResult{Kind: "postgresql", Host: "localhost", Port: 5432}
	for _, hint := range inferURLConnectionHints("http://localhost:5432/", "X") {
		if serviceHintMatches(probe, hint) {
			t.Fatal("http url should not link to a postgres probe")
		}
	}
}

func TestInferInfraConnectionsStableEvidence(t *testing.T) {
	probes := []infrasvc.ProbeResult{{Name: "API", Kind: "http", Host: "localhost", Port: 8080, Source: infrasvc.SourceUser}}
	lookup := func(ref string) (string, string, bool) {
		if ref != "api-secret" {
			return "", "", false
		}
		return "api-secret", `{"url":"http://127.0.0.1:8080"}`, true
	}
	functions := []*types.FunctionConfig{{FunctionName: "fn", Environment: map[string]string{
		"Z_API_URL": "http://localhost:8080",
		"A_API_URL": "http://localhost:8080/v2",
		"SECRET":    "api-secret",
	}}}
	for i := 0; i < 20; i++ {
		got := inferInfraConnections(functions, probes, lookup)
		if len(got) != 1 || got[0].Evidence != "secret" || got[0].Source != "secret:api-secret" {
			t.Fatalf("run %d: unstable or wrong link: %+v", i, got)
		}
	}
}
