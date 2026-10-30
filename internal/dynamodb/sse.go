package dynamodb

import (
	"fmt"
	"strings"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// sseDescription turns an SSESpecification into the SSEDescription DynamoDB
// reports. Tables are always encrypted at rest in AWS; without a KMS
// specification they use an AWS owned key and report no SSEDescription.
// Tarn records the key but encrypts nothing with it.
func sseDescription(cfg *config.Config, spec *types.DynamoDBSSESpecification) (*types.DynamoDBSSEDescription, error) {
	if spec == nil || spec.Enabled == nil || !*spec.Enabled {
		if spec != nil && spec.KMSMasterKeyId != "" {
			return nil, validationError("One or more parameter values were invalid: KMSMasterKeyId can only be specified when SSE is enabled")
		}
		return nil, nil
	}
	if spec.SSEType != "" && spec.SSEType != "KMS" {
		return nil, validationError("One or more parameter values were invalid: Unsupported SSEType %s, only KMS is supported", spec.SSEType)
	}
	return &types.DynamoDBSSEDescription{
		Status:          "ENABLED",
		SSEType:         "KMS",
		KMSMasterKeyArn: kmsKeyARN(cfg, spec.KMSMasterKeyId),
	}, nil
}

// kmsKeyARN expands a key ID or alias to an ARN in the table's account and
// region. No key means the AWS managed key, alias/aws/dynamodb.
func kmsKeyARN(cfg *config.Config, keyID string) string {
	keyID = strings.TrimSpace(keyID)
	switch {
	case strings.HasPrefix(keyID, "arn:"):
		return keyID
	case keyID == "":
		keyID = "alias/aws/dynamodb"
	case !strings.HasPrefix(keyID, "alias/"):
		keyID = "key/" + keyID
	}
	return fmt.Sprintf("arn:aws:kms:%s:%s:%s", cfg.Region, cfg.AccountID, keyID)
}
