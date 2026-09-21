package lambda

import (
	"context"
	"errors"
	"testing"

	"github.com/aircwo-systems/tarn/internal/engine"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestRecoverDeadWarmContainer_ReplacesAndRetries(t *testing.T) {
	s := &Service{}
	dead := &engine.ContainerInfo{ID: "deadcontainer0001", FunctionName: "fn"}
	fresh := &engine.ContainerInfo{ID: "freshcontainer001", FunctionName: "fn"}
	info := dead
	cause := errors.New("RIE invocation failed: dial tcp 127.0.0.1:63530: connect: connection refused")

	var removed *engine.ContainerInfo
	var invokedOn *engine.ContainerInfo
	var invokedCold bool
	out, err := s.recoverDeadWarmContainer(context.Background(), &info, cause, warmRecovery{
		remove:    func(c *engine.ContainerInfo) { removed = c },
		reacquire: func() (*engine.ContainerInfo, bool, error) { return fresh, true, nil },
		invoke: func(c *engine.ContainerInfo, cold bool) (*types.InvokeOutput, error) {
			invokedOn, invokedCold = c, cold
			return &types.InvokeOutput{StatusCode: 200, Payload: []byte(`"ok"`)}, nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out.Payload) != `"ok"` {
		t.Fatalf("payload = %s", out.Payload)
	}
	if removed != dead {
		t.Fatalf("dead container not removed")
	}
	if invokedOn != fresh || !invokedCold {
		t.Fatalf("retry went to %v (cold=%v), want fresh cold container", invokedOn, invokedCold)
	}
	if info != fresh {
		t.Fatalf("caller's info not swapped to replacement, so wrong container would be released")
	}
}

func TestRecoverDeadWarmContainer_ReacquireFails(t *testing.T) {
	s := &Service{}
	info := &engine.ContainerInfo{ID: "dead", FunctionName: "fn"}
	cause := errors.New("RIE invocation failed: connection refused")
	invoked := false

	_, err := s.recoverDeadWarmContainer(context.Background(), &info, cause, warmRecovery{
		remove:    func(*engine.ContainerInfo) {},
		reacquire: func() (*engine.ContainerInfo, bool, error) { return nil, false, errors.New("docker down") },
		invoke: func(*engine.ContainerInfo, bool) (*types.InvokeOutput, error) {
			invoked = true
			return nil, nil
		},
	})
	if !errors.Is(err, cause) {
		t.Fatalf("error should wrap original cause, got %v", err)
	}
	if invoked {
		t.Fatalf("invoke should not run without a replacement")
	}
	if info != nil {
		t.Fatalf("removed container must not be released, info = %v", info)
	}
}

func TestRecoverDeadWarmContainer_ContextDone(t *testing.T) {
	s := &Service{}
	info := &engine.ContainerInfo{ID: "dead", FunctionName: "fn"}
	cause := errors.New("RIE invocation failed: connection refused")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	removed, reacquired := false, false

	_, err := s.recoverDeadWarmContainer(ctx, &info, cause, warmRecovery{
		remove:    func(*engine.ContainerInfo) { removed = true },
		reacquire: func() (*engine.ContainerInfo, bool, error) { reacquired = true; return nil, false, nil },
		invoke:    func(*engine.ContainerInfo, bool) (*types.InvokeOutput, error) { return nil, nil },
	})
	if !errors.Is(err, cause) || !removed || reacquired {
		t.Fatalf("err=%v removed=%v reacquired=%v; want cause, removed, no reacquire", err, removed, reacquired)
	}
}
