package dynamodb

import (
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func sseTable(name string, spec *types.DynamoDBSSESpecification) *types.DynamoDBTable {
	return &types.DynamoDBTable{
		TableName:            name,
		AttributeDefinitions: []types.DynamoDBAttributeDefinition{{AttributeName: "pk", AttributeType: "S"}},
		KeySchema:            []types.DynamoDBKeySchemaElement{{AttributeName: "pk", KeyType: "HASH"}},
		SSESpecification:     spec,
	}
}

func enabled(v bool) *bool { return &v }

func TestCreateTableRecordsKMSEncryption(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	store := NewStore(cfg)
	prefix := "arn:aws:kms:" + cfg.Region + ":" + cfg.AccountID + ":"
	keyARN := prefix + "key/1234abcd-12ab-34cd-56ef-1234567890ab"

	for _, tc := range []struct {
		name string
		spec *types.DynamoDBSSESpecification
		want string // KMSMasterKeyArn; empty means no SSEDescription
	}{
		{"owned-default", nil, ""},
		{"owned-disabled", &types.DynamoDBSSESpecification{Enabled: enabled(false)}, ""},
		{"managed", &types.DynamoDBSSESpecification{Enabled: enabled(true)}, prefix + "alias/aws/dynamodb"},
		{"by-id", &types.DynamoDBSSESpecification{Enabled: enabled(true), SSEType: "KMS", KMSMasterKeyId: "1234abcd-12ab-34cd-56ef-1234567890ab"}, keyARN},
		{"by-alias", &types.DynamoDBSSESpecification{Enabled: enabled(true), KMSMasterKeyId: "alias/app"}, prefix + "alias/app"},
		{"by-arn", &types.DynamoDBSSESpecification{Enabled: enabled(true), KMSMasterKeyId: keyARN}, keyARN},
	} {
		if _, err := store.CreateTable(sseTable(tc.name, tc.spec)); err != nil {
			t.Fatalf("%s: CreateTable: %v", tc.name, err)
		}
		got, err := store.DescribeTable(tc.name)
		if err != nil {
			t.Fatalf("%s: DescribeTable: %v", tc.name, err)
		}
		switch {
		case tc.want == "" && got.SSEDescription != nil:
			t.Fatalf("%s: SSEDescription = %+v, want none", tc.name, got.SSEDescription)
		case tc.want != "" && (got.SSEDescription == nil || got.SSEDescription.Status != "ENABLED" || got.SSEDescription.SSEType != "KMS" || got.SSEDescription.KMSMasterKeyArn != tc.want):
			t.Fatalf("%s: SSEDescription = %+v, want KMS %s", tc.name, got.SSEDescription, tc.want)
		}
	}

	for _, spec := range []*types.DynamoDBSSESpecification{
		{Enabled: enabled(true), SSEType: "AES256"},
		{Enabled: enabled(false), KMSMasterKeyId: "alias/app"},
	} {
		_, err := store.CreateTable(sseTable("invalid", spec))
		if se, ok := err.(*ServiceError); !ok || se.Code != "ValidationException" {
			t.Fatalf("CreateTable(%+v) error = %v, want ValidationException", spec, err)
		}
	}
}

func TestUpdateTableChangesAndPersistsKMSEncryption(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := store.CreateTable(sseTable("orders", nil)); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	if _, err := store.UpdateTable(&UpdateTableInput{TableName: "orders", SSESpecification: &types.DynamoDBSSESpecification{Enabled: enabled(true), KMSMasterKeyId: "alias/app"}}); err != nil {
		t.Fatalf("UpdateTable enable: %v", err)
	}
	// An update that does not mention SSE leaves it as it was.
	if _, err := store.UpdateTable(&UpdateTableInput{TableName: "orders", DeletionProtectionEnabled: enabled(false)}); err != nil {
		t.Fatalf("UpdateTable other: %v", err)
	}
	store.Close()

	reloaded := NewStore(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := reloaded.DescribeTable("orders")
	if err != nil || got.SSEDescription == nil || got.SSEDescription.KMSMasterKeyArn != kmsKeyARN(cfg, "alias/app") {
		t.Fatalf("after reload SSEDescription = %+v, %v", got.SSEDescription, err)
	}

	if _, err := reloaded.UpdateTable(&UpdateTableInput{TableName: "orders", SSESpecification: &types.DynamoDBSSESpecification{Enabled: enabled(false)}}); err != nil {
		t.Fatalf("UpdateTable disable: %v", err)
	}
	got, _ = reloaded.DescribeTable("orders")
	if got.SSEDescription != nil {
		t.Fatalf("after disable SSEDescription = %+v", got.SSEDescription)
	}
	reloaded.Close()
}
