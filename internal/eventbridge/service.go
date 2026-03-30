package eventbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/aircwo-systems/tarn/internal/config"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const (
	defaultEventBusName       = "default"
	maxTargetBatchSize        = 10
	defaultRuleListLimit      = 100
	maxRuleListLimit          = 100
	defaultTargetListLimit    = 100
	maxTargetListLimit        = 100
	defaultRuleFireTimeout    = 30 * time.Second
	schedulerTickInterval     = 1 * time.Second
	manualInvocationTypeEvent = "Event"
)

// LambdaInterface defines the Lambda behavior required by EventBridge.
type LambdaInterface interface {
	Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error)
}

// ServiceError is returned for AWS-compatible API failures.
type ServiceError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *ServiceError) Error() string { return e.Message }

func (e *ServiceError) StatusCode() int {
	if e == nil || e.HTTPStatus == 0 {
		return 400
	}
	return e.HTTPStatus
}

func validationError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "ValidationException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func notFoundError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "ResourceNotFoundException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func internalError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "InternalException", Message: fmt.Sprintf(format, args...), HTTPStatus: 500}
}

// FailedEntry is used by PutTargets/RemoveTargets batch responses.
type FailedEntry struct {
	TargetID     string `json:"TargetId,omitempty"`
	ErrorCode    string `json:"ErrorCode,omitempty"`
	ErrorMessage string `json:"ErrorMessage,omitempty"`
}

// FireResult captures one manual/scheduled rule invocation result.
type FireResult struct {
	RuleName   string    `json:"ruleName"`
	TraceID    string    `json:"traceId,omitempty"`
	FiredAt    time.Time `json:"firedAt"`
	Targets    int       `json:"targets"`
	Successful int       `json:"successful"`
	Failed     int       `json:"failed"`
}

// RaceResult captures a concurrent race run.
type RaceResult struct {
	SessionID   string    `json:"sessionId"`
	RuleName    string    `json:"ruleName"`
	Runs        int       `json:"runs"`
	Concurrency int       `json:"concurrency"`
	Successful  int       `json:"successful"`
	Failed      int       `json:"failed"`
	TraceIDs    []string  `json:"traceIds,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt"`
}

// Service manages EventBridge scheduled rules and target execution.
type Service struct {
	cfg        *config.Config
	store      *Store
	lambda     LambdaInterface
	traceStore *tracesvc.Store
	collector  *tracesvc.Collector

	schedulerDone chan struct{}
	schedulerWG   sync.WaitGroup

	mu sync.Mutex
}

func NewService(cfg *config.Config, store *Store, lambda LambdaInterface) *Service {
	if store == nil {
		store = NewStore(cfg)
	}
	return &Service{
		cfg:           cfg,
		store:         store,
		lambda:        lambda,
		schedulerDone: make(chan struct{}),
	}
}

func (s *Service) SetTraceStore(ts *tracesvc.Store)   { s.traceStore = ts }
func (s *Service) SetCollector(c *tracesvc.Collector) { s.collector = c }

func (s *Service) Init() error {
	return s.store.Init()
}

func (s *Service) Start() {
	s.schedulerWG.Add(1)
	go s.schedulerLoop()
}

func (s *Service) Stop() {
	close(s.schedulerDone)
	s.schedulerWG.Wait()
}

func (s *Service) schedulerLoop() {
	defer s.schedulerWG.Done()

	ticker := time.NewTicker(schedulerTickInterval)
	defer ticker.Stop()

	lastMinute := int64(0)
	for {
		select {
		case <-s.schedulerDone:
			return
		case <-ticker.C:
			now := time.Now().UTC().Truncate(time.Minute)
			if now.Unix() == lastMinute {
				continue
			}
			lastMinute = now.Unix()
			s.executeDueRules(now)
		}
	}
}

func (s *Service) executeDueRules(now time.Time) {
	rules := s.store.ListRules()
	for _, rule := range rules {
		if strings.ToUpper(rule.State) != types.EventBridgeRuleStateEnabled || rule.NextRunAt == nil {
			continue
		}
		if rule.NextRunAt.After(now) {
			continue
		}
		_, _ = s.fireRule(rule.Name, true, nil)
	}
}

func (s *Service) PutRule(name, scheduleExpression, state, description, eventBusName string) (*types.EventBridgeRule, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, validationError("Parameter Name is required")
	}
	if len(name) > 64 {
		return nil, validationError("Parameter Name is too long")
	}

	if strings.TrimSpace(scheduleExpression) == "" {
		return nil, validationError("Parameter ScheduleExpression is required for scheduled rules")
	}
	if err := validateScheduleExpression(scheduleExpression); err != nil {
		return nil, validationError("Parameter ScheduleExpression is not valid: %v", err)
	}

	bus, err := normalizeEventBusName(eventBusName)
	if err != nil {
		return nil, err
	}

	normalizedState := strings.ToUpper(strings.TrimSpace(state))
	if normalizedState == "" {
		normalizedState = types.EventBridgeRuleStateEnabled
	}
	if normalizedState != types.EventBridgeRuleStateEnabled && normalizedState != types.EventBridgeRuleStateDisabled {
		return nil, validationError("Parameter State must be ENABLED or DISABLED")
	}

	now := time.Now().UTC().Truncate(time.Minute)

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, _ := s.store.GetRule(name)
	rule := &types.EventBridgeRule{}
	if existing != nil {
		rule = existing
	} else {
		rule.Name = name
		rule.CreatedAt = now
	}

	prevExpr := rule.ScheduleExpression
	rule.Name = name
	rule.EventBusName = bus
	rule.Description = description
	rule.ScheduleExpression = scheduleExpression
	rule.State = normalizedState
	rule.Arn = ruleARN(s.cfg, name)
	rule.LastModifiedAt = now

	if rule.ScheduleAnchor.IsZero() || !strings.EqualFold(prevExpr, scheduleExpression) {
		rule.ScheduleAnchor = now
	}

	if normalizedState == types.EventBridgeRuleStateEnabled {
		next, nextErr := computeNextRun(rule.ScheduleExpression, rule.ScheduleAnchor, now)
		if nextErr != nil {
			return nil, validationError("Parameter ScheduleExpression is not valid: %v", nextErr)
		}
		rule.NextRunAt = &next
	} else {
		rule.NextRunAt = nil
	}

	if err := s.store.SaveRule(rule); err != nil {
		return nil, internalError("failed to save rule: %v", err)
	}
	return cloneRule(rule), nil
}

func (s *Service) DescribeRule(name, eventBusName string) (*types.EventBridgeRule, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, validationError("Parameter Name is required")
	}
	if _, err := normalizeEventBusName(eventBusName); err != nil {
		return nil, err
	}

	rule, err := s.store.GetRule(name)
	if err != nil {
		return nil, notFoundError("Rule %s does not exist", name)
	}
	return rule, nil
}

func (s *Service) ListRules(namePrefix, eventBusName string, limit int, nextToken string) ([]*types.EventBridgeRule, string, error) {
	if _, err := normalizeEventBusName(eventBusName); err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = defaultRuleListLimit
	}
	if limit > maxRuleListLimit {
		limit = maxRuleListLimit
	}

	offset, err := parseNextToken(nextToken)
	if err != nil {
		return nil, "", validationError("Parameter NextToken is invalid")
	}

	rules := s.store.ListRules()
	filtered := make([]*types.EventBridgeRule, 0, len(rules))
	for _, rule := range rules {
		if namePrefix != "" && !strings.HasPrefix(rule.Name, namePrefix) {
			continue
		}
		filtered = append(filtered, rule)
	}
	if offset >= len(filtered) {
		return []*types.EventBridgeRule{}, "", nil
	}

	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}

	next := ""
	if end < len(filtered) {
		next = strconv.Itoa(end)
	}
	return filtered[offset:end], next, nil
}

func (s *Service) DeleteRule(name, eventBusName string, force bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return validationError("Parameter Name is required")
	}
	if _, err := normalizeEventBusName(eventBusName); err != nil {
		return err
	}

	rule, err := s.store.GetRule(name)
	if err != nil {
		return notFoundError("Rule %s does not exist", name)
	}
	if len(rule.Targets) > 0 && !force {
		return validationError("Rule %s cannot be deleted because it still has targets", name)
	}

	if err := s.store.DeleteRule(name); err != nil {
		return internalError("failed to delete rule: %v", err)
	}
	return nil
}

func (s *Service) EnableRule(name, eventBusName string) error {
	return s.setRuleState(name, eventBusName, types.EventBridgeRuleStateEnabled)
}

func (s *Service) DisableRule(name, eventBusName string) error {
	return s.setRuleState(name, eventBusName, types.EventBridgeRuleStateDisabled)
}

func (s *Service) setRuleState(name, eventBusName, state string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return validationError("Parameter Name is required")
	}
	if _, err := normalizeEventBusName(eventBusName); err != nil {
		return err
	}

	rule, err := s.store.GetRule(name)
	if err != nil {
		return notFoundError("Rule %s does not exist", name)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	rule.State = state
	rule.LastModifiedAt = now
	if state == types.EventBridgeRuleStateEnabled {
		next, nextErr := computeNextRun(rule.ScheduleExpression, rule.ScheduleAnchor, now)
		if nextErr != nil {
			return validationError("Parameter ScheduleExpression is not valid: %v", nextErr)
		}
		rule.NextRunAt = &next
	} else {
		rule.NextRunAt = nil
	}

	if err := s.store.SaveRule(rule); err != nil {
		return internalError("failed to update rule state: %v", err)
	}
	return nil
}

func (s *Service) PutTargets(ruleName, eventBusName string, targets []types.EventBridgeTarget) ([]FailedEntry, error) {
	if len(targets) == 0 {
		return nil, validationError("Parameter Targets must contain at least one entry")
	}
	if len(targets) > maxTargetBatchSize {
		return nil, validationError("Parameter Targets cannot exceed %d entries", maxTargetBatchSize)
	}
	if _, err := normalizeEventBusName(eventBusName); err != nil {
		return nil, err
	}

	rule, err := s.store.GetRule(ruleName)
	if err != nil {
		return nil, notFoundError("Rule %s does not exist", ruleName)
	}

	existingByID := make(map[string]types.EventBridgeTarget, len(rule.Targets))
	for _, target := range rule.Targets {
		existingByID[target.ID] = target
	}

	seenRequestIDs := map[string]struct{}{}
	failed := make([]FailedEntry, 0)
	for _, target := range targets {
		id := strings.TrimSpace(target.ID)
		if id == "" {
			failed = append(failed, FailedEntry{ErrorCode: "ValidationException", ErrorMessage: "Target Id is required"})
			continue
		}
		if _, exists := seenRequestIDs[id]; exists {
			failed = append(failed, FailedEntry{TargetID: id, ErrorCode: "ValidationException", ErrorMessage: "Duplicate target Id in request"})
			continue
		}
		seenRequestIDs[id] = struct{}{}

		canonicalArn, canonicalErr := canonicalLambdaTargetARN(s.cfg, strings.TrimSpace(target.Arn))
		if canonicalErr != nil {
			failed = append(failed, FailedEntry{TargetID: id, ErrorCode: "ValidationException", ErrorMessage: canonicalErr.Error()})
			continue
		}

		next := target
		next.ID = id
		next.Arn = canonicalArn

		prev := existingByID[id]
		next.LastInvokedAt = prev.LastInvokedAt
		next.LastResult = prev.LastResult
		existingByID[id] = next
	}

	nextTargets := make([]types.EventBridgeTarget, 0, len(existingByID))
	for _, target := range existingByID {
		nextTargets = append(nextTargets, target)
	}
	sort.Slice(nextTargets, func(i, j int) bool { return nextTargets[i].ID < nextTargets[j].ID })

	rule.Targets = nextTargets
	rule.LastModifiedAt = time.Now().UTC()
	if err := s.store.SaveRule(rule); err != nil {
		return nil, internalError("failed to save targets: %v", err)
	}

	return failed, nil
}

func (s *Service) ListTargetsByRule(ruleName, eventBusName string, limit int, nextToken string) ([]types.EventBridgeTarget, string, error) {
	if _, err := normalizeEventBusName(eventBusName); err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = defaultTargetListLimit
	}
	if limit > maxTargetListLimit {
		limit = maxTargetListLimit
	}
	offset, err := parseNextToken(nextToken)
	if err != nil {
		return nil, "", validationError("Parameter NextToken is invalid")
	}

	rule, getErr := s.store.GetRule(ruleName)
	if getErr != nil {
		return nil, "", notFoundError("Rule %s does not exist", ruleName)
	}
	if offset >= len(rule.Targets) {
		return []types.EventBridgeTarget{}, "", nil
	}
	end := offset + limit
	if end > len(rule.Targets) {
		end = len(rule.Targets)
	}
	next := ""
	if end < len(rule.Targets) {
		next = strconv.Itoa(end)
	}
	out := make([]types.EventBridgeTarget, end-offset)
	copy(out, rule.Targets[offset:end])
	return out, next, nil
}

func (s *Service) RemoveTargets(ruleName, eventBusName string, targetIDs []string) ([]FailedEntry, error) {
	if len(targetIDs) == 0 {
		return nil, validationError("Parameter Ids must contain at least one entry")
	}
	if len(targetIDs) > maxTargetBatchSize {
		return nil, validationError("Parameter Ids cannot exceed %d entries", maxTargetBatchSize)
	}
	if _, err := normalizeEventBusName(eventBusName); err != nil {
		return nil, err
	}

	rule, err := s.store.GetRule(ruleName)
	if err != nil {
		return nil, notFoundError("Rule %s does not exist", ruleName)
	}

	removeSet := make(map[string]struct{}, len(targetIDs))
	failed := make([]FailedEntry, 0)
	for _, id := range targetIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			failed = append(failed, FailedEntry{ErrorCode: "ValidationException", ErrorMessage: "Target Id is required"})
			continue
		}
		removeSet[id] = struct{}{}
	}

	nextTargets := make([]types.EventBridgeTarget, 0, len(rule.Targets))
	existingIDs := make(map[string]struct{}, len(rule.Targets))
	for _, target := range rule.Targets {
		existingIDs[target.ID] = struct{}{}
		if _, remove := removeSet[target.ID]; remove {
			continue
		}
		nextTargets = append(nextTargets, target)
	}

	for id := range removeSet {
		if _, exists := existingIDs[id]; !exists {
			failed = append(failed, FailedEntry{TargetID: id, ErrorCode: "ResourceNotFoundException", ErrorMessage: "Target ID does not exist"})
		}
	}

	rule.Targets = nextTargets
	rule.LastModifiedAt = time.Now().UTC()
	if err := s.store.SaveRule(rule); err != nil {
		return nil, internalError("failed to remove targets: %v", err)
	}
	return failed, nil
}

func (s *Service) ListRuleNamesByTarget(targetARN, eventBusName string, limit int, nextToken string) ([]string, string, error) {
	if strings.TrimSpace(targetARN) == "" {
		return nil, "", validationError("Parameter TargetArn is required")
	}
	if _, err := normalizeEventBusName(eventBusName); err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = defaultRuleListLimit
	}
	if limit > maxRuleListLimit {
		limit = maxRuleListLimit
	}
	offset, err := parseNextToken(nextToken)
	if err != nil {
		return nil, "", validationError("Parameter NextToken is invalid")
	}

	canonical, canonicalErr := canonicalLambdaTargetARN(s.cfg, targetARN)
	if canonicalErr != nil {
		return nil, "", validationError("Parameter TargetArn is invalid: %v", canonicalErr)
	}

	rules := s.store.ListRules()
	names := make([]string, 0)
	for _, rule := range rules {
		for _, target := range rule.Targets {
			if target.Arn == canonical {
				names = append(names, rule.Name)
				break
			}
		}
	}
	sort.Strings(names)

	if offset >= len(names) {
		return []string{}, "", nil
	}
	end := offset + limit
	if end > len(names) {
		end = len(names)
	}
	next := ""
	if end < len(names) {
		next = strconv.Itoa(end)
	}
	return names[offset:end], next, nil
}

func (s *Service) ListTagsForResource(resourceARN string) (map[string]string, error) {
	rule, err := s.ruleByResourceARN(resourceARN)
	if err != nil {
		return nil, err
	}
	return cloneTags(rule.Tags), nil
}

func (s *Service) TagResource(resourceARN string, tags map[string]string) error {
	rule, err := s.ruleByResourceARN(resourceARN)
	if err != nil {
		return err
	}

	if rule.Tags == nil {
		rule.Tags = make(map[string]string)
	}
	for key, value := range tags {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		rule.Tags[k] = value
	}
	rule.LastModifiedAt = time.Now().UTC()
	if err := s.store.SaveRule(rule); err != nil {
		return internalError("failed to tag resource: %v", err)
	}
	return nil
}

func (s *Service) UntagResource(resourceARN string, tagKeys []string) error {
	rule, err := s.ruleByResourceARN(resourceARN)
	if err != nil {
		return err
	}
	if len(rule.Tags) == 0 {
		return nil
	}

	for _, key := range tagKeys {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		delete(rule.Tags, k)
	}
	rule.LastModifiedAt = time.Now().UTC()
	if err := s.store.SaveRule(rule); err != nil {
		return internalError("failed to untag resource: %v", err)
	}
	return nil
}

func (s *Service) FireRuleNow(ruleName string, sessionMeta map[string]string) (*FireResult, error) {
	return s.fireRule(ruleName, false, sessionMeta)
}

func (s *Service) fireRule(ruleName string, scheduled bool, sessionMeta map[string]string) (*FireResult, error) {
	rule, err := s.store.GetRule(ruleName)
	if err != nil {
		return nil, notFoundError("Rule %s does not exist", ruleName)
	}

	started := time.Now().UTC()
	correlationID := tracesvc.CorrelationIDFromMap(sessionMeta)
	if correlationID == "" {
		correlationID = tracesvc.NewCorrelationID()
	}
	eventPayload := buildScheduledEventPayload(rule, started, sessionMeta, correlationID)

	result := &FireResult{
		RuleName: rule.Name,
		FiredAt:  started,
		Targets:  len(rule.Targets),
	}

	spans := make([]tracesvc.Span, 0, len(rule.Targets)+1)
	eventbridgeMeta := map[string]string{
		"rule":      rule.Name,
		"ruleArn":   rule.Arn,
		"scheduled": strconv.FormatBool(scheduled),
	}
	for k, v := range sessionMeta {
		eventbridgeMeta[k] = v
	}
	eventbridgeMeta["correlationId"] = correlationID
	spans = append(spans, tracesvc.Span{Kind: "eventbridge", Name: rule.Name, Status: "ok", Meta: eventbridgeMeta})

	for i := range rule.Targets {
		target := &rule.Targets[i]
		functionName, fnErr := lambdaNameFromTarget(target.Arn)
		if fnErr != nil {
			target.LastResult = "ERROR: invalid target ARN"
			result.Failed++
			spans = append(spans, tracesvc.Span{Kind: "lambda", Name: target.Arn, Status: "error"})
			continue
		}

		payload, payloadErr := buildTargetPayload(eventPayload, target)
		if payloadErr != nil {
			target.LastResult = "ERROR: " + payloadErr.Error()
			result.Failed++
			spans = append(spans, tracesvc.Span{Kind: "lambda", Name: functionName, Status: "error", Meta: map[string]string{"targetId": target.ID}})
			continue
		}

		if s.lambda == nil {
			target.LastResult = "ERROR: lambda service unavailable"
			result.Failed++
			spans = append(spans, tracesvc.Span{Kind: "lambda", Name: functionName, Status: "error", Meta: map[string]string{"targetId": target.ID}})
			continue
		}

		invokeStart := time.Now()
		if s.collector != nil {
			s.collector.Begin(functionName)
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultRuleFireTimeout)
		invokeOut, invokeErr := s.lambda.Invoke(ctx, &types.InvokeInput{
			FunctionName:   functionName,
			Payload:        payload,
			InvocationType: manualInvocationTypeEvent,
		})
		cancel()
		duration := time.Since(invokeStart).Milliseconds()

		target.LastInvokedAt = &started
		spanStatus := "ok"
		if invokeErr != nil || (invokeOut != nil && invokeOut.FunctionError != "") {
			detail := "invoke failed"
			if invokeErr != nil {
				detail = invokeErr.Error()
			} else if invokeOut != nil && invokeOut.FunctionError != "" {
				detail = invokeOut.FunctionError
			}
			target.LastResult = "ERROR: " + detail
			result.Failed++
			spanStatus = "error"
			spans = append(spans, tracesvc.Span{
				Kind:       "lambda",
				Name:       functionName,
				DurationMs: duration,
				Status:     spanStatus,
				Meta: map[string]string{
					"targetId": target.ID,
					"error":    detail,
				},
			})
			if s.collector != nil {
				spans = append(spans, tracesvc.SubSpansToSpans(s.collector.CollectWithFlush(functionName))...)
			}
			continue
		} else {
			target.LastResult = "OK"
			result.Successful++
		}
		spans = append(spans, tracesvc.Span{Kind: "lambda", Name: functionName, DurationMs: duration, Status: spanStatus, Meta: map[string]string{"targetId": target.ID}})
		if s.collector != nil {
			spans = append(spans, tracesvc.SubSpansToSpans(s.collector.CollectWithFlush(functionName))...)
		}
	}

	rule.LastRunAt = &started
	if result.Failed > 0 {
		rule.LastResult = fmt.Sprintf("ERROR: %d/%d targets failed", result.Failed, result.Targets)
		spans[0].Status = "error"
	} else {
		rule.LastResult = "OK"
	}

	if scheduled && strings.ToUpper(rule.State) == types.EventBridgeRuleStateEnabled {
		next, nextErr := computeNextRun(rule.ScheduleExpression, rule.ScheduleAnchor, started)
		if nextErr == nil {
			rule.NextRunAt = &next
		}
	}

	if err := s.store.SaveRule(rule); err != nil {
		return nil, internalError("failed to update rule run metadata: %v", err)
	}

	if s.traceStore != nil {
		traceID := uuid.NewString()[:8]
		result.TraceID = traceID
		status := 200
		if result.Failed > 0 {
			status = 500
		}
		s.traceStore.Add(&tracesvc.Trace{
			ID:            traceID,
			CorrelationID: correlationID,
			StartedAt:     started,
			DurationMs:    time.Since(started).Milliseconds(),
			Status:        status,
			Method:        "EVENTBRIDGE",
			Path:          "/rules/" + rule.Name,
			Spans:         spans,
		})
	}

	return result, nil
}

func (s *Service) RunRuleRace(ruleName string, runs, concurrency int) (*RaceResult, error) {
	if runs <= 0 || runs > 500 {
		return nil, validationError("Parameter runs must be between 1 and 500")
	}
	if concurrency <= 0 || concurrency > 100 {
		return nil, validationError("Parameter concurrency must be between 1 and 100")
	}
	if _, err := s.store.GetRule(ruleName); err != nil {
		return nil, notFoundError("Rule %s does not exist", ruleName)
	}

	sessionID := uuid.NewString()[:8]
	started := time.Now().UTC()
	result := &RaceResult{
		SessionID:   sessionID,
		RuleName:    ruleName,
		Runs:        runs,
		Concurrency: concurrency,
		StartedAt:   started,
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	var successful int64
	var failed int64
	traceMu := sync.Mutex{}
	traceIDs := make([]string, 0, runs)

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				fire, err := s.FireRuleNow(ruleName, map[string]string{
					"raceSession": sessionID,
					"raceRun":     strconv.Itoa(n + 1),
					"raceRuns":    strconv.Itoa(runs),
				})
				if err != nil {
					atomic.AddInt64(&failed, 1)
					continue
				}
				if fire.Failed > 0 {
					atomic.AddInt64(&failed, 1)
				} else {
					atomic.AddInt64(&successful, 1)
				}
				if fire.TraceID != "" {
					traceMu.Lock()
					traceIDs = append(traceIDs, fire.TraceID)
					traceMu.Unlock()
				}
			}
		}()
	}

	for i := 0; i < runs; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	result.Successful = int(successful)
	result.Failed = int(failed)
	result.TraceIDs = traceIDs
	result.FinishedAt = time.Now().UTC()
	return result, nil
}

func normalizeEventBusName(eventBusName string) (string, error) {
	n := strings.TrimSpace(eventBusName)
	if n == "" || n == defaultEventBusName {
		return defaultEventBusName, nil
	}
	if strings.HasSuffix(n, types.EventBridgeDefaultBusARNSuffix) {
		return defaultEventBusName, nil
	}
	return "", validationError("Only the default event bus is supported for scheduled rules")
}

func (s *Service) ruleByResourceARN(resourceARN string) (*types.EventBridgeRule, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, validationError("Parameter ResourceARN is required")
	}

	name := resourceARN
	if strings.HasPrefix(resourceARN, "arn:aws:events:") {
		const marker = ":rule/"
		idx := strings.Index(resourceARN, marker)
		if idx < 0 {
			return nil, validationError("Parameter ResourceARN is invalid")
		}
		tail := strings.Trim(resourceARN[idx+len(marker):], "/")
		if tail == "" {
			return nil, validationError("Parameter ResourceARN is invalid")
		}
		parts := strings.Split(tail, "/")
		name = strings.TrimSpace(parts[len(parts)-1])
		if name == "" {
			return nil, validationError("Parameter ResourceARN is invalid")
		}
	}

	rule, err := s.store.GetRule(name)
	if err != nil {
		return nil, notFoundError("Resource %s does not exist", resourceARN)
	}
	return rule, nil
}

func ruleARN(cfg *config.Config, ruleName string) string {
	return fmt.Sprintf("arn:aws:events:%s:%s:rule/%s", cfg.Region, cfg.AccountID, ruleName)
}

func parseNextToken(token string) (int, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(token)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid token")
	}
	return value, nil
}

func canonicalLambdaTargetARN(cfg *config.Config, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("target Arn is required")
	}
	if strings.HasPrefix(target, "arn:aws:lambda:") && strings.Contains(target, ":function:") {
		return target, nil
	}
	if strings.HasPrefix(target, "arn:aws:") {
		return "", fmt.Errorf("target Arn must be a Lambda function")
	}
	return fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", cfg.Region, cfg.AccountID, target), nil
}

func lambdaNameFromTarget(arn string) (string, error) {
	arn = strings.TrimSpace(arn)
	if arn == "" {
		return "", fmt.Errorf("empty target Arn")
	}
	if !strings.Contains(arn, ":") {
		return arn, nil
	}
	const marker = ":function:"
	idx := strings.Index(arn, marker)
	if idx < 0 {
		return "", fmt.Errorf("target Arn is not a Lambda function")
	}
	tail := arn[idx+len(marker):]
	if colon := strings.IndexByte(tail, ':'); colon >= 0 {
		tail = tail[:colon]
	}
	if tail == "" {
		return "", fmt.Errorf("target Arn is invalid")
	}
	return tail, nil
}

func buildScheduledEventPayload(rule *types.EventBridgeRule, at time.Time, sessionMeta map[string]string, correlationID string) []byte {
	detail := map[string]any{}
	if len(sessionMeta) > 0 {
		tarnMeta := make(map[string]string, len(sessionMeta)+1)
		for key, value := range sessionMeta {
			tarnMeta[key] = value
		}
		if correlationID != "" {
			tarnMeta["correlationId"] = correlationID
		}
		detail["tarn"] = tarnMeta
	} else if correlationID != "" {
		detail["tarn"] = map[string]string{"correlationId": correlationID}
	}
	ruleARN := ""
	account := "000000000000"
	region := "us-east-1"
	if rule != nil {
		ruleARN = rule.Arn
		account = parseAccountFromRuleARN(rule.Arn)
		region = parseRegionFromRuleARN(rule.Arn)
	}
	payload := map[string]any{
		"version":     "0",
		"id":          uuid.NewString(),
		"detail-type": "Scheduled Event",
		"source":      "aws.events",
		"account":     account,
		"time":        at.Format(time.RFC3339),
		"region":      region,
		"resources":   []string{ruleARN},
		"detail":      detail,
		"correlationId": correlationID,
	}
	body, _ := json.Marshal(payload)
	return body
}

func parseRegionFromRuleARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 4 && parts[3] != "" {
		return parts[3]
	}
	return "us-east-1"
}

func parseAccountFromRuleARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 5 && parts[4] != "" {
		return parts[4]
	}
	return "000000000000"
}

func buildTargetPayload(eventPayload []byte, target *types.EventBridgeTarget) ([]byte, error) {
	if target == nil {
		return nil, fmt.Errorf("target is nil")
	}
	if strings.TrimSpace(target.Input) != "" {
		return []byte(target.Input), nil
	}

	if strings.TrimSpace(target.InputPath) != "" {
		return extractInputPath(eventPayload, target.InputPath)
	}

	if target.InputTransformer != nil && strings.TrimSpace(target.InputTransformer.InputTemplate) != "" {
		return applyInputTransformer(eventPayload, target.InputTransformer)
	}

	return eventPayload, nil
}

func extractInputPath(payload []byte, path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "$" {
		return payload, nil
	}
	if !strings.HasPrefix(path, "$.") {
		return nil, fmt.Errorf("unsupported InputPath %q", path)
	}

	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, fmt.Errorf("invalid source payload")
	}

	current := value
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			current = nil
			break
		}
		current = obj[part]
	}
	if current == nil {
		return []byte("null"), nil
	}
	out, err := json.Marshal(current)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal InputPath output")
	}
	return out, nil
}

func applyInputTransformer(payload []byte, transformer *types.InputTransformer) ([]byte, error) {
	if transformer == nil {
		return nil, fmt.Errorf("InputTransformer is required")
	}
	template := transformer.InputTemplate
	if strings.TrimSpace(template) == "" {
		return nil, fmt.Errorf("InputTemplate is required")
	}

	for key, path := range transformer.InputPathsMap {
		repl, err := extractInputPath(payload, path)
		if err != nil {
			return nil, err
		}
		template = strings.ReplaceAll(template, "<"+key+">", string(repl))
	}
	return []byte(template), nil
}

func cloneTags(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
