package s3

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openstack-project/openstack/pkg/types"
)

// Store is a filesystem-backed S3 object store.
type Store struct {
	mu            sync.RWMutex
	baseDir       string
	buckets       map[string]*bucketState
	notifications map[string]*types.BucketNotificationConfiguration
}

type bucketState struct {
	mu   sync.RWMutex
	meta *types.Bucket
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

		// Load notification config if present
		notifPath := filepath.Join(s.baseDir, name, ".notifications.json")
		notifData, err := os.ReadFile(notifPath)
		if err == nil {
			var cfg types.BucketNotificationConfiguration
			if err := json.Unmarshal(notifData, &cfg); err == nil {
				s.notifications[name] = &cfg
			}
		}
	}

	return nil
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

	data, _ := json.Marshal(bucket)
	if err := os.WriteFile(filepath.Join(dir, ".meta.json"), data, 0644); err != nil {
		return nil, fmt.Errorf("write bucket meta: %w", err)
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
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		_ = os.Remove(objPath)
		return nil, fmt.Errorf("write object meta: %w", err)
	}

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

	encoded := encodeKey(key)
	_ = os.Remove(filepath.Join(s.objectsDir(bucket), encoded))
	_ = os.Remove(filepath.Join(s.objmetaDir(bucket), encoded+".json"))
	return nil
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
		encoded := encodeKey(key)
		_ = os.Remove(filepath.Join(s.objectsDir(bucket), encoded))
		_ = os.Remove(filepath.Join(s.objmetaDir(bucket), encoded+".json"))
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

// ObjectCount returns the number of objects in a bucket.
func (s *Store) ObjectCount(bucket string) int {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return 0
	}

	bs.mu.RLock()
	defer bs.mu.RUnlock()

	entries, err := os.ReadDir(s.objectsDir(bucket))
	if err != nil {
		return 0
	}
	return len(entries)
}

// TotalSize returns the total size of all objects in a bucket.
func (s *Store) TotalSize(bucket string) int64 {
	s.mu.RLock()
	bs, exists := s.buckets[bucket]
	s.mu.RUnlock()
	if !exists {
		return 0
	}

	bs.mu.RLock()
	defer bs.mu.RUnlock()

	var total int64
	entries, err := os.ReadDir(s.objectsDir(bucket))
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
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
	return os.WriteFile(notifPath, data, 0644)
}

// GetBucketNotification returns notification config for a bucket.
func (s *Store) GetBucketNotification(bucket string) *types.BucketNotificationConfiguration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notifications[bucket]
}
