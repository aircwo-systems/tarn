package s3

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBucketCRUD(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Create
	bucket, err := store.CreateBucket("test-bucket", "us-east-1")
	if err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if bucket.Name != "test-bucket" {
		t.Fatalf("name = %q, want %q", bucket.Name, "test-bucket")
	}

	// Duplicate
	_, err = store.CreateBucket("test-bucket", "us-east-1")
	if err == nil || !strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") {
		t.Fatalf("expected BucketAlreadyOwnedByYou, got %v", err)
	}

	// Head
	if err := store.HeadBucket("test-bucket"); err != nil {
		t.Fatalf("head bucket: %v", err)
	}
	if err := store.HeadBucket("missing"); err == nil {
		t.Fatal("head missing bucket should fail")
	}

	// List
	buckets := store.ListBuckets()
	if len(buckets) != 1 {
		t.Fatalf("list buckets = %d, want 1", len(buckets))
	}

	// Delete
	if err := store.DeleteBucket("test-bucket"); err != nil {
		t.Fatalf("delete bucket: %v", err)
	}
	if err := store.HeadBucket("test-bucket"); err == nil {
		t.Fatal("bucket should not exist after delete")
	}
}

func TestDeleteNonEmptyBucketFails(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Init()

	store.CreateBucket("bucket", "us-east-1")
	store.PutObject("bucket", "key", "text/plain", strings.NewReader("data"), nil)

	err := store.DeleteBucket("bucket")
	if err == nil || !strings.Contains(err.Error(), "BucketNotEmpty") {
		t.Fatalf("expected BucketNotEmpty, got %v", err)
	}
}

func TestObjectPutGetHeadDelete(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Init()
	store.CreateBucket("bucket", "us-east-1")

	// Put
	obj, err := store.PutObject("bucket", "hello.txt", "text/plain", strings.NewReader("hello world"), map[string]string{"author": "test"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if obj.Size != 11 {
		t.Fatalf("size = %d, want 11", obj.Size)
	}
	if obj.ETag == "" {
		t.Fatal("ETag is empty")
	}
	if obj.ContentType != "text/plain" {
		t.Fatalf("content type = %q, want text/plain", obj.ContentType)
	}

	// Get
	obj2, reader, err := store.GetObject("bucket", "hello.txt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	data, _ := io.ReadAll(reader)
	reader.Close()
	if string(data) != "hello world" {
		t.Fatalf("body = %q, want %q", string(data), "hello world")
	}
	if obj2.ETag != obj.ETag {
		t.Fatalf("etag mismatch: %q vs %q", obj2.ETag, obj.ETag)
	}
	if obj2.Metadata["author"] != "test" {
		t.Fatalf("metadata author = %q, want %q", obj2.Metadata["author"], "test")
	}

	// Head
	obj3, err := store.HeadObject("bucket", "hello.txt")
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if obj3.Size != 11 {
		t.Fatalf("head size = %d, want 11", obj3.Size)
	}

	// Delete
	if err := store.DeleteObject("bucket", "hello.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify gone
	_, err = store.HeadObject("bucket", "hello.txt")
	if err == nil || !strings.Contains(err.Error(), "NoSuchKey") {
		t.Fatalf("expected NoSuchKey, got %v", err)
	}
}

func TestListObjectsWithPrefixDelimiter(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Init()
	store.CreateBucket("bucket", "us-east-1")

	keys := []string{
		"photos/2024/jan.jpg",
		"photos/2024/feb.jpg",
		"photos/2025/mar.jpg",
		"docs/readme.txt",
	}
	for _, key := range keys {
		store.PutObject("bucket", key, "application/octet-stream", strings.NewReader("x"), nil)
	}

	// List all
	result, err := store.ListObjects("bucket", "", "", "", 1000)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Contents) != 4 {
		t.Fatalf("list all = %d, want 4", len(result.Contents))
	}

	// List with prefix
	result, err = store.ListObjects("bucket", "photos/", "", "", 1000)
	if err != nil {
		t.Fatalf("list prefix: %v", err)
	}
	if len(result.Contents) != 3 {
		t.Fatalf("list photos/ = %d, want 3", len(result.Contents))
	}

	// List with prefix and delimiter
	result, err = store.ListObjects("bucket", "photos/", "/", "", 1000)
	if err != nil {
		t.Fatalf("list prefix+delim: %v", err)
	}
	if len(result.CommonPrefixes) != 2 {
		t.Fatalf("common prefixes = %d, want 2", len(result.CommonPrefixes))
	}
	if len(result.Contents) != 0 {
		t.Fatalf("contents = %d, want 0", len(result.Contents))
	}
}

func TestCopyObject(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Init()
	store.CreateBucket("src", "us-east-1")
	store.CreateBucket("dst", "us-east-1")

	store.PutObject("src", "file.txt", "text/plain", strings.NewReader("copy me"), nil)

	obj, err := store.CopyObject("src", "file.txt", "dst", "copied.txt")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if obj.Size != 7 {
		t.Fatalf("size = %d, want 7", obj.Size)
	}

	// Verify in destination
	_, reader, err := store.GetObject("dst", "copied.txt")
	if err != nil {
		t.Fatalf("get copied: %v", err)
	}
	data, _ := io.ReadAll(reader)
	reader.Close()
	if string(data) != "copy me" {
		t.Fatalf("body = %q, want %q", string(data), "copy me")
	}
}

func TestBatchDelete(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Init()
	store.CreateBucket("bucket", "us-east-1")

	for _, key := range []string{"a.txt", "b.txt", "c.txt"} {
		store.PutObject("bucket", key, "text/plain", strings.NewReader("x"), nil)
	}

	errs := store.DeleteObjects("bucket", []string{"a.txt", "c.txt"})
	if len(errs) != 0 {
		t.Fatalf("delete errors = %d, want 0", len(errs))
	}

	// Only b.txt should remain
	result, _ := store.ListObjects("bucket", "", "", "", 1000)
	if len(result.Contents) != 1 {
		t.Fatalf("remaining = %d, want 1", len(result.Contents))
	}
	if result.Contents[0].Key != "b.txt" {
		t.Fatalf("remaining key = %q, want b.txt", result.Contents[0].Key)
	}
}

func TestETagIsMD5(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Init()
	store.CreateBucket("bucket", "us-east-1")

	obj, _ := store.PutObject("bucket", "key", "", bytes.NewReader([]byte("")), nil)
	// MD5 of empty string is d41d8cd98f00b204e9800998ecf8427e
	expected := "\"d41d8cd98f00b204e9800998ecf8427e\""
	if obj.ETag != expected {
		t.Fatalf("etag = %q, want %q", obj.ETag, expected)
	}
}

func TestObjectCountAndTotalSize(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Init()
	store.CreateBucket("bucket", "us-east-1")

	store.PutObject("bucket", "a", "", strings.NewReader("hello"), nil)
	store.PutObject("bucket", "b", "", strings.NewReader("world!"), nil)

	if c := store.ObjectCount("bucket"); c != 2 {
		t.Fatalf("count = %d, want 2", c)
	}
	if s := store.TotalSize("bucket"); s != 11 {
		t.Fatalf("size = %d, want 11", s)
	}
}
