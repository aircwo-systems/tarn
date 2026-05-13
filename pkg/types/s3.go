package types

import "time"

// Bucket represents an S3 bucket.
type Bucket struct {
	Name         string    `json:"Name"`
	CreationDate time.Time `json:"CreationDate"`
	Region       string    `json:"Region"`
	Tags         map[string]string `json:"Tags,omitempty"`
}

// Object represents metadata for an S3 object.
type Object struct {
	Key          string            `json:"Key"`
	Size         int64             `json:"Size"`
	ETag         string            `json:"ETag"`
	ContentType  string            `json:"ContentType"`
	LastModified time.Time         `json:"LastModified"`
	Metadata     map[string]string `json:"Metadata,omitempty"`
}

// ListResult holds the response for ListObjectsV2.
type ListResult struct {
	Name                  string   `json:"Name"`
	Prefix                string   `json:"Prefix"`
	Delimiter             string   `json:"Delimiter"`
	MaxKeys               int      `json:"MaxKeys"`
	IsTruncated           bool     `json:"IsTruncated"`
	Contents              []Object `json:"Contents"`
	CommonPrefixes        []string `json:"CommonPrefixes"`
	NextContinuationToken string   `json:"NextContinuationToken,omitempty"`
	KeyCount              int      `json:"KeyCount"`
}

// DeleteError represents an error deleting a single object in a batch delete.
type DeleteError struct {
	Key     string `json:"Key"`
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

// S3EventName is a type for S3 event notification event names.
type S3EventName string

const (
	S3EventObjectCreatedPut    S3EventName = "s3:ObjectCreated:Put"
	S3EventObjectRemovedDelete S3EventName = "s3:ObjectRemoved:Delete"
)

// S3LambdaNotification configures Lambda invocation on S3 events.
type S3LambdaNotification struct {
	ID                 string        `json:"Id"`
	LambdaFunctionArn  string        `json:"LambdaFunctionArn"`
	LambdaFunctionName string        `json:"LambdaFunctionName"`
	Events             []S3EventName `json:"Events"`
	FilterPrefix       string        `json:"FilterPrefix,omitempty"`
	FilterSuffix       string        `json:"FilterSuffix,omitempty"`
}

// BucketNotificationConfiguration holds all notification configs for a bucket.
type BucketNotificationConfiguration struct {
	LambdaConfigurations []S3LambdaNotification `json:"LambdaFunctionConfigurations,omitempty"`
}

// BucketConfig stores all mutable bucket configuration settings.
type BucketConfig struct {
	Versioning        *BucketVersioning        `json:"Versioning,omitempty"`
	ACL               string                   `json:"ACL,omitempty"`
	Tags              map[string]string        `json:"Tags,omitempty"`
	Policy            string                   `json:"Policy,omitempty"`
	CORS              []CORSRule               `json:"CORS,omitempty"`
	Encryption        *BucketEncryption        `json:"Encryption,omitempty"`
	PublicAccessBlock *PublicAccessBlockConfig `json:"PublicAccessBlock,omitempty"`
	Lifecycle         []LifecycleRule          `json:"Lifecycle,omitempty"`
	Logging           *BucketLogging           `json:"Logging,omitempty"`
	OwnershipControls string                   `json:"OwnershipControls,omitempty"`
	ObjectLock        *ObjectLockConfig        `json:"ObjectLock,omitempty"`
}

// BucketVersioning holds the versioning state for a bucket.
type BucketVersioning struct {
	Status    string `json:"Status"`              // "Enabled" | "Suspended"
	MFADelete string `json:"MFADelete,omitempty"` // "Enabled" | "Disabled"
}

// CORSRule defines a single CORS rule.
type CORSRule struct {
	ID             string   `json:"ID,omitempty"`
	AllowedHeaders []string `json:"AllowedHeaders,omitempty"`
	AllowedMethods []string `json:"AllowedMethods"`
	AllowedOrigins []string `json:"AllowedOrigins"`
	ExposeHeaders  []string `json:"ExposeHeaders,omitempty"`
	MaxAgeSeconds  int      `json:"MaxAgeSeconds,omitempty"`
}

// BucketEncryption holds the default SSE configuration.
type BucketEncryption struct {
	Rules []SSERule `json:"Rules"`
}

// SSERule defines a single server-side encryption rule.
type SSERule struct {
	Algorithm        string `json:"Algorithm"`
	KMSMasterKeyID   string `json:"KMSMasterKeyID,omitempty"`
	BucketKeyEnabled bool   `json:"BucketKeyEnabled,omitempty"`
}

// PublicAccessBlockConfig holds the public access block settings.
type PublicAccessBlockConfig struct {
	BlockPublicAcls       bool `json:"BlockPublicAcls"`
	IgnorePublicAcls      bool `json:"IgnorePublicAcls"`
	BlockPublicPolicy     bool `json:"BlockPublicPolicy"`
	RestrictPublicBuckets bool `json:"RestrictPublicBuckets"`
}

// LifecycleRule defines a single bucket lifecycle rule.
type LifecycleRule struct {
	ID                             string                          `json:"ID,omitempty"`
	Status                         string                          `json:"Status"`
	Prefix                         string                          `json:"Prefix,omitempty"`
	Expiration                     *LifecycleExpiration            `json:"Expiration,omitempty"`
	NoncurrentVersionExpiration    *NoncurrentVersionExpiration    `json:"NoncurrentVersionExpiration,omitempty"`
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUpload `json:"AbortIncompleteMultipartUpload,omitempty"`
}

// LifecycleExpiration defines when objects expire.
type LifecycleExpiration struct {
	Days                      int    `json:"Days,omitempty"`
	Date                      string `json:"Date,omitempty"`
	ExpiredObjectDeleteMarker bool   `json:"ExpiredObjectDeleteMarker,omitempty"`
}

// NoncurrentVersionExpiration defines when non-current versions expire.
type NoncurrentVersionExpiration struct {
	NoncurrentDays int `json:"NoncurrentDays"`
}

// AbortIncompleteMultipartUpload defines when to abort incomplete MPU.
type AbortIncompleteMultipartUpload struct {
	DaysAfterInitiation int `json:"DaysAfterInitiation"`
}

// BucketLogging configures access log delivery to another bucket.
type BucketLogging struct {
	TargetBucket string `json:"TargetBucket"`
	TargetPrefix string `json:"TargetPrefix,omitempty"`
}

// ObjectLockConfig holds the object lock configuration.
type ObjectLockConfig struct {
	ObjectLockEnabled string          `json:"ObjectLockEnabled"` // "Enabled"
	Rule              *ObjectLockRule `json:"Rule,omitempty"`
}

// ObjectLockRule defines the default retention settings.
type ObjectLockRule struct {
	DefaultRetention ObjectLockRetention `json:"DefaultRetention"`
}

// ObjectLockRetention specifies the default retention mode and period.
type ObjectLockRetention struct {
	Mode  string `json:"Mode"` // "GOVERNANCE" | "COMPLIANCE"
	Days  int    `json:"Days,omitempty"`
	Years int    `json:"Years,omitempty"`
}
