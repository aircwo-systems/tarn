package cli

import (
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestMatchesNotificationEventPattern(t *testing.T) {
	tests := []struct {
		name      string
		notif     types.S3LambdaNotification
		eventName string
		key       string
		want      bool
	}{
		{
			name: "exact event match",
			notif: types.S3LambdaNotification{
				Events: []types.S3EventName{"s3:ObjectCreated:Put"},
			},
			eventName: "s3:ObjectCreated:Put",
			key:       "incoming/file.json",
			want:      true,
		},
		{
			name: "wildcard created event match",
			notif: types.S3LambdaNotification{
				Events: []types.S3EventName{"s3:ObjectCreated:*"},
			},
			eventName: "s3:ObjectCreated:Put",
			key:       "incoming/file.json",
			want:      true,
		},
		{
			name: "wildcard mismatch",
			notif: types.S3LambdaNotification{
				Events: []types.S3EventName{"s3:ObjectRemoved:*"},
			},
			eventName: "s3:ObjectCreated:Put",
			key:       "incoming/file.json",
			want:      false,
		},
		{
			name: "prefix and suffix filters respected",
			notif: types.S3LambdaNotification{
				Events:       []types.S3EventName{"s3:ObjectCreated:*"},
				FilterPrefix: "incoming/",
				FilterSuffix: ".json",
			},
			eventName: "s3:ObjectCreated:Put",
			key:       "incoming/file.json",
			want:      true,
		},
		{
			name: "prefix filter blocks",
			notif: types.S3LambdaNotification{
				Events:       []types.S3EventName{"s3:ObjectCreated:*"},
				FilterPrefix: "incoming/",
			},
			eventName: "s3:ObjectCreated:Put",
			key:       "other/file.json",
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesNotification(tc.notif, tc.eventName, tc.key)
			if got != tc.want {
				t.Fatalf("matchesNotification() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchEventPattern(t *testing.T) {
	tests := []struct {
		pattern string
		event   string
		want    bool
	}{
		{pattern: "s3:ObjectCreated:*", event: "s3:ObjectCreated:Put", want: true},
		{pattern: "s3:ObjectCreated:Put", event: "s3:ObjectCreated:Put", want: true},
		{pattern: "*", event: "s3:ObjectRemoved:Delete", want: true},
		{pattern: "s3:ObjectRemoved:*", event: "s3:ObjectCreated:Put", want: false},
		{pattern: "", event: "s3:ObjectCreated:Put", want: false},
	}

	for _, tc := range tests {
		if got := matchEventPattern(tc.pattern, tc.event); got != tc.want {
			t.Fatalf("matchEventPattern(%q, %q) = %v, want %v", tc.pattern, tc.event, got, tc.want)
		}
	}
}
