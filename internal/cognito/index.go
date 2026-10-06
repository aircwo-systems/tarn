package cognito

import "sync"

// Index maps pool and client IDs to the account that owns them. It is shared
// by every account's Service: Cognito's public operations are unsigned, so the
// account cannot come from SigV4 and must be found from the ClientId, the pool
// in an access token's issuer, or the pool ID in a JWKS URL.
//
// Every persisted, non-archived account is initialised at startup, and each
// Service registers its pools while loading, so the index always mirrors the
// stores it was built from. A nil *Index is a valid no-op.
type Index struct {
	mu      sync.RWMutex
	pools   map[string]string // pool ID -> account ID
	clients map[string]string // client ID -> pool ID
}

// NewIndex returns an empty index.
func NewIndex() *Index {
	return &Index{pools: make(map[string]string), clients: make(map[string]string)}
}

// AccountForPool returns the account that owns poolID.
func (x *Index) AccountForPool(poolID string) (string, bool) {
	if x == nil {
		return "", false
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	acct, ok := x.pools[poolID]
	return acct, ok
}

// AccountForClient returns the account that owns the pool clientID belongs to.
func (x *Index) AccountForClient(clientID string) (string, bool) {
	if x == nil {
		return "", false
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	poolID, ok := x.clients[clientID]
	if !ok {
		return "", false
	}
	acct, ok := x.pools[poolID]
	return acct, ok
}

func (x *Index) addPool(accountID, poolID string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	x.pools[poolID] = accountID
	x.mu.Unlock()
}

func (x *Index) addClient(poolID, clientID string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	x.clients[clientID] = poolID
	x.mu.Unlock()
}

func (x *Index) removeClient(clientID string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	delete(x.clients, clientID)
	x.mu.Unlock()
}

func (x *Index) removePool(poolID string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	delete(x.pools, poolID)
	for clientID, p := range x.clients {
		if p == poolID {
			delete(x.clients, clientID)
		}
	}
}

// RemoveAccount drops every pool and client owned by accountID, for when the
// account is archived, deleted or shut down.
func (x *Index) RemoveAccount(accountID string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for poolID, acct := range x.pools {
		if acct != accountID {
			continue
		}
		delete(x.pools, poolID)
		for clientID, p := range x.clients {
			if p == poolID {
				delete(x.clients, clientID)
			}
		}
	}
}
