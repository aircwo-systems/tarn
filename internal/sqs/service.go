package sqs

import (
	"fmt"
	"log"
	"time"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// Service implements SQS business logic.
type Service struct {
	cfg   *config.Config
	store *Store
	done  chan struct{}
}

// NewService creates a new SQS service.
func NewService(cfg *config.Config) *Service {
	return &Service{
		cfg:   cfg,
		store: NewStore(cfg),
		done:  make(chan struct{}),
	}
}

// Init loads persisted queue state if configured.
func (s *Service) Init() error {
	return s.store.Init()
}

// Start begins the background reaper goroutine.
func (s *Service) Start() {
	go s.reaper()
	log.Println("[sqs] service started")
}

// Stop shuts down the background reaper.
func (s *Service) Stop() {
	close(s.done)
}

func (s *Service) reaper() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.store.Reap()
		}
	}
}

// CreateQueue creates a new SQS queue.
func (s *Service) CreateQueue(name string, attrs map[string]string, tags map[string]string) (*types.QueueConfig, error) {
	return s.store.CreateQueue(name, attrs, tags)
}

// GetQueue returns a queue by name.
func (s *Service) GetQueue(name string) (*types.QueueConfig, error) {
	return s.store.GetQueue(name)
}

// GetQueueUrl returns the queue URL for a given name.
func (s *Service) GetQueueUrl(name string) (string, error) {
	q, err := s.store.GetQueue(name)
	if err != nil {
		return "", err
	}
	return q.QueueUrl, nil
}

// ListQueues returns all queues, optionally filtered by prefix.
func (s *Service) ListQueues(prefix string) []*types.QueueConfig {
	return s.store.ListQueues(prefix)
}

// DeleteQueue removes a queue.
func (s *Service) DeleteQueue(name string) error {
	return s.store.DeleteQueue(name)
}

// SetQueueAttributes updates queue attributes.
func (s *Service) SetQueueAttributes(name string, attrs map[string]string) error {
	return s.store.SetQueueAttributes(name, attrs)
}

// GetQueueAttributes returns requested queue attributes.
func (s *Service) GetQueueAttributes(name string, attrNames []string) (map[string]string, error) {
	return s.store.GetQueueAttributes(name, attrNames)
}

// SendMessage sends a message to a queue.
func (s *Service) SendMessage(queueName, body string, delaySec int, attrs map[string]*types.MessageAttribute, groupId, dedupId string) (*types.SQSMessage, error) {
	return s.store.SendMessage(queueName, body, delaySec, attrs, groupId, dedupId)
}

// ReceiveMessage receives messages from a queue, supporting long polling.
func (s *Service) ReceiveMessage(queueName string, maxCount, visTimeout, waitTimeSec int) ([]*types.SQSMessage, error) {
	if visTimeout < 0 {
		// Use queue default
		q, err := s.store.GetQueue(queueName)
		if err != nil {
			return nil, err
		}
		visTimeout = q.VisibilityTimeout
	}

	// Immediate receive
	if waitTimeSec <= 0 {
		return s.store.ReceiveMessage(queueName, maxCount, visTimeout)
	}

	// Long polling: wait up to waitTimeSec for messages
	deadline := time.Now().Add(time.Duration(waitTimeSec) * time.Second)
	pollInterval := 100 * time.Millisecond

	for {
		msgs, err := s.store.ReceiveMessage(queueName, maxCount, visTimeout)
		if err != nil {
			return nil, err
		}
		if len(msgs) > 0 {
			return msgs, nil
		}

		if time.Now().After(deadline) {
			return []*types.SQSMessage{}, nil
		}

		select {
		case <-time.After(pollInterval):
		case <-s.done:
			return nil, fmt.Errorf("service shutting down")
		}
	}
}

// PeekMessages returns non-expired messages without mutating queue state.
func (s *Service) PeekMessages(queueName string, limit int) ([]*types.SQSMessage, error) {
	return s.store.PeekMessages(queueName, limit)
}

// DeleteMessage deletes a message by receipt handle.
func (s *Service) DeleteMessage(queueName, receiptHandle string) error {
	return s.store.DeleteMessage(queueName, receiptHandle)
}

// ChangeMessageVisibility changes the visibility timeout of a received message.
func (s *Service) ChangeMessageVisibility(queueName, receiptHandle string, timeout int) error {
	return s.store.ChangeMessageVisibility(queueName, receiptHandle, timeout)
}

// MoveToDLQIfExceeded checks whether msg has exceeded the queue's maxReceiveCount.
// If so, it delivers the message to the configured DLQ and deletes it from srcQueue.
// Returns (true, dlqName, nil) when moved, (false, "", nil) when below threshold or no DLQ configured.
func (s *Service) MoveToDLQIfExceeded(srcQueue string, msg *types.SQSMessage) (bool, string, error) {
	q, err := s.store.GetQueue(srcQueue)
	if err != nil {
		return false, "", err
	}
	if q.MaxReceiveCount <= 0 || q.DeadLetterTargetArn == "" {
		return false, "", nil
	}
	if msg.ApproximateReceiveCount < q.MaxReceiveCount {
		return false, "", nil
	}

	dlqName := queueNameFromArn(q.DeadLetterTargetArn)
	if _, err := s.SendMessage(dlqName, msg.Body, 0, msg.MessageAttributes, "", ""); err != nil {
		return false, "", fmt.Errorf("send to DLQ %q: %w", dlqName, err)
	}
	if err := s.DeleteMessage(srcQueue, msg.ReceiptHandle); err != nil {
		return false, "", fmt.Errorf("delete from source queue: %w", err)
	}
	return true, dlqName, nil
}

// PurgeQueue removes all messages from a queue.
func (s *Service) PurgeQueue(queueName string) error {
	return s.store.PurgeQueue(queueName)
}

// TagQueue adds or overwrites tags on a queue.
func (s *Service) TagQueue(queueName string, tags map[string]string) error {
	return s.store.TagQueue(queueName, tags)
}

// UntagQueue removes specified tag keys from a queue.
func (s *Service) UntagQueue(queueName string, tagKeys []string) error {
	return s.store.UntagQueue(queueName, tagKeys)
}

// ListQueueTags returns tags for a queue.
func (s *Service) ListQueueTags(queueName string) (map[string]string, error) {
	return s.store.ListQueueTags(queueName)
}
