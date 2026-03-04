package types

import "time"

// EventSourceMapping represents an SQS→Lambda event source mapping.
type EventSourceMapping struct {
	UUID                            string    `json:"UUID"`
	EventSourceArn                  string    `json:"EventSourceArn"`
	FunctionArn                     string    `json:"FunctionArn"`
	FunctionName                    string    `json:"FunctionName"`
	QueueName                       string    `json:"QueueName"`
	BatchSize                       int       `json:"BatchSize"`
	MaximumBatchingWindowInSeconds  int       `json:"MaximumBatchingWindowInSeconds"`
	Enabled                         bool      `json:"Enabled"`
	State                           string    `json:"State"`
	LastProcessingResult            string    `json:"LastProcessingResult,omitempty"`
	LastModified                    time.Time `json:"LastModified"`
}
