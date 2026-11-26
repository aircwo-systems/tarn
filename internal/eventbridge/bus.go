package eventbridge

import (
	"strconv"
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// CreateEventBus creates a custom event bus. Partner event sources
// (EventSourceName) are not supported.
func (s *Service) CreateEventBus(name, description string, tags map[string]string) (*types.EventBridgeEventBus, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, validationError("Parameter Name is required")
	}
	if name == defaultEventBusName {
		return nil, &ServiceError{Code: "ResourceAlreadyExistsException", Message: "Event bus default already exists.", HTTPStatus: 400}
	}
	if strings.Contains(name, "/") || !validEventBusName(name) {
		return nil, validationError("Parameter Name is not valid: %s", name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.store.GetBus(name); err == nil {
		return nil, &ServiceError{Code: "ResourceAlreadyExistsException", Message: "Event bus " + name + " already exists.", HTTPStatus: 400}
	}
	now := time.Now().UTC()
	bus := &types.EventBridgeEventBus{
		Name:           name,
		Arn:            eventBusARN(s.cfg, name),
		Description:    description,
		Tags:           cloneStringMap(tags),
		CreatedAt:      now,
		LastModifiedAt: now,
	}
	if err := s.store.SaveBus(bus); err != nil {
		return nil, internalError("failed to save event bus: %v", err)
	}
	return cloneEventBus(bus), nil
}

// DescribeEventBus returns a bus by name or ARN; "" describes the default bus.
func (s *Service) DescribeEventBus(eventBusName string) (*types.EventBridgeEventBus, error) {
	bus, err := normalizeEventBusName(eventBusName)
	if err != nil {
		return nil, err
	}
	return s.busByName(bus)
}

// DeleteEventBus deletes a custom bus. Like the other rule-owning deletes in
// Tarn it refuses while rules remain on the bus, so a misordered teardown
// fails loudly instead of orphaning rules.
func (s *Service) DeleteEventBus(name string) error {
	bus, err := normalizeEventBusName(name)
	if err != nil {
		return err
	}
	if bus == defaultEventBusName {
		return validationError("Cannot delete event bus default.")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.store.GetBus(bus); err != nil {
		// Deleting a missing bus succeeds so teardown is idempotent.
		return nil
	}
	for _, rule := range s.store.ListRules() {
		if ruleBus(rule) == bus {
			return validationError("Event bus %s cannot be deleted because it still has rules", bus)
		}
	}
	if err := s.store.DeleteBus(bus); err != nil {
		return internalError("failed to delete event bus: %v", err)
	}
	return nil
}

// ListEventBuses lists the default bus and every custom bus.
func (s *Service) ListEventBuses(namePrefix string, limit int, nextToken string) ([]*types.EventBridgeEventBus, string, error) {
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

	all := append([]*types.EventBridgeEventBus{s.defaultBus()}, s.store.ListBuses()...)
	filtered := make([]*types.EventBridgeEventBus, 0, len(all))
	for _, bus := range all {
		if namePrefix == "" || strings.HasPrefix(bus.Name, namePrefix) {
			filtered = append(filtered, bus)
		}
	}
	if offset >= len(filtered) {
		return []*types.EventBridgeEventBus{}, "", nil
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

// AllRules returns every rule on every event bus, for the admin overview.
func (s *Service) AllRules() []*types.EventBridgeRule {
	return s.store.ListRules()
}

// CustomEventBuses returns the custom (non-default) event buses.
func (s *Service) CustomEventBuses() []*types.EventBridgeEventBus {
	return s.store.ListBuses()
}

func (s *Service) defaultBus() *types.EventBridgeEventBus {
	return &types.EventBridgeEventBus{Name: defaultEventBusName, Arn: eventBusARN(s.cfg, defaultEventBusName)}
}

func (s *Service) busByName(bus string) (*types.EventBridgeEventBus, error) {
	if bus == defaultEventBusName {
		return s.defaultBus(), nil
	}
	found, err := s.store.GetBus(bus)
	if err != nil {
		return nil, notFoundError("Event bus %s does not exist.", bus)
	}
	return found, nil
}

// updateBusTags applies mutate to a custom bus's tags. Callers hold s.mu.
func (s *Service) updateBusTags(busName string, mutate func(map[string]string)) error {
	if busName == defaultEventBusName {
		return validationError("Tagging the default event bus is not supported")
	}
	bus, err := s.store.GetBus(busName)
	if err != nil {
		return notFoundError("Event bus %s does not exist.", busName)
	}
	if bus.Tags == nil {
		bus.Tags = map[string]string{}
	}
	mutate(bus.Tags)
	bus.LastModifiedAt = time.Now().UTC()
	if err := s.store.SaveBus(bus); err != nil {
		return internalError("failed to tag event bus: %v", err)
	}
	return nil
}

// eventBusNameFromResourceARN recognises arn:aws:events:<r>:<a>:event-bus/<name>.
func eventBusNameFromResourceARN(resourceARN string) (string, bool) {
	resourceARN = strings.TrimSpace(resourceARN)
	const marker = ":event-bus/"
	idx := strings.Index(resourceARN, marker)
	if !strings.HasPrefix(resourceARN, "arn:aws:events:") || idx < 0 {
		return "", false
	}
	name := resourceARN[idx+len(marker):]
	if name == "" {
		return "", false
	}
	return name, true
}
