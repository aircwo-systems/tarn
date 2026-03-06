package types

import "time"

// Runtime represents a supported Lambda runtime.
type Runtime string

const (
	RuntimeNodeJS18  Runtime = "nodejs18.x"
	RuntimeNodeJS20  Runtime = "nodejs20.x"
	RuntimeNodeJS22  Runtime = "nodejs22.x"
	RuntimeNodeJS24  Runtime = "nodejs24.x"
	RuntimePython39  Runtime = "python3.9"
	RuntimePython310 Runtime = "python3.10"
	RuntimePython311 Runtime = "python3.11"
	RuntimePython312 Runtime = "python3.12"
	RuntimePython313 Runtime = "python3.13"
	RuntimeGo        Runtime = "provided.al2023"
	RuntimeDotNet6   Runtime = "dotnet6"
	RuntimeDotNet8   Runtime = "dotnet8"
	RuntimeJava11    Runtime = "java11"
	RuntimeJava17    Runtime = "java17"
	RuntimeJava21    Runtime = "java21"
	RuntimeRuby32    Runtime = "ruby3.2"
	RuntimeRuby33    Runtime = "ruby3.3"
)

// RuntimeImageMap maps runtimes to their official AWS base images.
var RuntimeImageMap = map[Runtime]string{
	RuntimeNodeJS18:  "public.ecr.aws/lambda/nodejs:18",
	RuntimeNodeJS20:  "public.ecr.aws/lambda/nodejs:20",
	RuntimeNodeJS22:  "public.ecr.aws/lambda/nodejs:22",
	RuntimeNodeJS24:  "public.ecr.aws/lambda/nodejs:24",
	RuntimePython39:  "public.ecr.aws/lambda/python:3.9",
	RuntimePython310: "public.ecr.aws/lambda/python:3.10",
	RuntimePython311: "public.ecr.aws/lambda/python:3.11",
	RuntimePython312: "public.ecr.aws/lambda/python:3.12",
	RuntimePython313: "public.ecr.aws/lambda/python:3.13",
	RuntimeGo:        "public.ecr.aws/lambda/provided:al2023",
	RuntimeDotNet6:   "public.ecr.aws/lambda/dotnet:6",
	RuntimeDotNet8:   "public.ecr.aws/lambda/dotnet:8",
	RuntimeJava11:    "public.ecr.aws/lambda/java:11",
	RuntimeJava17:    "public.ecr.aws/lambda/java:17",
	RuntimeJava21:    "public.ecr.aws/lambda/java:21",
	RuntimeRuby32:    "public.ecr.aws/lambda/ruby:3.2",
	RuntimeRuby33:    "public.ecr.aws/lambda/ruby:3.3",
}

// ValidRuntime checks if a runtime string is supported.
func ValidRuntime(r string) bool {
	_, ok := RuntimeImageMap[Runtime(r)]
	return ok
}

// FunctionState represents the current state of a Lambda function.
type FunctionState string

const (
	FunctionStatePending  FunctionState = "Pending"
	FunctionStateActive   FunctionState = "Active"
	FunctionStateInactive FunctionState = "Inactive"
	FunctionStateFailed   FunctionState = "Failed"
)

// Last update status values; used by the TF AWS provider waiter.
const (
	LastUpdateStatusInProgress = "InProgress"
	LastUpdateStatusPending    = "Pending"
	LastUpdateStatusSuccessful = "Successful"
	LastUpdateStatusFailed     = "Failed"
)

// DeadLetterConfig specifies the DLQ (SQS queue or SNS topic) for failed async invocations.
type DeadLetterConfig struct {
	TargetArn string `json:"TargetArn,omitempty"`
}

// FunctionConfig holds the configuration for a Lambda function.
type FunctionConfig struct {
	FunctionName     string            `json:"FunctionName"`
	FunctionArn      string            `json:"FunctionArn"`
	Runtime          Runtime           `json:"Runtime"`
	Handler          string            `json:"Handler"`
	Role             string            `json:"Role"`
	Description      string            `json:"Description,omitempty"`
	Timeout          int               `json:"Timeout"`
	MemorySize       int               `json:"MemorySize"`
	Environment      map[string]string `json:"Environment,omitempty"`
	Layers           []string          `json:"Layers,omitempty"`
	Tags             map[string]string `json:"Tags,omitempty"`
	DeadLetterConfig *DeadLetterConfig `json:"DeadLetterConfig,omitempty"`
	State            FunctionState     `json:"State"`
	LastUpdateStatus string            `json:"LastUpdateStatus,omitempty"`
	CodeSHA256       string            `json:"CodeSha256"`
	CodeSize         int64             `json:"CodeSize"`
	Version          string            `json:"Version"`
	LastModified     time.Time         `json:"LastModified"`
}

// LayerConfig holds the configuration for a Lambda layer.
type LayerConfig struct {
	LayerName          string   `json:"LayerName"`
	LayerArn           string   `json:"LayerArn"`
	LayerVersionArn    string   `json:"LayerVersionArn"`
	VersionNumber      int64    `json:"Version"`
	Description        string   `json:"Description,omitempty"`
	CodeSHA256         string   `json:"CodeSha256"`
	CodeSize           int64    `json:"CodeSize"`
	CompatibleRuntimes []string `json:"CompatibleRuntimes,omitempty"`
	CreatedDate        string   `json:"CreatedDate"`
}

// LayerVersionContent holds the content metadata returned after publishing.
type LayerVersionContent struct {
	CodeSHA256 string `json:"CodeSha256"`
	CodeSize   int64  `json:"CodeSize"`
}

// FunctionConfigUpdate holds optional fields for updating function configuration.
// Pointer types and zero-value checks allow distinguishing "not provided" from "set to zero."
type FunctionConfigUpdate struct {
	Handler          string            `json:"Handler,omitempty"`
	Description      *string           `json:"Description,omitempty"`
	Timeout          int               `json:"Timeout,omitempty"`
	MemorySize       int               `json:"MemorySize,omitempty"`
	Role             string            `json:"Role,omitempty"`
	Runtime          string            `json:"Runtime,omitempty"`
	Environment      *EnvVars          `json:"Environment,omitempty"`
	Layers           []string          `json:"Layers,omitempty"`
	DeadLetterConfig *DeadLetterConfig `json:"DeadLetterConfig,omitempty"`
}

// EnvVars wraps environment variable maps (matches AWS API shape).
type EnvVars struct {
	Variables map[string]string `json:"Variables,omitempty"`
}

// InvokeInput represents a Lambda invocation request.
type InvokeInput struct {
	FunctionName   string `json:"FunctionName"`
	Payload        []byte `json:"Payload,omitempty"`
	InvocationType string `json:"InvocationType,omitempty"` // RequestResponse, Event, DryRun
	LogType        string `json:"LogType,omitempty"`        // None, Tail
}

// InvokeOutput represents a Lambda invocation response.
type InvokeOutput struct {
	StatusCode      int    `json:"StatusCode"`
	Payload         []byte `json:"Payload,omitempty"`
	FunctionError   string `json:"FunctionError,omitempty"`
	LogResult       string `json:"LogResult,omitempty"`
	ExecutedVersion string `json:"ExecutedVersion,omitempty"`
}
