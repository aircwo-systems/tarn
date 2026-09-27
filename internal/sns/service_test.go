package sns

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// blockingLambda queues invokes on goroutines, like the Lambda service's async
// queue, and holds each invoke until release is closed.
type blockingLambda struct {
	release chan struct{}
	wg      sync.WaitGroup

	mu      sync.Mutex
	invoked []string
}

func (l *blockingLambda) Invoke(_ context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	<-l.release
	l.mu.Lock()
	l.invoked = append(l.invoked, input.FunctionName)
	l.mu.Unlock()
	return &types.InvokeOutput{StatusCode: 200, FunctionError: map[bool]string{true: "Unhandled"}[input.FunctionName == "broken"]}, nil
}

func (l *blockingLambda) Enqueue(_ string, run func(ctx context.Context)) error {
	l.wg.Add(1)
	go func() { defer l.wg.Done(); run(context.Background()) }()
	return nil
}

func TestPublishDoesNotWaitForLambdaSubscribers(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	lambda := &blockingLambda{release: make(chan struct{})}
	svc := NewService(cfg, nil, lambda)
	traces := tracesvc.NewStore()
	svc.SetTraceStore(traces)

	topic, err := svc.CreateTopic("orders", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"audit", "broken"} {
		if _, err := svc.Subscribe(topic.TopicArn, "lambda", "arn:aws:lambda:us-east-1:000000000000:function:"+fn, nil); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.Publish(context.Background(), PublishInput{TopicArn: topic.TopicArn, Message: "hi"})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		close(lambda.release)
		t.Fatal("Publish waited for its Lambda subscribers to finish")
	}
	if traces.Count() != 0 {
		t.Fatal("trace recorded before the Lambda deliveries finished")
	}

	close(lambda.release)
	lambda.wg.Wait()
	recent := traces.Recent(1)
	if len(recent) != 1 {
		t.Fatalf("got %d traces after delivery, want 1", len(recent))
	}
	statuses := map[string]string{}
	for _, span := range recent[0].Spans {
		statuses[span.Name] = span.Status
	}
	if statuses["audit"] != "ok" || statuses["broken"] != "error" || recent[0].Status != 500 {
		t.Fatalf("trace status %d spans %v: want audit ok, broken error, overall 500", recent[0].Status, statuses)
	}
}
