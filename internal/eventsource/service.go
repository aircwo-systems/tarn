package eventsource

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// Service manages event source mappings and their background pollers.
type Service struct {
	cfg     *config.Config
	store   *Store
	lambda  LambdaInterface
	sqs     SQSInterface
	mu      sync.Mutex
	pollers map[string]*poller
	done    chan struct{}
}

// NewService creates a new event source mapping service.
func NewService(cfg *config.Config, store *Store, lambdaSvc LambdaInterface, sqsSvc SQSInterface) *Service {
	return &Service{
		cfg:     cfg,
		store:   store,
		lambda:  lambdaSvc,
		sqs:     sqsSvc,
		pollers: make(map[string]*poller),
		done:    make(chan struct{}),
	}
}

// Init loads persisted state.
func (s *Service) Init() error {
	return s.store.Init()
}

// Start begins pollers for all enabled mappings.
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.store.List() {
		if m.Enabled && m.State == "Enabled" {
			p := newPoller(m, s.sqs, s.lambda, s.store)
			p.start()
			s.pollers[m.UUID] = p
		}
	}
	log.Printf("[eventsource] service started with %d active pollers", len(s.pollers))
}

// Stop shuts down all pollers.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, p := range s.pollers {
		p.stop()
		delete(s.pollers, id)
	}
	close(s.done)
}

// CreateMapping creates and starts a new event source mapping.
func (s *Service) CreateMapping(eventSourceArn, functionArn, functionName string, batchSize, maxBatchingWindow int, enabled bool) (*types.EventSourceMapping, error) {
	queueName, err := parseQueueNameFromArn(eventSourceArn)
	if err != nil {
		return nil, err
	}

	if batchSize <= 0 {
		batchSize = 10
	}
	if batchSize > 10 {
		batchSize = 10
	}
	if maxBatchingWindow < 0 {
		maxBatchingWindow = 0
	}

	state := "Enabled"
	if !enabled {
		state = "Disabled"
	}

	now := time.Now().UTC()
	normalizedFunctionName := normalizeLambdaFunctionName(functionName)
	mapping := &types.EventSourceMapping{
		UUID:                           uuid.NewString(),
		EventSourceArn:                 eventSourceArn,
		FunctionArn:                    functionArn,
		FunctionName:                   normalizedFunctionName,
		QueueName:                      queueName,
		BatchSize:                      batchSize,
		MaximumBatchingWindowInSeconds: maxBatchingWindow,
		Enabled:                        enabled,
		State:                          state,
		LastModified:                   now,
	}

	if err := s.store.Save(mapping); err != nil {
		return nil, err
	}

	if mapping.Enabled {
		s.startPoller(mapping)
	}

	return mapping, nil
}

// GetMapping returns a mapping by UUID.
func (s *Service) GetMapping(uuid string) (*types.EventSourceMapping, error) {
	return s.store.Get(uuid)
}

// ListMappings returns all mappings, optionally filtered.
func (s *Service) ListMappings(eventSourceArn, functionName string) []*types.EventSourceMapping {
	all := s.store.List()
	if eventSourceArn == "" && functionName == "" {
		return all
	}

	var filtered []*types.EventSourceMapping
	for _, m := range all {
		if eventSourceArn != "" && m.EventSourceArn != eventSourceArn {
			continue
		}
		if functionName != "" && m.FunctionName != functionName {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// UpdateMapping updates a mapping's configuration.
func (s *Service) UpdateMapping(uuid string, batchSize *int, maxBatchingWindow *int, enabled *bool, functionName *string) (*types.EventSourceMapping, error) {
	mapping, err := s.store.Get(uuid)
	if err != nil {
		return nil, err
	}

	if batchSize != nil {
		bs := *batchSize
		if bs <= 0 {
			bs = 10
		}
		if bs > 10 {
			bs = 10
		}
		mapping.BatchSize = bs
	}
	if maxBatchingWindow != nil {
		mbw := *maxBatchingWindow
		if mbw < 0 {
			mbw = 0
		}
		mapping.MaximumBatchingWindowInSeconds = mbw
	}
	if functionName != nil {
		mapping.FunctionName = normalizeLambdaFunctionName(*functionName)
		// Terraform may send an ARN in FunctionName updates; keep FunctionArn in sync.
		if strings.HasPrefix(*functionName, "arn:") {
			mapping.FunctionArn = *functionName
		}
	}
	if enabled != nil {
		mapping.Enabled = *enabled
		if *enabled {
			mapping.State = "Enabled"
		} else {
			mapping.State = "Disabled"
		}
	}

	mapping.LastModified = time.Now().UTC()

	if err := s.store.Save(mapping); err != nil {
		return nil, err
	}

	// Restart or stop poller based on enabled state
	s.mu.Lock()
	if p, exists := s.pollers[uuid]; exists {
		p.stop()
		delete(s.pollers, uuid)
	}
	if mapping.Enabled {
		p := newPoller(mapping, s.sqs, s.lambda, s.store)
		p.start()
		s.pollers[uuid] = p
	}
	s.mu.Unlock()

	return mapping, nil
}

// DeleteMapping removes a mapping and stops its poller.
func (s *Service) DeleteMapping(uuid string) error {
	s.mu.Lock()
	if p, exists := s.pollers[uuid]; exists {
		p.stop()
		delete(s.pollers, uuid)
	}
	s.mu.Unlock()

	return s.store.Delete(uuid)
}

func (s *Service) startPoller(mapping *types.EventSourceMapping) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := newPoller(mapping, s.sqs, s.lambda, s.store)
	p.start()
	s.pollers[mapping.UUID] = p
}

// parseQueueNameFromArn extracts queue name from arn:aws:sqs:{region}:{account}:{queueName}
func parseQueueNameFromArn(arn string) (string, error) {
	if !strings.HasPrefix(arn, "arn:aws:sqs:") {
		return "", fmt.Errorf("invalid SQS ARN: %s", arn)
	}
	parts := strings.Split(arn, ":")
	if len(parts) < 6 || parts[5] == "" {
		return "", fmt.Errorf("invalid SQS ARN: %s", arn)
	}
	return parts[5], nil
}
