package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	infrasvc "github.com/aircwo-systems/tarn/internal/infrastructure"
)

func TestUserServicesAreAccountScoped(t *testing.T) {
	infra := infrasvc.NewService("", false)
	a := &Handler{cfg: &config.Config{AccountID: "111111111111"}, infra: infra}
	b := &Handler{cfg: &config.Config{AccountID: "222222222222"}, infra: infra}
	response := httptest.NewRecorder()
	a.SetUserServices(response, httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(`{"services":[{"name":"Account A API","url":"http://localhost:8080"}]}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	b.ListUserServices(response, httptest.NewRequest(http.MethodGet, "/", nil))
	var payload userServicesPayload
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Services) != 0 {
		t.Fatalf("account B can see account A's %d registered services", len(payload.Services))
	}
}

func serviceRequest(t *testing.T, h *Handler, body string) []infrasvc.UserTarget {
	t.Helper()
	w := httptest.NewRecorder()
	if body == "" {
		h.ListUserServices(w, httptest.NewRequest(http.MethodGet, "/", nil))
	} else {
		h.SetUserServices(w, httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body)))
	}
	if w.Code != http.StatusOK {
		t.Fatalf("services: %d %s", w.Code, w.Body.String())
	}
	var payload userServicesPayload
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Services
}

func TestUserServiceScopeChangesPreserveOtherAccounts(t *testing.T) {
	infra := infrasvc.NewService("", false)
	a := &Handler{cfg: &config.Config{AccountID: "111111111111"}, infra: infra}
	b := &Handler{cfg: &config.Config{AccountID: "222222222222"}, infra: infra}
	serviceRequest(t, a, `{"services":[{"name":"A","url":"localhost:8080"}]}`)
	serviceRequest(t, b, `{"services":[{"name":"B","url":"localhost:8081"}]}`)
	promoted := serviceRequest(t, a, `{"services":[{"name":"A","url":"localhost:8080","scope":"global"}]}`)
	if promoted[0].Scope != infrasvc.ScopeGlobal || promoted[0].AccountID != "" {
		t.Fatalf("not global: %+v", promoted)
	}
	if got := serviceRequest(t, b, ""); len(got) != 2 {
		t.Fatalf("B's service or global service lost: %+v", got)
	}
	serviceRequest(t, a, `{"services":[{"name":"A","url":"localhost:8080","scope":"account"}]}`)
	if got := serviceRequest(t, b, ""); len(got) != 1 || got[0].Name != "B" {
		t.Fatalf("demotion leaked or lost B's service: %+v", got)
	}
	serviceRequest(t, a, `{"services":[]}`)
	if got := serviceRequest(t, b, ""); len(got) != 1 || got[0].AccountID != b.cfg.AccountID {
		t.Fatalf("A's save changed B: %+v", got)
	}
}

func TestUserServicesRejectAnotherAccountOwner(t *testing.T) {
	h := &Handler{cfg: &config.Config{AccountID: "111111111111"}, infra: infrasvc.NewService("", false)}
	w := httptest.NewRecorder()
	h.SetUserServices(w, httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(`{"services":[{"url":"localhost:8080","scope":"account","accountId":"222222222222"}]}`)))
	if w.Code != http.StatusBadRequest || len(h.infra.UserTargets()) != 0 {
		t.Fatalf("foreign owner accepted: %d", w.Code)
	}
}

func TestUserServicesFilterInfrastructureAndOverview(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer endpoint.Close()
	a := newTestHandler(t)
	b := newTestHandler(t)
	a.cfg.AccountID, b.cfg.AccountID = "111111111111", "222222222222"
	a.infra = infrasvc.NewService("", false)
	b.infra = a.infra
	body, _ := json.Marshal(map[string]any{"services": []map[string]string{{"name": "A API", "url": endpoint.URL}}})
	serviceRequest(t, a, string(body))
	a.infra.ProbeAll(t.Context())
	for _, h := range []*Handler{a, b} {
		w := httptest.NewRecorder()
		h.Infrastructure(w, httptest.NewRequest(http.MethodGet, "/", nil))
		var probes []infrasvc.ProbeResult
		if err := json.Unmarshal(w.Body.Bytes(), &probes); err != nil {
			t.Fatal(err)
		}
		want := 0
		if h == a {
			want = 1
		}
		if len(probes) != want {
			t.Fatalf("infrastructure leak for %s: %+v", h.cfg.AccountID, probes)
		}
		w = httptest.NewRecorder()
		h.Overview(w, httptest.NewRequest(http.MethodGet, "/", nil))
		var overview overviewResponse
		if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
			t.Fatal(err)
		}
		if len(overview.Infrastructure) != want {
			t.Fatalf("overview leak for %s: %+v", h.cfg.AccountID, overview.Infrastructure)
		}
	}
}
