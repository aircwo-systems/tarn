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

	"github.com/aircwo-systems/tarn/internal/config"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

const (
	defaultEventBusName       = "default"
	maxTargetBatchSize        = 10
	defaultRuleListLimit      = 100
	maxRuleListLimit          = 100
	defaultTargetListLimit    = 100
	maxTargetListLimit        = 100
	defaultRuleFireTimeout    = 30 * time.Second
	defaultECSRuleConcurrency = 10
	schedulerTickInterval     = 1 * time.Second
	manualInvocationTypeEvent = "Event"
	ecsEventPayloadEnvName    = "EVENT_PAYLOAD"
	maxEventPayloadEnvBytes   = types.TaskEventPayloadEnvMaxBytes
)

// LambdaInterface defines the Lambda behavior required by EventBridge.
type LambdaInterface interface {
	Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error)
}

// TaskRunner is the ECS behavior required by EventBridge. It is an alias to
// the shared ECS seam so existing callers can pass the concrete runner or a
// Docker-free fake without changing NewService's constructor signature.
type TaskRunner = types.TaskRunner

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
	cfg          *config.Config
	store        *Store
	lambda       LambdaInterface
	taskRunner   TaskRunner
	traceStore   *tracesvc.Store
	collector    *tracesvc.Collector
	taskRunnerMu sync.RWMutex
	ecsLimitMu   sync.Mutex
	ecsInFlight  map[string]int
	ecsLimit     int

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
		ecsInFlight:   make(map[string]int),
		ecsLimit:      defaultECSRuleConcurrency,
	}
}

func (s *Service) SetTraceStore(ts *tracesvc.Store)   { s.traceStore = ts }
func (s *Service) SetCollector(c *tracesvc.Collector) { s.collector = c }

// SetTaskRunner wires ECS RunTask/StopTask behavior into EventBridge. It is
// intentionally a setter so the existing NewService(cfg, store, lambda)
// callers remain source-compatible while account wiring can attach ECS later.
func (s *Service) SetTaskRunner(runner TaskRunner) {
	s.taskRunnerMu.Lock()
	s.taskRunner = runner
	s.taskRunnerMu.Unlock()
}

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

func (s *Service) PutRule(name, scheduleExpression, eventPattern, state, description, eventBusName string) (*types.EventBridgeRule, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, validationError("Parameter Name is required")
	}
	if len(name) > 64 {
		return nil, validationError("Parameter Name is too long")
	}

	hasSchedule := strings.TrimSpace(scheduleExpression) != ""
	hasPattern := strings.TrimSpace(eventPattern) != ""

	if !hasSchedule && !hasPattern {
		return nil, validationError("Either ScheduleExpression or EventPattern is required")
	}
	if hasSchedule && hasPattern {
		return nil, validationError("ScheduleExpression and EventPattern are mutually exclusive")
	}

	if hasSchedule {
		if err := validateScheduleExpression(scheduleExpression); err != nil {
			return nil, validationError("Parameter ScheduleExpression is not valid: %v", err)
		}
	}
	if hasPattern {
		if err := ValidateEventPattern(eventPattern); err != nil {
			return nil, validationError("Parameter EventPattern is not valid: %v", err)
		}
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
	rule.State = normalizedState
	rule.Arn = ruleARN(s.cfg, name)
	rule.LastModifiedAt = now

	if hasSchedule {
		rule.ScheduleExpression = scheduleExpression
		rule.EventPattern = ""

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
	} else {
		rule.EventPattern = eventPattern
		rule.ScheduleExpression = ""
		rule.NextRunAt = nil
		rule.ScheduleAnchor = time.Time{}
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

		canonicalArn, canonicalErr := canonicalEventBridgeTargetARN(s.cfg, &target)
		if canonicalErr != nil {
			failed = append(failed, FailedEntry{TargetID: id, ErrorCode: "ValidationException", ErrorMessage: canonicalErr.Error()})
			continue
		}

		next := target
		next.ID = id
		next.Arn = canonicalArn
		next.EcsParameters = cloneECSParameters(target.EcsParameters)

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

	canonical, canonicalErr := canonicalTargetARNReference(s.cfg, targetARN)
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

// PutEvents accepts a batch of events, matches them against event-pattern rules,
// and dispatches to matching targets. Returns one result entry per input event.
func (s *Service) PutEvents(entries []types.PutEventsEntry) ([]types.PutEventsResultEntry, int, error) {
	if len(entries) == 0 {
		return nil, 0, validationError("Parameter Entries must contain at least one entry")
	}
	if len(entries) > 10 {
		return nil, 0, validationError("Parameter Entries cannot exceed 10 entries")
	}

	rules := s.store.ListRules()
	results := make([]types.PutEventsResultEntry, len(entries))
	failedCount := 0

	for i, entry := range entries {
		eventID := uuid.NewString()

		if strings.TrimSpace(entry.Source) == "" || strings.TrimSpace(entry.DetailType) == "" {
			results[i] = types.PutEventsResultEntry{
				ErrorCode:    "ValidationException",
				ErrorMessage: "Source and DetailType are required",
			}
			failedCount++
			continue
		}

		// Build the full event envelope
		now := time.Now().UTC()
		eventTime := now.Format(time.RFC3339)
		if strings.TrimSpace(entry.Time) != "" {
			eventTime = entry.Time
		}

		var detailObj any
		if strings.TrimSpace(entry.Detail) != "" {
			if err := json.Unmarshal([]byte(entry.Detail), &detailObj); err != nil {
				results[i] = types.PutEventsResultEntry{
					ErrorCode:    "ValidationException",
					ErrorMessage: "Detail must be valid JSON",
				}
				failedCount++
				continue
			}
		} else {
			detailObj = map[string]any{}
		}

		resources := entry.Resources
		if resources == nil {
			resources = []string{}
		}

		event := map[string]any{
			"version":     "0",
			"id":          eventID,
			"detail-type": entry.DetailType,
			"source":      entry.Source,
			"account":     s.cfg.AccountID,
			"time":        eventTime,
			"region":      s.cfg.Region,
			"resources":   resources,
			"detail":      detailObj,
		}

		eventJSON, _ := json.Marshal(event)

		// Match against all event-pattern rules and dispatch
		s.dispatchEvent(rules, eventJSON, event)

		results[i] = types.PutEventsResultEntry{EventId: eventID}
	}

	return results, failedCount, nil
}

// dispatchEvent matches one event against all enabled event-pattern rules
// and invokes targets for each matching rule.
func (s *Service) dispatchEvent(rules []*types.EventBridgeRule, eventJSON []byte, event map[string]any) {
	for _, rule := range rules {
		if strings.ToUpper(rule.State) != types.EventBridgeRuleStateEnabled {
			continue
		}
		if strings.TrimSpace(rule.EventPattern) == "" {
			continue
		}

		matched, err := MatchEventPattern([]byte(rule.EventPattern), eventJSON)
		if err != nil || !matched {
			continue
		}

		// Fire targets for this matched rule
		s.fireTargets(rule, eventJSON)
	}
}

// targetDispatchOptions captures the three known behavioral differences between the
// event-pattern dispatch path (fireTargets) and the scheduled-rule dispatch path
// (fireRule). They are preserved here rather than silently unified, since they were
// present before this refactor and are not obviously a bug.
type targetDispatchOptions struct {
	// ruleKey identifies the owning rule for ECS in-flight accounting. It is
	// populated by both dispatch call sites; the target ID is only a fallback
	// for direct unit calls to dispatchTarget.
	ruleKey string
	// invokedAt, when non-nil, is stamped onto target.LastInvokedAt verbatim instead of
	// the actual post-invoke wall-clock time. fireRule stamps every target with the
	// rule's fire-start time; fireTargets stamps the real invoke-completion time.
	invokedAt *time.Time
	// earlyFailureSpans controls whether a span is recorded for a target that fails
	// before reaching s.lambda.Invoke (invalid ARN, bad payload, lambda unavailable).
	// fireRule records these; fireTargets does not.
	earlyFailureSpans bool
	// errorDetailMeta adds an "error" Meta key (in addition to "targetId") to a failed
	// invoke's span. fireRule sets this; fireTargets does not.
	errorDetailMeta bool
	// correlationID is the rule-fire's trace correlation ID. dispatchECSTarget
	// forwards it onto the RunTaskInput so the ECS runner's own trace (RUNNING
	// and STOPPED) shares it with this EventBridge delivery's trace, instead
	// of minting an unrelated one.
	correlationID string
}

// targetKind classifies a target ARN by delivery mechanism. ECS target ARNs
// point at a cluster; all other non-ECS values retain the Lambda path for
// backwards compatibility with bare function names.
func targetKind(arn string) string {
	if strings.HasPrefix(strings.TrimSpace(arn), "arn:aws:ecs:") {
		return "ecs"
	}
	return "lambda"
}

// dispatchTarget delivers one event payload to one rule target and returns the spans
// describing the delivery, plus whether delivery succeeded. It mutates
// target.LastResult and target.LastInvokedAt in place; callers are still responsible
// for persisting the owning rule via s.store.SaveRule. Kind-switching on the target ARN
// lives here so a new target kind needs no changes at the call sites.
func (s *Service) dispatchTarget(target *types.EventBridgeTarget, eventPayload []byte, opts targetDispatchOptions) (bool, []tracesvc.Span) {
	if target == nil {
		return false, nil
	}
	if target.EcsParameters != nil {
		return s.dispatchECSTarget(target, eventPayload, opts)
	}

	switch targetKind(target.Arn) {
	case "lambda":
		return s.dispatchLambdaTarget(target, eventPayload, opts)
	case "ecs":
		return s.dispatchECSTarget(target, eventPayload, opts)
	default:
		target.LastResult = "ERROR: unsupported target kind"
		return false, nil
	}
}

func (s *Service) dispatchLambdaTarget(target *types.EventBridgeTarget, eventPayload []byte, opts targetDispatchOptions) (bool, []tracesvc.Span) {
	var spans []tracesvc.Span

	functionName, fnErr := lambdaNameFromTarget(target.Arn)
	if fnErr != nil {
		target.LastResult = "ERROR: invalid target ARN"
		if opts.earlyFailureSpans {
			spans = append(spans, tracesvc.Span{Kind: "lambda", Name: target.Arn, Status: "error"})
		}
		return false, spans
	}

	payload, payloadErr := buildTargetPayload(eventPayload, target)
	if payloadErr != nil {
		target.LastResult = "ERROR: " + payloadErr.Error()
		if opts.earlyFailureSpans {
			spans = append(spans, tracesvc.Span{Kind: "lambda", Name: functionName, Status: "error", Meta: map[string]string{"targetId": target.ID}})
		}
		return false, spans
	}

	if s.lambda == nil {
		target.LastResult = "ERROR: lambda service unavailable"
		if opts.earlyFailureSpans {
			spans = append(spans, tracesvc.Span{Kind: "lambda", Name: functionName, Status: "error", Meta: map[string]string{"targetId": target.ID}})
		}
		return false, spans
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

	if opts.invokedAt != nil {
		target.LastInvokedAt = opts.invokedAt
	} else {
		now := time.Now().UTC()
		target.LastInvokedAt = &now
	}

	success := true
	spanStatus := "ok"
	meta := map[string]string{"targetId": target.ID}
	if invokeErr != nil || (invokeOut != nil && invokeOut.FunctionError != "") {
		detail := "invoke failed"
		if invokeErr != nil {
			detail = invokeErr.Error()
		} else if invokeOut != nil && invokeOut.FunctionError != "" {
			detail = invokeOut.FunctionError
		}
		target.LastResult = "ERROR: " + detail
		spanStatus = "error"
		success = false
		if opts.errorDetailMeta {
			meta["error"] = detail
		}
	} else {
		target.LastResult = "OK"
	}

	spans = append(spans, tracesvc.Span{Kind: "lambda", Name: functionName, DurationMs: duration, Status: spanStatus, Meta: meta})
	if s.collector != nil {
		spans = append(spans, tracesvc.SubSpansToSpans(s.collector.CollectWithFlush(functionName))...)
	}

	return success, spans
}

// dispatchECSTarget transforms the event and starts one-shot ECS tasks through
// the injected TaskRunner. RunTask is intentionally bounded to the dispatch
// call: the runner owns the task's longer container lifecycle, while this
// method records the EventBridge delivery span and target result.
func (s *Service) dispatchECSTarget(target *types.EventBridgeTarget, eventPayload []byte, opts targetDispatchOptions) (bool, []tracesvc.Span) {
	params := target.EcsParameters
	family := ""
	if params != nil {
		family = taskDefinitionFamily(params.TaskDefinitionArn)
	}

	spanMeta := map[string]string{
		"targetId":             target.ID,
		"taskDefinitionFamily": family,
	}
	if target.Arn != "" {
		spanMeta["clusterArn"] = target.Arn
	}

	fail := func(detail string) (bool, []tracesvc.Span) {
		if detail == "" {
			detail = "ECS target dispatch failed"
		}
		target.LastResult = "ERROR: " + detail
		spanMeta["error"] = detail
		return false, []tracesvc.Span{{
			Kind:   "ecs",
			Name:   family,
			Status: "error",
			Meta:   spanMeta,
		}}
	}

	if params == nil {
		return fail("EcsParameters are required for an ECS target")
	}
	if err := validateECSParameters(params); err != nil {
		return fail(err.Error())
	}

	payload, payloadErr := buildTargetPayload(eventPayload, target)
	if payloadErr != nil {
		return fail(payloadErr.Error())
	}
	runInput, inputErr := buildECSTaskInput(target, payload, eventPayload)
	if inputErr != nil {
		return fail(inputErr.Error())
	}
	runInput.CorrelationID = opts.correlationID

	ruleKey := opts.ruleKey
	if ruleKey == "" {
		ruleKey = targetRuleKey(target)
	}
	if !s.acquireECSRuleSlot(ruleKey) {
		return fail(fmt.Sprintf("ThrottlingException: ECS target concurrency limit exceeded (limit %d)", s.ecsConcurrencyLimit()))
	}
	defer s.releaseECSRuleSlot(ruleKey)

	runner := s.getTaskRunner()
	if runner == nil {
		return fail("ECS task runner is not configured")
	}

	invokeStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), defaultRuleFireTimeout)
	runOut, runErr := runner.RunTask(ctx, runInput)
	cancel()
	duration := time.Since(invokeStart).Milliseconds()
	if runOut != nil {
		duration = ecsTaskDuration(runOut.Tasks, duration)
	}

	if opts.invokedAt != nil {
		target.LastInvokedAt = opts.invokedAt
	} else {
		now := time.Now().UTC()
		target.LastInvokedAt = &now
	}

	spanMeta["taskCount"] = strconv.Itoa(runInput.Count)
	if runOut != nil {
		spanMeta["startedTasks"] = strconv.Itoa(len(runOut.Tasks))
		if len(runOut.Tasks) > 0 {
			spanMeta["taskArn"] = runOut.Tasks[0].TaskArn
		}
	}

	success := runErr == nil
	spanStatus := "ok"
	detail := ""
	if runErr != nil {
		detail = runErr.Error()
	} else if runOut == nil {
		detail = "RunTask returned no output"
	} else if len(runOut.Failures) > 0 {
		detail = formatECSRunTaskFailures(runOut.Failures)
	} else if len(runOut.Tasks) == 0 {
		detail = "RunTask returned no tasks"
	} else if stoppedTaskFailure := stoppedTaskFailure(runOut.Tasks); stoppedTaskFailure != "" {
		detail = stoppedTaskFailure
	}
	if detail != "" {
		success = false
		spanStatus = "error"
		target.LastResult = "ERROR: " + detail
		spanMeta["error"] = detail
	} else {
		target.LastResult = "OK"
	}

	return success, []tracesvc.Span{{
		Kind:       "ecs",
		Name:       family,
		DurationMs: duration,
		Status:     spanStatus,
		Meta:       spanMeta,
	}}
}

func (s *Service) getTaskRunner() TaskRunner {
	s.taskRunnerMu.RLock()
	defer s.taskRunnerMu.RUnlock()
	return s.taskRunner
}

func (s *Service) ecsConcurrencyLimit() int {
	s.ecsLimitMu.Lock()
	defer s.ecsLimitMu.Unlock()
	if s.ecsLimit <= 0 {
		return defaultECSRuleConcurrency
	}
	return s.ecsLimit
}

func (s *Service) acquireECSRuleSlot(ruleKey string) bool {
	if ruleKey == "" {
		ruleKey = "<unknown-rule>"
	}
	s.ecsLimitMu.Lock()
	defer s.ecsLimitMu.Unlock()
	if s.ecsInFlight == nil {
		s.ecsInFlight = make(map[string]int)
	}
	limit := s.ecsLimit
	if limit <= 0 {
		limit = defaultECSRuleConcurrency
	}
	if s.ecsInFlight[ruleKey] >= limit {
		return false
	}
	s.ecsInFlight[ruleKey]++
	return true
}

func (s *Service) releaseECSRuleSlot(ruleKey string) {
	if ruleKey == "" {
		ruleKey = "<unknown-rule>"
	}
	s.ecsLimitMu.Lock()
	defer s.ecsLimitMu.Unlock()
	if s.ecsInFlight[ruleKey] <= 1 {
		delete(s.ecsInFlight, ruleKey)
		return
	}
	s.ecsInFlight[ruleKey]--
}

func targetRuleKey(target *types.EventBridgeTarget) string {
	if target == nil {
		return ""
	}
	if strings.TrimSpace(target.ID) != "" {
		return target.ID
	}
	return strings.TrimSpace(target.Arn)
}

// buildECSTaskInput assembles the RunTaskInput for an ECS target. payload is
// the resolved target input (static Input, InputPath, or InputTransformer
// result); eventPayload is the original matched EventBridge event, before
// any of those transforms. They usually carry the same document, except
// when payload is itself an ECS containerOverrides document -- in that case
// eventPayload is what the container should see as EVENT_PAYLOAD, since
// payload describes container overrides, not the event.
func buildECSTaskInput(target *types.EventBridgeTarget, payload, eventPayload []byte) (*types.RunTaskInput, error) {
	if target == nil || target.EcsParameters == nil {
		return nil, fmt.Errorf("EcsParameters are required for an ECS target")
	}
	params := target.EcsParameters
	overrideValues, payloadIsOverrideDocument, err := ecsContainerOverridesFromTarget(target, payload)
	if err != nil {
		return nil, err
	}

	deliveredPayload := payload
	if payloadIsOverrideDocument {
		deliveredPayload = eventPayload
	}

	var overrides []types.ContainerOverride
	if len(overrideValues) > 0 {
		overrides = make([]types.ContainerOverride, len(overrideValues))
	}
	for i, requested := range overrideValues {
		name := strings.TrimSpace(requested.Name)
		if name == "" {
			return nil, fmt.Errorf("ECS container override name is required")
		}
		overrides[i] = requested
		overrides[i].Name = name
		// Keep the small-payload environment behavior for compatibility with
		// existing local targets. The runner receives EventPayload separately and
		// uses a mounted file once the Linux per-value limit would be exceeded.
		if len(deliveredPayload) <= maxEventPayloadEnvBytes {
			overrides[i].Environment = upsertContainerEnvironment(requested.Environment, types.KeyValuePair{
				Name:  ecsEventPayloadEnvName,
				Value: string(deliveredPayload),
			})
		}
	}

	count := params.TaskCount
	if count == 0 {
		count = 1
	}
	var taskOverrides *types.TaskOverride
	if len(overrides) > 0 {
		taskOverrides = &types.TaskOverride{ContainerOverrides: overrides}
	}
	return &types.RunTaskInput{
		Cluster:        target.Arn,
		TaskDefinition: params.TaskDefinitionArn,
		Count:          count,
		LaunchType:     params.LaunchType,
		Overrides:      taskOverrides,
		EventPayload:   append([]byte(nil), deliveredPayload...),
	}, nil
}

// ecsContainerOverridesFromTarget follows the AWS target shape. EcsParameters
// contains task count/launch metadata; container overrides are part of the
// target input document, normally under containerOverrides in Input or the
// result of InputTransformer. ContainerOverrides remains a compatibility
// fallback for older local state written before this matched AWS.
//
// The second return value reports whether payload itself was consumed as an
// ECS override document (i.e. a JSON object with a containerOverrides key),
// as opposed to the legacy EcsParameters.ContainerOverrides fallback or
// payload having no overrides at all. Callers use this to decide whether
// payload is safe to also deliver as EVENT_PAYLOAD, or whether that would
// hand the container its own override instructions instead of the event.
func ecsContainerOverridesFromTarget(target *types.EventBridgeTarget, payload []byte) ([]types.ContainerOverride, bool, error) {
	if target == nil || target.EcsParameters == nil {
		return nil, false, fmt.Errorf("EcsParameters are required for an ECS target")
	}
	if len(target.EcsParameters.ContainerOverrides) > 0 {
		return append([]types.ContainerOverride(nil), target.EcsParameters.ContainerOverrides...), false, nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		// Target input is also the task event payload. Arbitrary non-object
		// payloads do not contain overrides and should still be delivered.
		return nil, false, nil
	}
	raw, ok := object["containerOverrides"]
	if !ok {
		for key, candidate := range object {
			if strings.EqualFold(key, "containerOverrides") {
				raw, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return nil, false, nil
	}
	var overrides []types.ContainerOverride
	if err := json.Unmarshal(raw, &overrides); err != nil {
		return nil, true, fmt.Errorf("invalid ECS containerOverrides in target input: %w", err)
	}
	return overrides, true, nil
}

func upsertContainerEnvironment(base []types.KeyValuePair, entry types.KeyValuePair) []types.KeyValuePair {
	env := make([]types.KeyValuePair, 0, len(base)+1)
	replaced := false
	for _, current := range base {
		if current.Name == entry.Name {
			if !replaced {
				env = append(env, entry)
				replaced = true
			}
			continue
		}
		env = append(env, current)
	}
	if !replaced {
		env = append(env, entry)
	}
	return env
}

func validateECSParameters(params *types.EcsParameters) error {
	if params == nil {
		return fmt.Errorf("EcsParameters are required for an ECS target")
	}
	if _, err := canonicalECSTaskDefinitionARN(params.TaskDefinitionArn); err != nil {
		return err
	}
	if params.TaskCount < 0 || params.TaskCount > maxTargetBatchSize {
		return fmt.Errorf("TaskCount must be between 1 and %d when specified", maxTargetBatchSize)
	}
	if launchType := strings.ToUpper(strings.TrimSpace(params.LaunchType)); launchType != "" &&
		launchType != types.LaunchTypeFargate && launchType != types.LaunchTypeEC2 {
		return fmt.Errorf("unsupported ECS LaunchType %q", params.LaunchType)
	}
	for _, override := range params.ContainerOverrides {
		if strings.TrimSpace(override.Name) == "" {
			return fmt.Errorf("ECS container override name is required")
		}
	}
	return nil
}

func formatECSRunTaskFailures(failures []types.Failure) string {
	if len(failures) == 0 {
		return ""
	}
	details := make([]string, 0, len(failures))
	for _, failure := range failures {
		detail := strings.TrimSpace(failure.Detail)
		if detail == "" {
			detail = strings.TrimSpace(failure.Reason)
		}
		if detail == "" {
			detail = "task failed to start"
		}
		details = append(details, detail)
	}
	return "RunTask reported failure: " + strings.Join(details, "; ")
}

func stoppedTaskFailure(tasks []types.Task) string {
	for _, task := range tasks {
		for _, container := range task.Containers {
			if container.ExitCode == nil || *container.ExitCode == 0 {
				continue
			}
			return fmt.Sprintf("task %s container %s exited with code %d", task.TaskArn, container.Name, *container.ExitCode)
		}
	}
	return ""
}

func ecsTaskDuration(tasks []types.Task, fallback int64) int64 {
	var earliest, latest time.Time
	for _, task := range tasks {
		if task.StartedAt != nil && (earliest.IsZero() || task.StartedAt.Before(earliest)) {
			earliest = *task.StartedAt
		}
		if task.StoppedAt != nil && (latest.IsZero() || task.StoppedAt.After(latest)) {
			latest = *task.StoppedAt
		}
	}
	if earliest.IsZero() || latest.IsZero() || latest.Before(earliest) {
		return fallback
	}
	return latest.Sub(earliest).Milliseconds()
}

// fireTargets invokes all targets on a rule with the given event payload.
func (s *Service) fireTargets(rule *types.EventBridgeRule, eventPayload []byte) {
	if len(rule.Targets) == 0 {
		return
	}

	started := time.Now().UTC()
	correlationID := tracesvc.NewCorrelationID()

	spans := make([]tracesvc.Span, 0, len(rule.Targets)+1)
	spans = append(spans, tracesvc.Span{
		Kind:   "eventbridge",
		Name:   rule.Name,
		Status: "ok",
		Meta: map[string]string{
			"rule":          rule.Name,
			"ruleArn":       rule.Arn,
			"trigger":       "event-pattern",
			"correlationId": correlationID,
		},
	})

	failed := 0
	for i := range rule.Targets {
		target := &rule.Targets[i]
		success, targetSpans := s.dispatchTarget(target, eventPayload, targetDispatchOptions{ruleKey: rule.Name, correlationID: correlationID})
		if !success {
			failed++
		}
		spans = append(spans, targetSpans...)
	}

	rule.LastRunAt = &started
	if failed > 0 {
		rule.LastResult = fmt.Sprintf("ERROR: %d/%d targets failed", failed, len(rule.Targets))
		spans[0].Status = "error"
	} else {
		rule.LastResult = "OK"
	}
	_ = s.store.SaveRule(rule)

	if s.traceStore != nil {
		traceID := uuid.NewString()[:8]
		status := 200
		if failed > 0 {
			status = 500
		}
		s.traceStore.Add(&tracesvc.Trace{
			ID:            traceID,
			CorrelationID: correlationID,
			StartedAt:     started,
			DurationMs:    time.Since(started).Milliseconds(),
			Status:        status,
			Method:        "EVENTBRIDGE",
			Path:          "/events/" + rule.Name,
			Spans:         spans,
		})
	}
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
		success, targetSpans := s.dispatchTarget(target, eventPayload, targetDispatchOptions{
			ruleKey:           rule.Name,
			invokedAt:         &started,
			earlyFailureSpans: true,
			errorDetailMeta:   true,
			correlationID:     correlationID,
		})
		if success {
			result.Successful++
		} else {
			result.Failed++
		}
		spans = append(spans, targetSpans...)
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
	return "", validationError("Only the default event bus is supported")
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

func canonicalEventBridgeTargetARN(cfg *config.Config, target *types.EventBridgeTarget) (string, error) {
	if target == nil {
		return "", fmt.Errorf("target is required")
	}
	if target.EcsParameters != nil || targetKind(target.Arn) == "ecs" {
		if target.EcsParameters == nil {
			return "", fmt.Errorf("EcsParameters are required for an ECS target")
		}
		canonical, err := canonicalECSClusterARN(target.Arn)
		if err != nil {
			return "", err
		}
		if err := validateECSParameters(target.EcsParameters); err != nil {
			return "", err
		}
		if err := validateECSInput(target); err != nil {
			return "", err
		}
		return canonical, nil
	}
	return canonicalLambdaTargetARN(cfg, target.Arn)
}

func validateECSInput(target *types.EventBridgeTarget) error {
	if target == nil || strings.TrimSpace(target.Input) == "" {
		return nil
	}
	var document any
	if err := json.Unmarshal([]byte(target.Input), &document); err != nil {
		return fmt.Errorf("ECS target Input must be valid JSON: %w", err)
	}
	object, ok := document.(map[string]any)
	if !ok {
		return nil
	}
	var value any
	value, ok = object["containerOverrides"]
	if !ok {
		for key, candidate := range object {
			if strings.EqualFold(key, "containerOverrides") {
				value, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("invalid ECS containerOverrides in target input: %w", err)
	}
	var overrides []types.ContainerOverride
	if err := json.Unmarshal(raw, &overrides); err != nil {
		return fmt.Errorf("invalid ECS containerOverrides in target input: %w", err)
	}
	for _, override := range overrides {
		if strings.TrimSpace(override.Name) == "" {
			return fmt.Errorf("ECS container override name is required")
		}
	}
	return nil
}

func canonicalTargetARNReference(cfg *config.Config, target string) (string, error) {
	if targetKind(target) == "ecs" {
		return canonicalECSClusterARN(target)
	}
	return canonicalLambdaTargetARN(cfg, target)
}

func canonicalECSClusterARN(target string) (string, error) {
	target = strings.TrimSpace(target)
	parts := strings.SplitN(target, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "ecs" || parts[3] == "" || parts[4] == "" {
		return "", fmt.Errorf("target Arn must be an ECS cluster")
	}
	const marker = "cluster/"
	if !strings.HasPrefix(parts[5], marker) {
		return "", fmt.Errorf("target Arn must be an ECS cluster")
	}
	if !validECSName(strings.TrimPrefix(parts[5], marker)) {
		return "", fmt.Errorf("target Arn must reference a valid ECS cluster")
	}
	return target, nil
}

func canonicalECSTaskDefinitionARN(target string) (string, error) {
	target = strings.TrimSpace(target)
	parts := strings.SplitN(target, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "ecs" || parts[3] == "" || parts[4] == "" {
		return "", fmt.Errorf("TaskDefinitionArn must be an ECS task definition ARN")
	}
	const marker = "task-definition/"
	if !strings.HasPrefix(parts[5], marker) {
		return "", fmt.Errorf("TaskDefinitionArn must be an ECS task definition ARN")
	}
	tail := strings.TrimPrefix(parts[5], marker)
	separator := strings.LastIndexByte(tail, ':')
	if separator <= 0 || separator == len(tail)-1 {
		return "", fmt.Errorf("TaskDefinitionArn must include a task definition revision")
	}
	family := tail[:separator]
	revision, err := strconv.Atoi(tail[separator+1:])
	if err != nil || revision <= 0 || !validECSName(family) {
		return "", fmt.Errorf("TaskDefinitionArn must include a valid family and revision")
	}
	return target, nil
}

func taskDefinitionFamily(arn string) string {
	const marker = "task-definition/"
	idx := strings.Index(arn, marker)
	if idx < 0 {
		return ""
	}
	tail := arn[idx+len(marker):]
	if separator := strings.LastIndexByte(tail, ':'); separator >= 0 {
		tail = tail[:separator]
	}
	return tail
}

func validECSName(value string) bool {
	if value == "" || len(value) > 255 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func cloneECSParameters(src *types.EcsParameters) *types.EcsParameters {
	if src == nil {
		return nil
	}
	dst := *src
	if src.ContainerOverrides != nil {
		dst.ContainerOverrides = make([]types.ContainerOverride, len(src.ContainerOverrides))
		for i, override := range src.ContainerOverrides {
			dst.ContainerOverrides[i] = override
			if override.Command != nil {
				dst.ContainerOverrides[i].Command = append([]string(nil), override.Command...)
			}
			if override.Environment != nil {
				dst.ContainerOverrides[i].Environment = append([]types.KeyValuePair(nil), override.Environment...)
			}
		}
	}
	dst.NetworkConfiguration = cloneECSNetworkConfiguration(src.NetworkConfiguration)
	return &dst
}

func cloneECSNetworkConfiguration(src *types.EcsNetworkConfiguration) *types.EcsNetworkConfiguration {
	if src == nil {
		return nil
	}
	dst := *src
	if src.AwsvpcConfiguration != nil {
		vpc := *src.AwsvpcConfiguration
		vpc.Subnets = append([]string(nil), src.AwsvpcConfiguration.Subnets...)
		vpc.SecurityGroups = append([]string(nil), src.AwsvpcConfiguration.SecurityGroups...)
		dst.AwsvpcConfiguration = &vpc
	}
	return &dst
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
		"version":       "0",
		"id":            uuid.NewString(),
		"detail-type":   "Scheduled Event",
		"source":        "aws.events",
		"account":       account,
		"time":          at.Format(time.RFC3339),
		"region":        region,
		"resources":     []string{ruleARN},
		"detail":        detail,
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
