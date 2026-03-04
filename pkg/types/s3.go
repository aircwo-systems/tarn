package types

import "time"

// Bucket represents an S3 bucket.
type Bucket struct {
	Name         string    `json:"Name"`
	CreationDate time.Time `json:"CreationDate"`
	Region       string    `json:"Region"`
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
