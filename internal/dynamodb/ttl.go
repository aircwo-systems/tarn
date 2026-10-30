package dynamodb

import (
	"math/big"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// ttlIdentity is the identity DynamoDB stamps on REMOVE records it emits for
// TTL expiry, as opposed to user-initiated deletes.
var ttlIdentity = &types.StreamUserIdentity{Type: "Service", PrincipalID: "dynamodb.amazonaws.com"}

// sweepExpired deletes items whose TTL attribute has expired as of now, on
// every table with TTL enabled. An item expires when its TTL attribute is a
// Number holding epoch seconds <= now; any other value, or a missing
// attribute, never expires. Each deletion emits a REMOVE stream record
// attributed to the DynamoDB service. It returns the number of items deleted.
func (s *Store) sweepExpired(now time.Time) int {
	nowValue := new(big.Float).SetInt64(now.Unix())
	deleted := 0

	s.mu.Lock()
	for _, state := range s.tables {
		if state.TTL == nil || state.TTL.Status != "ENABLED" || state.TTL.AttributeName == "" {
			continue
		}
		attrName := state.TTL.AttributeName
		for keyID, stored := range state.Items {
			if !ttlExpired(stored.Item, attrName, nowValue) {
				continue
			}
			key, err := buildKeyMap(stored.Item, state.Table.KeySchema)
			if err != nil {
				continue
			}
			s.commitLocked(&pendingWrite{state: state, keyID: keyID, key: key, old: stored.Item}, now, ttlIdentity)
			deleted++
		}
	}
	s.mu.Unlock()

	if deleted > 0 {
		_ = s.persist()
	}
	return deleted
}

// ttlExpired reports whether item's ttlAttr is a Number <= nowValue.
func ttlExpired(item map[string]any, ttlAttr string, nowValue *big.Float) bool {
	raw, ok := item[ttlAttr]
	if !ok {
		return false
	}
	epoch, ok := asNumber(raw)
	if !ok {
		return false
	}
	return epoch.Cmp(nowValue) <= 0
}

// startTTLSweeper runs sweepExpired every interval until the returned stop
// function is called. stop blocks until the sweeper goroutine has exited and
// is safe to call more than once. A non-positive interval starts nothing and
// returns a no-op stop.
func (s *Store) startTTLSweeper(interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}
	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	exited := make(chan struct{})

	go func() {
		defer close(exited)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.sweepExpired(time.Now())
			case <-done:
				return
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-exited
	}
}
