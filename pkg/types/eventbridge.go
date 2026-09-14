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
	EcsParameters    *EcsParameters    `json:"EcsParameters,omitempty"`
	LastResult       string            `json:"LastResult,omitempty"`
	LastInvokedAt    *time.Time        `json:"LastInvokedAt,omitempty"`
}

// EcsParameters describes the ECS RunTask request associated with an
// EventBridge target. The target's Arn is the ECS cluster ARN; these fields
// identify the task definition and the per-run container overrides layered on
// top of it.
type EcsParameters struct {
	TaskDefinitionArn  string              `json:"TaskDefinitionArn"`
	TaskCount          int                 `json:"TaskCount,omitempty"`
	LaunchType         string              `json:"LaunchType,omitempty"`
	ContainerOverrides []ContainerOverride `json:"ContainerOverrides,omitempty"`
	// NetworkConfiguration is required for awsvpc-mode task definitions and
	// is a ForceNew attribute on Terraform's aws_cloudwatch_event_target
	// resource; failing to echo back what PutTargets was given makes every
	// subsequent plan see drift and replace the target. This type mirrors
	// EventBridge's own wire shape exactly (there is no separate wire.go
	// translation layer for eventbridge, unlike internal/api/ecs), which is
	// NOT the same casing ECS's own NetworkConfiguration uses on
	// DescribeServices: EventBridge nests an "awsvpcConfiguration" object
	// (lowercase leading letter) whose Subnets/SecurityGroups/AssignPublicIp
	// stay PascalCase, under an outer PascalCase "NetworkConfiguration".
	NetworkConfiguration *EcsNetworkConfiguration `json:"NetworkConfiguration,omitempty"`
}

// EcsNetworkConfiguration mirrors the EventBridge (not ECS) wire shape for an
// ECS target's network configuration.
type EcsNetworkConfiguration struct {
	AwsvpcConfiguration *EcsAwsVpcConfiguration `json:"awsvpcConfiguration,omitempty"`
}

// EcsAwsVpcConfiguration mirrors the EventBridge wire shape for the nested
// awsvpcConfiguration object.
type EcsAwsVpcConfiguration struct {
	Subnets        []string `json:"Subnets,omitempty"`
	SecurityGroups []string `json:"SecurityGroups,omitempty"`
	AssignPublicIp string   `json:"AssignPublicIp,omitempty"`
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
