package s3

import (
	"fmt"
	"io"
	"regexp"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

var bucketNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// Service implements S3 business logic.
type Service struct {
	cfg   *config.Config
	store *Store
}

// NewService creates a new S3 service.
func NewService(cfg *config.Config) *Service {
	return &Service{
		cfg:   cfg,
		store: NewStore(cfg.S3Dir()),
	}
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

func (s *Service) generateARN(bucketName string) string {
	return fmt.Sprintf("arn:aws:s3:::%s", bucketName)
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
	return s.store.PutObject(bucket, key, contentType, body, metadata)
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
	return s.store.DeleteObject(bucket, key)
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
