package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/logs"
)

const otherAccount = "111111111111"

// accountHarness is a server whose registry builds a real bundle per account
// and records which accounts were loaded, stopped and released.
type accountHarness struct {
	t        *testing.T
	cfg      *config.Config
	registry *HandlerRegistry
	handler  http.Handler

	mu       sync.Mutex
	loads    map[string]int
	stops    map[string]int
	released []string
}

func newAccountHarness(t *testing.T) *accountHarness {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	h := &accountHarness{t: t, cfg: cfg, loads: map[string]int{}, stops: map[string]int{}}
	h.registry = h.newRegistry()
	// Background activity writes must land before the temp dir is removed.
	t.Cleanup(func() { h.registry.writes.Wait() })
	return h
}

func (h *accountHarness) newRegistry() *HandlerRegistry {
	registry := NewHandlerRegistry(func(id string) (*AccountBundle, error) {
		acctCfg := h.cfg.ForAccount(id)
		if err := acctCfg.EnsureDataDir(); err != nil {
			return nil, err
		}
		b := newTestBundle(h.t, acctCfg)
		h.mu.Lock()
		h.loads[id]++
		h.mu.Unlock()
		b.stop = func() {
			h.mu.Lock()
			h.stops[id]++
			h.mu.Unlock()
		}
		return b, nil
	})
	registry.ConfigureAccounts(h.cfg.DataDir, h.cfg.AccountID, func(id string) {
		h.mu.Lock()
		h.released = append(h.released, id)
		h.mu.Unlock()
	})
	s := NewServer(h.cfg, registry, logs.NewService(h.cfg), nil)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	h.handler = s.withLogging(mux)
	return registry
}

// do sends req as accountID (empty for the dashboard, which signs nothing).
func (h *accountHarness) do(method, path, accountID string, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if accountID != "" {
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accountID+"/20260930/us-east-1/sqs/aws4_request, SignedHeaders=host, Signature=0")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
		req.Header.Set("X-Amz-Target", "AmazonSQS.ListQueues")
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func (h *accountHarness) listQueues(accountID string) *httptest.ResponseRecorder {
	return h.do(http.MethodPost, "/", accountID, "{}")
}

func (h *accountHarness) accounts() map[string]AccountInfo {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/_tarn/admin/accounts", "", "")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("list accounts: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Accounts []AccountInfo `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		h.t.Fatal(err)
	}
	m := map[string]AccountInfo{}
	for _, a := range out.Accounts {
		m[a.ID] = a
	}
	return m
}

func TestArchivedAccountIsRejectedNotFallenBackToDefault(t *testing.T) {
	h := newAccountHarness(t)
	if rec := h.listQueues(otherAccount); rec.Code != http.StatusOK {
		t.Fatalf("first request: %d %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodPost, "/_tarn/admin/accounts/"+otherAccount+"/archive", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body.String())
	}
	if h.stops[otherAccount] != 1 {
		t.Errorf("archive stopped the account %d times, want 1", h.stops[otherAccount])
	}
	if len(h.released) != 1 || h.released[0] != otherAccount {
		t.Errorf("released = %v, want the archived account's shared resources freed", h.released)
	}

	defaultLoads := h.loads[h.cfg.AccountID]
	rec = h.listQueues(otherAccount)
	if rec.Code != http.StatusForbidden || rec.Header().Get("x-amzn-ErrorType") != "AccountArchived" {
		t.Fatalf("archived account request: %d %q %s", rec.Code, rec.Header().Get("x-amzn-ErrorType"), rec.Body.String())
	}
	if h.loads[otherAccount] != 1 || h.loads[h.cfg.AccountID] != defaultLoads {
		t.Errorf("an archived account's request must not load it or the default account: loads=%v", h.loads)
	}

	// The accounts endpoints still answer, so the dashboard can restore it.
	if a := h.accounts()[otherAccount]; !a.Archived || a.Loaded || a.ArchivedAt == nil {
		t.Errorf("listed as %+v, want archived and unloaded", a)
	}
}

func TestArchiveSurvivesRestartAndRestoreReloads(t *testing.T) {
	h := newAccountHarness(t)
	h.listQueues(otherAccount)
	if err := h.registry.Archive(otherAccount); err != nil {
		t.Fatal(err)
	}

	// A new registry over the same data dir, as after a restart.
	h.registry.writes.Wait()
	h.registry = h.newRegistry()
	if !h.registry.IsArchived(otherAccount) {
		t.Fatal("archive was not persisted")
	}
	if _, err := h.registry.PreInit(otherAccount); err == nil {
		t.Fatal("startup must not load an archived account")
	}

	if err := h.registry.Restore(otherAccount); err != nil {
		t.Fatal(err)
	}
	if rec := h.listQueues(otherAccount); rec.Code != http.StatusOK {
		t.Fatalf("restored account request: %d %s", rec.Code, rec.Body.String())
	}
	if h.loads[otherAccount] != 2 {
		t.Errorf("loads = %d, want the restored account loaded again on its next request", h.loads[otherAccount])
	}
	if a := h.accounts()[otherAccount]; a.Archived || !a.Loaded || a.LastActivityAt == nil {
		t.Errorf("restored account listed as %+v", a)
	}
}

func TestDeleteAccountRemovesData(t *testing.T) {
	h := newAccountHarness(t)
	h.listQueues(otherAccount)
	dir := filepath.Join(h.cfg.DataDir, "accounts", otherAccount)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("account dir not created: %v", err)
	}

	rec := h.do(http.MethodDelete, "/_tarn/admin/accounts/"+otherAccount, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("account dir still present: %v", err)
	}
	if _, ok := h.accounts()[otherAccount]; ok {
		t.Error("deleted account still listed")
	}
	if h.stops[otherAccount] != 1 {
		t.Errorf("delete stopped the account %d times, want 1", h.stops[otherAccount])
	}
}

func TestAccountLifecycleValidation(t *testing.T) {
	h := newAccountHarness(t)
	cases := []struct {
		id   string
		want error
	}{
		{h.cfg.AccountID, ErrDefaultAccount},
		{"12345", ErrInvalidAccountID},
		{"../../etc/pwd", ErrInvalidAccountID},
		{"222222222222", ErrAccountNotFound},
	}
	for _, c := range cases {
		for name, action := range map[string]func(string) error{"archive": h.registry.Archive, "delete": h.registry.Delete} {
			if err := action(c.id); !errors.Is(err, c.want) {
				t.Errorf("%s(%q) = %v, want %v", name, c.id, err, c.want)
			}
		}
	}
	if rec := h.do(http.MethodDelete, "/_tarn/admin/accounts/"+h.cfg.AccountID, "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("deleting the default account over HTTP: %d, want 400", rec.Code)
	}
}

func TestOnlyAWSCallsCountAsActivity(t *testing.T) {
	h := newAccountHarness(t)
	h.listQueues(otherAccount)
	first := h.accounts()[otherAccount].LastActivityAt
	if first == nil {
		t.Fatal("an AWS API call must record activity")
	}

	// The dashboard polls with the selected account's key.
	h.do(http.MethodGet, "/_tarn/admin/overview", otherAccount, "")
	if got := h.accounts()[otherAccount].LastActivityAt; !got.Equal(*first) {
		t.Errorf("dashboard polling moved last activity from %s to %s", first, got)
	}
}

func TestAccountsListsDefaultAndAccountsOnDisk(t *testing.T) {
	h := newAccountHarness(t)
	if err := os.MkdirAll(filepath.Join(h.cfg.DataDir, "accounts", "333333333333"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.listQueues(h.cfg.AccountID)

	got := h.accounts()
	if d, ok := got[h.cfg.AccountID]; !ok || !d.Default || !d.Loaded {
		t.Errorf("default account = %+v", d)
	}
	if a, ok := got["333333333333"]; !ok || a.Loaded || a.Archived {
		t.Errorf("on-disk account = %+v, want listed and unloaded", a)
	}
	if a := got["333333333333"]; a.Resources == nil {
		t.Error("resources must serialise as an object, not null")
	}
}
