package sns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// Store is an in-memory store for SNS topics and subscriptions.
type Store struct {
	mu            sync.RWMutex
	persistMu     sync.Mutex
	cfg           *config.Config
	topics        map[string]*types.SNSTopic        // key: topic ARN
	topicByName   map[string]string                 // key: topic name, value: ARN
	subscriptions map[string]*types.SNSSubscription // key: subscription ARN
}

// NewStore creates a new SNS store.
func NewStore(cfg *config.Config) *Store {
	return &Store{
		cfg:           cfg,
		topics:        make(map[string]*types.SNSTopic),
		topicByName:   make(map[string]string),
		subscriptions: make(map[string]*types.SNSSubscription),
	}
}

// Init loads persisted SNS state if persistence is enabled.
func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}

	if err := os.MkdirAll(s.cfg.SNSDir(), 0755); err != nil {
		return fmt.Errorf("create sns dir: %w", err)
	}

	data, err := os.ReadFile(s.cfg.SNSStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read sns state: %w", err)
	}

	var snapshot struct {
		Topics        []*types.SNSTopic        `json:"topics"`
		Subscriptions []*types.SNSSubscription `json:"subscriptions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&snapshot); err != nil {
		return fmt.Errorf("decode sns state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.topics = make(map[string]*types.SNSTopic, len(snapshot.Topics))
	s.topicByName = make(map[string]string, len(snapshot.Topics))
	s.subscriptions = make(map[string]*types.SNSSubscription, len(snapshot.Subscriptions))

	for _, topic := range snapshot.Topics {
		if topic == nil || topic.TopicArn == "" || topic.Name == "" {
			continue
		}
		topicCopy := cloneTopic(topic)
		s.topics[topicCopy.TopicArn] = topicCopy
		s.topicByName[topicCopy.Name] = topicCopy.TopicArn
	}

	for _, sub := range snapshot.Subscriptions {
		if sub == nil || sub.SubscriptionArn == "" || sub.TopicArn == "" {
			continue
		}
		if _, exists := s.topics[sub.TopicArn]; !exists {
			continue
		}
		s.subscriptions[sub.SubscriptionArn] = cloneSubscription(sub)
	}

	return nil
}

func (s *Store) CreateTopic(name string, attrs map[string]string, tags map[string]string) (*types.SNSTopic, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("TopicName is required")
	}

	fifoTopic := parseBool(attrs["FifoTopic"])
	if strings.HasSuffix(name, ".fifo") {
		fifoTopic = true
	}
	if fifoTopic && !strings.HasSuffix(name, ".fifo") {
		return nil, fmt.Errorf("FIFO topic name must end with .fifo")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if arn, exists := s.topicByName[name]; exists {
		return cloneTopic(s.topics[arn]), nil
	}

	now := time.Now().Unix()
	topicArn := topicARN(s.cfg, name)
	topic := &types.SNSTopic{
		TopicArn:                  topicArn,
		Name:                      name,
		Owner:                     s.cfg.AccountID,
		DisplayName:               attrs["DisplayName"],
		Policy:                    attrs["Policy"],
		DeliveryPolicy:            attrs["DeliveryPolicy"],
		EffectiveDeliveryPolicy:   attrs["EffectiveDeliveryPolicy"],
		KmsMasterKeyId:            attrs["KmsMasterKeyId"],
		FifoTopic:                 fifoTopic,
		ContentBasedDeduplication: parseBool(attrs["ContentBasedDeduplication"]),
		Attributes:                make(map[string]string),
		Tags:                      cloneStringMap(tags),
		CreatedTimestamp:          now,
		LastModifiedTimestamp:     now,
	}
	for key, value := range attrs {
		if isManagedTopicAttribute(key) {
			continue
		}
		topic.Attributes[key] = value
	}

	s.topics[topicArn] = topic
	s.topicByName[name] = topicArn
	s.persistLocked()
	return cloneTopic(topic), nil
}

func (s *Store) DeleteTopic(topicArn string) error {
	topicArn = strings.TrimSpace(topicArn)
	if topicArn == "" {
		return fmt.Errorf("TopicArn is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	topic, exists := s.topics[topicArn]
	if !exists {
		return fmt.Errorf("topic %s not found", topicArn)
	}
	delete(s.topics, topicArn)
	delete(s.topicByName, topic.Name)

	for arn, sub := range s.subscriptions {
		if sub.TopicArn == topicArn {
			delete(s.subscriptions, arn)
		}
	}

	s.persistLocked()
	return nil
}

func (s *Store) GetTopic(topicArn string) (*types.SNSTopic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	topic, exists := s.topics[strings.TrimSpace(topicArn)]
	if !exists {
		return nil, fmt.Errorf("topic %s not found", topicArn)
	}
	return cloneTopic(topic), nil
}

func (s *Store) GetTopicByName(name string) (*types.SNSTopic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	arn, exists := s.topicByName[strings.TrimSpace(name)]
	if !exists {
		return nil, fmt.Errorf("topic %s not found", name)
	}
	return cloneTopic(s.topics[arn]), nil
}

func (s *Store) ListTopics() []*types.SNSTopic {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*types.SNSTopic, 0, len(s.topics))
	for _, topic := range s.topics {
		out = append(out, cloneTopic(topic))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) GetTopicAttributes(topicArn string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	topic, exists := s.topics[strings.TrimSpace(topicArn)]
	if !exists {
		return nil, fmt.Errorf("topic %s not found", topicArn)
	}

	confirmed, pending, deleted := s.subscriptionCountsLocked(topic.TopicArn)
	policy := strings.TrimSpace(topic.Policy)
	if policy == "" {
		policy = defaultTopicPolicy(s.cfg, topic.TopicArn)
	}

	attrs := map[string]string{
		"TopicArn":                  topic.TopicArn,
		"Owner":                     topic.Owner,
		"DisplayName":               topic.DisplayName,
		"Policy":                    policy,
		"DeliveryPolicy":            topic.DeliveryPolicy,
		"EffectiveDeliveryPolicy":   topic.EffectiveDeliveryPolicy,
		"KmsMasterKeyId":            topic.KmsMasterKeyId,
		"FifoTopic":                 boolToString(topic.FifoTopic),
		"ContentBasedDeduplication": boolToString(topic.ContentBasedDeduplication),
		"SubscriptionsConfirmed":    fmt.Sprintf("%d", confirmed),
		"SubscriptionsPending":      fmt.Sprintf("%d", pending),
		"SubscriptionsDeleted":      fmt.Sprintf("%d", deleted),
	}
	for key, value := range topic.Attributes {
		attrs[key] = value
	}
	return attrs, nil
}

func (s *Store) SetTopicAttribute(topicArn, name, value string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("AttributeName is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	topic, exists := s.topics[strings.TrimSpace(topicArn)]
	if !exists {
		return fmt.Errorf("topic %s not found", topicArn)
	}

	switch name {
	case "DisplayName":
		topic.DisplayName = value
	case "Policy":
		topic.Policy = value
	case "DeliveryPolicy":
		topic.DeliveryPolicy = value
	case "EffectiveDeliveryPolicy":
		topic.EffectiveDeliveryPolicy = value
	case "KmsMasterKeyId":
		topic.KmsMasterKeyId = value
	case "ContentBasedDeduplication":
		topic.ContentBasedDeduplication = parseBool(value)
	case "FifoTopic":
		fifoTopic := parseBool(value)
		if fifoTopic && !strings.HasSuffix(topic.Name, ".fifo") {
			return fmt.Errorf("FIFO topic name must end with .fifo")
		}
		topic.FifoTopic = fifoTopic
	default:
		if topic.Attributes == nil {
			topic.Attributes = make(map[string]string)
		}
		topic.Attributes[name] = value
	}

	topic.LastModifiedTimestamp = time.Now().Unix()
	s.persistLocked()
	return nil
}

func (s *Store) TagTopic(topicArn string, tags map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	topic, exists := s.topics[strings.TrimSpace(topicArn)]
	if !exists {
		return fmt.Errorf("topic %s not found", topicArn)
	}
	if topic.Tags == nil {
		topic.Tags = make(map[string]string)
	}
	for k, v := range tags {
		topic.Tags[k] = v
	}
	topic.LastModifiedTimestamp = time.Now().Unix()
	s.persistLocked()
	return nil
}

func (s *Store) UntagTopic(topicArn string, tagKeys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	topic, exists := s.topics[strings.TrimSpace(topicArn)]
	if !exists {
		return fmt.Errorf("topic %s not found", topicArn)
	}
	if topic.Tags != nil {
		for _, key := range tagKeys {
			delete(topic.Tags, key)
		}
	}
	topic.LastModifiedTimestamp = time.Now().Unix()
	s.persistLocked()
	return nil
}

func (s *Store) ListTopicTags(topicArn string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	topic, exists := s.topics[strings.TrimSpace(topicArn)]
	if !exists {
		return nil, fmt.Errorf("topic %s not found", topicArn)
	}
	return cloneStringMap(topic.Tags), nil
}

func (s *Store) Subscribe(topicArn, protocol, endpoint string, attrs map[string]string) (*types.SNSSubscription, error) {
	topicArn = strings.TrimSpace(topicArn)
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	endpoint = strings.TrimSpace(endpoint)
	if topicArn == "" {
		return nil, fmt.Errorf("TopicArn is required")
	}
	if protocol == "" {
		return nil, fmt.Errorf("protocol is required")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.topics[topicArn]; !exists {
		return nil, fmt.Errorf("topic %s not found", topicArn)
	}

	for _, sub := range s.subscriptions {
		if sub.TopicArn == topicArn && sub.Protocol == protocol && sub.Endpoint == endpoint {
			s.applySubscriptionAttributesLocked(sub, attrs)
			if sub.LastModifiedTimestamp == 0 {
				sub.LastModifiedTimestamp = time.Now().Unix()
			}
			s.persistLocked()
			return cloneSubscription(sub), nil
		}
	}

	now := time.Now().Unix()
	subArn := subscriptionARN(s.cfg, topicArn)
	sub := &types.SNSSubscription{
		SubscriptionArn:       subArn,
		TopicArn:              topicArn,
		Protocol:              protocol,
		Endpoint:              endpoint,
		Owner:                 s.cfg.AccountID,
		FilterPolicyScope:     "MessageAttributes",
		CreatedTimestamp:      now,
		LastModifiedTimestamp: now,
	}
	s.applySubscriptionAttributesLocked(sub, attrs)
	s.subscriptions[subArn] = sub
	s.persistLocked()
	return cloneSubscription(sub), nil
}

func (s *Store) Unsubscribe(subscriptionArn string) error {
	subscriptionArn = strings.TrimSpace(subscriptionArn)
	if subscriptionArn == "" {
		return fmt.Errorf("SubscriptionArn is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscriptions[subscriptionArn]; !exists {
		return fmt.Errorf("subscription %s not found", subscriptionArn)
	}
	delete(s.subscriptions, subscriptionArn)
	s.persistLocked()
	return nil
}

func (s *Store) GetSubscription(subscriptionArn string) (*types.SNSSubscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sub, exists := s.subscriptions[strings.TrimSpace(subscriptionArn)]
	if !exists {
		return nil, fmt.Errorf("subscription %s not found", subscriptionArn)
	}
	return cloneSubscription(sub), nil
}

func (s *Store) GetSubscriptionAttributes(subscriptionArn string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sub, exists := s.subscriptions[strings.TrimSpace(subscriptionArn)]
	if !exists {
		return nil, fmt.Errorf("subscription %s not found", subscriptionArn)
	}
	return map[string]string{
		"SubscriptionArn":     sub.SubscriptionArn,
		"TopicArn":            sub.TopicArn,
		"Protocol":            sub.Protocol,
		"Endpoint":            sub.Endpoint,
		"Owner":               sub.Owner,
		"PendingConfirmation": boolToString(sub.PendingConfirmation),
		"RawMessageDelivery":  boolToString(sub.RawMessageDelivery),
		"FilterPolicy":        sub.FilterPolicy,
		"FilterPolicyScope":   sub.FilterPolicyScope,
		"DeliveryPolicy":      sub.DeliveryPolicy,
		"RedrivePolicy":       sub.RedrivePolicy,
	}, nil
}

func (s *Store) SetSubscriptionAttribute(subscriptionArn, name, value string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("AttributeName is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sub, exists := s.subscriptions[strings.TrimSpace(subscriptionArn)]
	if !exists {
		return fmt.Errorf("subscription %s not found", subscriptionArn)
	}

	switch name {
	case "RawMessageDelivery":
		sub.RawMessageDelivery = parseBool(value)
	case "FilterPolicy":
		sub.FilterPolicy = value
	case "FilterPolicyScope":
		if value == "" {
			value = "MessageAttributes"
		}
		sub.FilterPolicyScope = value
	case "DeliveryPolicy":
		sub.DeliveryPolicy = value
	case "RedrivePolicy":
		sub.RedrivePolicy = value
	default:
		return fmt.Errorf("unsupported subscription attribute %q", name)
	}

	sub.LastModifiedTimestamp = time.Now().Unix()
	s.persistLocked()
	return nil
}

func (s *Store) ListSubscriptions() []*types.SNSSubscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*types.SNSSubscription, 0, len(s.subscriptions))
	for _, sub := range s.subscriptions {
		out = append(out, cloneSubscription(sub))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TopicArn == out[j].TopicArn {
			return out[i].SubscriptionArn < out[j].SubscriptionArn
		}
		return out[i].TopicArn < out[j].TopicArn
	})
	return out
}

func (s *Store) ListSubscriptionsByTopic(topicArn string) ([]*types.SNSSubscription, error) {
	topicArn = strings.TrimSpace(topicArn)
	if topicArn == "" {
		return nil, fmt.Errorf("TopicArn is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.topics[topicArn]; !exists {
		return nil, fmt.Errorf("topic %s not found", topicArn)
	}

	out := make([]*types.SNSSubscription, 0)
	for _, sub := range s.subscriptions {
		if sub.TopicArn == topicArn {
			out = append(out, cloneSubscription(sub))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SubscriptionArn < out[j].SubscriptionArn })
	return out, nil
}

func (s *Store) persistLocked() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	topics := make([]*types.SNSTopic, 0, len(s.topics))
	for _, topic := range s.topics {
		topics = append(topics, cloneTopic(topic))
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].Name < topics[j].Name })

	subs := make([]*types.SNSSubscription, 0, len(s.subscriptions))
	for _, sub := range s.subscriptions {
		subs = append(subs, cloneSubscription(sub))
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].SubscriptionArn < subs[j].SubscriptionArn })

	snapshot := struct {
		Topics        []*types.SNSTopic        `json:"topics"`
		Subscriptions []*types.SNSSubscription `json:"subscriptions"`
	}{
		Topics:        topics,
		Subscriptions: subs,
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}

	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	if err := os.MkdirAll(s.cfg.SNSDir(), 0755); err != nil {
		return
	}

	tmpPath := s.cfg.SNSStatePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return
	}
	if err := os.Rename(tmpPath, s.cfg.SNSStatePath()); err != nil {
		_ = os.Remove(tmpPath)
	}
}

func (s *Store) applySubscriptionAttributesLocked(sub *types.SNSSubscription, attrs map[string]string) {
	if attrs == nil {
		return
	}
	if v, ok := attrs["RawMessageDelivery"]; ok {
		sub.RawMessageDelivery = parseBool(v)
	}
	if v, ok := attrs["FilterPolicy"]; ok {
		sub.FilterPolicy = v
	}
	if v, ok := attrs["FilterPolicyScope"]; ok {
		if strings.TrimSpace(v) == "" {
			v = "MessageAttributes"
		}
		sub.FilterPolicyScope = v
	}
	if v, ok := attrs["DeliveryPolicy"]; ok {
		sub.DeliveryPolicy = v
	}
	if v, ok := attrs["RedrivePolicy"]; ok {
		sub.RedrivePolicy = v
	}
	sub.LastModifiedTimestamp = time.Now().Unix()
}

func (s *Store) subscriptionCountsLocked(topicArn string) (confirmed, pending, deleted int) {
	for _, sub := range s.subscriptions {
		if sub.TopicArn != topicArn {
			continue
		}
		if sub.PendingConfirmation {
			pending++
			continue
		}
		confirmed++
	}
	return confirmed, pending, deleted
}

func cloneTopic(in *types.SNSTopic) *types.SNSTopic {
	if in == nil {
		return nil
	}
	out := *in
	out.Attributes = cloneStringMap(in.Attributes)
	out.Tags = cloneStringMap(in.Tags)
	return &out
}

func cloneSubscription(in *types.SNSSubscription) *types.SNSSubscription {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func topicARN(cfg *config.Config, topicName string) string {
	return fmt.Sprintf("arn:aws:sns:%s:%s:%s", cfg.Region, cfg.AccountID, topicName)
}

func subscriptionARN(cfg *config.Config, topicArn string) string {
	topicName := topicNameFromARN(topicArn)
	return fmt.Sprintf("arn:aws:sns:%s:%s:%s:%s", cfg.Region, cfg.AccountID, topicName, uuid.NewString())
}

func topicNameFromARN(topicArn string) string {
	parts := strings.Split(topicArn, ":")
	if len(parts) == 0 {
		return topicArn
	}
	return parts[len(parts)-1]
}

func parseBool(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	return v == "1" || v == "true" || v == "yes"
}

func boolToString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func defaultTopicPolicy(cfg *config.Config, topicArn string) string {
	accountID := "000000000000"
	if cfg != nil && strings.TrimSpace(cfg.AccountID) != "" {
		accountID = cfg.AccountID
	}

	doc := map[string]any{
		"Version": "2008-10-17",
		"Id":      "__default_policy_ID",
		"Statement": []map[string]any{
			{
				"Sid":    "__default_statement_ID",
				"Effect": "Allow",
				"Principal": map[string]string{
					"AWS": "*",
				},
				"Action": []string{
					"SNS:Publish",
					"SNS:Subscribe",
					"SNS:SetTopicAttributes",
					"SNS:RemovePermission",
					"SNS:Receive",
					"SNS:DeleteTopic",
					"SNS:AddPermission",
					"SNS:GetTopicAttributes",
					"SNS:ListSubscriptionsByTopic",
				},
				"Resource": topicArn,
				"Condition": map[string]map[string]string{
					"StringEquals": {
						"AWS:SourceOwner": accountID,
					},
				},
			},
		},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func isManagedTopicAttribute(name string) bool {
	switch strings.TrimSpace(name) {
	case "TopicArn",
		"Owner",
		"DisplayName",
		"Policy",
		"DeliveryPolicy",
		"EffectiveDeliveryPolicy",
		"KmsMasterKeyId",
		"FifoTopic",
		"ContentBasedDeduplication",
		"SubscriptionsConfirmed",
		"SubscriptionsPending",
		"SubscriptionsDeleted":
		return true
	default:
		return false
	}
}
