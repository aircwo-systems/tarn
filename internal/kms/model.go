package kms

import (
	"strconv"
	"time"
)

// EpochTime is a timestamp encoded as fractional epoch seconds, the JSON 1.1
// protocol's timestamp format.
type EpochTime time.Time

func (t EpochTime) MarshalJSON() ([]byte, error) {
	return strconv.AppendFloat(nil, float64(time.Time(t).UnixMilli())/1000, 'f', -1, 64), nil
}

// Tag is a KMS resource tag.
type Tag struct {
	TagKey   string `json:"TagKey"`
	TagValue string `json:"TagValue"`
}

// KeyMetadata is the DescribeKey shape. Terraform reads most of these fields,
// so they are all filled even where Tarn has only one possible value.
type KeyMetadata struct {
	AWSAccountId          string     `json:"AWSAccountId"`
	KeyId                 string     `json:"KeyId"`
	Arn                   string     `json:"Arn"`
	CreationDate          EpochTime  `json:"CreationDate"`
	Enabled               bool       `json:"Enabled"`
	Description           string     `json:"Description"`
	KeyUsage              string     `json:"KeyUsage"`
	KeyState              string     `json:"KeyState"`
	DeletionDate          *EpochTime `json:"DeletionDate,omitempty"`
	Origin                string     `json:"Origin"`
	KeyManager            string     `json:"KeyManager"`
	CustomerMasterKeySpec string     `json:"CustomerMasterKeySpec"`
	KeySpec               string     `json:"KeySpec"`
	EncryptionAlgorithms  []string   `json:"EncryptionAlgorithms"`
	MultiRegion           bool       `json:"MultiRegion"`
}

type CreateKeyInput struct {
	Description           string `json:"Description"`
	KeyUsage              string `json:"KeyUsage"`
	KeySpec               string `json:"KeySpec"`
	CustomerMasterKeySpec string `json:"CustomerMasterKeySpec"`
	Origin                string `json:"Origin"`
	Policy                string `json:"Policy"`
	MultiRegion           bool   `json:"MultiRegion"`
	Tags                  []Tag  `json:"Tags"`
}

type KeyMetadataOutput struct {
	KeyMetadata KeyMetadata `json:"KeyMetadata"`
}

// KeyIDInput is the request shape of every operation that takes only a key.
type KeyIDInput struct {
	KeyId string `json:"KeyId"`
}

type KeyIDOutput struct {
	KeyId string `json:"KeyId"`
}

type ListKeysInput struct {
	Limit  int    `json:"Limit"`
	Marker string `json:"Marker"`
}

type KeyListEntry struct {
	KeyId  string `json:"KeyId"`
	KeyArn string `json:"KeyArn"`
}

type ListKeysOutput struct {
	Keys      []KeyListEntry `json:"Keys"`
	Truncated bool           `json:"Truncated"`
}

type UpdateKeyDescriptionInput struct {
	KeyId       string `json:"KeyId"`
	Description string `json:"Description"`
}

type ScheduleKeyDeletionInput struct {
	KeyId               string `json:"KeyId"`
	PendingWindowInDays int    `json:"PendingWindowInDays"`
}

type ScheduleKeyDeletionOutput struct {
	KeyId               string    `json:"KeyId"`
	DeletionDate        EpochTime `json:"DeletionDate"`
	KeyState            string    `json:"KeyState"`
	PendingWindowInDays int       `json:"PendingWindowInDays"`
}

type AliasInput struct {
	AliasName   string `json:"AliasName"`
	TargetKeyId string `json:"TargetKeyId"`
}

type ListAliasesInput struct {
	KeyId  string `json:"KeyId"`
	Limit  int    `json:"Limit"`
	Marker string `json:"Marker"`
}

type AliasListEntry struct {
	AliasName       string    `json:"AliasName"`
	AliasArn        string    `json:"AliasArn"`
	TargetKeyId     string    `json:"TargetKeyId,omitempty"`
	CreationDate    EpochTime `json:"CreationDate"`
	LastUpdatedDate EpochTime `json:"LastUpdatedDate"`
}

type ListAliasesOutput struct {
	Aliases   []AliasListEntry `json:"Aliases"`
	Truncated bool             `json:"Truncated"`
}

type TagResourceInput struct {
	KeyId string `json:"KeyId"`
	Tags  []Tag  `json:"Tags"`
}

type UntagResourceInput struct {
	KeyId   string   `json:"KeyId"`
	TagKeys []string `json:"TagKeys"`
}

type ListResourceTagsInput struct {
	KeyId  string `json:"KeyId"`
	Limit  int    `json:"Limit"`
	Marker string `json:"Marker"`
}

type ListResourceTagsOutput struct {
	Tags      []Tag `json:"Tags"`
	Truncated bool  `json:"Truncated"`
}

type KeyPolicyInput struct {
	KeyId      string `json:"KeyId"`
	PolicyName string `json:"PolicyName"`
	Policy     string `json:"Policy"`
}

type GetKeyPolicyOutput struct {
	Policy     string `json:"Policy"`
	PolicyName string `json:"PolicyName"`
}

type ListKeyPoliciesOutput struct {
	PolicyNames []string `json:"PolicyNames"`
	Truncated   bool     `json:"Truncated"`
}

type KeyRotationInput struct {
	KeyId                string `json:"KeyId"`
	RotationPeriodInDays int    `json:"RotationPeriodInDays"`
}

type GetKeyRotationStatusOutput struct {
	KeyId                string `json:"KeyId"`
	KeyRotationEnabled   bool   `json:"KeyRotationEnabled"`
	RotationPeriodInDays int    `json:"RotationPeriodInDays,omitempty"`
}

type EncryptInput struct {
	KeyId               string            `json:"KeyId"`
	Plaintext           []byte            `json:"Plaintext"`
	EncryptionContext   map[string]string `json:"EncryptionContext"`
	EncryptionAlgorithm string            `json:"EncryptionAlgorithm"`
}

type EncryptOutput struct {
	CiphertextBlob      []byte `json:"CiphertextBlob"`
	KeyId               string `json:"KeyId"`
	EncryptionAlgorithm string `json:"EncryptionAlgorithm"`
}

type DecryptInput struct {
	CiphertextBlob      []byte            `json:"CiphertextBlob"`
	KeyId               string            `json:"KeyId"`
	EncryptionContext   map[string]string `json:"EncryptionContext"`
	EncryptionAlgorithm string            `json:"EncryptionAlgorithm"`
}

type DecryptOutput struct {
	KeyId               string `json:"KeyId"`
	Plaintext           []byte `json:"Plaintext"`
	EncryptionAlgorithm string `json:"EncryptionAlgorithm"`
}

type ReEncryptInput struct {
	CiphertextBlob               []byte            `json:"CiphertextBlob"`
	SourceKeyId                  string            `json:"SourceKeyId"`
	SourceEncryptionContext      map[string]string `json:"SourceEncryptionContext"`
	DestinationKeyId             string            `json:"DestinationKeyId"`
	DestinationEncryptionContext map[string]string `json:"DestinationEncryptionContext"`
}

type ReEncryptOutput struct {
	CiphertextBlob                 []byte `json:"CiphertextBlob"`
	SourceKeyId                    string `json:"SourceKeyId"`
	KeyId                          string `json:"KeyId"`
	SourceEncryptionAlgorithm      string `json:"SourceEncryptionAlgorithm"`
	DestinationEncryptionAlgorithm string `json:"DestinationEncryptionAlgorithm"`
}

type GenerateDataKeyInput struct {
	KeyId             string            `json:"KeyId"`
	KeySpec           string            `json:"KeySpec"`
	NumberOfBytes     int               `json:"NumberOfBytes"`
	EncryptionContext map[string]string `json:"EncryptionContext"`
}

type GenerateDataKeyOutput struct {
	CiphertextBlob []byte `json:"CiphertextBlob"`
	Plaintext      []byte `json:"Plaintext,omitempty"`
	KeyId          string `json:"KeyId"`
}

type GenerateRandomInput struct {
	NumberOfBytes int `json:"NumberOfBytes"`
}

type GenerateRandomOutput struct {
	Plaintext []byte `json:"Plaintext"`
}
