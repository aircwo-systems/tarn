package s3

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// Store is a filesystem-backed S3 object store.
type Store struct {
	mu            sync.RWMutex
	baseDir       string
	buckets       map[string]*bucketState
	notifications map[string]*types.BucketNotificationConfiguration
	configs       map[string]*types.BucketConfig
}

type bucketState struct {
	mu   sync.RWMutex
	meta *types.Bucket

	// index is the per-bucket object summary the dashboard reads: how many
	// objects there are, their total size, and the most recently written ones.
	// It is maintained on write rather than derived from the filesystem, which
	// is what made a dashboard poll cost a directory walk, a stat per object
	// and a metadata read per object for every bucket, every five seconds.
	index objectIndex
}

// objectIndex is the maintained view of a bucket's object set. Guarded by
// bucketState.mu.
type objectIndex struct {
	count     int
	totalSize int64
	// recent holds the newest objects, newest first, capped at recentLimit so a
	// bucket that is written to constantly cannot grow it without bound.
	recent []types.Object
}

// recentLimit is how many of the newest objects a bucket keeps for the
// dashboard's preview. The preview asks for 12; the rest is slack so a small
// change in what the UI asks for does not need an index change.
const recentLimit = 32

// record adds or replaces an object in the index, given whether the key was
// already present and its previous size. Caller must hold the bucket write lock.
func (ix *objectIndex) record(prevSize int64, existed bool, obj types.Object) {
	if !existed {
		ix.count++
	}
	ix.totalSize += obj.Size - prevSize

	// Keep the newest first, replacing an existing entry for the same key
	// rather than duplicating it.
	for i, existing := range ix.recent {
		if existing.Key == obj.Key {
			ix.recent = slices.Delete(ix.recent, i, i+1)
			break
		}
	}
	ix.recent = append(ix.recent, obj)
	slices.SortFunc(ix.recent, func(a, b types.Object) int {
		return b.LastModified.Compare(a.LastModified)
	})
	if len(ix.recent) > recentLimit {
		ix.recent = ix.recent[:recentLimit]
	}
}

// forget removes an object from the index, given its size if it was present.
// Caller must hold the bucket write lock.
func (ix *objectIndex) forget(key string, size int64, existed bool) {
	if !existed {
		return
	}
	ix.count--
	ix.totalSize -= size
	for i, existing := range ix.recent {
		if existing.Key == key {
			ix.recent = slices.Delete(ix.recent, i, i+1)
			return
		}
	}
}

// RecentObjects returns up to limit of the bucket's newest objects, newest
// first. It reads the maintained index and touches no files.
func (bs *bucketState) recentObjects(limit int) []types.Object {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	if limit <= 0 || limit > len(bs.index.recent) {
		limit = len(bs.index.recent)
	}
	out := make([]types.Object, limit)
	copy(out, bs.index.recent[:limit])
	return out
}

type objectMeta struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"contentType"`
	LastModified time.Time         `json:"lastModified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// NewStore creates a new filesystem-backed S3 store.
func NewStore(baseDir string) *Store {
	return &Store{
		baseDir:       baseDir,
		buckets:       make(map[string]*bucketState),
		notifications: make(map[string]*types.BucketNotificationConfiguration),
		configs:       make(map[string]*types.BucketConfig),
	}
}

// Init loads existing buckets from disk.
func (s *Store) Init() error {
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return fmt.Errorf("create s3 dir: %w", err)
	}

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return fmt.Errorf("read s3 dir: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		metaPath := filepath.Join(s.baseDir, name, ".meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var bucket types.Bucket
		if err := json.Unmarshal(data, &bucket); err != nil {
			continue
		}
		s.buckets[name] = &bucketState{meta: &bucket}
		s.rebuildIndexLocked(name)

		// Load notification config if present
		notifPath := filepath.Join(s.baseDir, name, ".notifications.json")
		notifData, err := os.ReadFile(notifPath)
		if err == nil {
			var cfg types.BucketNotificationConfiguration
			if err := json.Unmarshal(notifData, &cfg); err == nil {
				s.notifications[name] = &cfg
			}
		}

		// Load bucket config if present
		configPath := filepath.Join(s.baseDir, name, ".config.json")
		configData, err := os.ReadFile(configPath)
		if err == nil {
			var cfg types.BucketConfig
			if err := json.Unmarshal(configData, &cfg); err == nil {
				s.configs[name] = &cfg
			}
		}
	}

	return nil
}

// rebuildIndexLocked fills a bucket's index from what is on disk. It runs once
// per bucket at Init — the same walk the dashboard used to force on every poll,
// paid once instead of every five seconds — and after that writes maintain the
// index. Caller must hold s.mu.
func (s *Store) rebuildIndexLocked(bucket string) {
	bs := s.buckets[bucket]
	bs.mu.Lock()
	defer bs.mu.Unlock()

	var index objectIndex
	metaDir := s.objmetaDir(bucket)
	entries, err := os.ReadDir(metaDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(metaDir, entry.Name()))
			if err != nil {
				continue
			}
			var meta objectMeta
			if json.Unmarshal(data, &meta) != nil {
				continue
			}
			index.count++
			index.totalSize += meta.Size
			index.recent = append(index.recent, types.Object{
				Key:          meta.Key,
				Size:         meta.Size,
				ETag:         meta.ETag,
				ContentType:  meta.ContentType,
				LastModified: meta.LastModified,
				Metadata:     meta.Metadata,
			})
		}
	}
	// An object whose body survived but whose metadata did not — a crash between
	// the two writes — is invisible to ListObjects, so leaving it out of the
	// index keeps the count and the preview telling the same story. Its bytes
	// are still on disk, so DeleteBucket still refuses to remove the bucket.
	slices.SortFunc(index.recent, func(a, b types.Object) int {
		return b.LastModified.Compare(a.LastModified)
	})
	if len(index.recent) > recentLimit {
		index.recent = index.recent[:recentLimit]
	}
	bs.index = index
}

func (s *Store) bucketDir(name string) string {
	return filepath.Join(s.baseDir, name)
}

func (s *Store) objectsDir(bucket string) string {
	return filepath.Join(s.baseDir, bucket, "objects")
}

func (s *Store) objmetaDir(bucket string) string {
	return filepath.Join(s.baseDir, bucket, ".objmeta")
}

func encodeKey(key string) string {
	return url.PathEscape(key)
}

func cloneBucketTags(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}

func (s *Store) persistBucketMeta(name string, bucket *types.Bucket) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return fmt.Errorf("marshal bucket meta: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.bucketDir(name), ".meta.json"), data, 0600); err != nil {
		return fmt.Errorf("write bucket meta: %w", err)
	}
	return nil
}

// CreateBucket creates a new bucket.
func (s *Store) CreateBucket(name, region string) (*types.Bucket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.buckets[name]; exists {
		return s.buckets[name].meta, fmt.Errorf("BucketAlreadyOwnedByYou")
	}

	bucket := &types.Bucket{
		Name:         name,
		CreationDate: time.Now().UTC(),
		Region:       region,
	}

	dir := s.bucketDir(name)
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0755); err != nil {
		return nil, fmt.Errorf("create bucket dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".objmeta"), 0755); err != nil {
		return nil, fmt.Errorf("create objmeta dir: %w", err)
	}

	if err := s.persistBucketMeta(name, bucket); err != nil {
		return nil, err
	}

	s.buckets[name] = &bucketState{meta: bucket}
	return bucket, nil
}

// HeadBucket checks if a bucket exists.
func (s *Store) HeadBucket(name string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.buckets[name]; !exists {
		return fmt.Errorf("NoSuchBucket")
	}
	return nil
}

// GetBucketRegion returns the region stored for a bucket.
func (s *Store) GetBucketRegion(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bs, exists := s.buckets[name]
	if !exists {
		return "", fmt.Errorf("NoSuchBucket")
	}
	return bs.meta.Region, nil
}

// DeleteBucket removes a bucket (must be empty).
func (s *Store) DeleteBucket(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bs, exists := s.buckets[name]
	if !exists {
		return fmt.Errorf("NoSuchBucket")
	}

	bs.mu.RLock()
	entries, _ := os.ReadDir(s.objectsDir(name))
	bs.mu.RUnlock()

	if len(entries) > 0 {
		return fmt.Errorf("BucketNotEmpty")
	}

	_ = os.RemoveAll(s.bucketDir(name))
	delete(s.buckets, name)
	return nil
}

// ListBuckets returns all buckets.
func (s *Store) ListBuckets() []types.Bucket {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]types.Bucket, 0, len(s.buckets))
	for _, bs := range s.buckets {
		result = append(result, *bs.meta)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// PutBucketTags stores bucket tags.
func (s *Store) PutBucketTags(name string, tags map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bs, exists := s.buckets[name]
	if !exists {
		return fmt.Errorf("NoSuchBucket")
	}

	bs.meta.Tags = cloneBucketTags(tags)
	return s.persistBucketMeta(name, bs.meta)
}

// GetBucketTags returns bucket tags.
func (s *Store) GetBucketTags(name string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bs, exists := s.buckets[name]
	if !exists {
		return nil, fmt.Errorf("NoSuchBucket")
	}

	return cloneBucketTags(bs.meta.Tags), nil
}

// DeleteBucketTags removes all bucket tags.
func (s *Store) DeleteBucketTags(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bs, exists := s.buckets[name]
	if !exists {
		return fmt.Errorf("NoSuchBucket")
	}

	bs.meta.Tags = nil
	return s.persistBucketMeta(name, bs.meta)
}

// PutObject stores an object in a bucket.
func (s *Store) PutObject(bucket, key, contentType string, body io.Reader, metadata map[string]string) (*types.Object, error) {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("NoSuchBucket")
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()

	encoded := encodeKey(key)
	objPath := filepath.Join(s.objectsDir(bucket), encoded)
	metaPath := filepath.Join(s.objmetaDir(bucket), encoded+".json")

	// Stat before os.Create truncates, so an overwrite can adjust the index by
	// the difference rather than counting the same key twice.
	prevSize, existed := int64(0), false
	if info, statErr := os.Stat(objPath); statErr == nil {
		prevSize, existed = info.Size(), true
	}

	f, err := os.Create(objPath)
	if err != nil {
		return nil, fmt.Errorf("create object file: %w", err)
	}

	hash := md5.New()
	w := io.MultiWriter(f, hash)
	size, err := io.Copy(w, body)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(objPath)
		return nil, fmt.Errorf("write object: %w", err)
	}

	etag := fmt.Sprintf("\"%x\"", hash.Sum(nil))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	obj := &types.Object{
		Key:          key,
		Size:         size,
		ETag:         etag,
		ContentType:  contentType,
		LastModified: time.Now().UTC(),
		Metadata:     metadata,
	}

	meta := objectMeta{
		Key:          key,
		Size:         size,
		ETag:         etag,
		ContentType:  contentType,
		LastModified: obj.LastModified,
		Metadata:     metadata,
	}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath, data, 0600); err != nil {
		_ = os.Remove(objPath)
		return nil, fmt.Errorf("write object meta: %w", err)
	}

	bs.index.record(prevSize, existed, *obj)
	return obj, nil
}

// GetObject returns an object's data as a ReadCloser.
func (s *Store) GetObject(bucket, key string) (*types.Object, io.ReadCloser, error) {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return nil, nil, fmt.Errorf("NoSuchBucket")
	}

	bs.mu.RLock()
	defer bs.mu.RUnlock()

	encoded := encodeKey(key)
	metaPath := filepath.Join(s.objmetaDir(bucket), encoded+".json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, nil, fmt.Errorf("NoSuchKey")
	}

	var meta objectMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, nil, fmt.Errorf("corrupt metadata: %w", err)
	}

	objPath := filepath.Join(s.objectsDir(bucket), encoded)
	f, err := os.Open(objPath)
	if err != nil {
		return nil, nil, fmt.Errorf("NoSuchKey")
	}

	obj := &types.Object{
		Key:          meta.Key,
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: meta.LastModified,
		Metadata:     meta.Metadata,
	}

	return obj, f, nil
}

// HeadObject returns object metadata without the body.
func (s *Store) HeadObject(bucket, key string) (*types.Object, error) {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("NoSuchBucket")
	}

	bs.mu.RLock()
	defer bs.mu.RUnlock()

	encoded := encodeKey(key)
	metaPath := filepath.Join(s.objmetaDir(bucket), encoded+".json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("NoSuchKey")
	}

	var meta objectMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("corrupt metadata: %w", err)
	}

	return &types.Object{
		Key:          meta.Key,
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: meta.LastModified,
		Metadata:     meta.Metadata,
	}, nil
}

// DeleteObject removes an object from a bucket.
func (s *Store) DeleteObject(bucket, key string) error {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("NoSuchBucket")
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Read the size before removing, so the index can subtract exactly what the
	// key contributed. A missing key leaves the index alone: S3 delete is
	// idempotent and must not double-count.
	size, existed := s.objectSizeLocked(bucket, key)
	encoded := encodeKey(key)
	_ = os.Remove(filepath.Join(s.objectsDir(bucket), encoded))
	_ = os.Remove(filepath.Join(s.objmetaDir(bucket), encoded+".json"))
	bs.index.forget(key, size, existed)
	return nil
}

// objectSizeLocked returns the stored size of a key and whether it exists,
// taking the object's own metadata first and falling back to a stat so an
// object whose metadata is missing is still accounted for. Caller must hold the
// bucket write lock.
func (s *Store) objectSizeLocked(bucket, key string) (int64, bool) {
	encoded := encodeKey(key)
	if data, err := os.ReadFile(filepath.Join(s.objmetaDir(bucket), encoded+".json")); err == nil {
		var meta objectMeta
		if json.Unmarshal(data, &meta) == nil {
			return meta.Size, true
		}
	}
	if info, err := os.Stat(filepath.Join(s.objectsDir(bucket), encoded)); err == nil {
		return info.Size(), true
	}
	return 0, false
}

// DeleteObjects removes multiple objects. Returns errors for any that failed.
func (s *Store) DeleteObjects(bucket string, keys []string) []types.DeleteError {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		errs := make([]types.DeleteError, len(keys))
		for i, key := range keys {
			errs[i] = types.DeleteError{Key: key, Code: "NoSuchBucket", Message: "The specified bucket does not exist"}
		}
		return errs
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()

	var errs []types.DeleteError
	for _, key := range keys {
		size, existed := s.objectSizeLocked(bucket, key)
		encoded := encodeKey(key)
		_ = os.Remove(filepath.Join(s.objectsDir(bucket), encoded))
		_ = os.Remove(filepath.Join(s.objmetaDir(bucket), encoded+".json"))
		bs.index.forget(key, size, existed)
	}
	return errs
}

// CopyObject copies an object between buckets (or within the same bucket).
func (s *Store) CopyObject(srcBucket, srcKey, dstBucket, dstKey string) (*types.Object, error) {
	obj, reader, err := s.GetObject(srcBucket, srcKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	return s.PutObject(dstBucket, dstKey, obj.ContentType, reader, obj.Metadata)
}

// ListObjects lists objects in a bucket with prefix/delimiter filtering.
func (s *Store) ListObjects(bucket, prefix, delimiter, continuationToken string, maxKeys int) (*types.ListResult, error) {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("NoSuchBucket")
	}

	bs.mu.RLock()
	defer bs.mu.RUnlock()

	if maxKeys <= 0 {
		maxKeys = 1000
	}

	// Read all object metadata
	metaDir := s.objmetaDir(bucket)
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		// Empty bucket — no .objmeta directory
		return &types.ListResult{
			Name:    bucket,
			Prefix:  prefix,
			MaxKeys: maxKeys,
		}, nil
	}

	// Collect all keys
	var allKeys []objectMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(metaDir, entry.Name()))
		if err != nil {
			continue
		}
		var meta objectMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if prefix != "" && !strings.HasPrefix(meta.Key, prefix) {
			continue
		}
		allKeys = append(allKeys, meta)
	}

	sort.Slice(allKeys, func(i, j int) bool { return allKeys[i].Key < allKeys[j].Key })

	// Apply continuation token (key to start after)
	if continuationToken != "" {
		idx := 0
		for i, m := range allKeys {
			if m.Key > continuationToken {
				idx = i
				break
			}
			if i == len(allKeys)-1 {
				idx = len(allKeys)
			}
		}
		allKeys = allKeys[idx:]
	}

	result := &types.ListResult{
		Name:      bucket,
		Prefix:    prefix,
		Delimiter: delimiter,
		MaxKeys:   maxKeys,
	}

	if delimiter == "" {
		// No delimiter — return flat list
		if len(allKeys) > maxKeys {
			result.IsTruncated = true
			result.NextContinuationToken = allKeys[maxKeys-1].Key
			allKeys = allKeys[:maxKeys]
		}
		result.Contents = make([]types.Object, len(allKeys))
		for i, m := range allKeys {
			result.Contents[i] = types.Object{
				Key:          m.Key,
				Size:         m.Size,
				ETag:         m.ETag,
				ContentType:  m.ContentType,
				LastModified: m.LastModified,
			}
		}
		result.KeyCount = len(result.Contents)
		return result, nil
	}

	// With delimiter — group into common prefixes
	prefixLen := len(prefix)
	seen := make(map[string]bool)
	var objects []types.Object

	for _, m := range allKeys {
		rest := m.Key[prefixLen:]
		delimIdx := strings.Index(rest, delimiter)
		if delimIdx >= 0 {
			commonPrefix := m.Key[:prefixLen+delimIdx+len(delimiter)]
			if !seen[commonPrefix] {
				seen[commonPrefix] = true
				result.CommonPrefixes = append(result.CommonPrefixes, commonPrefix)
			}
		} else {
			objects = append(objects, types.Object{
				Key:          m.Key,
				Size:         m.Size,
				ETag:         m.ETag,
				ContentType:  m.ContentType,
				LastModified: m.LastModified,
			})
		}

		if len(objects)+len(result.CommonPrefixes) >= maxKeys {
			result.IsTruncated = true
			result.NextContinuationToken = m.Key
			break
		}
	}

	result.Contents = objects
	if result.Contents == nil {
		result.Contents = []types.Object{}
	}
	result.KeyCount = len(result.Contents) + len(result.CommonPrefixes)
	return result, nil
}

// ObjectCount returns the number of objects in a bucket. It reads the
// maintained index, so it costs nothing and stays consistent with what
// RecentObjects and ListObjects report. Caller must hold at least s.mu.
func (s *Store) ObjectCount(bucket string) int {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return 0
	}

	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.index.count
}

// TotalSize returns the total size of all objects in a bucket, from the
// maintained index.
func (s *Store) TotalSize(bucket string) int64 {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return 0
	}

	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.index.totalSize
}

// RecentObjects returns up to limit of a bucket's newest objects, newest first.
// It reads the maintained index, so unlike ListObjects with a large MaxResults
// it neither walks the bucket nor reads every object's metadata.
func (s *Store) RecentObjects(bucket string, limit int) ([]types.Object, error) {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("NoSuchBucket")
	}
	return bs.recentObjects(limit), nil
}

// PutBucketNotification stores notification config for a bucket.
func (s *Store) PutBucketNotification(bucket string, cfg *types.BucketNotificationConfiguration) error {
	s.mu.RLock()
	_, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("NoSuchBucket")
	}

	s.mu.Lock()
	s.notifications[bucket] = cfg
	s.mu.Unlock()

	// Persist to disk
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	notifPath := filepath.Join(s.baseDir, bucket, ".notifications.json")
	return os.WriteFile(notifPath, data, 0600)
}

// GetBucketNotification returns notification config for a bucket.
func (s *Store) GetBucketNotification(bucket string) *types.BucketNotificationConfiguration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notifications[bucket]
}

// GetBucketConfig returns a copy of the bucket config (nil if not set).
func (s *Store) GetBucketConfig(bucket string) *types.BucketConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.configs[bucket]
}

// UpdateBucketConfig applies fn to the bucket's config under the write lock and persists to disk.
func (s *Store) UpdateBucketConfig(bucket string, fn func(*types.BucketConfig)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.buckets[bucket]; !exists {
		return fmt.Errorf("NoSuchBucket")
	}

	cfg := s.configs[bucket]
	if cfg == nil {
		cfg = &types.BucketConfig{}
	}
	fn(cfg)
	s.configs[bucket] = cfg

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.baseDir, bucket, ".config.json"), data, 0600)
}
