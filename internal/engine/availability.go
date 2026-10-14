package engine

import "strings"

// Availability is captured before inspecting a pool, so a release between the
// inspection and waiting cannot be missed. Notifications are scoped by account
// and function rather than waking every function in the engine.
func (e *Engine) Availability(accountID, functionName string) <-chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := poolKey(e.resolveAccount(accountID), functionName)
	if e.availability == nil {
		e.availability = make(map[string]chan struct{})
	}
	if e.availability[key] == nil {
		e.availability[key] = make(chan struct{})
	}
	return e.availability[key]
}

func (e *Engine) notifyLocked(key string) {
	if channel := e.availability[key]; channel != nil {
		close(channel)
		delete(e.availability, key)
	}
}

func (e *Engine) SignalAvailability(accountID, functionName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notifyLocked(poolKey(e.resolveAccount(accountID), functionName))
}

func (e *Engine) untrackLocked(id string) *ContainerInfo {
	for key, pool := range e.containers {
		for i, info := range pool {
			if info.ID != id {
				continue
			}
			copy(pool[i:], pool[i+1:])
			pool[len(pool)-1] = nil
			pool = pool[:len(pool)-1]
			if len(pool) == 0 {
				delete(e.containers, key)
			} else {
				e.containers[key] = pool
			}
			e.notifyLocked(key)
			return info
		}
	}
	return nil
}

func (e *Engine) BusyContainers(accountID, functionName string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var count int
	for _, info := range e.containers[poolKey(e.resolveAccount(accountID), functionName)] {
		if info.Busy {
			count++
		}
	}
	return count
}

// Retire old environments without interrupting running invocations. Their
// deployment lease is released only once Docker confirms removal.
func (e *Engine) retire(accountID, functionName string, matches func(*ContainerInfo) bool) {
	key := poolKey(e.resolveAccount(accountID), functionName)
	e.mu.Lock()
	pool := e.containers[key]
	kept := make([]*ContainerInfo, 0, len(pool))
	var removed []*ContainerInfo
	for _, info := range pool {
		if matches(info) {
			info.Retired = true
			if !info.Busy {
				removed = append(removed, info)
				continue
			}
		}
		kept = append(kept, info)
	}
	if len(kept) == 0 {
		delete(e.containers, key)
	} else {
		e.containers[key] = kept
	}
	e.notifyLocked(key)
	e.mu.Unlock()
	if len(removed) > 0 {
		go e.forceRemoveAll(removed)
	}
}

func (e *Engine) RetireLatestRevision(accountID, functionName, revision string) {
	e.retire(accountID, functionName, func(info *ContainerInfo) bool {
		return info.Revision != revision && (info.Revision == "" || strings.HasSuffix(info.Revision, ":$LATEST"))
	})
}

func (e *Engine) RetireFunction(accountID, functionName string) {
	e.retire(accountID, functionName, func(*ContainerInfo) bool { return true })
}
func (e *Engine) RetireFunctionVersion(accountID, functionName, version string) {
	e.retire(accountID, functionName, func(info *ContainerInfo) bool { return strings.HasSuffix(info.Revision, ":"+version) })
}

// DiscardIdleRevision frees a slot held by another idle version, preventing a
// pool full of idle published versions from blocking a new revision forever.
func (e *Engine) DiscardIdleRevision(accountID, functionName, revision string) bool {
	e.mu.Lock()
	var discarded *ContainerInfo
	for _, info := range e.containers[poolKey(e.resolveAccount(accountID), functionName)] {
		if !info.Busy && info.Revision != revision {
			discarded = e.untrackLocked(info.ID)
			break
		}
	}
	e.mu.Unlock()
	if discarded == nil {
		return false
	}
	go e.forceRemoveAll([]*ContainerInfo{discarded})
	return true
}

func (e *Engine) DiscardContainerAsync(info *ContainerInfo) {
	if info == nil {
		return
	}
	e.mu.Lock()
	e.untrackLocked(info.ID)
	e.mu.Unlock()
	go e.forceRemoveAll([]*ContainerInfo{info})
}
