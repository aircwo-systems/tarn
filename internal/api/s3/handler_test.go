package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	s3svc "github.com/aircwo-systems/tarn/internal/s3"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	svc := s3svc.NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init s3: %v", err)
	}
	return NewHandler(svc)
}

func TestCreateAndListBuckets(t *testing.T) {
	h := newTestHandler(t)

	// Create bucket
	req := httptest.NewRequest(http.MethodPut, "/_s3/test-bucket", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// List buckets
	req = httptest.NewRequest(http.MethodGet, "/_s3/", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "test-bucket") {
		t.Fatalf("response does not contain bucket name: %s", body)
	}
	if !strings.Contains(body, "ListAllMyBucketsResult") {
		t.Fatalf("response missing XML root element: %s", body)
	}
}

func TestPutGetObject(t *testing.T) {
	h := newTestHandler(t)

	// Create bucket
	req := httptest.NewRequest(http.MethodPut, "/_s3/mybucket", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	// Put object
	req = httptest.NewRequest(http.MethodPut, "/_s3/mybucket/hello.txt", strings.NewReader("hello world"))
	req.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("put response missing ETag header")
	}

	// Get object
	req = httptest.NewRequest(http.MethodGet, "/_s3/mybucket/hello.txt", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "hello world")
	}
	if rec.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("content-type = %q, want text/plain", rec.Header().Get("Content-Type"))
	}
}

func TestHeadObjectHeaders(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/_s3/bucket", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	req = httptest.NewRequest(http.MethodPut, "/_s3/bucket/file.bin", strings.NewReader("abc"))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("x-amz-meta-custom", "value123")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	req = httptest.NewRequest(http.MethodHead, "/_s3/bucket/file.bin", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("head status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("Content-Length") != "3" {
		t.Fatalf("content-length = %q, want 3", rec.Header().Get("Content-Length"))
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("missing ETag")
	}
	if rec.Header().Get("x-amz-meta-custom") != "value123" {
		t.Fatalf("metadata = %q, want value123", rec.Header().Get("x-amz-meta-custom"))
	}
}

func TestListObjectsV2XML(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/_s3/bucket", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	for _, key := range []string{"a.txt", "b.txt"} {
		req = httptest.NewRequest(http.MethodPut, "/_s3/bucket/"+key, strings.NewReader("x"))
		rec = httptest.NewRecorder()
		h.Dispatch(rec, req)
	}

	req = httptest.NewRequest(http.MethodGet, "/_s3/bucket?list-type=2", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var result struct {
		XMLName  xml.Name `xml:"ListBucketResult"`
		Name     string   `xml:"Name"`
		KeyCount int      `xml:"KeyCount"`
		Contents []struct {
			Key  string `xml:"Key"`
			Size int64  `xml:"Size"`
		} `xml:"Contents"`
	}
	if err := xml.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode xml: %v", err)
	}
	if result.KeyCount != 2 {
		t.Fatalf("key count = %d, want 2", result.KeyCount)
	}
	if result.Name != "bucket" {
		t.Fatalf("name = %q, want bucket", result.Name)
	}
}

func TestDeleteObjectsXML(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/_s3/bucket", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	for _, key := range []string{"x", "y", "z"} {
		req = httptest.NewRequest(http.MethodPut, "/_s3/bucket/"+key, strings.NewReader("data"))
		rec = httptest.NewRecorder()
		h.Dispatch(rec, req)
	}

	deleteXML := `<Delete><Object><Key>x</Key></Object><Object><Key>z</Key></Object></Delete>`
	req = httptest.NewRequest(http.MethodPost, "/_s3/bucket?delete", strings.NewReader(deleteXML))
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var result struct {
		XMLName xml.Name `xml:"DeleteResult"`
		Deleted []struct {
			Key string `xml:"Key"`
		} `xml:"Deleted"`
	}
	if err := xml.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Deleted) != 2 {
		t.Fatalf("deleted = %d, want 2", len(result.Deleted))
	}
}

func TestCopyObjectViaHeader(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/_s3/src", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	req = httptest.NewRequest(http.MethodPut, "/_s3/dst", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	req = httptest.NewRequest(http.MethodPut, "/_s3/src/file.txt", strings.NewReader("copy data"))
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	req = httptest.NewRequest(http.MethodPut, "/_s3/dst/copied.txt", nil)
	req.Header.Set("x-amz-copy-source", "/src/file.txt")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("copy status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify copied object
	req = httptest.NewRequest(http.MethodGet, "/_s3/dst/copied.txt", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	data, _ := io.ReadAll(rec.Body)
	if string(data) != "copy data" {
		t.Fatalf("body = %q, want %q", string(data), "copy data")
	}
}

func TestErrorResponses(t *testing.T) {
	h := newTestHandler(t)

	// Get from non-existent bucket
	req := httptest.NewRequest(http.MethodGet, "/_s3/nope/key", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "NoSuchBucket") {
		t.Fatalf("response missing error code: %s", body)
	}

	// Get non-existent key
	req = httptest.NewRequest(http.MethodPut, "/_s3/bucket", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/_s3/bucket/missing", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), "NoSuchKey") {
		t.Fatalf("response missing NoSuchKey: %s", rec.Body.String())
	}
}

func TestGetBucketPolicyNoPolicy(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/_s3/policy-bucket", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d", rec.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/_s3/policy-bucket?policy", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "NoSuchBucketPolicy") {
		t.Fatalf("expected NoSuchBucketPolicy error, got: %s", rec.Body.String())
	}
}

func TestBucketSubresourceTerraformStubs(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/_s3/tf-bucket", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d", rec.Code, http.StatusOK)
	}

	tests := []struct {
		name       string
		url        string
		wantStatus int
		wantBody   string
	}{
		{name: "versioning", url: "/_s3/tf-bucket?versioning", wantStatus: http.StatusOK, wantBody: "VersioningConfiguration"},
		{name: "encryption", url: "/_s3/tf-bucket?encryption", wantStatus: http.StatusNotFound, wantBody: "ServerSideEncryptionConfigurationNotFoundError"},
		{name: "cors", url: "/_s3/tf-bucket?cors", wantStatus: http.StatusNotFound, wantBody: "NoSuchCORSConfiguration"},
		{name: "logging", url: "/_s3/tf-bucket?logging", wantStatus: http.StatusOK, wantBody: "BucketLoggingStatus"},
		{name: "acl", url: "/_s3/tf-bucket?acl", wantStatus: http.StatusOK, wantBody: "AccessControlPolicy"},
		{name: "replication", url: "/_s3/tf-bucket?replication", wantStatus: http.StatusNotFound, wantBody: "ReplicationConfigurationNotFoundError"},
		{name: "accelerate", url: "/_s3/tf-bucket?accelerate", wantStatus: http.StatusOK, wantBody: "<Status>Suspended</Status>"},
		{name: "request-payment", url: "/_s3/tf-bucket?request-payment", wantStatus: http.StatusOK, wantBody: "<Payer>BucketOwner</Payer>"},
		{name: "object-lock", url: "/_s3/tf-bucket?object-lock", wantStatus: http.StatusNotFound, wantBody: "ObjectLockConfigurationNotFoundError"},
		{name: "tagging", url: "/_s3/tf-bucket?tagging", wantStatus: http.StatusNotFound, wantBody: "NoSuchTagSet"},
		{name: "lifecycle", url: "/_s3/tf-bucket?lifecycle", wantStatus: http.StatusNotFound, wantBody: "NoSuchLifecycleConfiguration"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			h.Dispatch(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body missing %q: %s", tc.wantBody, rec.Body.String())
			}
		})
	}
}

func TestBucketTaggingLifecycle(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/_s3/tf-bucket", nil)
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d", rec.Code, http.StatusOK)
	}

	taggingXML := `<Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet><Tag><Key>env</Key><Value>local</Value></Tag><Tag><Key>service</Key><Value>dpc</Value></Tag></TagSet></Tagging>`
	req = httptest.NewRequest(http.MethodPut, "/_s3/tf-bucket?tagging", strings.NewReader(taggingXML))
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put tagging status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/_s3/tf-bucket?tagging", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get tagging status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<Key>env</Key>") || !strings.Contains(body, "<Value>local</Value>") {
		t.Fatalf("tagging body missing env tag: %s", body)
	}
	if !strings.Contains(body, "<Key>service</Key>") || !strings.Contains(body, "<Value>dpc</Value>") {
		t.Fatalf("tagging body missing service tag: %s", body)
	}

	req = httptest.NewRequest(http.MethodDelete, "/_s3/tf-bucket?tagging", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete tagging status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	req = httptest.NewRequest(http.MethodGet, "/_s3/tf-bucket?tagging", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get tagging after delete status = %d, want %d, body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "NoSuchTagSet") {
		t.Fatalf("expected NoSuchTagSet after delete, got: %s", rec.Body.String())
	}
}

func TestCreateBucketPersistsTagsFromCreateRequest(t *testing.T) {
	h := newTestHandler(t)

	body := `<CreateBucketConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><LocationConstraint>eu-west-1</LocationConstraint><Tags><Tag><Key>env</Key><Value>local</Value></Tag></Tags></CreateBucketConfiguration>`
	req := httptest.NewRequest(http.MethodPut, "/_s3/tagged-bucket", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/_s3/tagged-bucket?tagging", nil)
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get tagging status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<Key>env</Key>") || !strings.Contains(rec.Body.String(), "<Value>local</Value>") {
		t.Fatalf("tagging body missing create tags: %s", rec.Body.String())
	}
}
