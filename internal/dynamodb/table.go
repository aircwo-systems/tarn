package dynamodb

import (
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// validateTableDefinition applies CreateTable's rules for keys and indexes.
func validateTableDefinition(table *types.DynamoDBTable) error {
	attrTypes := attributeTypeMap(table.AttributeDefinitions)
	if err := validateKeySchema(table.KeySchema, attrTypes); err != nil {
		return err
	}
	if len(table.LocalSecondaryIndexes) > 5 {
		return validationError("One or more parameter values were invalid: Number of LocalSecondaryIndexes exceeds per-table limit of 5")
	}
	if len(table.GlobalSecondaryIndexes) > 20 {
		return validationError("One or more parameter values were invalid: GlobalSecondaryIndex count exceeds the per-table limit of 20")
	}
	names := make(map[string]struct{})
	used := make(map[string]struct{})
	for _, part := range table.KeySchema {
		used[part.AttributeName] = struct{}{}
	}
	checkIndex := func(name string, schema []types.DynamoDBKeySchemaElement, projection types.DynamoDBProjection) error {
		if _, dup := names[name]; dup || name == "" {
			return validationError("One or more parameter values were invalid: Duplicate index name: %s", name)
		}
		names[name] = struct{}{}
		if err := validateKeySchema(schema, attrTypes); err != nil {
			return err
		}
		if (projection.ProjectionType == "INCLUDE") != (len(projection.NonKeyAttributes) > 0) {
			return validationError("One or more parameter values were invalid: NonKeyAttributes must be specified only with ProjectionType INCLUDE")
		}
		for _, part := range schema {
			used[part.AttributeName] = struct{}{}
		}
		return nil
	}
	for _, idx := range table.LocalSecondaryIndexes {
		if err := checkIndex(idx.IndexName, idx.KeySchema, idx.Projection); err != nil {
			return err
		}
		if len(table.KeySchema) != 2 || hashKey(idx.KeySchema) != hashKey(table.KeySchema) || len(idx.KeySchema) != 2 {
			return validationError("One or more parameter values were invalid: Table KeySchema and LocalSecondaryIndex KeySchema must have the same hash key and both must have a range key")
		}
	}
	for _, idx := range table.GlobalSecondaryIndexes {
		if err := checkIndex(idx.IndexName, idx.KeySchema, idx.Projection); err != nil {
			return err
		}
	}
	if len(used) != len(attrTypes) {
		return validationError("One or more parameter values were invalid: Number of attributes in KeySchema does not exactly match number of attributes defined in AttributeDefinitions")
	}
	return nil
}

func hashKey(schema []types.DynamoDBKeySchemaElement) string {
	for _, part := range schema {
		if part.KeyType == "HASH" {
			return part.AttributeName
		}
	}
	return ""
}

// prepareIndexes stamps the server-assigned fields every index carries.
func (s *Store) prepareIndexes(table *types.DynamoDBTable) {
	for i := range table.GlobalSecondaryIndexes {
		idx := &table.GlobalSecondaryIndexes[i]
		idx.IndexArn = table.TableArn + "/index/" + idx.IndexName
		idx.IndexStatus = "ACTIVE"
		idx.WarmThroughput = &types.DynamoDBWarmThroughput{ReadUnitsPerSecond: 1, WriteUnitsPerSecond: 1, Status: "ACTIVE"}
		if idx.ProvisionedThroughput == nil {
			idx.ProvisionedThroughput = &types.DynamoDBProvisionedThroughput{}
		}
	}
	for i := range table.LocalSecondaryIndexes {
		idx := &table.LocalSecondaryIndexes[i]
		idx.IndexArn = table.TableArn + "/index/" + idx.IndexName
	}
}

// describeLocked returns a copy of the table with its item and size counters
// filled in. Caller holds s.mu.
func describeLocked(state *tableState) *types.DynamoDBTable {
	out := cloneTable(state.Table)
	normalizeTableCompatibility(out)
	out.ItemCount = int64(len(state.Items))
	out.TableSizeBytes = 0
	for i := range out.GlobalSecondaryIndexes {
		out.GlobalSecondaryIndexes[i].ItemCount, out.GlobalSecondaryIndexes[i].IndexSizeBytes = 0, 0
	}
	for i := range out.LocalSecondaryIndexes {
		out.LocalSecondaryIndexes[i].ItemCount, out.LocalSecondaryIndexes[i].IndexSizeBytes = 0, 0
	}
	for _, stored := range state.Items {
		size := int64(itemSize(stored.Item))
		out.TableSizeBytes += size
		for i := range out.GlobalSecondaryIndexes {
			if _, err := buildKeyMap(stored.Item, out.GlobalSecondaryIndexes[i].KeySchema); err == nil {
				out.GlobalSecondaryIndexes[i].ItemCount++
				out.GlobalSecondaryIndexes[i].IndexSizeBytes += size
			}
		}
		for i := range out.LocalSecondaryIndexes {
			if _, err := buildKeyMap(stored.Item, out.LocalSecondaryIndexes[i].KeySchema); err == nil {
				out.LocalSecondaryIndexes[i].ItemCount++
				out.LocalSecondaryIndexes[i].IndexSizeBytes += size
			}
		}
	}
	return out
}

// UpdateTableInput is the UpdateTable request.
type UpdateTableInput struct {
	TableName                   string                               `json:"TableName"`
	AttributeDefinitions        []types.DynamoDBAttributeDefinition  `json:"AttributeDefinitions"`
	BillingMode                 string                               `json:"BillingMode"`
	ProvisionedThroughput       *types.DynamoDBProvisionedThroughput `json:"ProvisionedThroughput"`
	StreamSpecification         *types.DynamoDBStreamSpecification   `json:"StreamSpecification"`
	DeletionProtectionEnabled   *bool                                `json:"DeletionProtectionEnabled"`
	SSESpecification            *types.DynamoDBSSESpecification      `json:"SSESpecification"`
	TableClass                  string                               `json:"TableClass"`
	GlobalSecondaryIndexUpdates []GlobalSecondaryIndexUpdate         `json:"GlobalSecondaryIndexUpdates"`
}

// GlobalSecondaryIndexUpdate creates, updates or deletes one GSI.
type GlobalSecondaryIndexUpdate struct {
	Create *types.DynamoDBGlobalSecondaryIndex `json:"Create"`
	Update *struct {
		IndexName             string                               `json:"IndexName"`
		ProvisionedThroughput *types.DynamoDBProvisionedThroughput `json:"ProvisionedThroughput"`
	} `json:"Update"`
	Delete *struct {
		IndexName string `json:"IndexName"`
	} `json:"Delete"`
}

// UpdateTable changes a table's settings and GSIs. Changes are validated on a
// copy and applied together, so a rejected request changes nothing. New
// indexes are ACTIVE at once: there is no backfill to wait for.
func (s *Store) UpdateTable(in *UpdateTableInput) (*types.DynamoDBTable, error) {
	if strings.TrimSpace(in.TableName) == "" {
		return nil, validationError("TableName is required")
	}

	s.mu.Lock()
	_, state, err := s.getTableLocked(in.TableName)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	next := cloneTable(state.Table)
	if len(in.GlobalSecondaryIndexUpdates) > 0 {
		defined := attributeTypeMap(next.AttributeDefinitions)
		for _, def := range in.AttributeDefinitions {
			if _, ok := defined[def.AttributeName]; !ok {
				next.AttributeDefinitions = append(next.AttributeDefinitions, def)
			}
		}
		for _, update := range in.GlobalSecondaryIndexUpdates {
			switch {
			case update.Create != nil:
				next.GlobalSecondaryIndexes = append(next.GlobalSecondaryIndexes, *update.Create)
			case update.Delete != nil:
				if err := removeGSI(next, update.Delete.IndexName); err != nil {
					s.mu.Unlock()
					return nil, err
				}
			case update.Update != nil:
				idx := findGSI(next, update.Update.IndexName)
				if idx == nil {
					s.mu.Unlock()
					return nil, notFoundError("Requested resource not found: Index: %s", update.Update.IndexName)
				}
				idx.ProvisionedThroughput = update.Update.ProvisionedThroughput
			}
		}
		next.AttributeDefinitions = usedAttributeDefinitions(next)
		if err := validateTableDefinition(next); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.prepareIndexes(next)
	}
	if in.BillingMode != "" {
		next.BillingModeSummary = &types.DynamoDBBillingModeSummary{BillingMode: in.BillingMode}
	}
	if in.ProvisionedThroughput != nil {
		next.ProvisionedThroughput = in.ProvisionedThroughput
	}
	if in.DeletionProtectionEnabled != nil {
		next.DeletionProtectionEnabled = *in.DeletionProtectionEnabled
	}
	if in.TableClass != "" {
		next.TableClassSummary = &types.DynamoDBTableClassSummary{TableClass: in.TableClass}
	}
	if in.SSESpecification != nil {
		sse, err := sseDescription(s.cfg, in.SSESpecification)
		if err != nil {
			return nil, err
		}
		next.SSEDescription = sse
	}
	state.Table = next

	if in.StreamSpecification != nil {
		previousARN := ""
		if state.Stream != nil {
			previousARN = state.Stream.ARN
		}
		applyTableStreamUpdateLocked(s.cfg, state, in.StreamSpecification, time.Now().UTC())
		// Disabling the stream, or changing its view type, retires the old ARN.
		// Wake anything parked on it so it re-reads and reports it missing.
		if previousARN != "" && (state.Stream == nil || state.Stream.ARN != previousARN) {
			s.signalStreamLocked(previousARN)
		}
	}
	updated := describeLocked(state)
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return nil, internalError("failed to persist table update: %v", err)
	}
	return updated, nil
}

func findGSI(table *types.DynamoDBTable, name string) *types.DynamoDBGlobalSecondaryIndex {
	for i := range table.GlobalSecondaryIndexes {
		if table.GlobalSecondaryIndexes[i].IndexName == name {
			return &table.GlobalSecondaryIndexes[i]
		}
	}
	return nil
}

func removeGSI(table *types.DynamoDBTable, name string) error {
	for i, idx := range table.GlobalSecondaryIndexes {
		if idx.IndexName == name {
			table.GlobalSecondaryIndexes = append(table.GlobalSecondaryIndexes[:i], table.GlobalSecondaryIndexes[i+1:]...)
			return nil
		}
	}
	return notFoundError("Requested resource not found: Index: %s", name)
}

// usedAttributeDefinitions keeps only the definitions a key schema uses, as
// DynamoDB drops an index's attributes when the index is deleted.
func usedAttributeDefinitions(table *types.DynamoDBTable) []types.DynamoDBAttributeDefinition {
	used := make(map[string]struct{})
	mark := func(schema []types.DynamoDBKeySchemaElement) {
		for _, part := range schema {
			used[part.AttributeName] = struct{}{}
		}
	}
	mark(table.KeySchema)
	for _, idx := range table.GlobalSecondaryIndexes {
		mark(idx.KeySchema)
	}
	for _, idx := range table.LocalSecondaryIndexes {
		mark(idx.KeySchema)
	}
	out := make([]types.DynamoDBAttributeDefinition, 0, len(used))
	for _, def := range table.AttributeDefinitions {
		if _, ok := used[def.AttributeName]; ok {
			out = append(out, def)
		}
	}
	return out
}
