package types

import (
	"encoding/json"
	"math"
	"strconv"
	"time"
)

// UnixTime wraps time.Time and marshals/unmarshals as a Unix epoch float64,
// matching the AWS API Gateway REST API wire format (e.g. 1609459200.0).
type UnixTime struct{ time.Time }

func (u UnixTime) MarshalJSON() ([]byte, error) {
	f := float64(u.Unix())
	return []byte(strconv.FormatFloat(f, 'f', -1, 64)), nil
}

func (u *UnixTime) UnmarshalJSON(data []byte) error {
	// Accept a number (stored on disk) or a quoted RFC3339 string (legacy).
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return err
		}
		u.Time = t
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	secs := int64(f)
	nsecs := int64((f - math.Trunc(f)) * 1e9)
	u.Time = time.Unix(secs, nsecs).UTC()
	return nil
}

// RestAPI represents an API Gateway REST API (v1).
type RestAPI struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	RootResourceID string            `json:"rootResourceId"`
	APIArn         string            `json:"apiArn,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	CreatedDate    UnixTime          `json:"createdDate"`
}

// RestResource represents a resource (path segment) in the REST API resource tree.
type RestResource struct {
	ID           string `json:"id"`
	RestAPIID    string `json:"restApiId,omitempty"`
	ParentID     string `json:"parentId,omitempty"`
	PathPart     string `json:"pathPart,omitempty"`
	Path         string `json:"path"`
	ResourceArn  string `json:"resourceArn,omitempty"`
}

// RestMethod represents an HTTP method on a resource.
type RestMethod struct {
	HTTPMethod        string            `json:"httpMethod"`
	ResourceID        string            `json:"resourceId,omitempty"`
	RestAPIID         string            `json:"restApiId,omitempty"`
	AuthorizationType string            `json:"authorizationType"`
	RequestParameters map[string]bool   `json:"requestParameters,omitempty"`
}

// RestIntegration represents a backend integration for a REST API method.
type RestIntegration struct {
	Type                  string            `json:"type"`
	HTTPMethod            string            `json:"httpMethod,omitempty"`
	URI                   string            `json:"uri,omitempty"`
	ResourceID            string            `json:"resourceId,omitempty"`
	RestAPIID             string            `json:"restApiId,omitempty"`
	MethodHTTPMethod      string            `json:"methodHttpMethod,omitempty"`
	RequestParameters     map[string]string `json:"requestParameters,omitempty"`
	RequestTemplates      map[string]string `json:"requestTemplates,omitempty"`
	// Resolved backend fields (not serialized to API response)
	LambdaFunctionName string `json:"lambdaFunctionName,omitempty"`
	SQSQueueName       string `json:"sqsQueueName,omitempty"`
}

// RestMethodResponse represents a response definition for a REST API method.
type RestMethodResponse struct {
	StatusCode     string            `json:"statusCode"`
	ResourceID     string            `json:"resourceId,omitempty"`
	RestAPIID      string            `json:"restApiId,omitempty"`
	HTTPMethod     string            `json:"httpMethod,omitempty"`
	ResponseModels map[string]string `json:"responseModels,omitempty"`
}

// RestIntegrationResponse represents an integration response mapping.
type RestIntegrationResponse struct {
	StatusCode        string            `json:"statusCode"`
	SelectionPattern  string            `json:"selectionPattern,omitempty"`
	ResourceID        string            `json:"resourceId,omitempty"`
	RestAPIID         string            `json:"restApiId,omitempty"`
	HTTPMethod        string            `json:"httpMethod,omitempty"`
	ResponseTemplates map[string]string `json:"responseTemplates,omitempty"`
}

// RestDeployment represents a deployment of a REST API.
type RestDeployment struct {
	ID          string   `json:"id"`
	RestAPIID   string   `json:"restApiId,omitempty"`
	Description string   `json:"description,omitempty"`
	CreatedDate UnixTime `json:"createdDate"`
}

// RestStage represents a stage of a REST API deployment.
type RestStage struct {
	StageName    string   `json:"stageName"`
	RestAPIID    string   `json:"restApiId,omitempty"`
	DeploymentID string   `json:"deploymentId"`
	Description  string   `json:"description,omitempty"`
	InvokeURL    string   `json:"invokeUrl,omitempty"`
	StageArn     string   `json:"stageArn,omitempty"`
	CreatedDate  UnixTime `json:"createdDate"`
}
