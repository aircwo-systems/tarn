package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// activityPersistInterval bounds how often an account's last-activity time is
// written to disk from the request path. Staleness is measured in days, so an
// hour of drift is invisible, and it keeps the request path free of disk I/O.
const activityPersistInterval = time.Hour

const accountMetaFile = "account.json"

var (
	// ErrAccountNotFound reports an account with no data and no live services.
	ErrAccountNotFound = errors.New("account not found")
	// ErrDefaultAccount reports an attempt to archive or delete the default account.
	ErrDefaultAccount = errors.New("the default account cannot be archived or deleted")
	// ErrInvalidAccountID reports an ID that is not twelve digits.
	ErrInvalidAccountID = errors.New("account ID must be exactly 12 digits")
)

// accountMeta is the per-account lifecycle record kept in account.json.
type accountMeta struct {
	LastActivityAt *time.Time     `json:"lastActivityAt,omitempty"`
	Archived       bool           `json:"archived,omitempty"`
	ArchivedAt     *time.Time     `json:"archivedAt,omitempty"`
	Resources      map[string]int `json:"resources,omitempty"` // counts saved when the account was unloaded

	persistedAt time.Time
}

// AccountInfo describes one account for the dashboard's accounts view.
type AccountInfo struct {
	ID             string         `json:"id"`
	Default        bool           `json:"default"`
	Loaded         bool           `json:"loaded"`
	Archived       bool           `json:"archived"`
	ArchivedAt     *time.Time     `json:"archivedAt,omitempty"`
	LastActivityAt *time.Time     `json:"lastActivityAt,omitempty"`
	Resources      map[string]int `json:"resources"`
	ResourceTotal  int            `json:"resourceTotal"`
}

// ConfigureAccounts enables account lifecycle tracking. baseDir is the
// instance data directory (non-default accounts live under baseDir/accounts),
// defaultID is the account that can never be archived, and onRelease is called
// after an account's services stop so shared resources it holds, such as warm
// Lambda containers, can be freed.
func (r *HandlerRegistry) ConfigureAccounts(baseDir, defaultID string, onRelease func(accountID string)) {
	r.metaMu.Lock()
	defer r.metaMu.Unlock()
	r.baseDir = baseDir
	r.defaultID = defaultID
	r.onRelease = onRelease
	r.metas = map[string]*accountMeta{}

	entries, err := os.ReadDir(filepath.Join(baseDir, "accounts"))
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !validAccountID(e.Name()) {
			continue
		}
		if m := readAccountMeta(r.metaPath(e.Name())); m != nil {
			r.metas[e.Name()] = m
		}
	}
	if m := readAccountMeta(r.metaPath(defaultID)); m != nil {
		r.metas[defaultID] = m
	}
}

// IsArchived reports whether accountID has been archived.
func (r *HandlerRegistry) IsArchived(accountID string) bool {
	r.metaMu.RLock()
	defer r.metaMu.RUnlock()
	m := r.metas[accountID]
	return m != nil && m.Archived
}

// Touch records AWS API activity for accountID. The time is kept in memory and
// written to disk at most once per activityPersistInterval.
func (r *HandlerRegistry) Touch(accountID string) {
	if r.baseDir == "" {
		return
	}
	now := time.Now().UTC()
	r.metaMu.Lock()
	m := r.metaLocked(accountID)
	m.LastActivityAt = &now
	persist := now.Sub(m.persistedAt) >= activityPersistInterval
	var snapshot accountMeta
	if persist {
		m.persistedAt = now
		snapshot = *m
	}
	r.metaMu.Unlock()
	if persist {
		r.writes.Add(1)
		go func() {
			defer r.writes.Done()
			_ = r.writeMeta(accountID, &snapshot)
		}()
	}
}

// Archive stops an account's services and marks it inactive. Requests with its
// access key are rejected until it is restored, and it is not loaded at
// startup. Its data stays on disk.
func (r *HandlerRegistry) Archive(accountID string) error {
	if err := r.checkMutable(accountID); err != nil {
		return err
	}
	if r.IsArchived(accountID) {
		return nil
	}
	now := time.Now().UTC()
	counts := r.unload(accountID, func(m *accountMeta) {
		m.Archived = true
		m.ArchivedAt = &now
	})

	r.metaMu.Lock()
	m := r.metaLocked(accountID)
	if counts != nil {
		m.Resources = counts
	}
	snapshot := *m
	r.metaMu.Unlock()
	return r.writeMeta(accountID, &snapshot)
}

// Restore reactivates an archived account. Its services load on the next request.
func (r *HandlerRegistry) Restore(accountID string) error {
	if err := r.checkMutable(accountID); err != nil {
		return err
	}
	now := time.Now().UTC()
	r.metaMu.Lock()
	m := r.metaLocked(accountID)
	m.Archived = false
	m.ArchivedAt = nil
	// A restore is a deliberate use; don't flag it stale straight away.
	m.LastActivityAt = &now
	snapshot := *m
	r.metaMu.Unlock()
	return r.writeMeta(accountID, &snapshot)
}

// Delete stops an account's services and removes all of its data from disk.
// Traces are shared across accounts and are not removed.
func (r *HandlerRegistry) Delete(accountID string) error {
	if err := r.checkMutable(accountID); err != nil {
		return err
	}
	// Mark it archived while tearing down so no request reloads it midway.
	r.unload(accountID, func(m *accountMeta) { m.Archived = true })

	// Hold writeMu so a pending activity write cannot recreate the directory.
	r.writeMu.Lock()
	r.metaMu.Lock()
	delete(r.metas, accountID)
	r.metaMu.Unlock()
	err := os.RemoveAll(filepath.Join(r.baseDir, "accounts", accountID))
	r.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("remove account data: %w", err)
	}
	return nil
}

// Accounts lists every account the instance knows about: the default account,
// every account with data on disk, and every account currently loaded.
func (r *HandlerRegistry) Accounts() []AccountInfo {
	ids := map[string]bool{}
	r.metaMu.RLock()
	defaultID, baseDir := r.defaultID, r.baseDir
	for id := range r.metas {
		ids[id] = true
	}
	r.metaMu.RUnlock()
	if defaultID != "" {
		ids[defaultID] = true
	}
	if entries, err := os.ReadDir(filepath.Join(baseDir, "accounts")); err == nil {
		for _, e := range entries {
			if e.IsDir() && validAccountID(e.Name()) {
				ids[e.Name()] = true
			}
		}
	}

	r.mu.RLock()
	loaded := make(map[string]*AccountBundle, len(r.bundles))
	for id, b := range r.bundles {
		loaded[id] = b
		ids[id] = true
	}
	r.mu.RUnlock()

	out := make([]AccountInfo, 0, len(ids))
	for id := range ids {
		info := AccountInfo{ID: id, Default: id == defaultID}
		r.metaMu.RLock()
		if m := r.metas[id]; m != nil {
			info.Archived = m.Archived
			info.ArchivedAt = m.ArchivedAt
			info.LastActivityAt = m.LastActivityAt
			info.Resources = maps.Clone(m.Resources)
		}
		r.metaMu.RUnlock()
		if b, ok := loaded[id]; ok {
			info.Loaded = true
			if b.handlers != nil && b.handlers.Admin != nil {
				info.Resources = b.handlers.Admin.ResourceCounts()
			}
		}
		if info.Resources == nil {
			info.Resources = map[string]int{}
		}
		for _, n := range info.Resources {
			info.ResourceTotal += n
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// persistActivity writes every account's in-memory activity time, after any
// background writes finish. Call on shutdown.
func (r *HandlerRegistry) persistActivity() {
	r.writes.Wait()
	r.metaMu.Lock()
	pending := map[string]accountMeta{}
	for id, m := range r.metas {
		if m.LastActivityAt != nil && m.LastActivityAt.After(m.persistedAt) {
			m.persistedAt = *m.LastActivityAt
			pending[id] = *m
		}
	}
	r.metaMu.Unlock()
	for id, m := range pending {
		_ = r.writeMeta(id, &m)
	}
}

// unload removes an account's bundle from the registry, applies mark to its
// metadata in the same critical section (so no request can reload it), then
// stops its services and releases shared resources. It returns the account's
// resource counts taken just before stopping, or nil if it was not loaded.
func (r *HandlerRegistry) unload(accountID string, mark func(*accountMeta)) map[string]int {
	r.mu.Lock()
	r.metaMu.Lock()
	mark(r.metaLocked(accountID))
	r.metaMu.Unlock()
	b := r.bundles[accountID]
	delete(r.bundles, accountID)
	r.mu.Unlock()

	var counts map[string]int
	if b != nil {
		if b.handlers != nil && b.handlers.Admin != nil {
			counts = b.handlers.Admin.ResourceCounts()
		}
		if b.stop != nil {
			b.stop()
		}
	}
	if r.onRelease != nil {
		r.onRelease(accountID)
	}
	return counts
}

func (r *HandlerRegistry) checkMutable(accountID string) error {
	if !validAccountID(accountID) {
		return ErrInvalidAccountID
	}
	r.metaMu.RLock()
	defaultID, baseDir := r.defaultID, r.baseDir
	_, known := r.metas[accountID]
	r.metaMu.RUnlock()
	if accountID == defaultID {
		return ErrDefaultAccount
	}
	r.mu.RLock()
	_, loaded := r.bundles[accountID]
	r.mu.RUnlock()
	if !known && !loaded {
		if _, err := os.Stat(filepath.Join(baseDir, "accounts", accountID)); err != nil {
			return ErrAccountNotFound
		}
	}
	return nil
}

// metaLocked returns accountID's metadata, creating it. Caller holds metaMu.
func (r *HandlerRegistry) metaLocked(accountID string) *accountMeta {
	if r.metas == nil {
		r.metas = map[string]*accountMeta{}
	}
	m := r.metas[accountID]
	if m == nil {
		m = &accountMeta{}
		r.metas[accountID] = m
	}
	return m
}

func (r *HandlerRegistry) metaPath(accountID string) string {
	if accountID == r.defaultID {
		return filepath.Join(r.baseDir, accountMetaFile)
	}
	return filepath.Join(r.baseDir, "accounts", accountID, accountMetaFile)
}

func (r *HandlerRegistry) writeMeta(accountID string, m *accountMeta) error {
	if r.baseDir == "" {
		return nil
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.metaMu.RLock()
	_, exists := r.metas[accountID]
	r.metaMu.RUnlock()
	if !exists {
		return nil // deleted since the write was scheduled
	}
	path := r.metaPath(accountID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("[account] failed to save %s metadata: %v", accountID, err)
		return err
	}
	return nil
}

func readAccountMeta(path string) *accountMeta {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m accountMeta
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("[account] ignoring unreadable %s: %v", path, err)
		return nil
	}
	if m.LastActivityAt != nil {
		m.persistedAt = *m.LastActivityAt
	}
	return &m
}

func validAccountID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// writeAccountArchived rejects a request made with an archived account's
// access key. It never falls back to the default account: that would read and
// write another account's resources.
func writeAccountArchived(w http.ResponseWriter, accountID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-amzn-ErrorType", "AccountArchived")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"__type":  "AccountArchived",
		"message": fmt.Sprintf("account %s is archived in Tarn; restore it from the dashboard's Settings to use it again", accountID),
	})
}

func (s *Server) listAccountsHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"defaultAccountId": s.cfg.AccountID,
		"accounts":         s.registry.Accounts(),
	})
}

func (s *Server) accountActionHandler(action func(string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := action(id); err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, ErrAccountNotFound):
				status = http.StatusNotFound
			case errors.Is(err, ErrDefaultAccount), errors.Is(err, ErrInvalidAccountID):
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		s.listAccountsHandler(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
