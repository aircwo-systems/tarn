package dynamodb

import (
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

type Service struct {
	cfg   *config.Config
	store *Store
}

func NewService(cfg *config.Config) *Service {
	return &Service{
		cfg:   cfg,
		store: NewStore(cfg),
	}
}

func (s *Service) Init() error {
	return s.store.Init()
}

func (s *Service) CreateTable(input *types.DynamoDBTable) (*types.DynamoDBTable, error) {
	return s.store.CreateTable(input)
}

func (s *Service) DescribeTable(name string) (*types.DynamoDBTable, error) {
	return s.store.DescribeTable(name)
}

func (s *Service) ListTables(limit int, exclusiveStart string) ([]string, string, error) {
	return s.store.ListTables(limit, exclusiveStart)
}

func (s *Service) DeleteTable(name string) (*types.DynamoDBTable, error) {
	return s.store.DeleteTable(name)
}

func (s *Service) UpdateTable(name string, streamSpec *types.DynamoDBStreamSpecification) (*types.DynamoDBTable, error) {
	return s.store.UpdateTableDefinition(name, streamSpec)
}

func (s *Service) TagResource(resource string, tags []types.DynamoDBTag) error {
	return s.store.TagResource(resource, tags)
}

func (s *Service) UntagResource(resource string, tagKeys []string) error {
	return s.store.UntagResource(resource, tagKeys)
}

func (s *Service) ListTagsOfResource(resource string) ([]types.DynamoDBTag, error) {
	return s.store.ListTagsOfResource(resource)
}

func (s *Service) DescribeTimeToLive(name string) (map[string]any, error) {
	return s.store.DescribeTimeToLive(name)
}

func (s *Service) UpdateTimeToLive(name, attributeName string, enabled bool) (map[string]any, error) {
	return s.store.UpdateTimeToLive(name, attributeName, enabled)
}

func (s *Service) DescribeContinuousBackups(name string) (map[string]any, error) {
	return s.store.DescribeContinuousBackups(name)
}

func (s *Service) UpdateContinuousBackups(name string, enabled bool) (map[string]any, error) {
	return s.store.UpdateContinuousBackups(name, enabled)
}

func (s *Service) DescribeContributorInsights(name, indexName string) (map[string]any, error) {
	return s.store.DescribeContributorInsights(name, indexName)
}

func (s *Service) UpdateContributorInsights(name, indexName, action string) (map[string]any, error) {
	return s.store.UpdateContributorInsights(name, indexName, action)
}

func (s *Service) DescribeKinesisStreamingDestination(name string) (map[string]any, error) {
	return s.store.DescribeKinesisStreamingDestination(name)
}

func (s *Service) EnableKinesisStreamingDestination(name, streamArn string) (map[string]any, error) {
	return s.store.EnableKinesisStreamingDestination(name, streamArn)
}

func (s *Service) DisableKinesisStreamingDestination(name, streamArn string) (map[string]any, error) {
	return s.store.DisableKinesisStreamingDestination(name, streamArn)
}

func (s *Service) PutItem(tableName string, item map[string]any, condition string, names map[string]string, values map[string]any, returnValues string) (*putItemResult, error) {
	return s.store.PutItem(tableName, item, condition, names, values, returnValues)
}

func (s *Service) GetItem(tableName string, key map[string]any, projection string, names map[string]string) (*getItemResult, error) {
	return s.store.GetItem(tableName, key, projection, names)
}

func (s *Service) UpdateItem(tableName string, key map[string]any, updateExpr string, condition string, names map[string]string, values map[string]any, returnValues string) (*updateItemResult, error) {
	return s.store.UpdateItem(tableName, key, updateExpr, condition, names, values, returnValues)
}

func (s *Service) DeleteItem(tableName string, key map[string]any, condition string, names map[string]string, values map[string]any, returnValues string) (*deleteItemResult, error) {
	return s.store.DeleteItem(tableName, key, condition, names, values, returnValues)
}

func (s *Service) Scan(tableName, indexName, filterExpr, projection string, names map[string]string, values map[string]any, limit int, exclusiveStartKey map[string]any) (*scanResult, error) {
	return s.store.Scan(tableName, indexName, filterExpr, projection, names, values, limit, exclusiveStartKey)
}

func (s *Service) Query(tableName, indexName, keyCondition, filterExpr, projection string, names map[string]string, values map[string]any, limit int, exclusiveStartKey map[string]any, scanIndexForward bool) (*queryResult, error) {
	return s.store.Query(tableName, indexName, keyCondition, filterExpr, projection, names, values, limit, exclusiveStartKey, scanIndexForward)
}

func (s *Service) ListStreams(tableName string, limit int, exclusiveStartArn string) ([]map[string]any, string, error) {
	return s.store.ListStreams(tableName, limit, exclusiveStartArn)
}

func (s *Service) DescribeStream(streamArn string, limit int, exclusiveStartShardID string) (map[string]any, error) {
	return s.store.DescribeStream(streamArn, limit, exclusiveStartShardID)
}

func (s *Service) GetShardIterator(streamArn, shardID, iteratorType, sequenceNumber string) (string, error) {
	return s.store.GetShardIterator(streamArn, shardID, iteratorType, sequenceNumber)
}

func (s *Service) GetRecords(iterator string, limit int) ([]map[string]any, string, error) {
	return s.store.GetRecords(iterator, limit)
}

func (s *Service) StreamBatch(streamArn, lastSequence string, limit int) ([]*types.StreamRecord, string, error) {
	return s.store.StreamBatch(streamArn, lastSequence, limit)
}
