package trace

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const correlationIDKey = "correlationId"

func NewCorrelationID() string {
	return uuid.NewString()
}

func CorrelationIDFromHeaders(headers http.Header) string {
	if headers == nil {
		return ""
	}
	for _, key := range []string{
		"X-Correlation-Id",
		"Correlation-Id",
		"X-Request-Id",
		"Request-Id",
	} {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func CorrelationIDFromMap(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	for _, key := range []string{
		"correlationId",
		"correlation-id",
		"x-correlation-id",
		"requestId",
		"request-id",
	} {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func CorrelationIDFromPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}

	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return ""
	}

	return correlationIDFromValue(raw)
}

func correlationIDFromValue(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"correlationId", "correlationID", "requestId", "requestID"} {
			if text, ok := current[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}

		if headers, ok := current["headers"].(map[string]any); ok {
			for _, key := range []string{"x-correlation-id", "correlation-id", "x-request-id", "request-id"} {
				if text, ok := headers[key].(string); ok && strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
			}
		}

		if detail, ok := current["detail"].(map[string]any); ok {
			if tarn, ok := detail["tarn"].(map[string]any); ok {
				if text, ok := tarn[correlationIDKey].(string); ok && strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
			}
			if value := correlationIDFromValue(detail); value != "" {
				return value
			}
		}

		if records, ok := current["Records"].([]any); ok {
			for _, record := range records {
				if value := correlationIDFromValue(record); value != "" {
					return value
				}
			}
		}

		if attrs, ok := current["MessageAttributes"].(map[string]any); ok {
			for _, key := range []string{"correlationId", "CorrelationId", "x-correlation-id", "X-Correlation-Id"} {
				if attr, ok := attrs[key].(map[string]any); ok {
					for _, valueKey := range []string{"StringValue", "Value"} {
						if text, ok := attr[valueKey].(string); ok && strings.TrimSpace(text) != "" {
							return strings.TrimSpace(text)
						}
					}
				}
			}
		}

		for _, nestedKey := range []string{"Sns", "s3", "body"} {
			if nested, ok := current[nestedKey]; ok {
				if value := correlationIDFromValue(nested); value != "" {
					return value
				}
			}
		}
	case []any:
		for _, item := range current {
			if value := correlationIDFromValue(item); value != "" {
				return value
			}
		}
	}

	return ""
}
