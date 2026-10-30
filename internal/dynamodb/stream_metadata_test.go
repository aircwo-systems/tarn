package dynamodb

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestDescribeStreamReturnsSchemaSnapshot(t *testing.T) {
	store, arn := newStreamWaitStore(t)
	out, err := store.DescribeStream(arn, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	schema := out["StreamDescription"].(map[string]any)["KeySchema"].([]types.DynamoDBKeySchemaElement)
	schema[0].AttributeName = "changed"

	table, err := store.DescribeTable("orders")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(table.KeySchema, testTableDefinition().KeySchema) {
		t.Fatalf("stream response changed the stored key schema: %+v", table.KeySchema)
	}
	if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: testItem("p", "a", "OPEN", 1, "1")}); err != nil {
		t.Fatalf("put after response mutation: %v", err)
	}
}

func TestLatestIteratorReadsOnlySubsequentWrites(t *testing.T) {
	for _, existing := range []int{0, 2} {
		t.Run(fmt.Sprintf("existing=%d", existing), func(t *testing.T) {
			store, arn := newStreamWaitStore(t)
			for i := range existing {
				if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: testItem("p", fmt.Sprint(i), "OPEN", i, "1")}); err != nil {
					t.Fatal(err)
				}
			}
			iterator, err := store.GetShardIterator(arn, streamShardID, "LATEST", "")
			if err != nil {
				t.Fatal(err)
			}
			records, next, err := store.GetRecords(iterator, 10)
			if err != nil || len(records) != 0 {
				t.Fatalf("LATEST replayed existing records: %v %v", records, err)
			}
			if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: testItem("p", "new", "OPEN", existing, "1")}); err != nil {
				t.Fatal(err)
			}
			records, _, err = store.GetRecords(next, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("subsequent write: %v %v", records, err)
			}
			want := strconv.Itoa(existing + 1)
			if records[0].Dynamodb.SequenceNumber != want {
				t.Fatalf("sequence=%v, want %s", records[0], want)
			}
		})
	}
}

func TestShardIteratorBoundariesAndValidation(t *testing.T) {
	store, arn := newStreamWaitStore(t)
	for i := range 2 {
		if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: testItem("p", fmt.Sprint(i), "OPEN", i, "1")}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		kind     string
		sequence string
		want     []string
	}{
		{kind: "TRIM_HORIZON", want: []string{"1", "2"}},
		{kind: "AT_SEQUENCE_NUMBER", sequence: "1", want: []string{"1", "2"}},
		{kind: "AT_SEQUENCE_NUMBER", sequence: "2", want: []string{"2"}},
		{kind: "AFTER_SEQUENCE_NUMBER", sequence: "1", want: []string{"2"}},
		{kind: "LATEST", want: []string{}},
	} {
		t.Run(tc.kind+tc.sequence, func(t *testing.T) {
			iterator, err := store.GetShardIterator(arn, streamShardID, tc.kind, tc.sequence)
			if err != nil {
				t.Fatal(err)
			}
			records, _, err := store.GetRecords(iterator, 10)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(records))
			for _, record := range records {
				got = append(got, record.Dynamodb.SequenceNumber)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sequences=%v, want %v", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		arn      string
		shard    string
		kind     string
		sequence string
		code     string
	}{
		{arn: arn, shard: "missing", kind: "LATEST", code: "ValidationException"},
		{arn: arn, shard: streamShardID, kind: "invalid", code: "ValidationException"},
		{arn: arn, shard: streamShardID, kind: "AT_SEQUENCE_NUMBER", sequence: "invalid", code: "ValidationException"},
		{arn: "missing", shard: streamShardID, kind: "LATEST", code: "ResourceNotFoundException"},
	} {
		_, err := store.GetShardIterator(tc.arn, tc.shard, tc.kind, tc.sequence)
		var svcErr *ServiceError
		if !errors.As(err, &svcErr) || svcErr.Code != tc.code {
			t.Fatalf("iterator error=%v, want %s", err, tc.code)
		}
	}
}

func TestStreamMetadataDuringWritesAndReconfiguration(t *testing.T) {
	store, arn := newStreamWaitStore(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		<-start
		for i := range 100 {
			if _, err := store.PutItem(&WriteInput{TableName: "orders", Item: testItem("p", fmt.Sprint(i), "OPEN", i, "1")}); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Go(func() {
		<-start
		for range 50 {
			for _, enabled := range []bool{false, true} {
				if _, err := store.UpdateTable(&UpdateTableInput{TableName: "orders", StreamSpecification: &types.DynamoDBStreamSpecification{StreamEnabled: enabled, StreamViewType: "NEW_IMAGE"}}); err != nil {
					t.Error(err)
					return
				}
			}
		}
	})
	wg.Go(func() {
		<-start
		for range 200 {
			if iterator, err := store.GetShardIterator(arn, streamShardID, "LATEST", ""); err != nil {
				var svcErr *ServiceError
				if !errors.As(err, &svcErr) || svcErr.Code != "ResourceNotFoundException" {
					t.Error(err)
					return
				}
			} else if _, _, err := decodeShardIterator(iterator); err != nil {
				t.Error(err)
				return
			}
			out, err := store.DescribeStream(arn, 100, "")
			if err != nil {
				var svcErr *ServiceError
				if !errors.As(err, &svcErr) || svcErr.Code != "ResourceNotFoundException" {
					t.Error(err)
					return
				}
				continue
			}
			desc := out["StreamDescription"].(map[string]any)
			if desc["StreamArn"] != arn || !reflect.DeepEqual(desc["KeySchema"], testTableDefinition().KeySchema) {
				t.Errorf("inconsistent stream description: %v", desc)
				return
			}
		}
	})
	close(start)
	wg.Wait()
	if _, err := store.UpdateTable(&UpdateTableInput{TableName: "orders", StreamSpecification: &types.DynamoDBStreamSpecification{StreamEnabled: false}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DescribeStream(arn, 100, ""); err == nil {
		t.Fatal("disabled stream still described")
	}
	if _, err := store.GetShardIterator(arn, streamShardID, "LATEST", ""); err == nil {
		t.Fatal("disabled stream still returned an iterator")
	}
}
