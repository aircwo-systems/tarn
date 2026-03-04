package types

import "time"

// APIGatewayAPI represents an API Gateway HTTP API (v2).
type APIGatewayAPI struct {
	APIID                    string            `json:"apiId"`
	Name                     string            `json:"name"`
	Description              string            `json:"description,omitempty"`
	ProtocolType             string            `json:"protocolType"`
	RouteSelectionExpression string            `json:"routeSelectionExpression,omitempty"`
	APIEndpoint              string            `json:"apiEndpoint,omitempty"`
	APIArn                   string            `json:"apiArn,omitempty"`
	Tags                     map[string]string `json:"tags,omitempty"`
	CreatedDate              time.Time         `json:"createdDate"`
}

// APIGatewayIntegration represents an integration attached to an HTTP API.
type APIGatewayIntegration struct {
	IntegrationID        string            `json:"integrationId"`
	APIID                string            `json:"apiId,omitempty"`
	IntegrationType      string            `json:"integrationType"`
	IntegrationMethod    string            `json:"integrationMethod,omitempty"`
	IntegrationURI       string            `json:"integrationUri"`
	PayloadFormatVersion string            `json:"payloadFormatVersion,omitempty"`
	TimeoutInMillis      int               `json:"timeoutInMillis,omitempty"`
	RequestParameters    map[string]string `json:"requestParameters,omitempty"`
	LambdaFunctionArn    string            `json:"lambdaFunctionArn,omitempty"`
	LambdaFunctionName   string            `json:"lambdaFunctionName,omitempty"`
	SQSQueueArn          string            `json:"sqsQueueArn,omitempty"`
	SQSQueueName         string            `json:"sqsQueueName,omitempty"`
	IntegrationArn       string            `json:"integrationArn,omitempty"`
	CreatedDate          time.Time         `json:"createdDate"`
}

// APIGatewayRoute represents an API route.
type APIGatewayRoute struct {
	RouteID     string    `json:"routeId"`
	APIID       string    `json:"apiId,omitempty"`
	RouteKey    string    `json:"routeKey"`
	Target      string    `json:"target,omitempty"`
	RouteArn    string    `json:"routeArn,omitempty"`
	CreatedDate time.Time `json:"createdDate"`
}

// APIGatewayStage represents an API stage.
type APIGatewayStage struct {
	StageName       string    `json:"stageName"`
	APIID           string    `json:"apiId,omitempty"`
	AutoDeploy      bool      `json:"autoDeploy"`
	Description     string    `json:"description,omitempty"`
	InvokeURL       string    `json:"invokeUrl,omitempty"`
	StageArn        string    `json:"stageArn,omitempty"`
	CreatedDate     time.Time `json:"createdDate"`
	LastUpdatedDate time.Time `json:"lastUpdatedDate"`
}
