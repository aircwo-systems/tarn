package dynamodb

import (
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// pendingWrite is one validated item change, computed under the store lock and
// applied by commitLocked. Planning never mutates state, so a transaction or
// batch can plan every action before applying any.
type pendingWrite struct {
	state *tableState
	keyID string
	key   map[string]any
	old   map[string]any // nil when the item did not exist
	new   map[string]any // nil for a delete
}

// WriteInput is the request shape shared by PutItem, UpdateItem, DeleteItem
// and the transaction actions; each reads the fields it needs.
type WriteInput struct {
	TableName                           string            `json:"TableName"`
	Item                                map[string]any    `json:"Item"`
	Key                                 map[string]any    `json:"Key"`
	UpdateExpression                    string            `json:"UpdateExpression"`
	ConditionExpression                 string            `json:"ConditionExpression"`
	ExpressionAttributeNames            map[string]string `json:"ExpressionAttributeNames"`
	ExpressionAttributeValues           map[string]any    `json:"ExpressionAttributeValues"`
	ReturnValues                        string            `json:"ReturnValues"`
	ReturnValuesOnConditionCheckFailure string            `json:"ReturnValuesOnConditionCheckFailure"`
}

type writeKind int

const (
	writePut writeKind = iota
	writeUpdate
	writeDelete
	writeCheck
)

// planLocked validates one write against current state. Caller holds s.mu.
func (s *Store) planLocked(kind writeKind, in *WriteInput) (*pendingWrite, error) {
	_, state, err := s.getTableLocked(in.TableName)
	if err != nil {
		return nil, err
	}
	values, err := canonicalizeItem(in.ExpressionAttributeValues)
	if err != nil {
		return nil, err
	}
	w := &pendingWrite{state: state}
	if kind == writePut {
		if w.new, err = canonicalizeItem(in.Item); err != nil {
			return nil, err
		}
		if err := validateItem(state.Table, w.new); err != nil {
			return nil, err
		}
		if w.key, err = buildKeyMap(w.new, state.Table.KeySchema); err != nil {
			return nil, err
		}
	} else if w.key, err = canonicalKey(state.Table, in.Key); err != nil {
		return nil, err
	}
	if w.keyID, err = serializeKey(w.key, state.Table.KeySchema); err != nil {
		return nil, err
	}
	w.old = state.Items[w.keyID].Item

	if ok, err := evaluateConditionExpression(in.ConditionExpression, w.old, in.ExpressionAttributeNames, values); err != nil {
		return nil, err
	} else if !ok {
		failed := conditionalCheckFailed("The conditional request failed")
		if strings.EqualFold(in.ReturnValuesOnConditionCheckFailure, "ALL_OLD") {
			failed.Item = cloneItem(w.old)
		}
		return nil, failed
	}

	switch kind {
	case writeUpdate:
		w.new = cloneItem(w.old)
		if w.new == nil {
			w.new = cloneItem(w.key)
		}
		if err := applyUpdateExpression(w.new, in.UpdateExpression, in.ExpressionAttributeNames, values); err != nil {
			return nil, err
		}
		for name := range w.key {
			if !attributeValueEqual(w.new[name], w.key[name]) {
				return nil, validationError("One or more parameter values were invalid: Cannot update attribute %s. This attribute is part of the key", name)
			}
		}
		if w.new, err = canonicalizeItem(w.new); err != nil {
			return nil, err
		}
		if err := validateItem(state.Table, w.new); err != nil {
			return nil, err
		}
	case writeDelete, writeCheck:
		w.new = nil
	}
	return w, nil
}

// commitLocked applies a planned write and records it on the stream. A write
// that changes nothing emits no stream record, as in DynamoDB.
func (s *Store) commitLocked(w *pendingWrite, now time.Time, identity *types.StreamUserIdentity) {
	state := w.state
	s.pruneStreamLocked(state, now)
	switch {
	case w.new == nil && w.old == nil:
		return
	case w.new == nil:
		delete(state.Items, w.keyID)
		s.appendStreamRecordLocked(state, "REMOVE", w.key, w.old, nil, now, identity)
	default:
		state.Items[w.keyID] = itemState{Item: w.new}
		if w.old == nil {
			s.appendStreamRecordLocked(state, "INSERT", w.key, nil, w.new, now, identity)
		} else if !attributeValueEqual(map[string]any{"M": w.old}, map[string]any{"M": w.new}) {
			s.appendStreamRecordLocked(state, "MODIFY", w.key, w.old, w.new, now, identity)
		}
	}
	state.Table.ItemCount = int64(len(state.Items))
}

// writeOne plans and commits a single write.
func (s *Store) writeOne(kind writeKind, in *WriteInput) (*pendingWrite, error) {
	s.mu.Lock()
	w, err := s.planLocked(kind, in)
	if err == nil {
		s.commitLocked(w, time.Now().UTC(), nil)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return w, s.persist()
}

// PutItem writes a whole item, returning the old one for ReturnValues ALL_OLD.
func (s *Store) PutItem(in *WriteInput) (map[string]any, error) {
	if err := checkReturnValues(in.ReturnValues, "NONE", "ALL_OLD"); err != nil {
		return nil, err
	}
	w, err := s.writeOne(writePut, in)
	if err != nil {
		return nil, err
	}
	return returnedAttributes(in.ReturnValues, w), nil
}

// UpdateItem applies an update expression, creating the item if needed.
func (s *Store) UpdateItem(in *WriteInput) (map[string]any, error) {
	if err := checkReturnValues(in.ReturnValues, "NONE", "ALL_OLD", "ALL_NEW", "UPDATED_OLD", "UPDATED_NEW"); err != nil {
		return nil, err
	}
	w, err := s.writeOne(writeUpdate, in)
	if err != nil {
		return nil, err
	}
	return returnedAttributes(in.ReturnValues, w), nil
}

// DeleteItem removes an item, returning it for ReturnValues ALL_OLD.
func (s *Store) DeleteItem(in *WriteInput) (map[string]any, error) {
	if err := checkReturnValues(in.ReturnValues, "NONE", "ALL_OLD"); err != nil {
		return nil, err
	}
	w, err := s.writeOne(writeDelete, in)
	if err != nil {
		return nil, err
	}
	return returnedAttributes(in.ReturnValues, w), nil
}

func checkReturnValues(value string, allowed ...string) error {
	if value == "" {
		return nil
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return validationError("ReturnValues can only be %s", strings.Join(allowed, ", "))
}

func returnedAttributes(mode string, w *pendingWrite) map[string]any {
	switch mode {
	case "ALL_OLD":
		return cloneItem(w.old)
	case "ALL_NEW":
		return cloneItem(w.new)
	case "UPDATED_NEW":
		return emptyToNil(diffItemAttributes(w.old, w.new))
	case "UPDATED_OLD":
		return emptyToNil(diffItemAttributes(w.new, w.old))
	}
	return nil
}

func emptyToNil(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	return m
}

// GetItem returns the item with the given key, or nil.
func (s *Store) GetItem(tableName string, key map[string]any, projection string, names map[string]string) (map[string]any, error) {
	var out map[string]any
	err := s.readTable(tableName, func(state *tableState) error {
		var err error
		out, _, err = getItemLocked(state, key)
		return err
	})
	if err != nil || out == nil || projection == "" {
		return out, err
	}
	return applyProjectionExpression(out, projection, names)
}

// getItemLocked returns a copy of the item with key, or nil, and its key ID.
func getItemLocked(state *tableState, key map[string]any) (map[string]any, string, error) {
	canonical, err := canonicalKey(state.Table, key)
	if err != nil {
		return nil, "", err
	}
	keyID, err := serializeKey(canonical, state.Table.KeySchema)
	if err != nil {
		return nil, "", err
	}
	return cloneItem(state.Items[keyID].Item), keyID, nil
}

// BatchWriteRequest is one entry of a BatchWriteItem RequestItems list.
type BatchWriteRequest struct {
	PutRequest *struct {
		Item map[string]any `json:"Item"`
	} `json:"PutRequest,omitempty"`
	DeleteRequest *struct {
		Key map[string]any `json:"Key"`
	} `json:"DeleteRequest,omitempty"`
}

// BatchWriteItem applies up to 25 puts and deletes. Every request is validated
// before any is applied, as DynamoDB rejects the whole batch on a bad entry.
func (s *Store) BatchWriteItem(requests map[string][]BatchWriteRequest) error {
	total := 0
	for _, list := range requests {
		total += len(list)
	}
	if total == 0 || total > 25 {
		return validationError("Too many items requested for the BatchWriteItem call")
	}
	s.mu.Lock()
	plans := make([]*pendingWrite, 0, total)
	seen := make(map[*tableState]map[string]struct{})
	for table, list := range requests {
		for _, req := range list {
			var w *pendingWrite
			var err error
			switch {
			case req.PutRequest != nil && req.DeleteRequest == nil:
				w, err = s.planLocked(writePut, &WriteInput{TableName: table, Item: req.PutRequest.Item})
			case req.DeleteRequest != nil && req.PutRequest == nil:
				w, err = s.planLocked(writeDelete, &WriteInput{TableName: table, Key: req.DeleteRequest.Key})
			default:
				err = validationError("Supplied AttributeValue has more than one or no request set")
			}
			if err == nil && markSeen(seen, w) {
				err = validationError("Provided list of item keys contains duplicates")
			}
			if err != nil {
				s.mu.Unlock()
				return err
			}
			plans = append(plans, w)
		}
	}
	now := time.Now().UTC()
	for _, w := range plans {
		s.commitLocked(w, now, nil)
	}
	s.mu.Unlock()
	return s.persist()
}

// markSeen records a planned write's item and reports whether the same item
// was already part of the request.
func markSeen(seen map[*tableState]map[string]struct{}, w *pendingWrite) bool {
	if seen[w.state] == nil {
		seen[w.state] = make(map[string]struct{})
	}
	if _, dup := seen[w.state][w.keyID]; dup {
		return true
	}
	seen[w.state][w.keyID] = struct{}{}
	return false
}

// KeysAndAttributes is one table's entry in a BatchGetItem request.
type KeysAndAttributes struct {
	Keys                     []map[string]any  `json:"Keys"`
	ProjectionExpression     string            `json:"ProjectionExpression"`
	ExpressionAttributeNames map[string]string `json:"ExpressionAttributeNames"`
	ConsistentRead           bool              `json:"ConsistentRead"`
}

// BatchGetItem reads up to 100 items across tables. Missing items are omitted.
func (s *Store) BatchGetItem(requests map[string]KeysAndAttributes) (map[string][]map[string]any, error) {
	total := 0
	for _, req := range requests {
		total += len(req.Keys)
	}
	if total == 0 || total > 100 {
		return nil, validationError("Too many items requested for the BatchGetItem call")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]map[string]any, len(requests))
	for table, req := range requests {
		_, state, err := s.getTableLocked(table)
		if err != nil {
			return nil, err
		}
		seen := make(map[string]struct{}, len(req.Keys))
		items := make([]map[string]any, 0, len(req.Keys))
		for _, key := range req.Keys {
			item, id, err := getItemLocked(state, key)
			if err != nil {
				return nil, err
			}
			if _, dup := seen[id]; dup {
				return nil, validationError("Provided list of item keys contains duplicates")
			}
			seen[id] = struct{}{}
			if item == nil {
				continue
			}
			if req.ProjectionExpression != "" {
				if item, err = applyProjectionExpression(item, req.ProjectionExpression, req.ExpressionAttributeNames); err != nil {
					return nil, err
				}
			}
			items = append(items, item)
		}
		out[table] = items
	}
	return out, nil
}

// TransactWriteItem is one action of a TransactWriteItems request.
type TransactWriteItem struct {
	ConditionCheck *WriteInput `json:"ConditionCheck,omitempty"`
	Put            *WriteInput `json:"Put,omitempty"`
	Update         *WriteInput `json:"Update,omitempty"`
	Delete         *WriteInput `json:"Delete,omitempty"`
}

// TransactWriteItems applies up to 100 actions atomically: every action is
// planned against the same state, and nothing is applied unless all succeed.
func (s *Store) TransactWriteItems(actions []TransactWriteItem) error {
	if len(actions) == 0 || len(actions) > 100 {
		return validationError("Member must have length less than or equal to 100")
	}
	s.mu.Lock()
	plans := make([]*pendingWrite, len(actions))
	reasons := make([]CancellationReason, len(actions))
	cancelled := false
	seen := make(map[*tableState]map[string]struct{})
	for i, action := range actions {
		kind, in, err := action.unpack()
		if err != nil {
			s.mu.Unlock()
			return err
		}
		w, err := s.planLocked(kind, in)
		reasons[i] = CancellationReason{Code: "None"}
		if err != nil {
			svcErr, ok := err.(*ServiceError)
			if !ok || svcErr.Code != "ConditionalCheckFailedException" {
				s.mu.Unlock()
				return err
			}
			reasons[i] = CancellationReason{Code: "ConditionalCheckFailed", Message: svcErr.Message, Item: svcErr.Item}
			cancelled = true
			continue
		}
		if markSeen(seen, w) {
			s.mu.Unlock()
			return validationError("Transaction request cannot include multiple operations on one item")
		}
		if kind != writeCheck {
			plans[i] = w
		}
	}
	if cancelled {
		s.mu.Unlock()
		return transactionCanceled(reasons)
	}
	now := time.Now().UTC()
	for _, w := range plans {
		if w != nil {
			s.commitLocked(w, now, nil)
		}
	}
	s.mu.Unlock()
	return s.persist()
}

func (a TransactWriteItem) unpack() (writeKind, *WriteInput, error) {
	var (
		kind  writeKind
		in    *WriteInput
		count int
	)
	for k, candidate := range map[writeKind]*WriteInput{writeCheck: a.ConditionCheck, writePut: a.Put, writeUpdate: a.Update, writeDelete: a.Delete} {
		if candidate != nil {
			kind, in = k, candidate
			count++
		}
	}
	if count != 1 {
		return 0, nil, validationError("TransactItems can only contain one of Check, Put, Update or Delete")
	}
	if kind == writeCheck && in.ConditionExpression == "" {
		return 0, nil, validationError("The expression can not be empty;")
	}
	return kind, in, nil
}

// TransactGetItem is one action of a TransactGetItems request.
type TransactGetItem struct {
	Get *struct {
		TableName                string            `json:"TableName"`
		Key                      map[string]any    `json:"Key"`
		ProjectionExpression     string            `json:"ProjectionExpression"`
		ExpressionAttributeNames map[string]string `json:"ExpressionAttributeNames"`
	} `json:"Get"`
}

// TransactGetItems reads up to 100 items from one consistent snapshot. Each
// response is nil when its item does not exist.
func (s *Store) TransactGetItems(actions []TransactGetItem) ([]map[string]any, error) {
	if len(actions) == 0 || len(actions) > 100 {
		return nil, validationError("Member must have length less than or equal to 100")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]map[string]any, len(actions))
	for i, action := range actions {
		if action.Get == nil {
			return nil, validationError("TransactItems must contain Get")
		}
		_, state, err := s.getTableLocked(action.Get.TableName)
		if err != nil {
			return nil, err
		}
		item, _, err := getItemLocked(state, action.Get.Key)
		if err != nil {
			return nil, err
		}
		if item != nil && action.Get.ProjectionExpression != "" {
			if item, err = applyProjectionExpression(item, action.Get.ProjectionExpression, action.Get.ExpressionAttributeNames); err != nil {
				return nil, err
			}
		}
		out[i] = item
	}
	return out, nil
}
