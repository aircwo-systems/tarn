package s3

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := store.PutObject("bucket", "key", "text/plain", strings.NewReader("data"), nil); err != nil {
		t.Fatalf("put object: %v", err)
	}

	err := store.DeleteBucket("bucket")
	if err == nil || !strings.Contains(err.Error(), "BucketNotEmpty") {
		t.Fatalf("expected BucketNotEmpty, got %v", err)
	}
}

func TestObjectPutGetHeadDelete(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

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
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
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
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	keys := []string{
		"photos/2024/jan.jpg",
		"photos/2024/feb.jpg",
		"photos/2025/mar.jpg",
		"docs/readme.txt",
	}
	for _, key := range keys {
		if _, err := store.PutObject("bucket", key, "application/octet-stream", strings.NewReader("x"), nil); err != nil {
			t.Fatalf("put object %q: %v", key, err)
		}
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
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("src", "us-east-1"); err != nil {
		t.Fatalf("create src bucket: %v", err)
	}
	if _, err := store.CreateBucket("dst", "us-east-1"); err != nil {
		t.Fatalf("create dst bucket: %v", err)
	}

	if _, err := store.PutObject("src", "file.txt", "text/plain", strings.NewReader("copy me"), nil); err != nil {
		t.Fatalf("put object: %v", err)
	}

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
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	if string(data) != "copy me" {
		t.Fatalf("body = %q, want %q", string(data), "copy me")
	}
}

func TestBatchDelete(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	for _, key := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := store.PutObject("bucket", key, "text/plain", strings.NewReader("x"), nil); err != nil {
			t.Fatalf("put object %q: %v", key, err)
		}
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
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	obj, err := store.PutObject("bucket", "key", "", bytes.NewReader([]byte("")), nil)
	if err != nil {
		t.Fatalf("put object: %v", err)
	}
	// MD5 of empty string is d41d8cd98f00b204e9800998ecf8427e
	expected := "\"d41d8cd98f00b204e9800998ecf8427e\""
	if obj.ETag != expected {
		t.Fatalf("etag = %q, want %q", obj.ETag, expected)
	}
}

func TestObjectCountAndTotalSize(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	if _, err := store.PutObject("bucket", "a", "", strings.NewReader("hello"), nil); err != nil {
		t.Fatalf("put object a: %v", err)
	}
	if _, err := store.PutObject("bucket", "b", "", strings.NewReader("world!"), nil); err != nil {
		t.Fatalf("put object b: %v", err)
	}

	if c := store.ObjectCount("bucket"); c != 2 {
		t.Fatalf("count = %d, want 2", c)
	}
	if s := store.TotalSize("bucket"); s != 11 {
		t.Fatalf("size = %d, want 11", s)
	}
}

// TestIndexTracksEveryMutationPath pins the maintained index against the
// filesystem-derived numbers it replaced, across every path that changes a
// bucket's object set. An index that drifts is worse than a slow one: the
// dashboard would report counts no bucket actually has.
func TestIndexTracksEveryMutationPath(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	// readTruth is the number the index has to agree with, recomputed the slow
	// way from the files on disk.
	readTruth := func() (int, int64) {
		entries, err := os.ReadDir(filepath.Join(dir, "bucket", "objects"))
		if err != nil {
			return 0, 0
		}
		var total int64
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				total += info.Size()
			}
		}
		return len(entries), total
	}
	assertIndex := func(step string, wantCount int) {
		t.Helper()
		if got := store.ObjectCount("bucket"); got != wantCount {
			t.Errorf("%s: count = %d, want %d", step, got, wantCount)
		}
		// Recompute the truth from disk rather than hardcoding sizes.
		_, wantSize := readTruth()
		if got := store.TotalSize("bucket"); got != wantSize {
			t.Errorf("%s: total size = %d, want %d", step, got, wantSize)
		}
	}

	put := func(key, body string) {
		t.Helper()
		if _, err := store.PutObject("bucket", key, "", strings.NewReader(body), nil); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}

	put("a", "hello")
	put("b", "world!")
	assertIndex("two puts", 2)

	// An overwrite must not count the key twice, and must replace its size even
	// when the object shrinks.
	put("a", "hi")
	assertIndex("overwrite with a shorter body", 2)

	put("a", strings.Repeat("x", 100))
	assertIndex("overwrite with a longer body", 2)

	if err := store.DeleteObject("bucket", "b"); err != nil {
		t.Fatalf("delete b: %v", err)
	}
	assertIndex("delete", 1)

	// Deleting a key that is not there is a no-op for the index.
	if err := store.DeleteObject("bucket", "missing"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	assertIndex("delete of a missing key", 1)

	put("c", "ccc")
	put("d", "dddd")
	if errs := store.DeleteObjects("bucket", []string{"c", "d", "also-missing"}); len(errs) != 0 {
		t.Fatalf("batch delete errors: %+v", errs)
	}
	assertIndex("batch delete", 1)

	// CopyObject writes through PutObject, so it must move both totals.
	if _, err := store.CreateBucket("other", "us-east-1"); err != nil {
		t.Fatalf("create other bucket: %v", err)
	}
	if _, err := store.CopyObject("bucket", "a", "other", "copied"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if got := store.ObjectCount("other"); got != 1 {
		t.Errorf("copied bucket count = %d, want 1", got)
	}
	if got, want := store.TotalSize("other"), store.TotalSize("bucket"); got != want {
		t.Errorf("copied bucket size = %d, want the source's %d", got, want)
	}
	assertIndex("after copy out", 1)

	// A same-bucket overwrite through copy must also stay at one key.
	if _, err := store.CopyObject("bucket", "a", "bucket", "a"); err != nil {
		t.Fatalf("same-bucket copy: %v", err)
	}
	assertIndex("same-bucket copy onto itself", 1)
}

// TestIndexSurvivesReopen covers the other half: the index is derived state, so
// it has to be rebuilt from disk on start or a restored bucket reads as empty.
func TestIndexSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	for _, key := range []string{"a", "b", "c"} {
		if _, err := store.PutObject("bucket", key, "text/plain", strings.NewReader("body-"+key), nil); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	before := store.ObjectCount("bucket")
	beforeSize := store.TotalSize("bucket")

	reopened := NewStore(dir)
	if err := reopened.Init(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.ObjectCount("bucket"); got != before {
		t.Errorf("count after reopen = %d, want %d", got, before)
	}
	if got := reopened.TotalSize("bucket"); got != beforeSize {
		t.Errorf("size after reopen = %d, want %d", got, beforeSize)
	}
	recent, err := reopened.RecentObjects("bucket", 3)
	if err != nil {
		t.Fatalf("recent objects: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("recent objects after reopen = %d, want 3", len(recent))
	}
	// Newest first, which is the order the dashboard preview relies on.
	for i := 1; i < len(recent); i++ {
		if recent[i-1].LastModified.Before(recent[i].LastModified) {
			t.Errorf("recent objects are not newest-first: %v", recent)
			break
		}
	}

	// And a delete after reopen must still subtract correctly against the
	// rebuilt index.
	if err := reopened.DeleteObject("bucket", "a"); err != nil {
		t.Fatalf("delete after reopen: %v", err)
	}
	if got, want := reopened.ObjectCount("bucket"), before-1; got != want {
		t.Errorf("count after delete = %d, want %d", got, want)
	}
}

// TestRecentObjectsIsCappedAndNewestFirst covers the preview's own contract: a
// bucket written to constantly must not grow the index, and the newest objects
// have to be the ones returned.
func TestRecentObjectsIsCappedAndNewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	total := recentLimit * 3
	for i := range total {
		key := "key-" + strconv.Itoa(i)
		if _, err := store.PutObject("bucket", key, "", strings.NewReader("x"), nil); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}

	// The count still reflects every object, even though the index only keeps
	// the newest few names.
	if got := store.ObjectCount("bucket"); got != total {
		t.Errorf("count = %d, want %d", got, total)
	}

	recent, err := store.RecentObjects("bucket", 12)
	if err != nil {
		t.Fatalf("recent objects: %v", err)
	}
	if len(recent) != 12 {
		t.Fatalf("recent objects = %d, want the 12 asked for", len(recent))
	}
	if want := "key-" + strconv.Itoa(total-1); recent[0].Key != want {
		t.Errorf("newest object = %q, want %q", recent[0].Key, want)
	}

	store.mu.RLock()
	bs := store.buckets["bucket"]
	bs.mu.RLock()
	kept := len(bs.index.recent)
	bs.mu.RUnlock()
	store.mu.RUnlock()
	if kept > recentLimit {
		t.Errorf("index kept %d recent objects, want at most %d", kept, recentLimit)
	}
}

// BenchmarkBucketSummaryStats measures what a dashboard poll spends per bucket:
// object count, total size, and the newest-object preview. It runs against a
// populated bucket because the cost being removed scaled with the object count.
func BenchmarkBucketSummaryStats(b *testing.B) {
	dir := b.TempDir()
	store := NewStore(dir)
	if err := store.Init(); err != nil {
		b.Fatalf("init: %v", err)
	}
	if _, err := store.CreateBucket("bucket", "us-east-1"); err != nil {
		b.Fatalf("create bucket: %v", err)
	}
	for i := range 2000 {
		key := "key-" + strconv.Itoa(i)
		if _, err := store.PutObject("bucket", key, "text/plain", strings.NewReader(strings.Repeat("x", 512)), nil); err != nil {
			b.Fatalf("put %s: %v", key, err)
		}
	}

	b.ResetTimer()
	for b.Loop() {
		_ = store.ObjectCount("bucket")
		_ = store.TotalSize("bucket")
		if _, err := store.RecentObjects("bucket", 12); err != nil {
			b.Fatal(err)
		}
	}
}
