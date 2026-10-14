package sqs

import (
	"fmt"
	"maps"
	"strconv"

	"github.com/aircwo-systems/tarn/pkg/types"
)

type dlqCandidate struct {
	id      string
	receipt string
}

// lockQueuePair uses the same order for transfers and redrive. Names differ.
func lockQueuePair(fromName, toName string, from, to *queue) func() {
	if fromName < toName {
		from.mu.Lock()
		to.mu.Lock()
	} else {
		to.mu.Lock()
		from.mu.Lock()
	}
	return func() { from.mu.Unlock(); to.mu.Unlock() }
}

// moveToDLQ resolves and locks both live queues, then revalidates the receipt,
// policy, and eligibility before appending and removing once. The reaper waits
// for visibility expiry; the poller keeps send disruption, delay, and dedup.
// Tracing happens after unlocking, using the returned destination snapshot.
func (s *Store) moveToDLQ(source string, candidate dlqCandidate, reaping bool, beforeSend func(string) error) (*types.SQSMessage, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	from, exists := s.queues[source]
	if !exists {
		return nil, "", fmt.Errorf("source queue %s not found", source)
	}
	from.mu.Lock()
	target := queueNameFromArn(from.config.DeadLetterTargetArn)
	eligible := dlqMessageLocked(from, candidate, reaping) != nil
	from.mu.Unlock()
	if !eligible {
		return nil, "", nil
	}
	to, exists := s.queues[target]
	if !exists {
		return nil, "", fmt.Errorf("DLQ %q not found", target)
	}
	if source == target {
		return nil, "", fmt.Errorf("source and DLQ must be different")
	}
	unlock := lockQueuePair(source, target, from, to)
	defer unlock()
	if from.config.MaxReceiveCount <= 0 || queueNameFromArn(from.config.DeadLetterTargetArn) != target {
		return nil, "", nil // Policy changed while acquiring the pair.
	}
	current := dlqMessageLocked(from, candidate, reaping)
	if current == nil {
		return nil, "", nil
	}
	if beforeSend != nil {
		if err := beforeSend(target); err != nil {
			return nil, "", fmt.Errorf("send to DLQ %q: %w", target, err)
		}
	}
	// Only the outer map changes here. Enqueue deep-copies attributes under
	// the locks, so binary values need not be copied twice during preparation.
	attrs := maps.Clone(current.MessageAttributes)
	if attrs == nil {
		attrs = make(map[string]*types.MessageAttribute, 1)
	}
	attrs[dlqRetryCountAttribute] = &types.MessageAttribute{DataType: "Number", StringValue: strconv.Itoa(current.ApproximateReceiveCount)}
	delay := 0
	if !reaping {
		delay = to.config.DelaySeconds
	}
	delivered, err := enqueueMessageLocked(to, current.Body, delay, attrs, current.MessageGroupId, current.MessageDeduplicationId, !reaping)
	if err != nil {
		return nil, "", fmt.Errorf("send to DLQ %q: %w", target, err)
	}
	current.Deleted = true
	if from.config.FifoQueue {
		from.signal()
	}
	s.dirty.Store(true)
	return cloneMessage(delivered), target, nil
}

// dlqMessageLocked checks current stored state, never caller-owned counters.
func dlqMessageLocked(q *queue, candidate dlqCandidate, reaping bool) *types.SQSMessage {
	if q.config.MaxReceiveCount <= 0 || q.config.DeadLetterTargetArn == "" {
		return nil
	}
	now := nowMs()
	for _, current := range q.messages {
		if current.MessageId != candidate.id || current.ReceiptHandle != candidate.receipt || current.Deleted || current.ExpiresAt <= now {
			continue
		}
		if current.ApproximateReceiveCount < q.config.MaxReceiveCount || (reaping && current.VisibleAt > now) {
			return nil
		}
		return current
	}
	return nil
}
