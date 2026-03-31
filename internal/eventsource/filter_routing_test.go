package eventsource

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

type recordingLambda struct {
	mu        sync.Mutex
	functions []string
}

func (r *recordingLambda) Invoke(_ context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	r.mu.Lock()
	r.functions = append(r.functions, input.FunctionName)
	r.mu.Unlock()
	return &types.InvokeOutput{StatusCode: 200, Payload: []byte(`{}`)}, nil
}

func (r *recordingLambda) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, fn := range r.functions {
		if fn == name {
			n++
		}
	}
	return n
}

// Regression test: with two filtered ESM pollers on one queue, a type2 message
// should reach the type2 function (not spin forever / route to DLQ).
func TestFilteredPollersRouteType2Message(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	sqs := sqssvc.NewService(cfg)
	if err := sqs.Init(); err != nil {
		t.Fatalf("sqs init: %v", err)
	}
	sqs.Start()
	defer sqs.Stop()

	_, err := sqs.CreateQueue("events-dlq", nil, nil)
	if err != nil {
		t.Fatalf("create dlq: %v", err)
	}

	_, err = sqs.CreateQueue("events", map[string]string{
		"RedrivePolicy": `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:000000000000:events-dlq","maxReceiveCount":3}`,
	}, nil)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	lambda := &recordingLambda{}

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("esm store init: %v", err)
	}
	svc := NewService(cfg, store, lambda, sqs, nil)
	if err := svc.Init(); err != nil {
		t.Fatalf("esm svc init: %v", err)
	}

	if _, err := svc.CreateMapping(
		"arn:aws:sqs:us-east-1:000000000000:events",
		"arn:aws:lambda:us-east-1:000000000000:function:type1-fn",
		"type1-fn",
		1,
		0,
		true,
		&types.FilterCriteria{Filters: []types.FilterCriteriaFilter{{Pattern: `{"body":{"type":["type1"]}}`}}},
	); err != nil {
		t.Fatalf("create mapping type1: %v", err)
	}

	if _, err := svc.CreateMapping(
		"arn:aws:sqs:us-east-1:000000000000:events",
		"arn:aws:lambda:us-east-1:000000000000:function:type2-fn",
		"type2-fn",
		1,
		0,
		true,
		&types.FilterCriteria{Filters: []types.FilterCriteriaFilter{{Pattern: `{"body":{"type":["type2"]}}`}}},
	); err != nil {
		t.Fatalf("create mapping type2: %v", err)
	}

	if _, err := sqs.SendMessage("events", `{"id":"req-2001","type":"type2","body":"hello"}`, 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if lambda.count("type2-fn") > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	svc.Stop()

	if got := lambda.count("type2-fn"); got == 0 {
		t.Fatalf("expected type2-fn to be invoked at least once, got %d", got)
	}
	if got := lambda.count("type1-fn"); got != 0 {
		t.Fatalf("expected type1-fn invocations = 0, got %d", got)
	}

	dlqMsgs, err := sqs.PeekMessages("events-dlq", 10)
	if err != nil {
		t.Fatalf("peek dlq: %v", err)
	}
	if len(dlqMsgs) != 0 {
		t.Fatalf("expected no DLQ messages for type2 routing, got %d", len(dlqMsgs))
	}
}
