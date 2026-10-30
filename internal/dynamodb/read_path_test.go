package dynamodb

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func newReadTestStore(t testing.TB, projection string) *Store {
	t.Helper()
	cfg := config.Default()
	cfg.PersistenceEnabled = false
	store := NewStore(cfg)
	table := testTableDefinition()
	table.StreamSpecification = nil
	indexProjection := types.DynamoDBProjection{ProjectionType: projection}
	if projection == "INCLUDE" {
		indexProjection.NonKeyAttributes = []string{"status"}
	}
	table.LocalSecondaryIndexes[0].Projection = indexProjection
	table.GlobalSecondaryIndexes[0].Projection = indexProjection
	if _, err := store.CreateTable(table); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestIndexProjectionIncludesTableAndIndexKeys(t *testing.T) {
	for _, projection := range []string{"KEYS_ONLY", "INCLUDE", "ALL"} {
		for _, index := range []string{"lsi-status", "gsi-type"} {
			for _, operation := range []string{"Query", "Scan"} {
				t.Run(projection+"/"+index+"/"+operation, func(t *testing.T) {
					store := newReadTestStore(t, projection)
					item := testItem("p", "a", "OPEN", 1, "2")
					if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: item}); err != nil {
						t.Fatal(err)
					}
					// An item without the index keys must remain absent from the index.
					if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: map[string]any{"pk": map[string]any{"S": "p"}, "sk": map[string]any{"S": "sparse"}}}); err != nil {
						t.Fatal(err)
					}
					keyCondition := "pk = :key"
					value := "p"
					keys := []string{"pk", "sk", "lsi_sk"}
					if index == "gsi-type" {
						keyCondition, value = "gsi_pk = :key", "type#OPEN"
						keys = []string{"pk", "sk", "gsi_pk", "gsi_sk"}
					}
					read := func(documentProjection string) ([]map[string]any, map[string]any, error) {
						if operation == "Scan" {
							out, err := store.Scan(&ReadInput{TableName: "orders", IndexName: index, ProjectionExpression: documentProjection, Limit: 1})
							if err != nil {
								return nil, nil, err
							}
							return out.Items, out.LastEvaluatedKey, nil
						}
						out, err := store.Query(&ReadInput{TableName: "orders", IndexName: index, KeyConditionExpression: keyCondition, ProjectionExpression: documentProjection, ExpressionAttributeValues: map[string]any{":key": map[string]any{"S": value}}, Limit: 1})
						if err != nil {
							return nil, nil, err
						}
						return out.Items, out.LastEvaluatedKey, nil
					}
					items, cursor, err := read("")
					if err != nil || len(items) != 1 {
						t.Fatalf("read: %v %v", items, err)
					}
					want := cloneItem(item)
					if projection != "ALL" {
						want = make(map[string]any)
						for _, key := range keys {
							want[key] = item[key]
						}
						if projection == "INCLUDE" {
							want["status"] = item["status"]
						}
					}
					if !reflect.DeepEqual(items[0], want) {
						t.Fatalf("projection=%v, want %v", items[0], want)
					}
					// Query may retain its current continuation key at the end of a full
					// page. Projection must not strip table or index keys from that key.
					if cursor != nil {
						for _, key := range keys {
							if !reflect.DeepEqual(cursor[key], item[key]) {
								t.Fatalf("cursor lost key %s: %v", key, cursor)
							}
						}
					}
					items, _, err = read(strings.Join(keys, ","))
					if err != nil || len(items) != 1 {
						t.Fatalf("document projection: %v %v", items, err)
					}
					if len(items[0]) != len(keys) {
						t.Fatalf("document projection returned extra attributes: %v", items[0])
					}
					items[0]["pk"].(map[string]any)["S"] = "caller mutation"
					stored, err := store.GetItem("orders", map[string]any{"pk": item["pk"], "sk": item["sk"]}, "", nil)
					if err != nil || !reflect.DeepEqual(stored, item) {
						t.Fatalf("result mutation changed stored item: %v %v", stored, err)
					}
				})
			}
		}
	}
}

func TestQueryOrderingAndPagination(t *testing.T) {
	store := newReadTestStore(t, "ALL")
	for i, input := range []struct{ key, status, number string }{
		{"a", "OPEN", "2"},
		{"b", "CLOSED", "10"},
		{"c", "OPEN", "2.0"},
		{"d", "CLOSED", "-1"},
		{"e", "Z", "1.0000000000000000000000000000000000001"},
		{"f", "Z", "1"},
	} {
		item := testItem("p", input.key, input.status, i, input.number)
		item["gsi_pk"] = map[string]any{"S": "g"}
		if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: item}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: map[string]any{"pk": map[string]any{"S": "p"}, "sk": map[string]any{"S": "g"}}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		index        string
		keyCondition string
		value        string
		want         []string
	}{
		{index: "", keyCondition: "pk = :key", value: "p", want: []string{"a", "b", "c", "d", "e", "f", "g"}},
		{index: "lsi-status", keyCondition: "pk = :key", value: "p", want: []string{"b", "d", "a", "c", "e", "f"}},
		{index: "gsi-type", keyCondition: "gsi_pk = :key", value: "g", want: []string{"d", "f", "e", "a", "c", "b"}},
	} {
		for _, forward := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/forward=%t", tc.index, forward), func(t *testing.T) {
				want := slices.Clone(tc.want)
				if !forward {
					slices.Reverse(want)
				}
				var cursor map[string]any
				var got []string
				for page := 0; ; page++ {
					if page > len(want) {
						t.Fatal("pagination did not terminate")
					}
					out, err := store.Query(&ReadInput{TableName: "orders", IndexName: tc.index, KeyConditionExpression: tc.keyCondition, ExpressionAttributeValues: map[string]any{":key": map[string]any{"S": tc.value}}, Limit: 2, ExclusiveStartKey: cursor, ScanIndexForward: &[]bool{forward}[0]})
					if err != nil {
						t.Fatal(err)
					}
					for _, item := range out.Items {
						got = append(got, item["sk"].(map[string]any)["S"].(string))
					}
					cursor = out.LastEvaluatedKey
					if cursor == nil {
						break
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("order=%v, want %v", got, want)
				}
			})
		}
	}
}

func TestSortKeysOrderByUTF8Bytes(t *testing.T) {
	store := newReadTestStore(t, "ALL")
	newline := string(rune(10))
	for _, key := range []string{"A", "z", "é", newline, `"`} {
		if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: testItem("p", key, "OPEN", 1, "1")}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := store.Query(&ReadInput{TableName: "orders", KeyConditionExpression: "pk = :key", ExpressionAttributeValues: map[string]any{":key": map[string]any{"S": "p"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range out.Items {
		got = append(got, item["sk"].(map[string]any)["S"].(string))
	}
	// DynamoDB orders strings by their UTF-8 bytes.
	want := []string{newline, `"`, "A", "z", "é"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordering = %q, want %q", got, want)
	}
}

func BenchmarkDynamoDBReadPath(b *testing.B) {
	for _, size := range []int{1000, 10000} {
		for _, operation := range []string{"ScanPage", "QueryMiss", "QueryPage"} {
			b.Run(fmt.Sprintf("%s/%d", operation, size), func(b *testing.B) {
				store := newReadTestStore(b, "ALL")
				payload := strings.Repeat("x", 256)
				for i := range size {
					item := map[string]any{
						"pk":      map[string]any{"S": fmt.Sprintf("p%03d", i%100)},
						"sk":      map[string]any{"S": fmt.Sprintf("%08d", i)},
						"status":  map[string]any{"S": "OPEN"},
						"payload": map[string]any{"S": payload},
						"nested":  map[string]any{"M": map[string]any{"list": map[string]any{"L": []any{map[string]any{"S": "a"}, map[string]any{"N": "2"}}}}},
					}
					if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: item}); err != nil {
						b.Fatal(err)
					}
				}
				value := "absent"
				if operation == "QueryPage" {
					value = "p050"
				}
				values := map[string]any{":key": map[string]any{"S": value}}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if operation == "ScanPage" {
						if _, err := store.Scan(&ReadInput{TableName: "orders", Limit: 10}); err != nil {
							b.Fatal(err)
						}
					} else if _, err := store.Query(&ReadInput{TableName: "orders", KeyConditionExpression: "pk = :key", ExpressionAttributeValues: values, Limit: 10}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// A key condition that also names a non-key attribute must not stop at the
// first item that fails it: later items can still match.
func TestQueryKeyConditionMatchesAreNotAssumedContiguous(t *testing.T) {
	store := newReadTestStore(t, "ALL")
	for _, item := range []map[string]any{testItem("p", "a", "A", 1, "1"), testItem("p", "b", "B", 1, "1"), testItem("p", "c", "A", 1, "1")} {
		if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: item}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := store.Query(&ReadInput{
		TableName:                 "orders",
		KeyConditionExpression:    "pk = :p AND #st = :a",
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]any{":p": map[string]any{"S": "p"}, ":a": map[string]any{"S": "A"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Fatalf("matches = %d, want 2", out.Count)
	}
}
