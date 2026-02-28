package lambda

import "testing"

func TestCountProcessedMessages(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    int64
	}{
		{name: "empty payload", payload: nil, want: 1},
		{name: "generic event", payload: []byte(`{"hello":"world"}`), want: 1},
		{name: "single sqs record", payload: []byte(`{"Records":[{"messageId":"1"}]}`), want: 1},
		{name: "multiple sqs records", payload: []byte(`{"Records":[{"messageId":"1"},{"messageId":"2"},{"messageId":"3"}]}`), want: 3},
		{name: "explicit empty records", payload: []byte(`{"Records":[]}`), want: 0},
		{name: "invalid json", payload: []byte(`{"Records":`), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countProcessedMessages(tt.payload)
			if got != tt.want {
				t.Fatalf("countProcessedMessages() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRecordInvocationAccumulatesMetrics(t *testing.T) {
	svc := &Service{
		metrics: make(map[string]*FunctionMetrics),
	}

	svc.recordInvocation("orders-consumer", []byte(`{"Records":[{"messageId":"1"},{"messageId":"2"}]}`))
	svc.recordInvocation("orders-consumer", []byte(`{"ok":true}`))

	metrics := svc.GetFunctionMetrics("orders-consumer")
	if metrics.Invocations != 2 {
		t.Fatalf("invocations = %d, want 2", metrics.Invocations)
	}
	if metrics.MessagesProcessed != 3 {
		t.Fatalf("messagesProcessed = %d, want 3", metrics.MessagesProcessed)
	}
	if metrics.LastInvokedAt.IsZero() {
		t.Fatal("lastInvokedAt should be set")
	}
}
