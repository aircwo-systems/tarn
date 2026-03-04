package eventsource

import "strings"

// normalizeLambdaFunctionName returns a Lambda function name from either
// a plain function name or a Lambda function ARN.
//
// Examples:
// - "order-logger" -> "order-logger"
// - "arn:aws:lambda:us-east-1:000000000000:function:order-logger" -> "order-logger"
// - "arn:aws:lambda:us-east-1:000000000000:function:order-logger:live" -> "order-logger"
func normalizeLambdaFunctionName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref
	}

	const marker = ":function:"
	if !strings.HasPrefix(ref, "arn:") {
		return ref
	}
	i := strings.Index(ref, marker)
	if i == -1 {
		return ref
	}

	name := ref[i+len(marker):]
	if j := strings.IndexByte(name, ':'); j != -1 {
		name = name[:j]
	}
	if name == "" {
		return ref
	}
	return name
}
