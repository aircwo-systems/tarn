package types

import "time"

const (
	EventBridgeDefaultBusARNSuffix = "event-bus/default"

	EventBridgeRuleStateEnabled  = "ENABLED"
	EventBridgeRuleStateDisabled = "DISABLED"
)

// EventBridgeRule models an EventBridge rule.
// Rules may have a ScheduleExpression (scheduled rules) or an EventPattern
// (event-matching rules), but not both.
type EventBridgeRule struct {
	Name               string              `json:"Name"`
	Arn                string              `json:"Arn"`
	EventBusName       string              `json:"EventBusName"`
	Description        string              `json:"Description,omitempty"`
	ScheduleExpression string              `json:"ScheduleExpression,omitempty"`
	EventPattern       string              `json:"EventPattern,omitempty"`
	State              string              `json:"State"`
	Tags               map[string]string   `json:"Tags,omitempty"`
	RoleArn            string              `json:"RoleArn,omitempty"`
	CreatedAt          time.Time           `json:"CreatedAt"`
	LastModifiedAt     time.Time           `json:"LastModifiedAt"`
	LastRunAt          *time.Time          `json:"LastRunAt,omitempty"`
	NextRunAt          *time.Time          `json:"NextRunAt,omitempty"`
	LastResult         string              `json:"LastResult,omitempty"`
	ScheduleAnchor     time.Time           `json:"ScheduleAnchor"`
	Targets            []EventBridgeTarget `json:"Targets,omitempty"`
}

// EventBridgeTarget models one target attached to an EventBridge rule.
type EventBridgeTarget struct {
	ID               string            `json:"Id"`
	Arn              string            `json:"Arn"`
	RoleArn          string            `json:"RoleArn,omitempty"`
	Input            string            `json:"Input,omitempty"`
	InputPath        string            `json:"InputPath,omitempty"`
	InputTransformer *InputTransformer `json:"InputTransformer,omitempty"`
	LastResult       string            `json:"LastResult,omitempty"`
	LastInvokedAt    *time.Time        `json:"LastInvokedAt,omitempty"`
}

// InputTransformer mirrors the AWS EventBridge shape for target input transforms.
type InputTransformer struct {
	InputPathsMap map[string]string `json:"InputPathsMap,omitempty"`
	InputTemplate string            `json:"InputTemplate,omitempty"`
}

// PutEventsEntry is one event in a PutEvents request.
type PutEventsEntry struct {
	Source       string   `json:"Source"`
	DetailType   string   `json:"DetailType"`
	Detail       string   `json:"Detail"`
	EventBusName string   `json:"EventBusName,omitempty"`
	Resources    []string `json:"Resources,omitempty"`
	Time         string   `json:"Time,omitempty"`
	TraceHeader  string   `json:"TraceHeader,omitempty"`
}

// PutEventsResultEntry is one result from a PutEvents response.
type PutEventsResultEntry struct {
	EventId      string `json:"EventId,omitempty"`
	ErrorCode    string `json:"ErrorCode,omitempty"`
	ErrorMessage string `json:"ErrorMessage,omitempty"`
}
