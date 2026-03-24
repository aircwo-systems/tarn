package types

import "time"

// FilterCriteriaFilter is a single filter pattern within FilterCriteria.
// Pattern is a JSON-encoded string of field conditions, e.g.:
//
//	{"body": {"type": ["order", "payment"]}}
type FilterCriteriaFilter struct {
	Pattern string `json:"Pattern"`
}

// FilterCriteria holds the filter rules for an event source mapping.
// Multiple Filters are OR'd: a message matches if any pattern matches.
type FilterCriteria struct {
	Filters []FilterCriteriaFilter `json:"Filters"`
}

// EventSourceMapping represents an SQS→Lambda event source mapping.
type EventSourceMapping struct {
	UUID                           string          `json:"UUID"`
	EventSourceArn                 string          `json:"EventSourceArn"`
	FunctionArn                    string          `json:"FunctionArn"`
	FunctionName                   string          `json:"FunctionName"`
	QueueName                      string          `json:"QueueName"`
	BatchSize                      int             `json:"BatchSize"`
	MaximumBatchingWindowInSeconds int             `json:"MaximumBatchingWindowInSeconds"`
	Enabled                        bool            `json:"Enabled"`
	State                          string          `json:"State"`
	LastProcessingResult           string          `json:"LastProcessingResult,omitempty"`
	LastModified                   time.Time       `json:"LastModified"`
	FilterCriteria                 *FilterCriteria `json:"FilterCriteria,omitempty"`
}
