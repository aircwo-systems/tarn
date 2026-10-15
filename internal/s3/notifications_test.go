package s3

import (
	"encoding/json"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/sqs"
	"github.com/aircwo-systems/tarn/pkg/types"
	"strings"
	"testing"
)

func TestQueueNotificationPersistsFiltersAndDeliversS3Event(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Region = "eu-west-2"
	cfg.AccountID = "123456789012"
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatal(err)
	}
	queue := sqs.NewService(cfg)
	if err := queue.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := queue.CreateQueue("events", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBucket("events-bucket"); err != nil {
		t.Fatal(err)
	}
	notification := &types.BucketNotificationConfiguration{QueueConfigurations: []types.S3QueueNotification{{ID: "object-events", QueueArn: q.QueueArn, Events: []types.S3EventName{"s3:ObjectCreated:*"}, FilterPrefix: "objects/", FilterSuffix: ".json"}}}
	if err := svc.PutBucketNotificationConfiguration("events-bucket", notification); err != nil {
		t.Fatal(err)
	}
	// Load the on-disk notification using a new service, then use the real SQS queue.
	svc = NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatal(err)
	}
	if len(svc.GetBucketNotificationConfiguration("events-bucket").QueueConfigurations) != 1 {
		t.Fatal("queue notification lost on restart")
	}
	svc.SetQueueSender(func(arn, body string) error {
		if arn != q.QueueArn {
			t.Fatalf("wrong destination %s", arn)
		}
		_, err := queue.SendMessage("events", body, 0, nil, "", "")
		return err
	})
	for _, key := range []string{"other/a.json", "objects/a.txt", "objects/a +b.json"} {
		if _, err := svc.PutObject("events-bucket", key, "application/json", strings.NewReader(`{"item":1}`), nil); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := queue.ReceiveMessage("events", 10, 30, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages=%d, want one matching event", len(msgs))
	}
	var event struct {
		Records []struct {
			EventName string
			AWSRegion string
			S3        struct {
				ConfigurationID string
				Bucket          struct{ Name string }
				Object          struct {
					Key  string
					Size int
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(msgs[0].Body), &event); err != nil {
		t.Fatal(err)
	}
	if len(event.Records) != 1 {
		t.Fatalf("event=%+v", event)
	}
	r := event.Records[0]
	if r.EventName != "ObjectCreated:Put" || r.AWSRegion != "eu-west-2" || r.S3.Bucket.Name != "events-bucket" || r.S3.Object.Key != "objects%2Fa+%2Bb.json" || r.S3.ConfigurationID != "object-events" || r.S3.Object.Size != 10 {
		t.Fatalf("event=%+v", r)
	}
	// Replacing the config with an empty document disables delivery.
	if err := svc.PutBucketNotificationConfiguration("events-bucket", &types.BucketNotificationConfiguration{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutObject("events-bucket", "objects/another.json", "", strings.NewReader("{}"), nil); err != nil {
		t.Fatal(err)
	}
	more, err := queue.ReceiveMessage("events", 10, 30, 0)
	if err != nil || len(more) != 0 {
		t.Fatalf("notification was not removed: %v %v", more, err)
	}
}
