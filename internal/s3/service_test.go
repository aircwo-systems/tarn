package s3

import (
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func testServiceConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	return cfg
}

func TestS3EventCallbackOnPutObject(t *testing.T) {
	cfg := testServiceConfig(t)
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreateBucket("test-bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	var gotEvent, gotBucket, gotKey string
	svc.SetEventCallback(func(eventName, bucket, key string, size int64, etag string) {
		gotEvent = eventName
		gotBucket = bucket
		gotKey = key
	})

	_, err := svc.PutObject("test-bucket", "docs/readme.txt", "text/plain", strings.NewReader("hello"), nil)
	if err != nil {
		t.Fatalf("put object: %v", err)
	}

	if gotEvent != "s3:ObjectCreated:Put" {
		t.Fatalf("event = %q, want %q", gotEvent, "s3:ObjectCreated:Put")
	}
	if gotBucket != "test-bucket" {
		t.Fatalf("bucket = %q, want %q", gotBucket, "test-bucket")
	}
	if gotKey != "docs/readme.txt" {
		t.Fatalf("key = %q, want %q", gotKey, "docs/readme.txt")
	}
}

func TestS3EventCallbackOnDeleteObject(t *testing.T) {
	cfg := testServiceConfig(t)
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreateBucket("test-bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := svc.PutObject("test-bucket", "file.txt", "text/plain", strings.NewReader("data"), nil); err != nil {
		t.Fatalf("put object: %v", err)
	}

	var gotEvent string
	svc.SetEventCallback(func(eventName, bucket, key string, size int64, etag string) {
		gotEvent = eventName
	})

	if err := svc.DeleteObject("test-bucket", "file.txt"); err != nil {
		t.Fatalf("delete object: %v", err)
	}

	if gotEvent != "s3:ObjectRemoved:Delete" {
		t.Fatalf("event = %q, want %q", gotEvent, "s3:ObjectRemoved:Delete")
	}
}

func TestNoCallbackWhenNotSet(t *testing.T) {
	cfg := testServiceConfig(t)
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreateBucket("test-bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	// Should not panic when no callback is set
	_, err := svc.PutObject("test-bucket", "file.txt", "text/plain", strings.NewReader("data"), nil)
	if err != nil {
		t.Fatalf("put object: %v", err)
	}
	if err := svc.DeleteObject("test-bucket", "file.txt"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
}

func TestBucketNotificationConfig(t *testing.T) {
	cfg := testServiceConfig(t)
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreateBucket("notif-bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	notifCfg := &types.BucketNotificationConfiguration{
		LambdaConfigurations: []types.S3LambdaNotification{
			{
				ID:                 "config1",
				LambdaFunctionArn:  "arn:aws:lambda:us-east-1:000000000000:function:process",
				LambdaFunctionName: "process",
				Events:             []types.S3EventName{types.S3EventObjectCreatedPut},
				FilterPrefix:       "uploads/",
			},
		},
	}

	if err := svc.PutBucketNotificationConfiguration("notif-bucket", notifCfg); err != nil {
		t.Fatalf("put notification config: %v", err)
	}

	got := svc.GetBucketNotificationConfiguration("notif-bucket")
	if got == nil {
		t.Fatal("expected non-nil notification config")
	}
	if len(got.LambdaConfigurations) != 1 {
		t.Fatalf("lambda configs len = %d, want 1", len(got.LambdaConfigurations))
	}
	if got.LambdaConfigurations[0].LambdaFunctionName != "process" {
		t.Fatalf("function name = %q, want %q", got.LambdaConfigurations[0].LambdaFunctionName, "process")
	}
}

func TestBucketNotificationConfigPersistence(t *testing.T) {
	cfg := testServiceConfig(t)
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreateBucket("persist-bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	notifCfg := &types.BucketNotificationConfiguration{
		LambdaConfigurations: []types.S3LambdaNotification{
			{
				ID:                 "c1",
				LambdaFunctionName: "handler",
				Events:             []types.S3EventName{types.S3EventObjectRemovedDelete},
			},
		},
	}
	if err := svc.PutBucketNotificationConfiguration("persist-bucket", notifCfg); err != nil {
		t.Fatalf("put notification config: %v", err)
	}

	// Reload from disk
	svc2 := NewService(cfg)
	if err := svc2.Init(); err != nil {
		t.Fatalf("init svc2: %v", err)
	}

	got := svc2.GetBucketNotificationConfiguration("persist-bucket")
	if got == nil {
		t.Fatal("expected notification config after reload")
	}
	if len(got.LambdaConfigurations) != 1 {
		t.Fatalf("configs len = %d, want 1", len(got.LambdaConfigurations))
	}
}

func TestNotificationConfigOnNonexistentBucket(t *testing.T) {
	cfg := testServiceConfig(t)
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	err := svc.PutBucketNotificationConfiguration("missing", &types.BucketNotificationConfiguration{})
	if err == nil {
		t.Fatal("expected error for nonexistent bucket")
	}

	got := svc.GetBucketNotificationConfiguration("missing")
	if got != nil {
		t.Fatalf("expected nil for nonexistent bucket, got %+v", got)
	}
}
