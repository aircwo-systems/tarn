package types

import "time"

// APIGatewayAPI represents an API Gateway HTTP API (v2).
type APIGatewayAPI struct {
	APIID                    string            `json:"ApiId"`
	Name                     string            `json:"Name"`
	Description              string            `json:"Description,omitempty"`
	ProtocolType             string            `json:"ProtocolType"`
	RouteSelectionExpression string            `json:"RouteSelectionExpression,omitempty"`
	APIEndpoint              string            `json:"ApiEndpoint,omitempty"`
	APIArn                   string            `json:"ApiArn,omitempty"`
	Tags                     map[string]string `json:"Tags,omitempty"`
	CreatedDate              time.Time         `json:"CreatedDate"`
}

// APIGatewayIntegration represents an integration attached to an HTTP API.
type APIGatewayIntegration struct {
	IntegrationID        string    `json:"IntegrationId"`
	APIID                string    `json:"ApiId,omitempty"`
	IntegrationType      string    `json:"IntegrationType"`
	IntegrationMethod    string    `json:"IntegrationMethod,omitempty"`
	IntegrationURI       string    `json:"IntegrationUri"`
	PayloadFormatVersion string    `json:"PayloadFormatVersion,omitempty"`
	TimeoutInMillis      int       `json:"TimeoutInMillis,omitempty"`
	LambdaFunctionArn    string    `json:"LambdaFunctionArn,omitempty"`
	LambdaFunctionName   string    `json:"LambdaFunctionName,omitempty"`
	IntegrationArn       string    `json:"IntegrationArn,omitempty"`
	CreatedDate          time.Time `json:"CreatedDate"`
}

// APIGatewayRoute represents an API route.
type APIGatewayRoute struct {
	RouteID     string    `json:"RouteId"`
	APIID       string    `json:"ApiId,omitempty"`
	RouteKey    string    `json:"RouteKey"`
	Target      string    `json:"Target,omitempty"`
	RouteArn    string    `json:"RouteArn,omitempty"`
	CreatedDate time.Time `json:"CreatedDate"`
}

// APIGatewayStage represents an API stage.
type APIGatewayStage struct {
	StageName       string    `json:"StageName"`
	APIID           string    `json:"ApiId,omitempty"`
	AutoDeploy      bool      `json:"AutoDeploy"`
	Description     string    `json:"Description,omitempty"`
	InvokeURL       string    `json:"InvokeUrl,omitempty"`
	StageArn        string    `json:"StageArn,omitempty"`
	CreatedDate     time.Time `json:"CreatedDate"`
	LastUpdatedDate time.Time `json:"LastUpdatedDate"`
}
