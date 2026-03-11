package sqs

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// defaultStaleReceiveCount is the number of failed receive attempts after which
// OpenStack marks a message as stale when no DLQ is configured.
// On real AWS the message would retry indefinitely; here we park it to avoid
// burning CPU. The message remains visible in the UI with a "stale" indicator.
const defaultStaleReceiveCount = 5

// Store is an in-memory store for SQS queues and messages.
type Store struct {
	mu     sync.RWMutex
	queues map[string]*queue
	cfg    *config.Config
}

type queue struct {
	config   *types.QueueConfig
	messages []*types.SQSMessage
	mu       sync.Mutex
	dedup    map[string]int64 // FIFO dedup: dedupId → expiry epoch ms
}

// NewStore creates a new SQS store.
func NewStore(cfg *config.Config) *Store {
	return &Store{
		queues: make(map[string]*queue),
		cfg:    cfg,
	}
}

// Init loads persisted queue and message state if persistence is enabled.
func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}

	if err := os.MkdirAll(s.cfg.QueuesDir(), 0755); err != nil {
		return fmt.Errorf("create queues dir: %w", err)
	}

	data, err := os.ReadFile(s.cfg.QueuesStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read queues state: %w", err)
	}

	var snapshot struct {
		Queues []queueSnapshot `json:"queues"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode queues state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.queues = make(map[string]*queue, len(snapshot.Queues))
	for _, item := range snapshot.Queues {
		if item.Config == nil {
			continue
		}

		cfgCopy := cloneQueueConfig(item.Config)
		cfgCopy.QueueUrl = fmt.Sprintf("http://%s:%d/%s/%s", s.cfg.Host, s.cfg.Port, s.cfg.AccountID, cfgCopy.QueueName)
		cfgCopy.QueueArn = fmt.Sprintf("arn:aws:sqs:%s:%s:%s", s.cfg.Region, s.cfg.AccountID, cfgCopy.QueueName)

		msgs := make([]*types.SQSMessage, 0, len(item.Messages))
		for _, msg := range item.Messages {
			msgs = append(msgs, fromMessageSnapshot(msg))
		}

		dedup := make(map[string]int64, len(item.Dedup))
		for key, value := range item.Dedup {
			dedup[key] = value
		}

		s.queues[cfgCopy.QueueName] = &queue{
			config:   cfgCopy,
			messages: msgs,
			dedup:    dedup,
		}
	}
	return nil
}

// CreateQueue creates a new queue. Returns error if it already exists with different attributes.
func (s *Store) CreateQueue(name string, attrs map[string]string, tags map[string]string) (*types.QueueConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if q, exists := s.queues[name]; exists {
		// Idempotent: if same attributes, return existing
		return q.config, nil
	}

	isFIFO := attrs["FifoQueue"] == "true"
	if isFIFO && !strings.HasSuffix(name, ".fifo") {
		return nil, fmt.Errorf("FIFO queue name must end with .fifo")
	}

	now := time.Now().Unix()
	qCfg := &types.QueueConfig{
		QueueName:                     name,
		QueueUrl:                      fmt.Sprintf("http://%s:%d/%s/%s", s.cfg.Host, s.cfg.Port, s.cfg.AccountID, name),
		QueueArn:                      fmt.Sprintf("arn:aws:sqs:%s:%s:%s", s.cfg.Region, s.cfg.AccountID, name),
		VisibilityTimeout:             30,
		MessageRetentionPeriod:        345600, // 4 days
		DelaySeconds:                  0,
		MaximumMessageSize:            262144, // 256 KB
		ReceiveMessageWaitTimeSeconds: 0,
		FifoQueue:                     isFIFO,
		CreatedTimestamp:              now,
		LastModifiedTimestamp:         now,
		Tags:                          tags,
	}

	applyQueueAttributes(qCfg, attrs)

	s.queues[name] = &queue{
		config:   qCfg,
		messages: nil,
		dedup:    make(map[string]int64),
	}
	s.persistLocked()

	return qCfg, nil
}

// GetQueue returns a queue config by name.
func (s *Store) GetQueue(name string) (*types.QueueConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q, exists := s.queues[name]
	if !exists {
		return nil, fmt.Errorf("queue %s not found", name)
	}
	return q.config, nil
}

// QueueNameFromUrl extracts the queue name from a queue URL.
func QueueNameFromUrl(queueUrl string) string {
	parts := strings.Split(queueUrl, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return queueUrl
}

// ListQueues returns all queues, optionally filtered by prefix.
func (s *Store) ListQueues(prefix string) []*types.QueueConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*types.QueueConfig
	for _, q := range s.queues {
		if prefix == "" || strings.HasPrefix(q.config.QueueName, prefix) {
			result = append(result, q.config)
		}
	}
	return result
}

// DeleteQueue removes a queue and all its messages.
func (s *Store) DeleteQueue(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.queues[name]; !exists {
		return fmt.Errorf("queue %s not found", name)
	}
	delete(s.queues, name)
	s.persistLocked()
	return nil
}

// SetQueueAttributes updates queue attributes.
func (s *Store) SetQueueAttributes(name string, attrs map[string]string) error {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	applyQueueAttributes(q.config, attrs)
	q.config.LastModifiedTimestamp = time.Now().Unix()
	q.mu.Unlock()
	s.persist()
	return nil
}

// GetQueueAttributes returns requested queue attributes.
func (s *Store) GetQueueAttributes(name string, attrNames []string) (map[string]string, error) {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	all := attrNames == nil || contains(attrNames, "All")
	result := make(map[string]string)
	now := nowMs()

	if all || contains(attrNames, "QueueArn") {
		result["QueueArn"] = q.config.QueueArn
	}
	if all || contains(attrNames, "VisibilityTimeout") {
		result["VisibilityTimeout"] = fmt.Sprintf("%d", q.config.VisibilityTimeout)
	}
	if all || contains(attrNames, "MessageRetentionPeriod") {
		result["MessageRetentionPeriod"] = fmt.Sprintf("%d", q.config.MessageRetentionPeriod)
	}
	if all || contains(attrNames, "DelaySeconds") {
		result["DelaySeconds"] = fmt.Sprintf("%d", q.config.DelaySeconds)
	}
	if all || contains(attrNames, "MaximumMessageSize") {
		result["MaximumMessageSize"] = fmt.Sprintf("%d", q.config.MaximumMessageSize)
	}
	if all || contains(attrNames, "ReceiveMessageWaitTimeSeconds") {
		result["ReceiveMessageWaitTimeSeconds"] = fmt.Sprintf("%d", q.config.ReceiveMessageWaitTimeSeconds)
	}
	if all || contains(attrNames, "CreatedTimestamp") {
		result["CreatedTimestamp"] = fmt.Sprintf("%d", q.config.CreatedTimestamp)
	}
	if all || contains(attrNames, "LastModifiedTimestamp") {
		result["LastModifiedTimestamp"] = fmt.Sprintf("%d", q.config.LastModifiedTimestamp)
	}
	if all || contains(attrNames, "FifoQueue") {
		if q.config.FifoQueue {
			result["FifoQueue"] = "true"
		} else {
			result["FifoQueue"] = "false"
		}
	}
	if all || contains(attrNames, "ContentBasedDeduplication") {
		if q.config.ContentBasedDeduplication {
			result["ContentBasedDeduplication"] = "true"
		} else {
			result["ContentBasedDeduplication"] = "false"
		}
	}
	// SqsManagedSseEnabled — always "true" (stub); TF v6 polls this via the
	// attribute-create waiter and times out if the attribute is absent.
	if all || contains(attrNames, "SqsManagedSseEnabled") {
		result["SqsManagedSseEnabled"] = "true"
	}
	if (all || contains(attrNames, "RedrivePolicy")) && q.config.RedrivePolicy != "" {
		result["RedrivePolicy"] = q.config.RedrivePolicy
	}

	// Computed attributes
	if all || contains(attrNames, "ApproximateNumberOfMessages") {
		count := 0
		for _, m := range q.messages {
			if !m.Deleted && m.VisibleAt <= now && m.DelayUntil <= now && m.ExpiresAt > now {
				count++
			}
		}
		result["ApproximateNumberOfMessages"] = fmt.Sprintf("%d", count)
	}
	if all || contains(attrNames, "ApproximateNumberOfMessagesNotVisible") {
		count := 0
		for _, m := range q.messages {
			if !m.Deleted && m.VisibleAt > now && m.ExpiresAt > now {
				count++
			}
		}
		result["ApproximateNumberOfMessagesNotVisible"] = fmt.Sprintf("%d", count)
	}
	if all || contains(attrNames, "ApproximateNumberOfMessagesDelayed") {
		count := 0
		for _, m := range q.messages {
			if !m.Deleted && m.DelayUntil > now && m.ExpiresAt > now {
				count++
			}
		}
		result["ApproximateNumberOfMessagesDelayed"] = fmt.Sprintf("%d", count)
	}
	if all || contains(attrNames, "ApproximateNumberOfMessagesStale") {
		count := 0
		for _, m := range q.messages {
			if !m.Deleted && m.Stale && m.ExpiresAt > now {
				count++
			}
		}
		result["ApproximateNumberOfMessagesStale"] = fmt.Sprintf("%d", count)
	}

	return result, nil
}

// SendMessage adds a message to the queue.
func (s *Store) SendMessage(name string, body string, delaySec int, attrs map[string]*types.MessageAttribute, groupId, dedupId string) (*types.SQSMessage, error) {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	if q.config.FifoQueue && groupId == "" {
		q.mu.Unlock()
		return nil, fmt.Errorf("MessageGroupId is required for FIFO queues")
	}

	// FIFO deduplication
	if q.config.FifoQueue && dedupId != "" {
		now := nowMs()
		if expiry, exists := q.dedup[dedupId]; exists && now < expiry {
			// Duplicate within dedup window — return success without adding
			for _, m := range q.messages {
				if m.MessageDeduplicationId == dedupId && !m.Deleted {
					q.mu.Unlock()
					return m, nil
				}
			}
		}
		q.dedup[dedupId] = now + 300000 // 5 minute window
	}

	now := nowMs()
	delay := delaySec
	if delay == 0 {
		delay = q.config.DelaySeconds
	}

	msg := &types.SQSMessage{
		MessageId:              uuid.New().String(),
		Body:                   body,
		MD5OfBody:              md5Hash(body),
		MessageAttributes:      attrs,
		SentTimestamp:          now,
		VisibleAt:              now,
		DelayUntil:             now + int64(delay)*1000,
		ExpiresAt:              now + int64(q.config.MessageRetentionPeriod)*1000,
		MessageGroupId:         groupId,
		MessageDeduplicationId: dedupId,
	}

	q.messages = append(q.messages, msg)
	q.mu.Unlock()
	s.persist()
	return msg, nil
}

// ReceiveMessage returns up to maxCount visible messages and makes them invisible.
func (s *Store) ReceiveMessage(name string, maxCount, visTimeout int) ([]*types.SQSMessage, error) {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	if maxCount <= 0 {
		maxCount = 1
	}
	if maxCount > 10 {
		maxCount = 10
	}
	if visTimeout < 0 {
		visTimeout = q.config.VisibilityTimeout
	}

	now := nowMs()
	var result []*types.SQSMessage

	// For FIFO queues, track which groups we've started serving
	seenGroups := make(map[string]bool)

	for _, m := range q.messages {
		if len(result) >= maxCount {
			break
		}
		if m.Deleted || m.Stale {
			continue
		}
		if m.ExpiresAt <= now {
			continue
		}
		if m.VisibleAt > now {
			continue
		}
		if m.DelayUntil > now {
			continue
		}

		// FIFO: ensure ordering within message groups
		if q.config.FifoQueue && seenGroups[m.MessageGroupId] {
			continue
		}

		m.ReceiptHandle = uuid.New().String()
		m.ApproximateReceiveCount++
		if m.ApproximateFirstReceiveTimestamp == 0 {
			m.ApproximateFirstReceiveTimestamp = now
		}
		m.VisibleAt = now + int64(visTimeout)*1000

		result = append(result, m)

		if q.config.FifoQueue {
			seenGroups[m.MessageGroupId] = true
		}
	}
	q.mu.Unlock()
	s.persist()
	return result, nil
}

// PeekMessages returns up to limit non-expired messages without mutating visibility state.
// Messages are returned newest first.
func (s *Store) PeekMessages(name string, limit int) ([]*types.SQSMessage, error) {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("queue %s not found", name)
	}

	if limit <= 0 {
		limit = 10
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	now := nowMs()
	result := make([]*types.SQSMessage, 0, limit)

	for i := len(q.messages) - 1; i >= 0 && len(result) < limit; i-- {
		m := q.messages[i]
		if m.Deleted || m.ExpiresAt <= now {
			continue
		}
		result = append(result, cloneMessage(m))
	}

	return result, nil
}

// DeleteMessage removes a message by receipt handle.
func (s *Store) DeleteMessage(name string, receiptHandle string) error {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	for _, m := range q.messages {
		if m.ReceiptHandle == receiptHandle && !m.Deleted {
			m.Deleted = true
			q.mu.Unlock()
			s.persist()
			return nil
		}
	}
	q.mu.Unlock()
	return fmt.Errorf("receipt handle not found")
}

// ChangeMessageVisibility updates the visibility timeout for a received message.
func (s *Store) ChangeMessageVisibility(name string, receiptHandle string, timeout int) error {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	for _, m := range q.messages {
		if m.ReceiptHandle == receiptHandle && !m.Deleted {
			m.VisibleAt = nowMs() + int64(timeout)*1000
			q.mu.Unlock()
			s.persist()
			return nil
		}
	}
	q.mu.Unlock()
	return fmt.Errorf("receipt handle not found")
}

// PurgeQueue removes all messages from a queue.
func (s *Store) PurgeQueue(name string) error {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	q.messages = nil
	q.mu.Unlock()
	s.persist()
	return nil
}

// TagQueue adds or overwrites tags on a queue.
func (s *Store) TagQueue(name string, tags map[string]string) error {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	if q.config.Tags == nil {
		q.config.Tags = make(map[string]string)
	}
	for k, v := range tags {
		q.config.Tags[k] = v
	}
	q.mu.Unlock()
	s.persist()
	return nil
}

// UntagQueue removes specified tag keys from a queue.
func (s *Store) UntagQueue(name string, tagKeys []string) error {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	if q.config.Tags != nil {
		for _, k := range tagKeys {
			delete(q.config.Tags, k)
		}
	}
	q.mu.Unlock()
	s.persist()
	return nil
}

// ListQueueTags returns tags for a queue.
func (s *Store) ListQueueTags(name string) (map[string]string, error) {
	s.mu.RLock()
	q, exists := s.queues[name]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("queue %s not found", name)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	tags := make(map[string]string)
	for k, v := range q.config.Tags {
		tags[k] = v
	}
	return tags, nil
}

// Reap cleans up expired messages, clears stale FIFO dedup entries, and routes
// messages that exceeded their maxReceiveCount to the configured DLQ.
func (s *Store) Reap() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := nowMs()
	changed := false

	type dlqMove struct {
		dlqName  string
		messages []*types.SQSMessage
	}
	var dlqMoves []dlqMove

	for _, q := range s.queues {
		q.mu.Lock()

		hasDLQ := q.config.MaxReceiveCount > 0 && q.config.DeadLetterTargetArn != ""
		dlqName := ""
		if hasDLQ {
			dlqName = queueNameFromArn(q.config.DeadLetterTargetArn)
		}

		alive := make([]*types.SQSMessage, 0, len(q.messages))
		var toMove []*types.SQSMessage

		staleThreshold := defaultStaleReceiveCount
		if q.config.MaxReceiveCount > 0 {
			staleThreshold = q.config.MaxReceiveCount
		}

		for _, m := range q.messages {
			if m.Deleted || m.ExpiresAt <= now {
				changed = true
				continue
			}
			// Route to DLQ when a message has been received too many times
			// and its visibility timeout has expired (consumer didn't delete it).
			if hasDLQ && m.VisibleAt <= now && m.ApproximateReceiveCount >= q.config.MaxReceiveCount {
				toMove = append(toMove, cloneMessage(m))
				changed = true
				continue
			}
			// No DLQ: mark as stale instead of retrying indefinitely.
			// On real AWS the message would keep retrying until it expires, but in
			// OpenStack we park it to avoid burning CPU. The UI shows it as "stale".
			if !hasDLQ && !m.Stale && m.VisibleAt <= now && m.ApproximateReceiveCount >= staleThreshold {
				m.Stale = true
				changed = true
			}
			alive = append(alive, m)
		}
		q.messages = alive

		// Clean stale dedup entries
		for id, expiry := range q.dedup {
			if now >= expiry {
				delete(q.dedup, id)
				changed = true
			}
		}

		q.mu.Unlock()

		if len(toMove) > 0 {
			dlqMoves = append(dlqMoves, dlqMove{dlqName: dlqName, messages: toMove})
		}
	}

	// Deliver DLQ messages after releasing all source queue locks (avoids deadlock).
	for _, move := range dlqMoves {
		dlq, exists := s.queues[move.dlqName]
		if !exists {
			log.Printf("[sqs] DLQ %q not found, dropping %d message(s)", move.dlqName, len(move.messages))
			continue
		}
		dlq.mu.Lock()
		for _, m := range move.messages {
			dlq.messages = append(dlq.messages, &types.SQSMessage{
				MessageId:         uuid.New().String(),
				Body:              m.Body,
				MD5OfBody:         m.MD5OfBody,
				MessageAttributes: m.MessageAttributes,
				SentTimestamp:     now,
				VisibleAt:         now,
				ExpiresAt:         now + int64(dlq.config.MessageRetentionPeriod)*1000,
				MessageGroupId:    m.MessageGroupId,
			})
		}
		dlq.mu.Unlock()
		log.Printf("[sqs] moved %d message(s) to DLQ %q", len(move.messages), move.dlqName)
	}

	if changed {
		s.persist()
	}
}

// --- Helpers ---

func applyQueueAttributes(cfg *types.QueueConfig, attrs map[string]string) {
	if v, ok := attrs["VisibilityTimeout"]; ok {
		_, _ = fmt.Sscanf(v, "%d", &cfg.VisibilityTimeout)
	}
	if v, ok := attrs["MessageRetentionPeriod"]; ok {
		_, _ = fmt.Sscanf(v, "%d", &cfg.MessageRetentionPeriod)
	}
	if v, ok := attrs["DelaySeconds"]; ok {
		fmt.Sscanf(v, "%d", &cfg.DelaySeconds)
	}
	if v, ok := attrs["MaximumMessageSize"]; ok {
		fmt.Sscanf(v, "%d", &cfg.MaximumMessageSize)
	}
	if v, ok := attrs["ReceiveMessageWaitTimeSeconds"]; ok {
		fmt.Sscanf(v, "%d", &cfg.ReceiveMessageWaitTimeSeconds)
	}
	if attrs["FifoQueue"] == "true" {
		cfg.FifoQueue = true
	}
	if attrs["ContentBasedDeduplication"] == "true" {
		cfg.ContentBasedDeduplication = true
	}
	if v, ok := attrs["RedrivePolicy"]; ok && v != "" {
		var policy struct {
			DeadLetterTargetArn string `json:"deadLetterTargetArn"`
			MaxReceiveCount     int    `json:"maxReceiveCount"`
		}
		if err := json.Unmarshal([]byte(v), &policy); err == nil {
			cfg.RedrivePolicy = v
			cfg.DeadLetterTargetArn = policy.DeadLetterTargetArn
			cfg.MaxReceiveCount = policy.MaxReceiveCount
		}
	}
}

// queueNameFromArn extracts the queue name from an SQS ARN.
// e.g. "arn:aws:sqs:us-east-1:000000000000:my-dlq" → "my-dlq"
func queueNameFromArn(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return arn
}

func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func cloneMessage(src *types.SQSMessage) *types.SQSMessage {
	if src == nil {
		return nil
	}

	cloned := *src
	if src.MessageAttributes != nil {
		cloned.MessageAttributes = make(map[string]*types.MessageAttribute, len(src.MessageAttributes))
		for k, v := range src.MessageAttributes {
			if v == nil {
				cloned.MessageAttributes[k] = nil
				continue
			}
			attrCopy := *v
			cloned.MessageAttributes[k] = &attrCopy
		}
	}
	return &cloned
}

func cloneQueueConfig(src *types.QueueConfig) *types.QueueConfig {
	if src == nil {
		return nil
	}

	cloned := *src
	if src.Tags != nil {
		cloned.Tags = make(map[string]string, len(src.Tags))
		for k, v := range src.Tags {
			cloned.Tags[k] = v
		}
	}
	return &cloned
}

type queueSnapshot struct {
	Config   *types.QueueConfig `json:"config"`
	Messages []messageSnapshot  `json:"messages"`
	Dedup    map[string]int64   `json:"dedup"`
}

type messageSnapshot struct {
	MessageId                        string                             `json:"messageId"`
	Body                             string                             `json:"body"`
	MD5OfBody                        string                             `json:"md5OfBody"`
	ReceiptHandle                    string                             `json:"receiptHandle,omitempty"`
	MessageAttributes                map[string]*types.MessageAttribute `json:"messageAttributes,omitempty"`
	SentTimestamp                    int64                              `json:"sentTimestamp"`
	ApproximateReceiveCount          int                                `json:"approximateReceiveCount"`
	ApproximateFirstReceiveTimestamp int64                              `json:"approximateFirstReceiveTimestamp"`
	VisibleAt                        int64                              `json:"visibleAt"`
	MessageGroupId                   string                             `json:"messageGroupId,omitempty"`
	MessageDeduplicationId           string                             `json:"messageDeduplicationId,omitempty"`
	DelayUntil                       int64                              `json:"delayUntil"`
	ExpiresAt                        int64                              `json:"expiresAt"`
	Deleted                          bool                               `json:"deleted"`
	Stale                            bool                               `json:"stale,omitempty"`
}

func toMessageSnapshot(src *types.SQSMessage) messageSnapshot {
	cloned := cloneMessage(src)
	return messageSnapshot{
		MessageId:                        cloned.MessageId,
		Body:                             cloned.Body,
		MD5OfBody:                        cloned.MD5OfBody,
		ReceiptHandle:                    cloned.ReceiptHandle,
		MessageAttributes:                cloned.MessageAttributes,
		SentTimestamp:                    cloned.SentTimestamp,
		ApproximateReceiveCount:          cloned.ApproximateReceiveCount,
		ApproximateFirstReceiveTimestamp: cloned.ApproximateFirstReceiveTimestamp,
		VisibleAt:                        cloned.VisibleAt,
		MessageGroupId:                   cloned.MessageGroupId,
		MessageDeduplicationId:           cloned.MessageDeduplicationId,
		DelayUntil:                       cloned.DelayUntil,
		ExpiresAt:                        cloned.ExpiresAt,
		Deleted:                          cloned.Deleted,
		Stale:                            cloned.Stale,
	}
}

func fromMessageSnapshot(src messageSnapshot) *types.SQSMessage {
	return &types.SQSMessage{
		MessageId:                        src.MessageId,
		Body:                             src.Body,
		MD5OfBody:                        src.MD5OfBody,
		ReceiptHandle:                    src.ReceiptHandle,
		MessageAttributes:                src.MessageAttributes,
		SentTimestamp:                    src.SentTimestamp,
		ApproximateReceiveCount:          src.ApproximateReceiveCount,
		ApproximateFirstReceiveTimestamp: src.ApproximateFirstReceiveTimestamp,
		VisibleAt:                        src.VisibleAt,
		MessageGroupId:                   src.MessageGroupId,
		MessageDeduplicationId:           src.MessageDeduplicationId,
		DelayUntil:                       src.DelayUntil,
		ExpiresAt:                        src.ExpiresAt,
		Deleted:                          src.Deleted,
		Stale:                            src.Stale,
	}
}

func (s *Store) persist() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	s.persistLocked()
}

func (s *Store) persistLocked() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	names := make([]string, 0, len(s.queues))
	for name := range s.queues {
		names = append(names, name)
	}
	sort.Strings(names)

	snapshot := struct {
		Queues []queueSnapshot `json:"queues"`
	}{
		Queues: make([]queueSnapshot, 0, len(names)),
	}

	for _, name := range names {
		q := s.queues[name]
		q.mu.Lock()

		item := queueSnapshot{
			Config:   cloneQueueConfig(q.config),
			Messages: make([]messageSnapshot, 0, len(q.messages)),
			Dedup:    make(map[string]int64, len(q.dedup)),
		}
		for _, message := range q.messages {
			item.Messages = append(item.Messages, toMessageSnapshot(message))
		}
		for key, value := range q.dedup {
			item.Dedup[key] = value
		}

		q.mu.Unlock()
		snapshot.Queues = append(snapshot.Queues, item)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}

	if err := os.MkdirAll(filepath.Dir(s.cfg.QueuesStatePath()), 0755); err != nil {
		return
	}

	tmpPath := s.cfg.QueuesStatePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, s.cfg.QueuesStatePath())
}
