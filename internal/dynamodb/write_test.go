package dynamodb

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func newWriteTestStore(t *testing.T) *Store {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	store := NewStore(cfg)
	if _, err := store.CreateTable(testTableDefinition()); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	return store
}

func requireServiceCode(t *testing.T, err error, code string) *ServiceError {
	t.Helper()
	var se *ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("want ServiceError %s, got %v", code, err)
	}
	if se.Code != code {
		t.Fatalf("want code %s, got %s (%s)", code, se.Code, se.Message)
	}
	return se
}

func writeTestKey(pk, sk string) map[string]any {
	return map[string]any{"pk": map[string]any{"S": pk}, "sk": map[string]any{"S": sk}}
}

func mustPut(t *testing.T, store *Store, item map[string]any) {
	t.Helper()
	if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: item}); err != nil {
		t.Fatalf("PutItem: %v", err)
	}
}

func allStreamRecords(t *testing.T, store *Store) []*types.StreamRecord {
	t.Helper()
	desc, err := store.DescribeTable("orders")
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	records, _, err := store.StreamBatch(desc.LatestStreamArn, "", 1000)
	if err != nil {
		t.Fatalf("StreamBatch: %v", err)
	}
	return records
}

func countEvents(records []*types.StreamRecord) map[string]int {
	out := map[string]int{}
	for _, r := range records {
		out[r.EventName]++
	}
	return out
}

func TestCanonicalNumberTable(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1.0", "1"},
		{"00012.500", "12.5"},
		{"1e2", "100"},
		{"-0.0", "0"},
		{"1.5e-3", "0.0015"},
		{"0", "0"},
	}
	for _, tc := range cases {
		got, err := canonicalNumber(tc.in)
		if err != nil {
			t.Errorf("canonicalNumber(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("canonicalNumber(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := canonicalNumber("abc"); err == nil {
		t.Errorf("canonicalNumber(\"abc\") want error")
	}
}

func TestPutItemCanonicalisesNumbers(t *testing.T) {
	store := newWriteTestStore(t)
	item := writeTestKey("num", "1")
	item["count"] = map[string]any{"N": "1.0"}
	item["gsi_pk"] = map[string]any{"S": "x"}
	item["gsi_sk"] = map[string]any{"N": "2.50"}
	mustPut(t, store, item)

	got, err := store.GetItem("orders", writeTestKey("num", "1"), "", nil)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got == nil {
		t.Fatal("item missing after put")
	}
	if n := got["count"].(map[string]any)["N"]; n != "1" {
		t.Errorf("count N = %v, want 1", n)
	}
	if n := got["gsi_sk"].(map[string]any)["N"]; n != "2.5" {
		t.Errorf("gsi_sk N = %v, want 2.5", n)
	}
}

func TestPutItemKeyTypeMismatchIsValidation(t *testing.T) {
	store := newWriteTestStore(t)
	item := writeTestKey("a", "b")
	item["pk"] = map[string]any{"N": "1"}
	_, err := store.PutItem(&WriteInput{TableName: "orders", Item: item})
	se := requireServiceCode(t, err, "ValidationException")
	if !strings.Contains(se.Message, "Type mismatch for key pk") {
		t.Errorf("message %q lacks key type mismatch", se.Message)
	}
}

func TestPutItemGSIKeyTypeMismatchNamesIndex(t *testing.T) {
	store := newWriteTestStore(t)
	item := writeTestKey("a", "b")
	item["gsi_pk"] = map[string]any{"S": "t"}
	item["gsi_sk"] = map[string]any{"S": "not-a-number"}
	_, err := store.PutItem(&WriteInput{TableName: "orders", Item: item})
	se := requireServiceCode(t, err, "ValidationException")
	if !strings.Contains(se.Message, "IndexName: gsi-type") {
		t.Errorf("message %q lacks IndexName: gsi-type", se.Message)
	}
}

func TestPutItemEmptyKeyStringIsValidation(t *testing.T) {
	store := newWriteTestStore(t)
	_, err := store.PutItem(&WriteInput{TableName: "orders", Item: writeTestKey("", "b")})
	requireServiceCode(t, err, "ValidationException")
}

func TestGetItemKeyShapeErrors(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, writeTestKey("a", "b"))

	missingSK := map[string]any{"pk": map[string]any{"S": "a"}}
	_, err := store.GetItem("orders", missingSK, "", nil)
	requireServiceCode(t, err, "ValidationException")

	extra := writeTestKey("a", "b")
	extra["status"] = map[string]any{"S": "x"}
	_, err = store.GetItem("orders", extra, "", nil)
	requireServiceCode(t, err, "ValidationException")

	_, err = store.DeleteItem(&WriteInput{TableName: "orders", Key: missingSK})
	requireServiceCode(t, err, "ValidationException")
	_, err = store.DeleteItem(&WriteInput{TableName: "orders", Key: extra})
	requireServiceCode(t, err, "ValidationException")
}

func TestBatchWriteItemApply(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, writeTestKey("del", "1"))
	reqs := map[string][]BatchWriteRequest{
		"orders": {
			{PutRequest: &struct {
				Item map[string]any `json:"Item"`
			}{Item: writeTestKey("bw", "1")}},
			{PutRequest: &struct {
				Item map[string]any `json:"Item"`
			}{Item: writeTestKey("bw", "2")}},
			{DeleteRequest: &struct {
				Key map[string]any `json:"Key"`
			}{Key: writeTestKey("del", "1")}},
		},
	}
	if err := store.BatchWriteItem(reqs); err != nil {
		t.Fatalf("BatchWriteItem: %v", err)
	}
	for _, sk := range []string{"1", "2"} {
		got, _ := store.GetItem("orders", writeTestKey("bw", sk), "", nil)
		if got == nil {
			t.Errorf("bw/%s missing after batch", sk)
		}
	}
	if got, _ := store.GetItem("orders", writeTestKey("del", "1"), "", nil); got != nil {
		t.Errorf("del/1 still present after batch delete")
	}
}

func TestBatchWriteItemDuplicateKeyRejected(t *testing.T) {
	store := newWriteTestStore(t)
	put := func(sk string) BatchWriteRequest {
		return BatchWriteRequest{PutRequest: &struct {
			Item map[string]any `json:"Item"`
		}{Item: writeTestKey("dup", sk)}}
	}
	err := store.BatchWriteItem(map[string][]BatchWriteRequest{"orders": {put("1"), put("1")}})
	requireServiceCode(t, err, "ValidationException")
	if got, _ := store.GetItem("orders", writeTestKey("dup", "1"), "", nil); got != nil {
		t.Errorf("batch with duplicate key partially applied")
	}
}

func TestBatchWriteItemLimitAndEmpty(t *testing.T) {
	store := newWriteTestStore(t)
	var many []BatchWriteRequest
	for i := 0; i < 26; i++ {
		many = append(many, BatchWriteRequest{PutRequest: &struct {
			Item map[string]any `json:"Item"`
		}{Item: writeTestKey("many", fmt.Sprint(i))}})
	}
	err := store.BatchWriteItem(map[string][]BatchWriteRequest{"orders": many})
	requireServiceCode(t, err, "ValidationException")

	err = store.BatchWriteItem(map[string][]BatchWriteRequest{})
	requireServiceCode(t, err, "ValidationException")
}

func TestBatchGetItemExistingMissingDuplicatesProjection(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("g", "1", "open", 1, "1"))
	mustPut(t, store, testItem("g", "2", "closed", 2, "2"))

	out, err := store.BatchGetItem(map[string]KeysAndAttributes{
		"orders": {
			Keys: []map[string]any{writeTestKey("g", "1"), writeTestKey("g", "missing")},
		},
	})
	if err != nil {
		t.Fatalf("BatchGetItem: %v", err)
	}
	if n := len(out["orders"]); n != 1 {
		t.Fatalf("want 1 existing item, got %d", n)
	}

	_, err = store.BatchGetItem(map[string]KeysAndAttributes{
		"orders": {Keys: []map[string]any{writeTestKey("g", "1"), writeTestKey("g", "1")}},
	})
	requireServiceCode(t, err, "ValidationException")

	out, err = store.BatchGetItem(map[string]KeysAndAttributes{
		"orders": {
			Keys:                     []map[string]any{writeTestKey("g", "1")},
			ProjectionExpression:     "pk, sk, #s",
			ExpressionAttributeNames: map[string]string{"#s": "status"},
		},
	})
	if err != nil {
		t.Fatalf("BatchGetItem projection: %v", err)
	}
	if len(out["orders"]) != 1 {
		t.Fatalf("want 1 item with projection, got %d", len(out["orders"]))
	}
	projected := out["orders"][0]
	if _, ok := projected["status"]; !ok {
		t.Errorf("projection dropped status")
	}
	if _, ok := projected["tags"]; ok {
		t.Errorf("projection kept tags")
	}
	if _, ok := projected["gsi_sk"]; ok {
		t.Errorf("projection kept gsi_sk")
	}
}

func TestTransactWriteItemsCancelReasons(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("tx", "exists", "open", 1, "1"))

	err := store.TransactWriteItems([]TransactWriteItem{
		{Put: &WriteInput{TableName: "orders", Item: testItem("tx", "new", "open", 1, "1")}},
		{ConditionCheck: &WriteInput{
			TableName:                 "orders",
			Key:                       writeTestKey("tx", "exists"),
			ConditionExpression:       "#s = :wrong",
			ExpressionAttributeNames:  map[string]string{"#s": "status"},
			ExpressionAttributeValues: map[string]any{":wrong": map[string]any{"S": "nope"}},
		}},
	})
	se := requireServiceCode(t, err, "TransactionCanceledException")
	if len(se.CancellationReasons) != 2 {
		t.Fatalf("want 2 cancellation reasons, got %d", len(se.CancellationReasons))
	}
	if se.CancellationReasons[0].Code != "None" {
		t.Errorf("reason[0] = %q, want None", se.CancellationReasons[0].Code)
	}
	if se.CancellationReasons[1].Code != "ConditionalCheckFailed" {
		t.Errorf("reason[1] = %q, want ConditionalCheckFailed", se.CancellationReasons[1].Code)
	}
	if got, _ := store.GetItem("orders", writeTestKey("tx", "new"), "", nil); got != nil {
		t.Errorf("cancelled transaction applied its put")
	}
}

func TestTransactWriteItemsSuccessMultiAction(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("tx", "upd", "open", 1, "1"))
	mustPut(t, store, testItem("tx", "del", "open", 1, "2"))

	err := store.TransactWriteItems([]TransactWriteItem{
		{Put: &WriteInput{TableName: "orders", Item: testItem("tx", "put", "open", 1, "3")}},
		{Update: &WriteInput{
			TableName:                 "orders",
			Key:                       writeTestKey("tx", "upd"),
			UpdateExpression:          "SET #s = :s",
			ExpressionAttributeNames:  map[string]string{"#s": "status"},
			ExpressionAttributeValues: map[string]any{":s": map[string]any{"S": "done"}},
		}},
		{Delete: &WriteInput{TableName: "orders", Key: writeTestKey("tx", "del")}},
	})
	if err != nil {
		t.Fatalf("TransactWriteItems: %v", err)
	}
	if got, _ := store.GetItem("orders", writeTestKey("tx", "put"), "", nil); got == nil {
		t.Error("put missing")
	}
	got, _ := store.GetItem("orders", writeTestKey("tx", "upd"), "", nil)
	if got == nil || got["status"].(map[string]any)["S"] != "done" {
		t.Errorf("update not applied: %v", got)
	}
	if got, _ := store.GetItem("orders", writeTestKey("tx", "del"), "", nil); got != nil {
		t.Error("delete not applied")
	}
}

func TestTransactWriteItemsSameKeyRejected(t *testing.T) {
	store := newWriteTestStore(t)
	err := store.TransactWriteItems([]TransactWriteItem{
		{Put: &WriteInput{TableName: "orders", Item: testItem("same", "1", "open", 1, "1")}},
		{Update: &WriteInput{
			TableName:                 "orders",
			Key:                       writeTestKey("same", "1"),
			UpdateExpression:          "SET #s = :s",
			ExpressionAttributeNames:  map[string]string{"#s": "status"},
			ExpressionAttributeValues: map[string]any{":s": map[string]any{"S": "x"}},
		}},
	})
	requireServiceCode(t, err, "ValidationException")
	if got, _ := store.GetItem("orders", writeTestKey("same", "1"), "", nil); got != nil {
		t.Error("rejected transaction applied its put")
	}
}

func TestTransactWriteItemsPutAndDeleteSameKeyRejected(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("pd", "1", "open", 1, "1"))
	err := store.TransactWriteItems([]TransactWriteItem{
		{Put: &WriteInput{TableName: "orders", Item: testItem("pd", "1", "closed", 1, "1")}},
		{Delete: &WriteInput{TableName: "orders", Key: writeTestKey("pd", "1")}},
	})
	requireServiceCode(t, err, "ValidationException")
	if got, _ := store.GetItem("orders", writeTestKey("pd", "1"), "", nil); got == nil {
		t.Error("rejected transaction deleted the item")
	}
}

func TestTransactWriteItemsAllOldOnConditionFailure(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("old", "1", "open", 1, "1"))
	err := store.TransactWriteItems([]TransactWriteItem{
		{Put: &WriteInput{
			TableName:                           "orders",
			Item:                                testItem("old", "1", "closed", 1, "1"),
			ConditionExpression:                 "attribute_not_exists(pk)",
			ReturnValuesOnConditionCheckFailure: "ALL_OLD",
		}},
	})
	se := requireServiceCode(t, err, "TransactionCanceledException")
	if len(se.CancellationReasons) != 1 {
		t.Fatalf("want 1 reason, got %d", len(se.CancellationReasons))
	}
	reason := se.CancellationReasons[0]
	if reason.Code != "ConditionalCheckFailed" {
		t.Errorf("reason code = %q", reason.Code)
	}
	if reason.Item == nil || reason.Item["status"].(map[string]any)["S"] != "open" {
		t.Errorf("ALL_OLD item missing or wrong: %v", reason.Item)
	}
}

func TestTransactGetItemsOrderingWithNil(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("tg", "a", "open", 1, "1"))
	mustPut(t, store, testItem("tg", "c", "closed", 2, "2"))

	get := func(sk string) TransactGetItem {
		return TransactGetItem{Get: &struct {
			TableName                string            `json:"TableName"`
			Key                      map[string]any    `json:"Key"`
			ProjectionExpression     string            `json:"ProjectionExpression"`
			ExpressionAttributeNames map[string]string `json:"ExpressionAttributeNames"`
		}{TableName: "orders", Key: writeTestKey("tg", sk)}}
	}
	out, err := store.TransactGetItems([]TransactGetItem{get("c"), get("missing"), get("a")})
	if err != nil {
		t.Fatalf("TransactGetItems: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("want 3 results, got %d", len(out))
	}
	if out[0] == nil || out[0]["sk"].(map[string]any)["S"] != "c" {
		t.Errorf("out[0] = %v, want sk c", out[0])
	}
	if out[1] != nil {
		t.Errorf("out[1] = %v, want nil for missing item", out[1])
	}
	if out[2] == nil || out[2]["sk"].(map[string]any)["S"] != "a" {
		t.Errorf("out[2] = %v, want sk a", out[2])
	}
}

func TestStreamIdenticalPutEmitsOnlyInsert(t *testing.T) {
	store := newWriteTestStore(t)
	item := testItem("s", "1", "open", 1, "1")
	mustPut(t, store, item)
	mustPut(t, store, testItem("s", "1", "open", 1, "1"))
	counts := countEvents(allStreamRecords(t, store))
	if counts["INSERT"] != 1 || counts["MODIFY"] != 0 {
		t.Errorf("events = %v, want one INSERT and no MODIFY", counts)
	}
}

func TestStreamDeleteMissingEmitsNothing(t *testing.T) {
	store := newWriteTestStore(t)
	if _, err := store.DeleteItem(&WriteInput{TableName: "orders", Key: writeTestKey("none", "1")}); err != nil {
		t.Fatalf("DeleteItem missing: %v", err)
	}
	if n := len(allStreamRecords(t, store)); n != 0 {
		t.Errorf("got %d stream records, want 0", n)
	}
}

func TestStreamChangedUpdateEmitsModify(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("s", "2", "open", 1, "1"))
	_, err := store.UpdateItem(&WriteInput{
		TableName:                 "orders",
		Key:                       writeTestKey("s", "2"),
		UpdateExpression:          "SET #s = :v",
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]any{":v": map[string]any{"S": "closed"}},
	})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	counts := countEvents(allStreamRecords(t, store))
	if counts["MODIFY"] != 1 {
		t.Errorf("events = %v, want one MODIFY", counts)
	}
}

func TestUpdateItemChangingKeyAttributeRejected(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("k", "1", "open", 1, "1"))
	_, err := store.UpdateItem(&WriteInput{
		TableName:                 "orders",
		Key:                       writeTestKey("k", "1"),
		UpdateExpression:          "SET pk = :v",
		ExpressionAttributeValues: map[string]any{":v": map[string]any{"S": "other"}},
	})
	se := requireServiceCode(t, err, "ValidationException")
	if !strings.Contains(se.Message, "part of the key") {
		t.Errorf("message %q lacks key immutability text", se.Message)
	}
}

func TestPutItemReturnValuesAllNewRejected(t *testing.T) {
	store := newWriteTestStore(t)
	_, err := store.PutItem(&WriteInput{
		TableName:    "orders",
		Item:         testItem("rv", "1", "open", 1, "1"),
		ReturnValues: "ALL_NEW",
	})
	requireServiceCode(t, err, "ValidationException")
}

func TestUpdateItemReturnValuesUpdatedNewOnlyChanged(t *testing.T) {
	store := newWriteTestStore(t)
	mustPut(t, store, testItem("rv", "2", "open", 1, "1"))
	out, err := store.UpdateItem(&WriteInput{
		TableName:                 "orders",
		Key:                       writeTestKey("rv", "2"),
		UpdateExpression:          "SET #s = :v",
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]any{":v": map[string]any{"S": "closed"}},
		ReturnValues:              "UPDATED_NEW",
	})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if _, ok := out["status"]; !ok {
		t.Errorf("UPDATED_NEW missing status: %v", out)
	}
	if _, ok := out["tags"]; ok {
		t.Errorf("UPDATED_NEW returned unchanged tags: %v", out)
	}
	if _, ok := out["pk"]; ok {
		t.Errorf("UPDATED_NEW returned unchanged key: %v", out)
	}
}

func TestScanLimitSelectCountAndSegments(t *testing.T) {
	store := newWriteTestStore(t)
	for i := 0; i < 20; i++ {
		mustPut(t, store, testItem(fmt.Sprintf("p%02d", i), "s", "open", i, "1"))
	}

	page, err := store.Scan(&ReadInput{TableName: "orders", Limit: 2})
	if err != nil {
		t.Fatalf("Scan limit: %v", err)
	}
	if page.Count != 2 || page.ScannedCount != 2 {
		t.Errorf("Count=%d ScannedCount=%d, want 2/2", page.Count, page.ScannedCount)
	}
	if page.LastEvaluatedKey == nil {
		t.Error("LastEvaluatedKey nil on truncated page")
	}

	counted, err := store.Scan(&ReadInput{TableName: "orders", Select: "COUNT"})
	if err != nil {
		t.Fatalf("Scan COUNT: %v", err)
	}
	if counted.Items != nil || counted.Count != 20 {
		t.Errorf("COUNT: items=%v count=%d, want nil/20", counted.Items, counted.Count)
	}

	seen := map[string]int{}
	total := 3
	for seg := 0; seg < total; seg++ {
		s := seg
		res, err := store.Scan(&ReadInput{TableName: "orders", Segment: &s, TotalSegments: total})
		if err != nil {
			t.Fatalf("Scan segment %d: %v", seg, err)
		}
		for _, item := range res.Items {
			seen[item["pk"].(map[string]any)["S"].(string)]++
		}
	}
	if len(seen) != 20 {
		t.Errorf("segment union has %d distinct items, want 20", len(seen))
	}
	for pk, n := range seen {
		if n != 1 {
			t.Errorf("item %s returned %d times across segments", pk, n)
		}
	}

	zero := 0
	_, err = store.Scan(&ReadInput{TableName: "orders", Segment: &zero})
	requireServiceCode(t, err, "ValidationException")
}

func TestQueryWithoutKeyConditionRejected(t *testing.T) {
	store := newWriteTestStore(t)
	_, err := store.Query(&ReadInput{TableName: "orders"})
	requireServiceCode(t, err, "ValidationException")
}

func TestScanPaginationSurvivesDeletedStartItem(t *testing.T) {
	store := newWriteTestStore(t)
	for _, sk := range []string{"a", "b", "c", "d", "e"} {
		mustPut(t, store, testItem("pg", sk, "open", 1, "1"))
	}
	page, err := store.Scan(&ReadInput{TableName: "orders", Limit: 2})
	if err != nil {
		t.Fatalf("Scan page 1: %v", err)
	}
	if page.LastEvaluatedKey == nil {
		t.Fatal("no LastEvaluatedKey on page 1")
	}
	if _, err := store.DeleteItem(&WriteInput{TableName: "orders", Key: page.LastEvaluatedKey}); err != nil {
		t.Fatalf("DeleteItem start key: %v", err)
	}
	next, err := store.Scan(&ReadInput{TableName: "orders", Limit: 2, ExclusiveStartKey: page.LastEvaluatedKey})
	if err != nil {
		t.Fatalf("Scan page 2: %v", err)
	}
	if next.Count == 0 {
		t.Fatal("page 2 empty after deleting start item")
	}
	if sk := next.Items[0]["sk"].(map[string]any)["S"]; sk != "c" {
		t.Errorf("page 2 first sk = %v, want c", sk)
	}
}

func TestUpdateTableCreateGSIBecomesActive(t *testing.T) {
	store := newWriteTestStore(t)
	out, err := store.UpdateTable(&UpdateTableInput{
		TableName: "orders",
		AttributeDefinitions: []types.DynamoDBAttributeDefinition{
			{AttributeName: "status", AttributeType: "S"},
		},
		GlobalSecondaryIndexUpdates: []GlobalSecondaryIndexUpdate{{
			Create: &types.DynamoDBGlobalSecondaryIndex{
				IndexName:  "gsi-status",
				KeySchema:  []types.DynamoDBKeySchemaElement{{AttributeName: "status", KeyType: "HASH"}},
				Projection: types.DynamoDBProjection{ProjectionType: "ALL"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("UpdateTable create GSI: %v", err)
	}
	idx := findGSI(out, "gsi-status")
	if idx == nil {
		t.Fatal("gsi-status not present after create")
	}
	if idx.IndexStatus != "ACTIVE" {
		t.Errorf("IndexStatus = %q, want ACTIVE", idx.IndexStatus)
	}
	// Terraform's provider waits on the index's own warm throughput too.
	if idx.WarmThroughput == nil || idx.WarmThroughput.Status != "ACTIVE" {
		t.Errorf("WarmThroughput = %+v, want ACTIVE", idx.WarmThroughput)
	}
}

func TestUpdateTableDeleteGSIRemovesAttributeDefinition(t *testing.T) {
	store := newWriteTestStore(t)
	out, err := store.UpdateTable(&UpdateTableInput{
		TableName: "orders",
		GlobalSecondaryIndexUpdates: []GlobalSecondaryIndexUpdate{{
			Delete: &struct {
				IndexName string `json:"IndexName"`
			}{IndexName: "gsi-type"},
		}},
	})
	if err != nil {
		t.Fatalf("UpdateTable delete GSI: %v", err)
	}
	if findGSI(out, "gsi-type") != nil {
		t.Error("gsi-type still present after delete")
	}
	for _, def := range out.AttributeDefinitions {
		if def.AttributeName == "gsi_sk" || def.AttributeName == "gsi_pk" {
			t.Errorf("attribute definition %s kept after GSI delete", def.AttributeName)
		}
	}
}

func TestUpdateTableDeleteUnknownIndexErrors(t *testing.T) {
	store := newWriteTestStore(t)
	_, err := store.UpdateTable(&UpdateTableInput{
		TableName: "orders",
		GlobalSecondaryIndexUpdates: []GlobalSecondaryIndexUpdate{{
			Delete: &struct {
				IndexName string `json:"IndexName"`
			}{IndexName: "nope"},
		}},
	})
	if err == nil {
		t.Fatal("want error deleting unknown index")
	}
}

func TestDeletionProtectionBlocksDeleteTable(t *testing.T) {
	store := newWriteTestStore(t)
	on := true
	if _, err := store.UpdateTable(&UpdateTableInput{TableName: "orders", DeletionProtectionEnabled: &on}); err != nil {
		t.Fatalf("UpdateTable protection: %v", err)
	}
	_, err := store.DeleteTable("orders")
	requireServiceCode(t, err, "ValidationException")
	if _, err := store.DescribeTable("orders"); err != nil {
		t.Errorf("table gone despite deletion protection: %v", err)
	}
}

func TestCreateTableDuplicateResourceInUse(t *testing.T) {
	store := newWriteTestStore(t)
	_, err := store.CreateTable(testTableDefinition())
	requireServiceCode(t, err, "ResourceInUseException")
}

func TestCreateTableUnusedAttributeDefinitionRejected(t *testing.T) {
	store := newWriteTestStore(t)
	def := testTableDefinition()
	def.TableName = "unused"
	def.AttributeDefinitions = append(def.AttributeDefinitions, types.DynamoDBAttributeDefinition{AttributeName: "extra", AttributeType: "S"})
	_, err := store.CreateTable(def)
	requireServiceCode(t, err, "ValidationException")
}
