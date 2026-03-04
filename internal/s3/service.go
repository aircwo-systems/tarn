package s3

import (
	"fmt"
	"io"
	"regexp"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

var bucketNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// S3EventCallback is called when an S3 event occurs (PutObject, DeleteObject).
type S3EventCallback func(eventName string, bucket, key string, size int64, etag string)

// Service implements S3 business logic.
type Service struct {
	cfg           *config.Config
	store         *Store
	eventCallback S3EventCallback
}

// NewService creates a new S3 service.
func NewService(cfg *config.Config) *Service {
	return &Service{
		cfg:   cfg,
		store: NewStore(cfg.S3Dir()),
	}
}

// SetEventCallback sets the callback invoked on PutObject/DeleteObject events.
func (s *Service) SetEventCallback(cb S3EventCallback) {
	s.eventCallback = cb
}

// Init ensures the S3 storage directory exists and loads state.
func (s *Service) Init() error {
	return s.store.Init()
}

func (s *Service) validateBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("InvalidBucketName: bucket name must be between 3 and 63 characters")
	}
	if !bucketNameRe.MatchString(name) {
		return fmt.Errorf("InvalidBucketName: bucket name must be lowercase alphanumeric, hyphens, or periods")
	}
	return nil
}

// CreateBucket creates a new bucket.
func (s *Service) CreateBucket(name string) (*types.Bucket, error) {
	if err := s.validateBucketName(name); err != nil {
		return nil, err
	}
	return s.store.CreateBucket(name, s.cfg.Region)
}

// HeadBucket checks if a bucket exists.
func (s *Service) HeadBucket(name string) error {
	return s.store.HeadBucket(name)
}

// DeleteBucket removes a bucket.
func (s *Service) DeleteBucket(name string) error {
	return s.store.DeleteBucket(name)
}

// ListBuckets returns all buckets.
func (s *Service) ListBuckets() []types.Bucket {
	return s.store.ListBuckets()
}

// PutObject stores an object.
func (s *Service) PutObject(bucket, key, contentType string, body io.Reader, metadata map[string]string) (*types.Object, error) {
	obj, err := s.store.PutObject(bucket, key, contentType, body, metadata)
	if err != nil {
		return nil, err
	}
	if s.eventCallback != nil {
		s.eventCallback("s3:ObjectCreated:Put", bucket, key, obj.Size, obj.ETag)
	}
	return obj, nil
}

// GetObject retrieves an object.
func (s *Service) GetObject(bucket, key string) (*types.Object, io.ReadCloser, error) {
	return s.store.GetObject(bucket, key)
}

// HeadObject returns object metadata.
func (s *Service) HeadObject(bucket, key string) (*types.Object, error) {
	return s.store.HeadObject(bucket, key)
}

// DeleteObject removes an object.
func (s *Service) DeleteObject(bucket, key string) error {
	err := s.store.DeleteObject(bucket, key)
	if err != nil {
		return err
	}
	if s.eventCallback != nil {
		s.eventCallback("s3:ObjectRemoved:Delete", bucket, key, 0, "")
	}
	return nil
}

// DeleteObjects removes multiple objects.
func (s *Service) DeleteObjects(bucket string, keys []string) []types.DeleteError {
	return s.store.DeleteObjects(bucket, keys)
}

// CopyObject copies an object.
func (s *Service) CopyObject(srcBucket, srcKey, dstBucket, dstKey string) (*types.Object, error) {
	return s.store.CopyObject(srcBucket, srcKey, dstBucket, dstKey)
}

// ListObjects lists objects in a bucket.
func (s *Service) ListObjects(bucket, prefix, delimiter, continuationToken string, maxKeys int) (*types.ListResult, error) {
	return s.store.ListObjects(bucket, prefix, delimiter, continuationToken, maxKeys)
}

// ObjectCount returns the number of objects in a bucket.
func (s *Service) ObjectCount(bucket string) int {
	return s.store.ObjectCount(bucket)
}

// TotalSize returns the total size of objects in a bucket.
func (s *Service) TotalSize(bucket string) int64 {
	return s.store.TotalSize(bucket)
}

// PutBucketNotificationConfiguration stores notification config for a bucket.
func (s *Service) PutBucketNotificationConfiguration(bucket string, cfg *types.BucketNotificationConfiguration) error {
	return s.store.PutBucketNotification(bucket, cfg)
}

// GetBucketNotificationConfiguration returns notification config for a bucket.
func (s *Service) GetBucketNotificationConfiguration(bucket string) *types.BucketNotificationConfiguration {
	return s.store.GetBucketNotification(bucket)
}
