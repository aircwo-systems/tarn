package types

// QueueConfig holds the configuration for an SQS queue.
type QueueConfig struct {
	QueueName                     string            `json:"QueueName"`
	QueueUrl                      string            `json:"QueueUrl"`
	QueueArn                      string            `json:"QueueArn"`
	VisibilityTimeout             int               `json:"VisibilityTimeout"`
	MessageRetentionPeriod        int               `json:"MessageRetentionPeriod"`
	DelaySeconds                  int               `json:"DelaySeconds"`
	MaximumMessageSize            int               `json:"MaximumMessageSize"`
	ReceiveMessageWaitTimeSeconds int               `json:"ReceiveMessageWaitTimeSeconds"`
	FifoQueue                     bool              `json:"FifoQueue"`
	ContentBasedDeduplication     bool              `json:"ContentBasedDeduplication"`
	CreatedTimestamp              int64             `json:"CreatedTimestamp"`
	LastModifiedTimestamp         int64             `json:"LastModifiedTimestamp"`
	Tags                          map[string]string `json:"Tags,omitempty"`
	KmsMasterKeyId                string            `json:"KmsMasterKeyId,omitempty"`
	KmsDataKeyReusePeriodSeconds  int               `json:"KmsDataKeyReusePeriodSeconds,omitempty"`
	// Dead Letter Queue support
	RedrivePolicy       string `json:"RedrivePolicy,omitempty"`       // raw JSON redrive policy
	DeadLetterTargetArn string `json:"DeadLetterTargetArn,omitempty"` // parsed DLQ ARN
	MaxReceiveCount     int    `json:"MaxReceiveCount,omitempty"`      // parsed max receive count
}

// SQSMessage represents a message in an SQS queue.
type SQSMessage struct {
	MessageId                       string                       `json:"MessageId"`
	Body                            string                       `json:"Body"`
	MD5OfBody                       string                       `json:"MD5OfBody"`
	ReceiptHandle                   string                       `json:"ReceiptHandle,omitempty"`
	MessageAttributes               map[string]*MessageAttribute `json:"MessageAttributes,omitempty"`
	SentTimestamp                   int64                        `json:"SentTimestamp"`
	ApproximateReceiveCount         int                          `json:"ApproximateReceiveCount"`
	ApproximateFirstReceiveTimestamp int64                       `json:"ApproximateFirstReceiveTimestamp"`
	VisibleAt                       int64                        `json:"-"` // epoch ms: when message becomes visible
	MessageGroupId                  string                       `json:"MessageGroupId,omitempty"`
	MessageDeduplicationId          string                       `json:"MessageDeduplicationId,omitempty"`
	DelayUntil                      int64                        `json:"-"` // epoch ms: per-message delay
	ExpiresAt                       int64                        `json:"-"` // epoch ms: retention expiry
	Deleted                         bool                         `json:"-"`
	// Stale is set by OpenStack when a message has failed too many times with no DLQ configured.
	// Stale messages are never re-delivered; they remain visible in the UI until they expire.
	Stale                           bool                         `json:"-"`
}

// MessageAttribute represents a user-defined SQS message attribute.
type MessageAttribute struct {
	DataType    string `json:"DataType"`
	StringValue string `json:"StringValue,omitempty"`
	BinaryValue []byte `json:"BinaryValue,omitempty"`
}
