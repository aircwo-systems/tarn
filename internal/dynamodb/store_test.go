package dynamodb

import (
	"os"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func testTableDefinition() *types.DynamoDBTable {
	return &types.DynamoDBTable{
		TableName: "orders",
		AttributeDefinitions: []types.DynamoDBAttributeDefinition{
			{AttributeName: "pk", AttributeType: "S"},
			{AttributeName: "sk", AttributeType: "S"},
			{AttributeName: "lsi_sk", AttributeType: "S"},
			{AttributeName: "gsi_pk", AttributeType: "S"},
			{AttributeName: "gsi_sk", AttributeType: "N"},
		},
		KeySchema: []types.DynamoDBKeySchemaElement{
			{AttributeName: "pk", KeyType: "HASH"},
			{AttributeName: "sk", KeyType: "RANGE"},
		},
		LocalSecondaryIndexes: []types.DynamoDBLocalSecondaryIndex{
			{
				IndexName: "lsi-status",
				KeySchema: []types.DynamoDBKeySchemaElement{
					{AttributeName: "pk", KeyType: "HASH"},
					{AttributeName: "lsi_sk", KeyType: "RANGE"},
				},
				Projection: types.DynamoDBProjection{ProjectionType: "ALL"},
			},
		},
		GlobalSecondaryIndexes: []types.DynamoDBGlobalSecondaryIndex{
			{
				IndexName: "gsi-type",
				KeySchema: []types.DynamoDBKeySchemaElement{
					{AttributeName: "gsi_pk", KeyType: "HASH"},
					{AttributeName: "gsi_sk", KeyType: "RANGE"},
				},
				Projection: types.DynamoDBProjection{ProjectionType: "ALL"},
			},
		},
		StreamSpecification: &types.DynamoDBStreamSpecification{
			StreamEnabled:  true,
			StreamViewType: "NEW_AND_OLD_IMAGES",
		},
	}
}

func testItem(pk, sk, status string, count int, gsiSK string) map[string]any {
	return map[string]any{
		"pk":     map[string]any{"S": pk},
		"sk":     map[string]any{"S": sk},
		"status": map[string]any{"S": status},
		"count":  map[string]any{"N": gsiSK},
		"lsi_sk": map[string]any{"S": status},
		"gsi_pk": map[string]any{"S": "type#" + status},
		"gsi_sk": map[string]any{"N": gsiSK},
		"tags":   map[string]any{"SS": []any{"one", "two"}},
		"meta": map[string]any{
			"M": map[string]any{
				"flag": map[string]any{"BOOL": count%2 == 0},
			},
		},
	}
}

func TestStoreCRUDQueryAndStreams(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	table, err := store.CreateTable(testTableDefinition())
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	if table.TableStatus != types.DynamoDBTableStatusActive {
		t.Fatalf("table status = %q", table.TableStatus)
	}
	if table.LatestStreamArn == "" {
		t.Fatal("expected LatestStreamArn")
	}
	if table.WarmThroughput == nil || table.WarmThroughput.Status != "ACTIVE" {
		t.Fatalf("warm throughput = %#v, want ACTIVE", table.WarmThroughput)
	}

	names, last, err := store.ListTables(10, "")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if len(names) != 1 || names[0] != "orders" || last != "" {
		t.Fatalf("list tables = %v last=%q", names, last)
	}

	firstItem := testItem("acct#1", "order#1", "PENDING", 1, "1")
	if _, err := store.PutItem("orders", firstItem, "", nil, nil, "NONE"); err != nil {
		t.Fatalf("put item: %v", err)
	}
	if _, err := store.PutItem("orders", testItem("acct#1", "order#2", "COMPLETE", 2, "2"), "", nil, nil, "NONE"); err != nil {
		t.Fatalf("put item 2: %v", err)
	}

	getOut, err := store.GetItem("orders", map[string]any{
		"pk": map[string]any{"S": "acct#1"},
		"sk": map[string]any{"S": "order#1"},
	}, "pk,status", nil)
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if getOut.Item["status"] == nil || getOut.Item["pk"] == nil {
		t.Fatalf("projected item = %#v", getOut.Item)
	}

	updateOut, err := store.UpdateItem(
		"orders",
		map[string]any{"pk": map[string]any{"S": "acct#1"}, "sk": map[string]any{"S": "order#1"}},
		"SET status = :next, meta.#flag = :flag ADD count :inc DELETE tags :remove",
		"attribute_exists(pk) AND status = :current",
		map[string]string{"#flag": "flag"},
		map[string]any{
			":next":    map[string]any{"S": "COMPLETE"},
			":current": map[string]any{"S": "PENDING"},
			":flag":    map[string]any{"BOOL": true},
			":inc":     map[string]any{"N": "2"},
			":remove":  map[string]any{"SS": []any{"one"}},
		},
		"ALL_NEW",
	)
	if err != nil {
		t.Fatalf("update item: %v", err)
	}
	if updateOut.Attributes["status"] == nil {
		t.Fatalf("updated attributes = %#v", updateOut.Attributes)
	}

	queryOut, err := store.Query(
		"orders",
		"",
		"pk = :pk AND begins_with(sk, :prefix)",
		"status = :status",
		"",
		nil,
		map[string]any{
			":pk":     map[string]any{"S": "acct#1"},
			":prefix": map[string]any{"S": "order#"},
			":status": map[string]any{"S": "COMPLETE"},
		},
		10,
		nil,
		true,
	)
	if err != nil {
		t.Fatalf("query table: %v", err)
	}
	if len(queryOut.Items) != 2 {
		t.Fatalf("query items len = %d, want 2", len(queryOut.Items))
	}

	indexOut, err := store.Query(
		"orders",
		"gsi-type",
		"gsi_pk = :pk AND gsi_sk >= :min",
		"",
		"pk,status,gsi_sk",
		nil,
		map[string]any{
			":pk":  map[string]any{"S": "type#COMPLETE"},
			":min": map[string]any{"N": "1"},
		},
		10,
		nil,
		true,
	)
	if err != nil {
		t.Fatalf("query gsi: %v", err)
	}
	if len(indexOut.Items) != 1 {
		t.Fatalf("gsi query len = %d, want 1", len(indexOut.Items))
	}

	scanOut, err := store.Scan(
		"orders",
		"lsi-status",
		"status = :status",
		"pk,status",
		nil,
		map[string]any{":status": map[string]any{"S": "COMPLETE"}},
		10,
		nil,
	)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(scanOut.Items) != 2 {
		t.Fatalf("scan len = %d, want 2", len(scanOut.Items))
	}

	deleteOut, err := store.DeleteItem(
		"orders",
		map[string]any{"pk": map[string]any{"S": "acct#1"}, "sk": map[string]any{"S": "order#2"}},
		"attribute_exists(pk)",
		nil,
		nil,
		"ALL_OLD",
	)
	if err != nil {
		t.Fatalf("delete item: %v", err)
	}
	if deleteOut.Attributes["pk"] == nil {
		t.Fatalf("deleted old attributes = %#v", deleteOut.Attributes)
	}

	streams, lastStream, err := store.ListStreams("orders", 10, "")
	if err != nil {
		t.Fatalf("list streams: %v", err)
	}
	if len(streams) != 1 || lastStream != "" {
		t.Fatalf("streams=%v last=%q", streams, lastStream)
	}

	describeOut, err := store.DescribeStream(table.LatestStreamArn, 10, "")
	if err != nil {
		t.Fatalf("describe stream: %v", err)
	}
	desc := describeOut["StreamDescription"].(map[string]any)
	if desc["StreamArn"] != table.LatestStreamArn {
		t.Fatalf("stream arn = %v", desc["StreamArn"])
	}

	iter, err := store.GetShardIterator(table.LatestStreamArn, streamShardID, "TRIM_HORIZON", "")
	if err != nil {
		t.Fatalf("get shard iterator: %v", err)
	}
	records, nextIter, err := store.GetRecords(iter, 10)
	if err != nil {
		t.Fatalf("get records: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("stream records len = %d, want >= 3", len(records))
	}
	if nextIter == "" {
		t.Fatal("expected next iterator")
	}

	deleted, err := store.DeleteTable("orders")
	if err != nil {
		t.Fatalf("delete table: %v", err)
	}
	if deleted.TableStatus != types.DynamoDBTableStatusDeleting {
		t.Fatalf("deleted table status = %q, want %q", deleted.TableStatus, types.DynamoDBTableStatusDeleting)
	}

	names, last, err = store.ListTables(10, "")
	if err != nil {
		t.Fatalf("list tables after delete: %v", err)
	}
	if len(names) != 0 || last != "" {
		t.Fatalf("list tables after delete = %v last=%q", names, last)
	}

	if _, err := store.DescribeTable("orders"); err == nil {
		t.Fatal("expected describe table to fail after delete")
	}
}

func TestStoreMutationsPersistWithoutDeadlock(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	if _, err := store.CreateTable(testTableDefinition()); err != nil {
		t.Fatalf("create table: %v", err)
	}

	run := func(name string, fn func() error) {
		t.Helper()

		done := make(chan error, 1)
		go func() {
			done <- fn()
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s timed out; likely deadlocked during persistence", name)
		}
	}

	run("put item", func() error {
		_, err := store.PutItem("orders", testItem("acct#1", "order#1", "PENDING", 1, "1"), "", nil, nil, "NONE")
		return err
	})

	run("update item", func() error {
		_, err := store.UpdateItem(
			"orders",
			map[string]any{"pk": map[string]any{"S": "acct#1"}, "sk": map[string]any{"S": "order#1"}},
			"SET status = :next",
			"",
			nil,
			map[string]any{":next": map[string]any{"S": "COMPLETE"}},
			"NONE",
		)
		return err
	})

	run("delete item", func() error {
		_, err := store.DeleteItem(
			"orders",
			map[string]any{"pk": map[string]any{"S": "acct#1"}, "sk": map[string]any{"S": "order#1"}},
			"",
			nil,
			nil,
			"NONE",
		)
		return err
	})

	store.Flush()
	if _, err := os.Stat(cfg.DynamoDBStatePath()); err != nil {
		t.Fatalf("expected persisted state file: %v", err)
	}
}

func TestStoreSupportsTableARNIdentifiers(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	table, err := store.CreateTable(testTableDefinition())
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	item := testItem("acct#1", "order#1", "PENDING", 1, "1")
	if _, err := store.PutItem(table.TableArn, item, "", nil, nil, "NONE"); err != nil {
		t.Fatalf("put item by arn: %v", err)
	}

	got, err := store.GetItem(
		table.TableArn,
		map[string]any{"pk": map[string]any{"S": "acct#1"}, "sk": map[string]any{"S": "order#1"}},
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("get item by arn: %v", err)
	}
	if got.Item["status"] == nil {
		t.Fatalf("get item by arn = %#v", got.Item)
	}

	streams, _, err := store.ListStreams(table.TableArn, 10, "")
	if err != nil {
		t.Fatalf("list streams by arn: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("streams by arn len = %d, want 1", len(streams))
	}

	if _, err := store.DescribeTable(table.TableArn); err != nil {
		t.Fatalf("describe table by arn: %v", err)
	}
}

func TestStoreUpdateTableStreamSpecification(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	tableDef := testTableDefinition()
	tableDef.StreamSpecification = nil

	table, err := store.CreateTable(tableDef)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	if table.LatestStreamArn != "" {
		t.Fatalf("expected no stream arn on create, got %q", table.LatestStreamArn)
	}

	updated, err := store.UpdateTableDefinition(table.TableName, &types.DynamoDBStreamSpecification{
		StreamEnabled:  true,
		StreamViewType: "NEW_AND_OLD_IMAGES",
	})
	if err != nil {
		t.Fatalf("enable stream: %v", err)
	}
	if updated.StreamSpecification == nil || !updated.StreamSpecification.StreamEnabled {
		t.Fatalf("stream spec after enable = %#v", updated.StreamSpecification)
	}
	if updated.LatestStreamArn == "" {
		t.Fatal("expected stream arn after enable")
	}

	streams, _, err := store.ListStreams(table.TableName, 10, "")
	if err != nil {
		t.Fatalf("list streams after enable: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("streams after enable len = %d, want 1", len(streams))
	}

	updated, err = store.UpdateTableDefinition(table.TableName, &types.DynamoDBStreamSpecification{
		StreamEnabled: false,
	})
	if err != nil {
		t.Fatalf("disable stream: %v", err)
	}
	if updated.StreamSpecification == nil || updated.StreamSpecification.StreamEnabled {
		t.Fatalf("stream spec after disable = %#v", updated.StreamSpecification)
	}
	if updated.LatestStreamArn != "" {
		t.Fatalf("expected empty stream arn after disable, got %q", updated.LatestStreamArn)
	}
}

func TestStoreTagResourceLifecycle(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	table, err := store.CreateTable(testTableDefinition())
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	if err := store.TagResource(table.TableArn, []types.DynamoDBTag{
		{Key: "component", Value: "source-table"},
		{Key: "flow", Value: "ddb-stream"},
	}); err != nil {
		t.Fatalf("tag resource: %v", err)
	}

	if err := store.TagResource(table.TableArn, []types.DynamoDBTag{
		{Key: "flow", Value: "ddb-stream-updated"},
		{Key: "stack", Value: "terraform"},
	}); err != nil {
		t.Fatalf("retag resource: %v", err)
	}

	tags, err := store.ListTagsOfResource(table.TableArn)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("tag count = %d, want 3", len(tags))
	}
	if tags[1].Key != "flow" || tags[1].Value != "ddb-stream-updated" {
		t.Fatalf("updated flow tag = %#v", tags[1])
	}

	if err := store.UntagResource(table.TableArn, []string{"component"}); err != nil {
		t.Fatalf("untag resource: %v", err)
	}

	tags, err = store.ListTagsOfResource(table.TableArn)
	if err != nil {
		t.Fatalf("list tags after untag: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("tag count after untag = %d, want 2", len(tags))
	}
	for _, tag := range tags {
		if tag.Key == "component" {
			t.Fatalf("component tag still present: %#v", tags)
		}
	}
}

func TestStoreTableFeatureCompatibilityAPIs(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	table, err := store.CreateTable(testTableDefinition())
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	ttl, err := store.DescribeTimeToLive(table.TableName)
	if err != nil {
		t.Fatalf("describe ttl: %v", err)
	}
	if got := ttl["TimeToLiveDescription"].(map[string]any)["TimeToLiveStatus"]; got != "DISABLED" {
		t.Fatalf("ttl status = %v, want DISABLED", got)
	}

	if _, err := store.UpdateTimeToLive(table.TableName, "expires_at", true); err != nil {
		t.Fatalf("update ttl: %v", err)
	}
	ttl, err = store.DescribeTimeToLive(table.TableArn)
	if err != nil {
		t.Fatalf("describe ttl by arn: %v", err)
	}
	ttlDesc := ttl["TimeToLiveDescription"].(map[string]any)
	if ttlDesc["TimeToLiveStatus"] != "ENABLED" || ttlDesc["AttributeName"] != "expires_at" {
		t.Fatalf("ttl description = %#v", ttlDesc)
	}

	backups, err := store.UpdateContinuousBackups(table.TableName, true)
	if err != nil {
		t.Fatalf("update continuous backups: %v", err)
	}
	pitrDesc := backups["ContinuousBackupsDescription"].(map[string]any)["PointInTimeRecoveryDescription"].(map[string]any)
	if pitrDesc["PointInTimeRecoveryStatus"] != "ENABLED" {
		t.Fatalf("pitr description = %#v", pitrDesc)
	}

	insights, err := store.UpdateContributorInsights(table.TableName, "", "ENABLE")
	if err != nil {
		t.Fatalf("update contributor insights: %v", err)
	}
	if insights["ContributorInsightsStatus"] != "ENABLED" {
		t.Fatalf("contributor insights = %#v", insights)
	}

	destinations, err := store.DescribeKinesisStreamingDestination(table.TableName)
	if err != nil {
		t.Fatalf("describe kinesis destinations: %v", err)
	}
	if len(destinations["KinesisDataStreamDestinations"].([]map[string]any)) != 0 {
		t.Fatalf("expected empty destinations, got %#v", destinations)
	}

	if _, err := store.EnableKinesisStreamingDestination(table.TableName, "arn:aws:kinesis:us-east-1:000000000000:stream/orders"); err != nil {
		t.Fatalf("enable kinesis destination: %v", err)
	}
	destinations, err = store.DescribeKinesisStreamingDestination(table.TableArn)
	if err != nil {
		t.Fatalf("describe kinesis destinations by arn: %v", err)
	}
	if len(destinations["KinesisDataStreamDestinations"].([]map[string]any)) != 1 {
		t.Fatalf("expected one destination, got %#v", destinations)
	}
}
