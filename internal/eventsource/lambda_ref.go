package eventsource

import (
	lambdasvc "github.com/aircwo-systems/tarn/internal/lambda"
	"strings"
)

// normalizeLambdaFunctionName returns a Lambda function name from either
// a plain function name or a Lambda function ARN.
//
// Examples:
// - "order-logger" -> "order-logger"
// - "arn:aws:lambda:us-east-1:000000000000:function:order-logger" -> "order-logger"
// - "arn:aws:lambda:us-east-1:000000000000:function:order-logger:live" -> "order-logger:live"
func normalizeLambdaFunctionName(reference string) string {
	name, qualifier, err := lambdasvc.ParseFunctionReference(reference, "")
	if err != nil {
		return strings.TrimSpace(reference)
	}
	if qualifier != "" {
		name += ":" + qualifier
	}
	return name
}
