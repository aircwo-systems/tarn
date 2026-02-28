package types

import "time"

// Secret represents an AWS Secrets Manager secret.
type Secret struct {
	ARN              string            `json:"ARN"`
	Name             string            `json:"Name"`
	Description      string            `json:"Description,omitempty"`
	SecretString     string            `json:"SecretString,omitempty"`
	SecretBinary     []byte            `json:"SecretBinary,omitempty"`
	VersionId        string            `json:"VersionId"`
	VersionStages    []string          `json:"VersionStages"`
	Tags             []SecretTag       `json:"Tags,omitempty"`
	CreatedDate      time.Time         `json:"CreatedDate"`
	LastChangedDate  time.Time         `json:"LastChangedDate"`
	LastAccessedDate time.Time         `json:"LastAccessedDate"`
}

// SecretTag is a key-value tag on a secret (AWS uses array of objects, not map).
type SecretTag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}
