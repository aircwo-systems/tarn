package eventsource

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

// Service manages event source mappings and their background pollers.
type Service struct {
	cfg        *config.Config
	store      *Store
	lambda     LambdaInterface
	sqs        SQSInterface
	streams    StreamInterface
	traceStore *tracesvc.Store
	collector  *tracesvc.Collector
	mu         sync.Mutex
	pollers    map[string]*poller
	done       chan struct{}
}

// NewService creates a new event source mapping service.
func NewService(cfg *config.Config, store *Store, lambdaSvc LambdaInterface, sqsSvc SQSInterface, streamsSvc StreamInterface) *Service {
	return &Service{
		cfg:     cfg,
		store:   store,
		lambda:  lambdaSvc,
		sqs:     sqsSvc,
		streams: streamsSvc,
		pollers: make(map[string]*poller),
		done:    make(chan struct{}),
	}
}

// SetTraceStore attaches a trace store so ESM invocations are recorded.
func (s *Service) SetTraceStore(ts *tracesvc.Store) { s.traceStore = ts }

// SetCollector attaches a trace collector so sub-spans during ESM invocations are captured.
func (s *Service) SetCollector(c *tracesvc.Collector) { s.collector = c }

// Init loads persisted state.
func (s *Service) Init() error {
	if err := s.store.Init(); err != nil {
		return err
	}
	return s.dedupeMappings()
}

func (s *Service) dedupeMappings() error {
	all := s.store.List()
	if len(all) <= 1 {
		return nil
	}

	type winner struct {
		mapping *types.EventSourceMapping
	}
	byKey := map[string]winner{}
	duplicates := make([]*types.EventSourceMapping, 0)

	for _, m := range all {
		normalizedFunctionName := normalizeLambdaFunctionName(m.FunctionName)
		key := m.EventSourceArn + "|" + normalizedFunctionName

		if cur, ok := byKey[key]; !ok {
			byKey[key] = winner{mapping: m}
			continue
		} else {
			// Keep the most recently modified mapping for this unique key.
			if m.LastModified.After(cur.mapping.LastModified) {
				duplicates = append(duplicates, cur.mapping)
				byKey[key] = winner{mapping: m}
			} else {
				duplicates = append(duplicates, m)
			}
		}
	}

	for _, dup := range duplicates {
		if err := s.store.Delete(dup.UUID); err != nil {
			return fmt.Errorf("delete duplicate mapping %s: %w", dup.UUID, err)
		}
	}

	// Normalize function names of surviving mappings so future lookups are stable.
	for _, item := range byKey {
		m := item.mapping
		normalized := normalizeLambdaFunctionName(m.FunctionName)
		if m.FunctionName == normalized {
			continue
		}
		m.FunctionName = normalized
		m.LastModified = time.Now().UTC()
		if err := s.store.Save(m); err != nil {
			return fmt.Errorf("normalize mapping %s function name: %w", m.UUID, err)
		}
	}

	if len(duplicates) > 0 {
		log.Printf("[eventsource] removed %d duplicate event source mapping(s)", len(duplicates))
	}

	return nil
}

// Start begins pollers for all mappings.
// Legacy compatibility: mappings disabled by an old bug (persisted as Disabled
// with an ERROR result) are re-enabled on startup.
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.store.List() {
		legacyDisabled := !m.Enabled && m.State == "Disabled" && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(m.LastProcessingResult)), "ERROR:")
		if legacyDisabled {
			m.Enabled = true
			m.State = "Enabled"
			_ = s.store.Save(m)
		}
		if !m.Enabled || m.State != "Enabled" {
			continue
		}
		p := newPoller(m, s.sqs, s.streams, s.lambda, s.store, s.traceStore, s.collector)
		p.start()
		s.pollers[m.UUID] = p
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
func (s *Service) CreateMapping(eventSourceArn, functionArn, functionName string, batchSize, maxBatchingWindow int, enabled bool, filterCriteria *types.FilterCriteria) (*types.EventSourceMapping, error) {
	sourceType, sourceName, err := parseSourceFromArn(eventSourceArn)
	if err != nil {
		return nil, err
	}

	normalizedFunctionName := normalizeLambdaFunctionName(functionName)
	var existing *types.EventSourceMapping
	for _, m := range s.store.List() {
		if m.EventSourceArn == eventSourceArn && normalizeLambdaFunctionName(m.FunctionName) == normalizedFunctionName {
			existing = m
			break
		}
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

	// Idempotent create/upsert for the same (eventSourceArn, functionName) pair.
	// This keeps mappings unique per lambda+event-source while making repeated
	// applies resilient when persisted mappings already exist.
	if existing != nil {
		// Stop any currently running poller before swapping mapping config.
		// This avoids mutating a mapping object while a poller goroutine is
		// concurrently reading from it.
		s.mu.Lock()
		if p, exists := s.pollers[existing.UUID]; exists {
			p.stop()
			delete(s.pollers, existing.UUID)
		}
		s.mu.Unlock()

		updated := *existing
		updated.FunctionArn = functionArn
		updated.FunctionName = normalizedFunctionName
		updated.SourceType = sourceType
		updated.SourceName = sourceName
		updated.QueueName = sourceName
		updated.BatchSize = batchSize
		updated.MaximumBatchingWindowInSeconds = maxBatchingWindow
		updated.Enabled = enabled
		updated.FilterCriteria = filterCriteria
		updated.LastModified = time.Now().UTC()
		if enabled {
			updated.State = "Enabled"
		} else {
			updated.State = "Disabled"
		}

		if err := s.store.Save(&updated); err != nil {
			return nil, err
		}

		// Reconcile poller state with updated mapping config.
		s.mu.Lock()
		if updated.Enabled && s.lambda != nil && sourceReady(&updated, s.sqs, s.streams) {
			p := newPoller(&updated, s.sqs, s.streams, s.lambda, s.store, s.traceStore, s.collector)
			p.start()
			s.pollers[updated.UUID] = p
		}
		s.mu.Unlock()

		return &updated, nil
	}

	state := "Enabled"
	if !enabled {
		state = "Disabled"
	}

	now := time.Now().UTC()
	mapping := &types.EventSourceMapping{
		UUID:                           uuid.NewString(),
		EventSourceArn:                 eventSourceArn,
		FunctionArn:                    functionArn,
		FunctionName:                   normalizedFunctionName,
		SourceType:                     sourceType,
		SourceName:                     sourceName,
		QueueName:                      sourceName,
		BatchSize:                      batchSize,
		MaximumBatchingWindowInSeconds: maxBatchingWindow,
		Enabled:                        enabled,
		State:                          state,
		LastModified:                   now,
		FilterCriteria:                 filterCriteria,
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
func (s *Service) UpdateMapping(uuid string, batchSize *int, maxBatchingWindow *int, enabled *bool, functionName *string, filterCriteria *types.FilterCriteria) (*types.EventSourceMapping, error) {
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
	if filterCriteria != nil {
		mapping.FilterCriteria = filterCriteria
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
	if mapping.Enabled && s.lambda != nil && sourceReady(mapping, s.sqs, s.streams) {
		p := newPoller(mapping, s.sqs, s.streams, s.lambda, s.store, s.traceStore, s.collector)
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
	if s.lambda == nil || !sourceReady(mapping, s.sqs, s.streams) {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p := newPoller(mapping, s.sqs, s.streams, s.lambda, s.store, s.traceStore, s.collector)
	p.start()
	s.pollers[mapping.UUID] = p
}

func parseSourceFromArn(arn string) (string, string, error) {
	switch {
	case strings.HasPrefix(arn, "arn:aws:sqs:"):
		parts := strings.Split(arn, ":")
		if len(parts) < 6 || parts[5] == "" {
			return "", "", fmt.Errorf("invalid SQS ARN: %s", arn)
		}
		return "sqs", parts[5], nil
	case strings.HasPrefix(arn, "arn:aws:dynamodb:") && strings.Contains(arn, "/stream/"):
		parts := strings.Split(arn, ":")
		if len(parts) < 6 || parts[5] == "" {
			return "", "", fmt.Errorf("invalid DynamoDB stream ARN: %s", arn)
		}
		resource := parts[5]
		resourceParts := strings.Split(resource, "/")
		if len(resourceParts) < 2 || resourceParts[0] != "table" {
			return "", "", fmt.Errorf("invalid DynamoDB stream ARN: %s", arn)
		}
		return "dynamodb-stream", resourceParts[1], nil
	default:
		return "", "", fmt.Errorf("unsupported event source ARN: %s", arn)
	}
}

func sourceReady(mapping *types.EventSourceMapping, sqsSvc SQSInterface, streamsSvc StreamInterface) bool {
	switch normalizeSourceType(mapping) {
	case "dynamodb-stream":
		return streamsSvc != nil
	default:
		return sqsSvc != nil
	}
}

func normalizeSourceType(mapping *types.EventSourceMapping) string {
	if mapping == nil {
		return "sqs"
	}
	if strings.TrimSpace(mapping.SourceType) != "" {
		return strings.TrimSpace(mapping.SourceType)
	}
	if strings.HasPrefix(mapping.EventSourceArn, "arn:aws:dynamodb:") {
		return "dynamodb-stream"
	}
	return "sqs"
}
