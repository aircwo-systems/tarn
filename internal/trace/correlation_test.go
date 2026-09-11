package trace

import (
	"testing"
)

func TestCorrelationIDFromValue_MessageAttributes(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name: "PascalCase MessageAttributes with StringValue",
			input: map[string]any{
				"MessageAttributes": map[string]any{
					"correlationId": map[string]any{
						"StringValue": "corr-123",
					},
				},
			},
			expected: "corr-123",
		},
		{
			name: "camelCase messageAttributes with stringValue",
			input: map[string]any{
				"messageAttributes": map[string]any{
					"correlationId": map[string]any{
						"stringValue": "corr-456",
					},
				},
			},
			expected: "corr-456",
		},
		{
			name: "Inside Records array with camelCase messageAttributes",
			input: map[string]any{
				"Records": []any{
					map[string]any{
						"messageAttributes": map[string]any{
							"x-correlation-id": map[string]any{
								"stringValue": "corr-789",
							},
						},
					},
				},
			},
			expected: "corr-789",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := correlationIDFromValue(tc.input)
			if got != tc.expected {
				t.Fatalf("correlationIDFromValue() = %q, want %q", got, tc.expected)
			}
		})
	}
}
