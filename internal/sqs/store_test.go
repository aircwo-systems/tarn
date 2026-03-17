package sqs

import (
	"os"
	"testing"
	"time"

	"github.com/openstack-project/openstack/internal/config"
)

func newTestStore() *Store {
	cfg := &config.Config{
		Host:      "localhost",
		Port:      4566,
		Region:    "us-east-1",
		AccountID: "000000000000",
	}
	return NewStore(cfg)
}

func TestCreateAndGetQueue(t *testing.T) {
	s := newTestStore()

	q, err := s.CreateQueue("test-queue", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if q.QueueName != "test-queue" {
		t.Fatalf("expected name 'test-queue', got %q", q.QueueName)
	}
	if q.VisibilityTimeout != 30 {
		t.Fatalf("expected default visibility timeout 30, got %d", q.VisibilityTimeout)
	}
	if q.QueueArn == "" {
		t.Fatal("expected non-empty ARN")
	}
	if q.QueueUrl == "" {
		t.Fatal("expected non-empty URL")
	}

	// Get
	got, err := s.GetQueue("test-queue")
	if err != nil {
		t.Fatal(err)
	}
	if got.QueueName != "test-queue" {
		t.Fatalf("expected 'test-queue', got %q", got.QueueName)
	}

	// Idempotent create
	q2, err := s.CreateQueue("test-queue", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if q2.QueueUrl != q.QueueUrl {
		t.Fatal("idempotent create should return same queue")
	}
}

func TestCreateFIFOQueue(t *testing.T) {
	s := newTestStore()

	// FIFO must end with .fifo
	_, err := s.CreateQueue("bad-name", map[string]string{"FifoQueue": "true"}, nil)
	if err == nil {
		t.Fatal("expected error for FIFO without .fifo suffix")
	}

	q, err := s.CreateQueue("test.fifo", map[string]string{"FifoQueue": "true"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !q.FifoQueue {
		t.Fatal("expected FIFO queue")
	}
}

func TestSendAndReceive(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q1", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	msg, err := s.SendMessage("q1", "hello world", 0, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if msg.MessageId == "" {
		t.Fatal("expected non-empty message ID")
	}
	if msg.MD5OfBody == "" {
		t.Fatal("expected non-empty MD5")
	}

	// Receive
	msgs, err := s.ReceiveMessage("q1", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Body != "hello world" {
		t.Fatalf("expected body 'hello world', got %q", msgs[0].Body)
	}
	if msgs[0].ReceiptHandle == "" {
		t.Fatal("expected non-empty receipt handle")
	}
	if msgs[0].ApproximateReceiveCount != 1 {
		t.Fatalf("expected receive count 1, got %d", msgs[0].ApproximateReceiveCount)
	}
}

func TestVisibilityTimeout(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q2", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	if _, err := s.SendMessage("q2", "hidden msg", 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	// Receive with 1 second visibility timeout
	msgs, _ := s.ReceiveMessage("q2", 1, 1)
	if len(msgs) != 1 {
		t.Fatal("expected 1 message")
	}

	// Should be invisible now
	msgs2, _ := s.ReceiveMessage("q2", 1, 1)
	if len(msgs2) != 0 {
		t.Fatal("expected 0 messages (visibility timeout)")
	}

	// Wait for visibility to expire
	time.Sleep(1100 * time.Millisecond)

	// Should be visible again
	msgs3, _ := s.ReceiveMessage("q2", 1, 30)
	if len(msgs3) != 1 {
		t.Fatal("expected 1 message after visibility expired")
	}
	if msgs3[0].ApproximateReceiveCount != 2 {
		t.Fatalf("expected receive count 2, got %d", msgs3[0].ApproximateReceiveCount)
	}
}

func TestDeleteMessage(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q3", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	if _, err := s.SendMessage("q3", "to delete", 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	msgs, _ := s.ReceiveMessage("q3", 1, 30)
	if len(msgs) != 1 {
		t.Fatal("expected 1 message")
	}

	err := s.DeleteMessage("q3", msgs[0].ReceiptHandle)
	if err != nil {
		t.Fatal(err)
	}

	// Force visibility to expire by setting VisibleAt to past
	// After delete, even with 0 visibility the message should be gone
	msgs2, _ := s.ReceiveMessage("q3", 1, 0)
	if len(msgs2) != 0 {
		t.Fatal("expected 0 messages after delete")
	}
}

func TestPurgeQueue(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("q4", nil, nil)

	for i := 0; i < 5; i++ {
		if _, err := s.SendMessage("q4", "msg", 0, nil, "", ""); err != nil {
			t.Fatalf("send message: %v", err)
		}
	}

	err := s.PurgeQueue("q4")
	if err != nil {
		t.Fatal(err)
	}

	msgs, _ := s.ReceiveMessage("q4", 10, 30)
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after purge, got %d", len(msgs))
	}
}

func TestMessageDelay(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("q5", nil, nil)

	s.SendMessage("q5", "delayed", 1, nil, "", "")

	// Should not be visible yet
	msgs, _ := s.ReceiveMessage("q5", 1, 30)
	if len(msgs) != 0 {
		t.Fatal("expected 0 messages (delay not expired)")
	}

	time.Sleep(1100 * time.Millisecond)

	// Now should be visible
	msgs2, _ := s.ReceiveMessage("q5", 1, 30)
	if len(msgs2) != 1 {
		t.Fatal("expected 1 message after delay expired")
	}
}

func TestFIFOOrdering(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("order.fifo", map[string]string{"FifoQueue": "true"}, nil)

	for i := 0; i < 5; i++ {
		s.SendMessage("order.fifo", string(rune('A'+i)), 0, nil, "group1", "")
	}

	msgs, _ := s.ReceiveMessage("order.fifo", 5, 30)
	if len(msgs) != 1 {
		// FIFO returns one message per group at a time
		t.Fatalf("expected 1 message (one per group), got %d", len(msgs))
	}
	if msgs[0].Body != "A" {
		t.Fatalf("expected first message 'A', got %q", msgs[0].Body)
	}
}

func TestFIFODeduplication(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("dedup.fifo", map[string]string{"FifoQueue": "true"}, nil)

	_, err := s.SendMessage("dedup.fifo", "msg1", 0, nil, "g1", "dedup-123")
	if err != nil {
		t.Fatal(err)
	}

	// Send duplicate within 5-minute window
	msg2, err := s.SendMessage("dedup.fifo", "msg1 duplicate", 0, nil, "g1", "dedup-123")
	if err != nil {
		t.Fatal(err)
	}

	// Should return original message (dedup)
	if msg2.Body != "msg1" {
		t.Fatalf("expected dedup to return original body 'msg1', got %q", msg2.Body)
	}

	// Only one message should be in the queue
	msgs, _ := s.ReceiveMessage("dedup.fifo", 10, 30)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (dedup), got %d", len(msgs))
	}
}

func TestChangeMessageVisibility(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("q6", nil, nil)

	s.SendMessage("q6", "vis test", 0, nil, "", "")

	msgs, _ := s.ReceiveMessage("q6", 1, 1)
	if len(msgs) != 1 {
		t.Fatal("expected 1 message")
	}

	// Extend visibility to 10 seconds
	err := s.ChangeMessageVisibility("q6", msgs[0].ReceiptHandle, 10)
	if err != nil {
		t.Fatal(err)
	}

	// Wait past original timeout but not extended one
	time.Sleep(1200 * time.Millisecond)

	// Should still be invisible (extended to 10s)
	msgs2, _ := s.ReceiveMessage("q6", 1, 1)
	if len(msgs2) != 0 {
		t.Fatal("expected 0 messages (visibility extended)")
	}
}

func TestReleaseMessageDoesNotAccumulateReceiveCount(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("q-release", nil, nil)

	if _, err := s.SendMessage("q-release", `{"type":"type2"}`, 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	msgs1, err := s.ReceiveMessage("q-release", 1, 30)
	if err != nil {
		t.Fatalf("receive #1: %v", err)
	}
	if len(msgs1) != 1 {
		t.Fatalf("receive #1 count = %d, want 1", len(msgs1))
	}
	if msgs1[0].ApproximateReceiveCount != 1 {
		t.Fatalf("receive #1 count = %d, want 1", msgs1[0].ApproximateReceiveCount)
	}

	if err := s.ReleaseMessage("q-release", msgs1[0].ReceiptHandle); err != nil {
		t.Fatalf("release message: %v", err)
	}

	msgs2, err := s.ReceiveMessage("q-release", 1, 30)
	if err != nil {
		t.Fatalf("receive #2: %v", err)
	}
	if len(msgs2) != 1 {
		t.Fatalf("receive #2 count = %d, want 1", len(msgs2))
	}
	if msgs2[0].ApproximateReceiveCount != 1 {
		t.Fatalf("receive #2 count = %d, want 1", msgs2[0].ApproximateReceiveCount)
	}
}

func TestQueueTags(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("tagged", nil, map[string]string{"env": "dev"})

	tags, err := s.ListQueueTags("tagged")
	if err != nil {
		t.Fatal(err)
	}
	if tags["env"] != "dev" {
		t.Fatalf("expected tag env=dev, got %v", tags)
	}

	// Add tag
	if err := s.TagQueue("tagged", map[string]string{"team": "backend"}); err != nil {
		t.Fatalf("tag queue: %v", err)
	}
	tags2, _ := s.ListQueueTags("tagged")
	if tags2["team"] != "backend" {
		t.Fatalf("expected tag team=backend, got %v", tags2)
	}

	// Remove tag
	if err := s.UntagQueue("tagged", []string{"env"}); err != nil {
		t.Fatalf("untag queue: %v", err)
	}
	tags3, _ := s.ListQueueTags("tagged")
	if _, exists := tags3["env"]; exists {
		t.Fatal("tag 'env' should have been removed")
	}
	if tags3["team"] != "backend" {
		t.Fatal("tag 'team' should still exist")
	}
}

func TestListQueues(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("app-queue-1", nil, nil)
	s.CreateQueue("app-queue-2", nil, nil)
	s.CreateQueue("other-queue", nil, nil)

	all := s.ListQueues("")
	if len(all) != 3 {
		t.Fatalf("expected 3 queues, got %d", len(all))
	}

	filtered := s.ListQueues("app-")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 queues with prefix 'app-', got %d", len(filtered))
	}
}

func TestDeleteQueue(t *testing.T) {
	s := newTestStore()
	s.CreateQueue("del-queue", nil, nil)

	err := s.DeleteQueue("del-queue")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.GetQueue("del-queue")
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestGetQueueAttributes(t *testing.T) {
	s := newTestStore()
	policy := `{"Version":"2012-10-17","Statement":[{"Sid":"AllowSNSPublish","Effect":"Allow"}]}`
	s.CreateQueue("attr-queue", map[string]string{
		"VisibilityTimeout": "60",
		"Policy":            policy,
	}, nil)

	s.SendMessage("attr-queue", "msg1", 0, nil, "", "")
	s.SendMessage("attr-queue", "msg2", 0, nil, "", "")

	attrs, err := s.GetQueueAttributes("attr-queue", []string{"All"})
	if err != nil {
		t.Fatal(err)
	}

	if attrs["VisibilityTimeout"] != "60" {
		t.Fatalf("expected VisibilityTimeout=60, got %q", attrs["VisibilityTimeout"])
	}
	if attrs["DelaySeconds"] != "0" {
		t.Fatalf("expected DelaySeconds=0, got %q", attrs["DelaySeconds"])
	}
	if attrs["ApproximateNumberOfMessages"] != "2" {
		t.Fatalf("expected 2 messages, got %q", attrs["ApproximateNumberOfMessages"])
	}
	if attrs["Policy"] != policy {
		t.Fatalf("expected Policy=%q, got %q", policy, attrs["Policy"])
	}
}

func TestMessageRetentionExpiry(t *testing.T) {
	s := newTestStore()
	// Create queue with 1 second retention
	if _, err := s.CreateQueue("expire-queue", map[string]string{
		"MessageRetentionPeriod": "1",
	}, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	if _, err := s.SendMessage("expire-queue", "ephemeral", 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	// Should be available immediately
	msgs, _ := s.ReceiveMessage("expire-queue", 1, 0)
	if len(msgs) != 1 {
		t.Fatal("expected 1 message before expiry")
	}
	// Make it visible again immediately
	if err := s.ChangeMessageVisibility("expire-queue", msgs[0].ReceiptHandle, 0); err != nil {
		t.Fatalf("change message visibility: %v", err)
	}

	// Wait for retention to expire
	time.Sleep(1100 * time.Millisecond)

	// Run reap to clean up
	s.Reap()

	msgs2, _ := s.ReceiveMessage("expire-queue", 1, 0)
	if len(msgs2) != 0 {
		t.Fatal("expected 0 messages after retention expiry")
	}
}

func TestQueueStatePersistsToDisk(t *testing.T) {
	cfg := &config.Config{
		Host:               "localhost",
		Port:               4566,
		Region:             "us-east-1",
		AccountID:          "000000000000",
		DataDir:            t.TempDir(),
		PersistenceEnabled: true,
	}

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	if _, err := store.CreateQueue("persisted-queue", map[string]string{"VisibilityTimeout": "45"}, map[string]string{"feature": "r10"}); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if _, err := store.SendMessage("persisted-queue", "hello persistence", 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	reloaded := NewStore(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("reload store: %v", err)
	}

	queue, err := reloaded.GetQueue("persisted-queue")
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	if queue.VisibilityTimeout != 45 {
		t.Fatalf("visibility timeout = %d, want 45", queue.VisibilityTimeout)
	}
	if queue.Tags["feature"] != "r10" {
		t.Fatalf("queue tags = %v, want feature=r10", queue.Tags)
	}

	msgs, err := reloaded.PeekMessages("persisted-queue", 10)
	if err != nil {
		t.Fatalf("peek messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != "hello persistence" {
		t.Fatalf("unexpected messages after reload: %+v", msgs)
	}
}

func TestDeletedQueueIsRemovedFromPersistedState(t *testing.T) {
	cfg := &config.Config{
		Host:               "localhost",
		Port:               4566,
		Region:             "us-east-1",
		AccountID:          "000000000000",
		DataDir:            t.TempDir(),
		PersistenceEnabled: true,
	}

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	if _, err := store.CreateQueue("ephemeral-queue", nil, map[string]string{"feature": "r10"}); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if err := store.DeleteQueue("ephemeral-queue"); err != nil {
		t.Fatalf("delete queue: %v", err)
	}

	reloaded := NewStore(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("reload store: %v", err)
	}

	if _, err := reloaded.GetQueue("ephemeral-queue"); err == nil {
		t.Fatal("expected deleted queue to stay deleted after reload")
	}
}

func TestInitToleratesTrailingGarbageInStateFile(t *testing.T) {
	cfg := &config.Config{
		Host:               "localhost",
		Port:               4566,
		Region:             "us-east-1",
		AccountID:          "000000000000",
		DataDir:            t.TempDir(),
		PersistenceEnabled: true,
	}

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	if _, err := store.CreateQueue("q-corrupt", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	original, err := os.ReadFile(cfg.QueuesStatePath())
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	corrupted := append(original, []byte("\n    }\n  ]\n}")...)
	if err := os.WriteFile(cfg.QueuesStatePath(), corrupted, 0644); err != nil {
		t.Fatalf("write corrupted state file: %v", err)
	}

	reloaded := NewStore(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("reload store with trailing garbage: %v", err)
	}

	if _, err := reloaded.GetQueue("q-corrupt"); err != nil {
		t.Fatalf("expected queue to load from tolerant decoder: %v", err)
	}
}
