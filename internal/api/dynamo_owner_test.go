package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dynamo sends a DynamoDB request signed with accessKey.
func (h *accountHarness) dynamo(accessKey, action, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/20260930/eu-west-2/dynamodb/aws4_request, SignedHeaders=host, Signature=0")
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810."+action)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

const usersTable = `{"TableName":"users","AttributeDefinitions":[{"AttributeName":"id","AttributeType":"S"}],"KeySchema":[{"AttributeName":"id","KeyType":"HASH"}],"BillingMode":"PAY_PER_REQUEST"}`

func TestDynamoDBNonNumericKeyFindsTheTablesOwner(t *testing.T) {
	h := newAccountHarness(t)
	if rec := h.dynamo(otherAccount, "CreateTable", usersTable); rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := h.dynamo("dummy", "PutItem", `{"TableName":"users","Item":{"id":{"S":"a"}}}`); rec.Code != http.StatusOK {
		t.Fatalf("put with dummy key: %d %s", rec.Code, rec.Body.String())
	}
	rec := h.dynamo(otherAccount, "GetItem", `{"TableName":"users","Key":{"id":{"S":"a"}}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"a"`) {
		t.Fatalf("item written with dummy key not in owner account: %d %s", rec.Code, rec.Body.String())
	}
	rec = h.dynamo("dummy", "BatchGetItem", `{"RequestItems":{"users":{"Keys":[{"id":{"S":"a"}}]}}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"a"`) {
		t.Fatalf("batch get with dummy key: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDynamoDBNumericKeyStaysIsolated(t *testing.T) {
	h := newAccountHarness(t)
	h.dynamo(otherAccount, "CreateTable", usersTable)
	if rec := h.dynamo("222222222222", "DescribeTable", `{"TableName":"users"}`); rec.Code == http.StatusOK {
		t.Fatalf("a 12-digit key reached another account's table: %s", rec.Body.String())
	}
}

func TestDynamoDBNonNumericKeyPrefersDefaultAndCreatesThere(t *testing.T) {
	h := newAccountHarness(t)
	h.dynamo(otherAccount, "CreateTable", usersTable)
	// A create belongs to the caller's account even when another holds the name.
	if rec := h.dynamo("dummy", "CreateTable", usersTable); rec.Code != http.StatusOK {
		t.Fatalf("create in default: %d %s", rec.Code, rec.Body.String())
	}
	h.dynamo("dummy", "PutItem", `{"TableName":"users","Item":{"id":{"S":"d"}}}`)
	rec := h.dynamo(otherAccount, "GetItem", `{"TableName":"users","Key":{"id":{"S":"d"}}}`)
	if strings.Contains(rec.Body.String(), `"d"`) {
		t.Fatalf("write went to the other account although the default holds the table")
	}
}

func TestDynamoDBAmbiguousOwnerFallsBackToDefault(t *testing.T) {
	h := newAccountHarness(t)
	h.dynamo(otherAccount, "CreateTable", usersTable)
	h.dynamo("222222222222", "CreateTable", usersTable)
	rec := h.dynamo("dummy", "DescribeTable", `{"TableName":"users"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("an ambiguous table must not be resolved: %s", rec.Body.String())
	}
}
