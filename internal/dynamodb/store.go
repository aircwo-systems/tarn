package dynamodb

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const (
	defaultStreamRetention = 24 * time.Hour
	streamShardID          = "shardId-000000000000"
)

type Store struct {
	mu     sync.RWMutex
	dirty  atomic.Bool
	cfg    *config.Config
	tables map[string]*tableState
}

type tableState struct {
	Table               *types.DynamoDBTable                `json:"table"`
	Items               map[string]itemState                `json:"items"`
	Stream              *streamState                        `json:"stream,omitempty"`
	TTL                 *ttlState                           `json:"ttl,omitempty"`
	ContinuousBackups   *continuousBackupsState             `json:"continuousBackups,omitempty"`
	ContributorInsights map[string]*contributorInsightState `json:"contributorInsights,omitempty"`
	KinesisDestinations []*kinesisStreamingDestinationState `json:"kinesisDestinations,omitempty"`
}

type itemState struct {
	Item map[string]any `json:"item"`
}

type streamState struct {
	ARN        string                `json:"arn"`
	Label      string                `json:"label"`
	ViewType   string                `json:"viewType"`
	NextSeq    int64                 `json:"nextSeq"`
	Records    []*types.StreamRecord `json:"records"`
	LastPruned time.Time             `json:"lastPruned"`
}

type ttlState struct {
	AttributeName string `json:"attributeName,omitempty"`
	Status        string `json:"status"`
}

type continuousBackupsState struct {
	PointInTimeRecoveryEnabled bool    `json:"pointInTimeRecoveryEnabled"`
	LastUpdated                float64 `json:"lastUpdated,omitempty"`
}

type contributorInsightState struct {
	Status      string  `json:"status"`
	LastUpdated float64 `json:"lastUpdated,omitempty"`
}

type kinesisStreamingDestinationState struct {
	StreamArn         string `json:"streamArn"`
	DestinationStatus string `json:"destinationStatus"`
}

type tableSnapshot struct {
	Tables map[string]*tableState `json:"tables"`
}

type itemView struct {
	item map[string]any
	key  map[string]any
}

type putItemResult struct {
	Attributes map[string]any
}

type getItemResult struct {
	Item map[string]any
}

type updateItemResult struct {
	Attributes map[string]any
}

type deleteItemResult struct {
	Attributes map[string]any
}

type scanResult struct {
	Items            []map[string]any
	Count            int
	LastEvaluatedKey map[string]any
}

type queryResult struct {
	Items            []map[string]any
	Count            int
	LastEvaluatedKey map[string]any
}

func NewStore(cfg *config.Config) *Store {
	return &Store{
		cfg:    cfg,
		tables: make(map[string]*tableState),
	}
}

func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}
	go s.startFlusher()

	if err := os.MkdirAll(s.cfg.DynamoDBDir(), 0o755); err != nil {
		return fmt.Errorf("create dynamodb dir: %w", err)
	}
	data, err := os.ReadFile(s.cfg.DynamoDBStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dynamodb state: %w", err)
	}
	if len(data) == 0 {
		return nil
	}

	var snapshot tableSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode dynamodb state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tables = make(map[string]*tableState, len(snapshot.Tables))
	for name, state := range snapshot.Tables {
		if state == nil || state.Table == nil {
			continue
		}
		normalizeTableCompatibility(state.Table)
		s.tables[name] = state
		s.pruneStreamLocked(state, time.Now().UTC())
	}
	return nil
}

func (s *Store) persist() error {
	s.dirty.Store(true)
	return nil
}

func (s *Store) Flush() { s.flushToDisk() }

func (s *Store) startFlusher() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		if s.dirty.Swap(false) {
			s.flushToDisk()
		}
	}
}

func (s *Store) flushToDisk() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	s.mu.RLock()
	data, err := json.Marshal(tableSnapshot{Tables: s.tables})
	s.mu.RUnlock()

	if err != nil {
		return
	}
	if err := os.MkdirAll(s.cfg.DynamoDBDir(), 0o755); err != nil {
		return
	}
	tmpFile, err := os.CreateTemp(s.cfg.DynamoDBDir(), "state-*.json.tmp")
	if err != nil {
		return
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, s.cfg.DynamoDBStatePath()); err != nil {
		_ = os.Remove(tmpPath)
	}
}

func (s *Store) CreateTable(input *types.DynamoDBTable) (*types.DynamoDBTable, error) {
	if input == nil {
		return nil, validationError("Table definition is required")
	}
	if strings.TrimSpace(input.TableName) == "" {
		return nil, validationError("TableName is required")
	}
	if len(input.KeySchema) == 0 {
		return nil, validationError("KeySchema is required")
	}
	attrTypes := attributeTypeMap(input.AttributeDefinitions)
	if err := validateKeySchema(input.KeySchema, attrTypes); err != nil {
		return nil, err
	}
	for _, idx := range input.LocalSecondaryIndexes {
		if err := validateKeySchema(idx.KeySchema, attrTypes); err != nil {
			return nil, err
		}
	}
	for _, idx := range input.GlobalSecondaryIndexes {
		if err := validateKeySchema(idx.KeySchema, attrTypes); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	table := cloneTable(input)
	table.TableArn = tableARN(s.cfg, table.TableName)
	table.TableId = fmt.Sprintf("%s-%d", table.TableName, now.UnixNano())
	table.TableStatus = types.DynamoDBTableStatusActive
	table.CreationDateTime = float64(now.UnixMilli()) / 1000.0
	table.ItemCount = 0
	table.TableSizeBytes = 0

	billingMode := "PAY_PER_REQUEST"
	if table.BillingModeSummary != nil && table.BillingModeSummary.BillingMode != "" {
		billingMode = table.BillingModeSummary.BillingMode
	}
	table.BillingModeSummary = &types.DynamoDBBillingModeSummary{BillingMode: billingMode}

	if table.ProvisionedThroughput == nil {
		table.ProvisionedThroughput = &types.DynamoDBProvisionedThroughput{}
	}
	normalizeTableCompatibility(table)

	if table.StreamSpecification != nil && table.StreamSpecification.StreamEnabled {
		table.LatestStreamLabel = now.Format("2006-01-02T15:04:05.000")
		table.LatestStreamArn = streamARN(s.cfg, table.TableName, table.LatestStreamLabel)
	}

	state := &tableState{
		Table: table,
		Items: make(map[string]itemState),
	}
	if table.StreamSpecification != nil && table.StreamSpecification.StreamEnabled {
		state.Stream = &streamState{
			ARN:      table.LatestStreamArn,
			Label:    table.LatestStreamLabel,
			ViewType: table.StreamSpecification.StreamViewType,
			Records:  make([]*types.StreamRecord, 0),
		}
	}

	s.mu.Lock()
	if _, exists := s.tables[table.TableName]; exists {
		s.mu.Unlock()
		return nil, validationError("Table already exists: %s", table.TableName)
	}
	s.tables[table.TableName] = state
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist table: %v", err)
	}
	return cloneTable(table), nil
}

func (s *Store) DescribeTable(name string) (*types.DynamoDBTable, error) {
	state, err := s.getTable(name)
	if err != nil {
		return nil, err
	}
	normalizeTableCompatibility(state.Table)
	return cloneTable(state.Table), nil
}

func (s *Store) ListTables(limit int, exclusiveStart string) ([]string, string, error) {
	s.mu.RLock()
	names := make([]string, 0, len(s.tables))
	for name := range s.tables {
		names = append(names, name)
	}
	s.mu.RUnlock()
	sort.Strings(names)

	start := 0
	if exclusiveStart != "" {
		idx := sort.SearchStrings(names, exclusiveStart)
		for idx < len(names) && names[idx] <= exclusiveStart {
			idx++
		}
		start = idx
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	end := start + limit
	if end > len(names) {
		end = len(names)
	}
	last := ""
	if end < len(names) && end > 0 {
		last = names[end-1]
	}
	return names[start:end], last, nil
}

func (s *Store) DeleteTable(name string) (*types.DynamoDBTable, error) {
	if strings.TrimSpace(name) == "" {
		return nil, validationError("TableName is required")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(name)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	deleted := cloneTable(state.Table)
	if deleted != nil {
		deleted.TableStatus = types.DynamoDBTableStatusDeleting
	}
	delete(s.tables, state.Table.TableName)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist table delete: %v", err)
	}
	return deleted, nil
}

func (s *Store) UpdateTableDefinition(name string, streamSpec *types.DynamoDBStreamSpecification) (*types.DynamoDBTable, error) {
	if strings.TrimSpace(name) == "" {
		return nil, validationError("TableName is required")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(name)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	if streamSpec != nil {
		applyTableStreamUpdateLocked(s.cfg, state, streamSpec, time.Now().UTC())
	}
	normalizeTableCompatibility(state.Table)

	updated := cloneTable(state.Table)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist table update: %v", err)
	}
	return updated, nil
}

func (s *Store) TagResource(resource string, tags []types.DynamoDBTag) error {
	if strings.TrimSpace(resource) == "" {
		return validationError("ResourceArn is required")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(resource)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	state.Table.Tags = mergeTableTags(state.Table.Tags, tags)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return internalError("failed to persist table tags: %v", err)
	}
	return nil
}

func (s *Store) UntagResource(resource string, tagKeys []string) error {
	if strings.TrimSpace(resource) == "" {
		return validationError("ResourceArn is required")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(resource)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	state.Table.Tags = removeTableTags(state.Table.Tags, tagKeys)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return internalError("failed to persist table tags: %v", err)
	}
	return nil
}

func (s *Store) ListTagsOfResource(resource string) ([]types.DynamoDBTag, error) {
	state, err := s.getTable(resource)
	if err != nil {
		return nil, err
	}
	return cloneTags(state.Table.Tags), nil
}

func (s *Store) DescribeTimeToLive(name string) (map[string]any, error) {
	state, err := s.getTable(name)
	if err != nil {
		return nil, err
	}
	return timeToLiveDescription(state), nil
}

func (s *Store) UpdateTimeToLive(name, attributeName string, enabled bool) (map[string]any, error) {
	if strings.TrimSpace(name) == "" {
		return nil, validationError("TableName is required")
	}
	if enabled && strings.TrimSpace(attributeName) == "" {
		return nil, validationError("AttributeName is required when enabling TTL")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(name)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if enabled {
		state.TTL = &ttlState{
			AttributeName: strings.TrimSpace(attributeName),
			Status:        "ENABLED",
		}
	} else {
		state.TTL = &ttlState{Status: "DISABLED"}
	}
	out := timeToLiveSpecification(state)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist ttl update: %v", err)
	}
	return out, nil
}

func (s *Store) DescribeContinuousBackups(name string) (map[string]any, error) {
	state, err := s.getTable(name)
	if err != nil {
		return nil, err
	}
	return continuousBackupsDescription(state), nil
}

func (s *Store) UpdateContinuousBackups(name string, enabled bool) (map[string]any, error) {
	if strings.TrimSpace(name) == "" {
		return nil, validationError("TableName is required")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(name)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	state.ContinuousBackups = &continuousBackupsState{
		PointInTimeRecoveryEnabled: enabled,
		LastUpdated:                nowEpoch(time.Now().UTC()),
	}
	out := continuousBackupsDescription(state)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist continuous backups update: %v", err)
	}
	return out, nil
}

func (s *Store) DescribeContributorInsights(name, indexName string) (map[string]any, error) {
	state, err := s.getTable(name)
	if err != nil {
		return nil, err
	}
	return contributorInsightsDescription(state, indexName), nil
}

func (s *Store) UpdateContributorInsights(name, indexName, action string) (map[string]any, error) {
	if strings.TrimSpace(name) == "" {
		return nil, validationError("TableName is required")
	}
	action = strings.ToUpper(strings.TrimSpace(action))
	if action == "" {
		return nil, validationError("ContributorInsightsAction is required")
	}
	if action != "ENABLE" && action != "DISABLE" {
		return nil, validationError("Invalid ContributorInsightsAction: %s", action)
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(name)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if state.ContributorInsights == nil {
		state.ContributorInsights = make(map[string]*contributorInsightState)
	}
	key := strings.TrimSpace(indexName)
	status := "DISABLED"
	if action == "ENABLE" {
		status = "ENABLED"
	}
	state.ContributorInsights[key] = &contributorInsightState{
		Status:      status,
		LastUpdated: nowEpoch(time.Now().UTC()),
	}
	out := contributorInsightsDescription(state, indexName)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist contributor insights update: %v", err)
	}
	return out, nil
}

func (s *Store) DescribeKinesisStreamingDestination(name string) (map[string]any, error) {
	state, err := s.getTable(name)
	if err != nil {
		return nil, err
	}
	return kinesisStreamingDestinationDescription(state), nil
}

func (s *Store) EnableKinesisStreamingDestination(name, streamArn string) (map[string]any, error) {
	if strings.TrimSpace(name) == "" {
		return nil, validationError("TableName is required")
	}
	if strings.TrimSpace(streamArn) == "" {
		return nil, validationError("StreamArn is required")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(name)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	state.KinesisDestinations = upsertKinesisDestination(state.KinesisDestinations, &kinesisStreamingDestinationState{
		StreamArn:         strings.TrimSpace(streamArn),
		DestinationStatus: "ACTIVE",
	})
	out := kinesisStreamingEnableResult(state, streamArn)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist kinesis streaming destination: %v", err)
	}
	return out, nil
}

func (s *Store) DisableKinesisStreamingDestination(name, streamArn string) (map[string]any, error) {
	if strings.TrimSpace(name) == "" {
		return nil, validationError("TableName is required")
	}
	if strings.TrimSpace(streamArn) == "" {
		return nil, validationError("StreamArn is required")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(name)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	state.KinesisDestinations = removeKinesisDestination(state.KinesisDestinations, streamArn)
	out := kinesisStreamingDisableResult(state, streamArn)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist kinesis streaming destination removal: %v", err)
	}
	return out, nil
}

func (s *Store) PutItem(
	tableName string,
	item map[string]any,
	condition string,
	names map[string]string,
	values map[string]any,
	returnValues string,
) (*putItemResult, error) {
	now := time.Now().UTC()

	s.mu.Lock()
	_, state, err := s.getTableLocked(tableName)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.pruneStreamLocked(state, now)

	if err := validateItemAgainstTable(state.Table, item); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	key, err := buildKeyMap(item, state.Table.KeySchema)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	keyID, err := serializeKey(key, state.Table.KeySchema)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	oldItem, exists := state.Items[keyID]
	if condition != "" {
		ok, evalErr := evaluateConditionExpression(condition, oldItem.Item, names, values)
		if evalErr != nil {
			s.mu.Unlock()
			return nil, evalErr
		}
		if !ok {
			s.mu.Unlock()
			return nil, conditionalCheckFailed("The conditional request failed")
		}
	}

	clonedItem := cloneItem(item)
	state.Items[keyID] = itemState{Item: clonedItem}
	state.Table.ItemCount = int64(len(state.Items))
	eventName := "INSERT"
	if exists {
		eventName = "MODIFY"
	}
	s.appendStreamRecordLocked(state, eventName, key, oldItem.Item, clonedItem, now)
	result := &putItemResult{}
	switch strings.ToUpper(strings.TrimSpace(returnValues)) {
	case "ALL_OLD":
		result.Attributes = cloneItem(oldItem.Item)
	}
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist item: %v", err)
	}
	return result, nil
}

func (s *Store) GetItem(
	tableName string,
	key map[string]any,
	projection string,
	names map[string]string,
) (*getItemResult, error) {
	state, err := s.getTable(tableName)
	if err != nil {
		return nil, err
	}
	keyID, err := serializeKey(key, state.Table.KeySchema)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	item, ok := state.Items[keyID]
	s.mu.RUnlock()
	if !ok {
		return &getItemResult{}, nil
	}
	out := cloneItem(item.Item)
	if projection != "" {
		out, err = applyProjectionExpression(out, projection, names)
		if err != nil {
			return nil, err
		}
	}
	return &getItemResult{Item: out}, nil
}

func (s *Store) DeleteItem(
	tableName string,
	key map[string]any,
	condition string,
	names map[string]string,
	values map[string]any,
	returnValues string,
) (*deleteItemResult, error) {
	now := time.Now().UTC()

	s.mu.Lock()
	_, state, err := s.getTableLocked(tableName)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	keyID, err := serializeKey(key, state.Table.KeySchema)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	item, exists := state.Items[keyID]
	if condition != "" {
		ok, evalErr := evaluateConditionExpression(condition, item.Item, names, values)
		if evalErr != nil {
			s.mu.Unlock()
			return nil, evalErr
		}
		if !ok {
			s.mu.Unlock()
			return nil, conditionalCheckFailed("The conditional request failed")
		}
	}
	if !exists {
		s.mu.Unlock()
		return &deleteItemResult{}, nil
	}
	delete(state.Items, keyID)
	state.Table.ItemCount = int64(len(state.Items))
	s.appendStreamRecordLocked(state, "REMOVE", key, item.Item, nil, now)
	result := &deleteItemResult{}
	if strings.ToUpper(strings.TrimSpace(returnValues)) == "ALL_OLD" {
		result.Attributes = cloneItem(item.Item)
	}
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist item delete: %v", err)
	}
	return result, nil
}

func (s *Store) UpdateItem(
	tableName string,
	key map[string]any,
	updateExpr string,
	condition string,
	names map[string]string,
	values map[string]any,
	returnValues string,
) (*updateItemResult, error) {
	now := time.Now().UTC()

	s.mu.Lock()
	_, state, err := s.getTableLocked(tableName)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.pruneStreamLocked(state, now)
	keyID, err := serializeKey(key, state.Table.KeySchema)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	current := cloneItem(state.Items[keyID].Item)
	if current == nil {
		current = cloneItem(key)
	}
	if err := validateItemAgainstTable(state.Table, current); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if condition != "" {
		ok, evalErr := evaluateConditionExpression(condition, state.Items[keyID].Item, names, values)
		if evalErr != nil {
			s.mu.Unlock()
			return nil, evalErr
		}
		if !ok {
			s.mu.Unlock()
			return nil, conditionalCheckFailed("The conditional request failed")
		}
	}
	oldItem := cloneItem(state.Items[keyID].Item)
	if err := applyUpdateExpression(current, updateExpr, names, values); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	for keyName, av := range key {
		current[keyName] = cloneAny(av)
	}
	if err := validateItemAgainstTable(state.Table, current); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	newKey, err := buildKeyMap(current, state.Table.KeySchema)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	newKeyID, err := serializeKey(newKey, state.Table.KeySchema)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if newKeyID != keyID {
		s.mu.Unlock()
		return nil, validationError("The document path provided in the update expression is invalid for update")
	}
	state.Items[keyID] = itemState{Item: current}
	state.Table.ItemCount = int64(len(state.Items))
	eventName := "MODIFY"
	if oldItem == nil {
		eventName = "INSERT"
	}
	s.appendStreamRecordLocked(state, eventName, newKey, oldItem, current, now)
	result := &updateItemResult{}
	switch strings.ToUpper(strings.TrimSpace(returnValues)) {
	case "ALL_NEW":
		result.Attributes = cloneItem(current)
	case "ALL_OLD":
		result.Attributes = cloneItem(oldItem)
	case "UPDATED_NEW":
		updated := diffItemAttributes(oldItem, current)
		result.Attributes = updated
	case "UPDATED_OLD":
		result.Attributes = diffItemAttributes(current, oldItem)
	}
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist item update: %v", err)
	}
	return result, nil
}

func (s *Store) Scan(
	tableName string,
	indexName string,
	filterExpr string,
	projection string,
	names map[string]string,
	values map[string]any,
	limit int,
	exclusiveStartKey map[string]any,
) (*scanResult, error) {
	state, err := s.getTable(tableName)
	if err != nil {
		return nil, err
	}
	var projectionDef *types.DynamoDBProjection
	if indexName != "" {
		_, idxProj, idxErr := lookupIndex(state.Table, indexName)
		if idxErr != nil {
			return nil, idxErr
		}
		projectionDef = idxProj
	}
	views, err := s.collectSortedItems(state, indexName, false)
	if err != nil {
		return nil, err
	}
	start := findExclusiveStart(views, state.Table.KeySchema, exclusiveStartKey)
	items := make([]map[string]any, 0)
	count := 0
	var lastKey map[string]any
	if limit <= 0 {
		limit = len(views)
	}
	for i := start; i < len(views); i++ {
		view := views[i]
		ok := true
		if filterExpr != "" {
			ok, err = evaluateConditionExpression(filterExpr, view.item, names, values)
			if err != nil {
				return nil, err
			}
		}
		if !ok {
			continue
		}
		count++
		item := cloneItem(view.item)
		if projectionDef != nil {
			item = applyIndexProjection(item, *projectionDef, state.Table.KeySchema)
		}
		if projection != "" {
			item, err = applyProjectionExpression(item, projection, names)
			if err != nil {
				return nil, err
			}
		}
		items = append(items, item)
		lastKey = cloneItem(view.key)
		if len(items) >= limit {
			if i+1 < len(views) {
				break
			}
		}
	}
	if len(items) == 0 || len(items) < limit || start+count >= len(views) {
		lastKey = nil
	}
	return &scanResult{Items: items, Count: len(items), LastEvaluatedKey: lastKey}, nil
}

func (s *Store) Query(
	tableName string,
	indexName string,
	keyCondition string,
	filterExpr string,
	projection string,
	names map[string]string,
	values map[string]any,
	limit int,
	exclusiveStartKey map[string]any,
	scanIndexForward bool,
) (*queryResult, error) {
	if strings.TrimSpace(keyCondition) == "" {
		return nil, validationError("Query condition missed key schema element")
	}
	state, err := s.getTable(tableName)
	if err != nil {
		return nil, err
	}
	_, projectionDef, err := lookupIndex(state.Table, indexName)
	if err != nil {
		return nil, err
	}
	views, err := s.collectSortedItems(state, indexName, !scanIndexForward)
	if err != nil {
		return nil, err
	}
	start := findExclusiveStart(views, state.Table.KeySchema, exclusiveStartKey)
	items := make([]map[string]any, 0)
	var lastKey map[string]any
	if limit <= 0 {
		limit = len(views)
	}
	for i := start; i < len(views); i++ {
		view := views[i]
		match, evalErr := evaluateConditionExpression(keyCondition, view.item, names, values)
		if evalErr != nil {
			return nil, evalErr
		}
		if !match {
			continue
		}
		if filterExpr != "" {
			match, evalErr = evaluateConditionExpression(filterExpr, view.item, names, values)
			if evalErr != nil {
				return nil, evalErr
			}
			if !match {
				continue
			}
		}
		item := cloneItem(view.item)
		if projectionDef != nil {
			item = applyIndexProjection(item, *projectionDef, state.Table.KeySchema)
		}
		if projection != "" {
			item, err = applyProjectionExpression(item, projection, names)
			if err != nil {
				return nil, err
			}
		}
		items = append(items, item)
		lastKey = cloneItem(view.key)
		if len(items) >= limit {
			if i+1 < len(views) {
				break
			}
		}
	}
	if len(items) == 0 || len(items) < limit {
		lastKey = nil
	}
	return &queryResult{Items: items, Count: len(items), LastEvaluatedKey: lastKey}, nil
}

func (s *Store) ListStreams(tableName string, limit int, exclusiveStartArn string) ([]map[string]any, string, error) {
	filterName := normalizeTableIdentifier(tableName)

	s.mu.RLock()
	streams := make([]map[string]any, 0)
	for _, state := range s.tables {
		if state.Stream == nil || state.Table == nil {
			continue
		}
		if filterName != "" && state.Table.TableName != filterName {
			continue
		}
		streams = append(streams, map[string]any{
			"StreamArn":   state.Stream.ARN,
			"TableName":   state.Table.TableName,
			"StreamLabel": state.Stream.Label,
		})
	}
	s.mu.RUnlock()
	sort.Slice(streams, func(i, j int) bool {
		return fmt.Sprint(streams[i]["StreamArn"]) < fmt.Sprint(streams[j]["StreamArn"])
	})
	start := 0
	if exclusiveStartArn != "" {
		for i, stream := range streams {
			if fmt.Sprint(stream["StreamArn"]) == exclusiveStartArn {
				start = i + 1
				break
			}
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	end := start + limit
	if end > len(streams) {
		end = len(streams)
	}
	last := ""
	if end < len(streams) && end > 0 {
		last = fmt.Sprint(streams[end-1]["StreamArn"])
	}
	return streams[start:end], last, nil
}

func (s *Store) DescribeStream(streamArn string, limit int, exclusiveStartShardID string) (map[string]any, error) {
	state, err := s.findStream(streamArn)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	shards := []map[string]any{
		{
			"ShardId": streamShardID,
			"SequenceNumberRange": map[string]any{
				"StartingSequenceNumber": "1",
			},
		},
	}
	if exclusiveStartShardID != "" {
		shards = nil
	}
	if len(shards) > limit {
		shards = shards[:limit]
	}
	return map[string]any{
		"StreamDescription": map[string]any{
			"StreamArn":               state.Stream.ARN,
			"StreamStatus":            "ENABLED",
			"StreamViewType":          state.Stream.ViewType,
			"TableName":               state.Table.TableName,
			"Shards":                  shards,
			"LastEvaluatedShardId":    nil,
			"CreationRequestDateTime": state.Table.CreationDateTime,
			"KeySchema":               state.Table.KeySchema,
		},
	}, nil
}

func (s *Store) GetShardIterator(streamArn, shardID, iteratorType, sequenceNumber string) (string, error) {
	state, err := s.findStream(streamArn)
	if err != nil {
		return "", err
	}
	if shardID != streamShardID {
		return "", validationError("Requested shard does not exist")
	}
	var startSeq int64
	switch strings.ToUpper(strings.TrimSpace(iteratorType)) {
	case "TRIM_HORIZON":
		startSeq = 1
	case "LATEST":
		startSeq = state.Stream.NextSeq
	case "AT_SEQUENCE_NUMBER":
		startSeq, err = strconv.ParseInt(sequenceNumber, 10, 64)
	case "AFTER_SEQUENCE_NUMBER":
		startSeq, err = strconv.ParseInt(sequenceNumber, 10, 64)
		startSeq++
	default:
		return "", validationError("Invalid ShardIteratorType: %s", iteratorType)
	}
	if err != nil {
		return "", validationError("Invalid SequenceNumber")
	}
	return encodeShardIterator(streamArn, startSeq), nil
}

func (s *Store) GetRecords(iterator string, limit int) ([]map[string]any, string, error) {
	streamArn, startSeq, err := decodeShardIterator(iterator)
	if err != nil {
		return nil, "", validationError("Invalid ShardIterator")
	}
	state, err := s.findStream(streamArn)
	if err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	now := time.Now().UTC()
	s.pruneStreamLocked(state, now)
	records := cloneStreamRecords(state.Stream.Records)
	s.mu.Unlock()

	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	out := make([]map[string]any, 0, limit)
	nextSeq := startSeq
	for _, record := range records {
		seq, _ := strconv.ParseInt(record.Dynamodb.SequenceNumber, 10, 64)
		if seq < startSeq {
			continue
		}
		out = append(out, streamRecordShape(record))
		nextSeq = seq + 1
		if len(out) >= limit {
			break
		}
	}
	return out, encodeShardIterator(streamArn, nextSeq), nil
}

func (s *Store) StreamBatch(streamArn, lastSequence string, limit int) ([]*types.StreamRecord, string, error) {
	state, err := s.findStream(streamArn)
	if err != nil {
		return nil, "", err
	}
	startSeq := int64(1)
	if strings.TrimSpace(lastSequence) != "" {
		startSeq, err = strconv.ParseInt(lastSequence, 10, 64)
		if err != nil {
			return nil, "", validationError("Invalid checkpoint sequence")
		}
		startSeq++
	}
	s.mu.Lock()
	now := time.Now().UTC()
	s.pruneStreamLocked(state, now)
	records := cloneStreamRecords(state.Stream.Records)
	s.mu.Unlock()

	if limit <= 0 {
		limit = 100
	}
	out := make([]*types.StreamRecord, 0, limit)
	next := ""
	for _, record := range records {
		seq, _ := strconv.ParseInt(record.Dynamodb.SequenceNumber, 10, 64)
		if seq < startSeq {
			continue
		}
		out = append(out, cloneStreamRecord(record))
		next = record.Dynamodb.SequenceNumber
		if len(out) >= limit {
			break
		}
	}
	return out, next, nil
}

func (s *Store) getTable(name string) (*tableState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, state, err := s.getTableLocked(name)
	return state, err
}

func (s *Store) getTableLocked(name string) (string, *tableState, error) {
	key := normalizeTableIdentifier(name)
	state, ok := s.tables[key]
	if !ok {
		return "", nil, notFoundError("Requested resource not found: Table: %s not found", name)
	}
	return key, state, nil
}

func normalizeTableIdentifier(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "arn:aws:dynamodb:") {
		return trimmed
	}
	idx := strings.Index(trimmed, "table/")
	if idx < 0 {
		return trimmed
	}
	rest := trimmed[idx+len("table/"):]
	if slash := strings.Index(rest, "/"); slash >= 0 {
		rest = rest[:slash]
	}
	return rest
}

func (s *Store) findStream(streamArn string) (*tableState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, state := range s.tables {
		if state.Stream != nil && state.Stream.ARN == streamArn {
			return state, nil
		}
	}
	return nil, notFoundError("Requested resource not found")
}

func (s *Store) collectSortedItems(state *tableState, indexName string, reverse bool) ([]itemView, error) {
	keySchema := state.Table.KeySchema
	if indexName != "" {
		indexSchema, _, err := lookupIndex(state.Table, indexName)
		if err != nil {
			return nil, err
		}
		keySchema = indexSchema
	}
	views := make([]itemView, 0, len(state.Items))
	for _, item := range state.Items {
		key, err := buildKeyMap(item.Item, unionKeySchema(state.Table.KeySchema, keySchema))
		if err != nil {
			continue
		}
		views = append(views, itemView{item: cloneItem(item.Item), key: key})
	}
	sort.Slice(views, func(i, j int) bool {
		cmp := compareForSchema(views[i].item, views[j].item, keySchema)
		if cmp == 0 {
			cmp = compareForSchema(views[i].item, views[j].item, state.Table.KeySchema)
		}
		if reverse {
			return cmp > 0
		}
		return cmp < 0
	})
	return views, nil
}

func (s *Store) appendStreamRecordLocked(state *tableState, eventName string, key, oldImage, newImage map[string]any, now time.Time) {
	if state == nil || state.Stream == nil {
		return
	}
	if eventName == "INSERT" && oldImage != nil {
		eventName = "MODIFY"
	}
	state.Stream.NextSeq++
	seq := strconv.FormatInt(state.Stream.NextSeq, 10)
	record := &types.StreamRecord{
		EventID:        seq,
		EventName:      eventName,
		EventVersion:   "1.1",
		EventSource:    "aws:dynamodb",
		AwsRegion:      s.cfg.Region,
		EventSourceARN: state.Stream.ARN,
		Dynamodb: types.StreamRecordData{
			ApproximateCreationDateTime: float64(now.Unix()),
			Keys:                        cloneItem(key),
			SequenceNumber:              seq,
			SizeBytes:                   int64(len(mustJSON(cloneItem(newImage)))),
			StreamViewType:              state.Stream.ViewType,
		},
	}
	switch state.Stream.ViewType {
	case "KEYS_ONLY":
	case "NEW_IMAGE":
		record.Dynamodb.NewImage = cloneItem(newImage)
	case "OLD_IMAGE":
		record.Dynamodb.OldImage = cloneItem(oldImage)
	case "NEW_AND_OLD_IMAGES":
		record.Dynamodb.NewImage = cloneItem(newImage)
		record.Dynamodb.OldImage = cloneItem(oldImage)
	}
	state.Stream.Records = append(state.Stream.Records, record)
}

func applyTableStreamUpdateLocked(cfg *config.Config, state *tableState, spec *types.DynamoDBStreamSpecification, now time.Time) {
	if state == nil || state.Table == nil || spec == nil {
		return
	}

	state.Table.StreamSpecification = &types.DynamoDBStreamSpecification{
		StreamEnabled:  spec.StreamEnabled,
		StreamViewType: spec.StreamViewType,
	}

	if !spec.StreamEnabled {
		state.Table.LatestStreamArn = ""
		state.Table.LatestStreamLabel = ""
		state.Stream = nil
		return
	}

	if spec.StreamViewType == "" {
		state.Table.StreamSpecification.StreamViewType = "NEW_AND_OLD_IMAGES"
	}

	if state.Stream != nil && state.Stream.ViewType == state.Table.StreamSpecification.StreamViewType {
		return
	}

	label := now.Format("2006-01-02T15:04:05.000")
	arn := streamARN(cfg, state.Table.TableName, label)
	state.Table.LatestStreamLabel = label
	state.Table.LatestStreamArn = arn
	state.Stream = &streamState{
		ARN:      arn,
		Label:    label,
		ViewType: state.Table.StreamSpecification.StreamViewType,
		Records:  make([]*types.StreamRecord, 0),
	}
}

func (s *Store) pruneStreamLocked(state *tableState, now time.Time) {
	if state == nil || state.Stream == nil {
		return
	}
	if !state.Stream.LastPruned.IsZero() && now.Sub(state.Stream.LastPruned) < time.Minute {
		return
	}
	keep := state.Stream.Records[:0]
	for _, record := range state.Stream.Records {
		if record == nil {
			continue
		}
		created := time.Unix(int64(record.Dynamodb.ApproximateCreationDateTime), 0)
		if now.Sub(created) <= defaultStreamRetention {
			keep = append(keep, record)
		}
	}
	state.Stream.Records = keep
	state.Stream.LastPruned = now
}

func tableARN(cfg *config.Config, tableName string) string {
	return fmt.Sprintf("arn:aws:dynamodb:%s:%s:table/%s", cfg.Region, cfg.AccountID, tableName)
}

func normalizeTableCompatibility(table *types.DynamoDBTable) {
	if table == nil {
		return
	}
	if table.TableClassSummary == nil {
		table.TableClassSummary = &types.DynamoDBTableClassSummary{TableClass: "STANDARD"}
	}
	if table.WarmThroughput == nil {
		table.WarmThroughput = &types.DynamoDBWarmThroughput{
			ReadUnitsPerSecond:  1,
			WriteUnitsPerSecond: 1,
			Status:              "ACTIVE",
		}
	}
}

func streamARN(cfg *config.Config, tableName, label string) string {
	return fmt.Sprintf("%s/stream/%s", tableARN(cfg, tableName), label)
}

func attributeTypeMap(defs []types.DynamoDBAttributeDefinition) map[string]string {
	out := make(map[string]string, len(defs))
	for _, def := range defs {
		out[def.AttributeName] = def.AttributeType
	}
	return out
}

func validateKeySchema(schema []types.DynamoDBKeySchemaElement, attrTypes map[string]string) error {
	if len(schema) == 0 || len(schema) > 2 {
		return validationError("KeySchema has invalid length")
	}
	hashCount := 0
	rangeCount := 0
	for _, part := range schema {
		if _, ok := attrTypes[part.AttributeName]; !ok {
			return validationError("One or more parameter values were invalid: Some index key attributes are not defined in AttributeDefinitions. Keys: [%s], AttributeDefinitions: %v", part.AttributeName, sortedAttrNames(attrTypes))
		}
		switch part.KeyType {
		case "HASH":
			hashCount++
		case "RANGE":
			rangeCount++
		default:
			return validationError("Invalid KeyType: %s", part.KeyType)
		}
	}
	if hashCount != 1 || rangeCount > 1 {
		return validationError("KeySchema is invalid")
	}
	return nil
}

func sortedAttrNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func validateItemAgainstTable(table *types.DynamoDBTable, item map[string]any) error {
	if table == nil {
		return validationError("Table does not exist")
	}
	for _, keyPart := range table.KeySchema {
		value, ok := item[keyPart.AttributeName]
		if !ok || value == nil {
			return validationError("One or more parameter values were invalid: Missing the key %s in the item", keyPart.AttributeName)
		}
	}
	return nil
}

func buildKeyMap(item map[string]any, schema []types.DynamoDBKeySchemaElement) (map[string]any, error) {
	out := make(map[string]any, len(schema))
	for _, part := range schema {
		value, ok := item[part.AttributeName]
		if !ok {
			return nil, validationError("Missing key attribute %s", part.AttributeName)
		}
		out[part.AttributeName] = cloneAny(value)
	}
	return out, nil
}

func serializeKey(key map[string]any, schema []types.DynamoDBKeySchemaElement) (string, error) {
	parts := make([]string, 0, len(schema))
	for _, part := range schema {
		value, ok := key[part.AttributeName]
		if !ok {
			return "", validationError("Missing key attribute %s", part.AttributeName)
		}
		data, err := json.Marshal(normalizeAny(value))
		if err != nil {
			return "", validationError("Invalid key attribute %s", part.AttributeName)
		}
		parts = append(parts, part.AttributeName+"="+string(data))
	}
	return strings.Join(parts, "|"), nil
}

func findExclusiveStart(items []itemView, schema []types.DynamoDBKeySchemaElement, startKey map[string]any) int {
	if len(startKey) == 0 {
		return 0
	}
	startID, err := serializeKey(startKey, schema)
	if err != nil {
		return 0
	}
	for i, item := range items {
		id, err := serializeKey(item.key, schema)
		if err == nil && id == startID {
			return i + 1
		}
	}
	return 0
}

func compareForSchema(left, right map[string]any, schema []types.DynamoDBKeySchemaElement) int {
	for _, part := range schema {
		cmp := compareAttributeValues(left[part.AttributeName], right[part.AttributeName])
		if cmp != 0 {
			return cmp
		}
	}
	return 0
}

func lookupIndex(table *types.DynamoDBTable, name string) ([]types.DynamoDBKeySchemaElement, *types.DynamoDBProjection, error) {
	if table == nil {
		return nil, nil, notFoundError("Requested resource not found")
	}
	if name == "" {
		return table.KeySchema, nil, nil
	}
	for _, idx := range table.LocalSecondaryIndexes {
		if idx.IndexName == name {
			proj := idx.Projection
			return idx.KeySchema, &proj, nil
		}
	}
	for _, idx := range table.GlobalSecondaryIndexes {
		if idx.IndexName == name {
			proj := idx.Projection
			return idx.KeySchema, &proj, nil
		}
	}
	return nil, nil, validationError("The table does not have the specified index: %s", name)
}

func unionKeySchema(primary, secondary []types.DynamoDBKeySchemaElement) []types.DynamoDBKeySchemaElement {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	out := make([]types.DynamoDBKeySchemaElement, 0, len(primary)+len(secondary))
	for _, item := range append(append([]types.DynamoDBKeySchemaElement{}, secondary...), primary...) {
		if _, ok := seen[item.AttributeName]; ok {
			continue
		}
		seen[item.AttributeName] = struct{}{}
		out = append(out, item)
	}
	return out
}

func applyIndexProjection(item map[string]any, projection types.DynamoDBProjection, primarySchema []types.DynamoDBKeySchemaElement) map[string]any {
	if item == nil {
		return nil
	}
	projType := strings.ToUpper(strings.TrimSpace(projection.ProjectionType))
	switch projType {
	case "", "ALL":
		return item
	case "KEYS_ONLY":
		allowed := make(map[string]struct{})
		for _, key := range primarySchema {
			allowed[key.AttributeName] = struct{}{}
		}
		out := make(map[string]any)
		for key, value := range item {
			if _, ok := allowed[key]; ok {
				out[key] = cloneAny(value)
			}
		}
		return out
	case "INCLUDE":
		allowed := make(map[string]struct{})
		for _, key := range primarySchema {
			allowed[key.AttributeName] = struct{}{}
		}
		for _, name := range projection.NonKeyAttributes {
			allowed[name] = struct{}{}
		}
		out := make(map[string]any)
		for key, value := range item {
			if _, ok := allowed[key]; ok {
				out[key] = cloneAny(value)
			}
		}
		return out
	default:
		return item
	}
}

func cloneTable(table *types.DynamoDBTable) *types.DynamoDBTable {
	if table == nil {
		return nil
	}
	var out types.DynamoDBTable
	roundTripClone(table, &out)
	return &out
}


func cloneTags(tags []types.DynamoDBTag) []types.DynamoDBTag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]types.DynamoDBTag, len(tags))
	copy(out, tags)
	return out
}

func cloneItem(item map[string]any) map[string]any {
	if item == nil {
		return nil
	}
	var out map[string]any
	roundTripClone(item, &out)
	return out
}

func cloneAny(value any) any {
	return normalizeAny(value)
}

func cloneStreamRecords(records []*types.StreamRecord) []*types.StreamRecord {
	out := make([]*types.StreamRecord, 0, len(records))
	for _, record := range records {
		out = append(out, cloneStreamRecord(record))
	}
	return out
}

func cloneStreamRecord(record *types.StreamRecord) *types.StreamRecord {
	if record == nil {
		return nil
	}
	var out types.StreamRecord
	roundTripClone(record, &out)
	return &out
}

func roundTripClone(src, dst any) {
	data, _ := json.Marshal(src)
	_ = json.Unmarshal(data, dst)
}

func normalizeAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, entry := range v {
			out[key] = normalizeAny(entry)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, entry := range v {
			out[i] = normalizeAny(entry)
		}
		return out
	default:
		return v
	}
}

func mergeTableTags(existing []types.DynamoDBTag, incoming []types.DynamoDBTag) []types.DynamoDBTag {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	merged := make(map[string]string, len(existing)+len(incoming))
	for _, tag := range existing {
		if strings.TrimSpace(tag.Key) == "" {
			continue
		}
		merged[tag.Key] = tag.Value
	}
	for _, tag := range incoming {
		if strings.TrimSpace(tag.Key) == "" {
			continue
		}
		merged[tag.Key] = tag.Value
	}
	return sortTags(merged)
}

func removeTableTags(existing []types.DynamoDBTag, tagKeys []string) []types.DynamoDBTag {
	if len(existing) == 0 {
		return nil
	}
	if len(tagKeys) == 0 {
		return cloneTags(existing)
	}
	remaining := make(map[string]string, len(existing))
	for _, tag := range existing {
		if strings.TrimSpace(tag.Key) == "" {
			continue
		}
		remaining[tag.Key] = tag.Value
	}
	for _, key := range tagKeys {
		delete(remaining, key)
	}
	return sortTags(remaining)
}

func sortTags(values map[string]string) []types.DynamoDBTag {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tags := make([]types.DynamoDBTag, 0, len(keys))
	for _, key := range keys {
		tags = append(tags, types.DynamoDBTag{Key: key, Value: values[key]})
	}
	return tags
}

func timeToLiveDescription(state *tableState) map[string]any {
	description := map[string]any{
		"TimeToLiveStatus": "DISABLED",
	}
	if state != nil && state.TTL != nil {
		description["TimeToLiveStatus"] = state.TTL.Status
		if strings.TrimSpace(state.TTL.AttributeName) != "" {
			description["AttributeName"] = state.TTL.AttributeName
		}
	}
	return map[string]any{"TimeToLiveDescription": description}
}

func timeToLiveSpecification(state *tableState) map[string]any {
	spec := map[string]any{"Enabled": false}
	if state != nil && state.TTL != nil {
		spec["Enabled"] = state.TTL.Status == "ENABLED"
		if strings.TrimSpace(state.TTL.AttributeName) != "" {
			spec["AttributeName"] = state.TTL.AttributeName
		}
	}
	return map[string]any{"TimeToLiveSpecification": spec}
}

func continuousBackupsDescription(state *tableState) map[string]any {
	status := "DISABLED"
	pitrStatus := "DISABLED"
	lastUpdated := 0.0
	if state != nil && state.ContinuousBackups != nil {
		lastUpdated = state.ContinuousBackups.LastUpdated
		if state.ContinuousBackups.PointInTimeRecoveryEnabled {
			status = "ENABLED"
			pitrStatus = "ENABLED"
		}
	}
	description := map[string]any{
		"ContinuousBackupsStatus": status,
		"PointInTimeRecoveryDescription": map[string]any{
			"PointInTimeRecoveryStatus": pitrStatus,
		},
	}
	if lastUpdated > 0 {
		description["PointInTimeRecoveryDescription"].(map[string]any)["LatestRestorableDateTime"] = lastUpdated
		description["PointInTimeRecoveryDescription"].(map[string]any)["EarliestRestorableDateTime"] = lastUpdated
	}
	return map[string]any{"ContinuousBackupsDescription": description}
}

func contributorInsightsDescription(state *tableState, indexName string) map[string]any {
	key := strings.TrimSpace(indexName)
	description := map[string]any{
		"ContributorInsightsStatus": "DISABLED",
	}
	if state != nil && state.Table != nil {
		description["TableName"] = state.Table.TableName
	}
	if key != "" {
		description["IndexName"] = key
	}
	if state != nil && state.ContributorInsights != nil {
		if insight := state.ContributorInsights[key]; insight != nil {
			description["ContributorInsightsStatus"] = insight.Status
			if insight.LastUpdated > 0 {
				description["LastUpdateDateTime"] = insight.LastUpdated
			}
		}
	}
	return description
}

func kinesisStreamingDestinationDescription(state *tableState) map[string]any {
	if state == nil {
		return map[string]any{"KinesisDataStreamDestinations": []map[string]any{}}
	}
	destinations := make([]map[string]any, 0, len(state.KinesisDestinations))
	for _, destination := range state.KinesisDestinations {
		if destination == nil || strings.TrimSpace(destination.StreamArn) == "" {
			continue
		}
		destinations = append(destinations, map[string]any{
			"StreamArn":         destination.StreamArn,
			"DestinationStatus": destination.DestinationStatus,
		})
	}
	return map[string]any{"KinesisDataStreamDestinations": destinations}
}

func kinesisStreamingEnableResult(state *tableState, streamArn string) map[string]any {
	return map[string]any{
		"TableName":         state.Table.TableName,
		"StreamArn":         strings.TrimSpace(streamArn),
		"DestinationStatus": "ACTIVE",
		"KinesisDataStreamDestination": map[string]any{
			"StreamArn":         strings.TrimSpace(streamArn),
			"DestinationStatus": "ACTIVE",
		},
	}
}

func kinesisStreamingDisableResult(state *tableState, streamArn string) map[string]any {
	return map[string]any{
		"TableName":         state.Table.TableName,
		"StreamArn":         strings.TrimSpace(streamArn),
		"DestinationStatus": "DISABLED",
		"KinesisDataStreamDestination": map[string]any{
			"StreamArn":         strings.TrimSpace(streamArn),
			"DestinationStatus": "DISABLED",
		},
	}
}

func upsertKinesisDestination(existing []*kinesisStreamingDestinationState, next *kinesisStreamingDestinationState) []*kinesisStreamingDestinationState {
	if next == nil || strings.TrimSpace(next.StreamArn) == "" {
		return existing
	}
	arn := strings.TrimSpace(next.StreamArn)
	out := make([]*kinesisStreamingDestinationState, 0, len(existing)+1)
	replaced := false
	for _, item := range existing {
		if item == nil || strings.TrimSpace(item.StreamArn) == "" {
			continue
		}
		if strings.TrimSpace(item.StreamArn) == arn {
			out = append(out, &kinesisStreamingDestinationState{
				StreamArn:         arn,
				DestinationStatus: next.DestinationStatus,
			})
			replaced = true
			continue
		}
		out = append(out, &kinesisStreamingDestinationState{
			StreamArn:         item.StreamArn,
			DestinationStatus: item.DestinationStatus,
		})
	}
	if !replaced {
		out = append(out, &kinesisStreamingDestinationState{
			StreamArn:         arn,
			DestinationStatus: next.DestinationStatus,
		})
	}
	return out
}

func removeKinesisDestination(existing []*kinesisStreamingDestinationState, streamArn string) []*kinesisStreamingDestinationState {
	if len(existing) == 0 {
		return nil
	}
	arn := strings.TrimSpace(streamArn)
	out := make([]*kinesisStreamingDestinationState, 0, len(existing))
	for _, item := range existing {
		if item == nil || strings.TrimSpace(item.StreamArn) == "" || strings.TrimSpace(item.StreamArn) == arn {
			continue
		}
		out = append(out, &kinesisStreamingDestinationState{
			StreamArn:         item.StreamArn,
			DestinationStatus: item.DestinationStatus,
		})
	}
	return out
}

func nowEpoch(value time.Time) float64 {
	return float64(value.UnixMilli()) / 1000.0
}

func diffItemAttributes(base, target map[string]any) map[string]any {
	if target == nil {
		return nil
	}
	out := make(map[string]any)
	for key, value := range target {
		if !attributeValueEqual(base[key], value) {
			out[key] = cloneAny(value)
		}
	}
	return out
}

func compareAttributeValues(left, right any) int {
	ln, lok := asNumber(left)
	rn, rok := asNumber(right)
	if lok && rok {
		return ln.Cmp(rn)
	}
	ls := attributeSortValue(left)
	rs := attributeSortValue(right)
	switch {
	case ls < rs:
		return -1
	case ls > rs:
		return 1
	default:
		return 0
	}
}

func attributeSortValue(value any) string {
	data, _ := json.Marshal(normalizeAny(value))
	return string(data)
}

func attributeValueEqual(left, right any) bool {
	return compareAttributeValues(left, right) == 0
}

func asNumber(value any) (*big.Float, bool) {
	attr, ok := value.(map[string]any)
	if !ok || len(attr) != 1 {
		return nil, false
	}
	raw, ok := attr["N"]
	if !ok {
		return nil, false
	}
	text, ok := raw.(string)
	if !ok {
		return nil, false
	}
	out, _, err := big.ParseFloat(text, 10, 256, big.ToNearestEven)
	if err != nil {
		return nil, false
	}
	return out, true
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func encodeShardIterator(streamArn string, startSeq int64) string {
	payload, _ := json.Marshal(map[string]any{"streamArn": streamArn, "startSeq": startSeq})
	return base64.StdEncoding.EncodeToString(payload)
}

func decodeShardIterator(token string) (string, int64, error) {
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", 0, err
	}
	var payload struct {
		StreamARN string `json:"streamArn"`
		StartSeq  int64  `json:"startSeq"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", 0, err
	}
	if payload.StreamARN == "" {
		return "", 0, fmt.Errorf("missing stream ARN")
	}
	if payload.StartSeq <= 0 {
		payload.StartSeq = 1
	}
	return payload.StreamARN, payload.StartSeq, nil
}

func streamRecordShape(record *types.StreamRecord) map[string]any {
	if record == nil {
		return nil
	}
	return map[string]any{
		"eventID":        record.EventID,
		"eventName":      record.EventName,
		"eventVersion":   record.EventVersion,
		"eventSource":    record.EventSource,
		"awsRegion":      record.AwsRegion,
		"eventSourceARN": record.EventSourceARN,
		"dynamodb": map[string]any{
			"ApproximateCreationDateTime": record.Dynamodb.ApproximateCreationDateTime,
			"Keys":                        cloneItem(record.Dynamodb.Keys),
			"NewImage":                    cloneItem(record.Dynamodb.NewImage),
			"OldImage":                    cloneItem(record.Dynamodb.OldImage),
			"SequenceNumber":              record.Dynamodb.SequenceNumber,
			"SizeBytes":                   record.Dynamodb.SizeBytes,
			"StreamViewType":              record.Dynamodb.StreamViewType,
		},
	}
}
