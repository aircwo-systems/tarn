package lambda

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aircwo-systems/tarn/internal/engine"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestRIEDisconnectAfterReadingEventIsNotRetried(t *testing.T) {
	const event = `{"action":"sign-out"}`
	var calls atomic.Int32
	rie := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil || string(payload) != event {
			t.Errorf("read invocation payload %q: %v", payload, err)
		}
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack RIE connection: %v", err)
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(rie.Close)

	for _, coldStart := range []bool{false, true} {
		name := "warm runtime"
		if coldStart {
			name = "cold runtime"
		}
		t.Run(name, func(t *testing.T) {
			before := calls.Load()
			s := &Service{invoker: engine.NewInvoker()}
			dead := &engine.ContainerInfo{ID: "dead", FunctionName: "fn", HostPort: strings.TrimPrefix(rie.URL, "http://127.0.0.1:")}
			info := dead
			input := &types.InvokeInput{FunctionName: "fn", Payload: []byte(event)}

			var removed *engine.ContainerInfo
			out, err := s.invokeWithRecovery(t.Context(), &info, input, coldStart,
				func(c *engine.ContainerInfo) { removed = c },
				func() (*engine.ContainerInfo, error) {
					t.Fatal("attempted to replace the container after delivering the event")
					return nil, nil
				})
			if got := calls.Load() - before; got != 1 {
				t.Errorf("runtime received %d invocations, want 1", got)
			}
			if info != nil || removed != dead {
				t.Error("disconnected container was not discarded")
			}
			if err != nil || out == nil || out.StatusCode != http.StatusOK || out.FunctionError != "Unhandled" {
				t.Fatalf("got output %+v, error %v, want Unhandled function error", out, err)
			}
			var payload struct{ ErrorType string }
			if err := json.Unmarshal(out.Payload, &payload); err != nil || payload.ErrorType != "Runtime.ExitError" {
				t.Fatalf("got payload %s, want Runtime.ExitError", out.Payload)
			}
		})
	}
}

func TestRIERefusedConnectionRetriesOnOneReplacement(t *testing.T) {
	var calls atomic.Int32
	rie := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `"ok"`)
	}))
	t.Cleanup(rie.Close)

	for _, replaced := range []bool{true, false} {
		name := "replacement reached"
		if !replaced {
			name = "replacement also refused"
		}
		t.Run(name, func(t *testing.T) {
			before := calls.Load()
			s := &Service{invoker: engine.NewInvoker()}
			dead := &engine.ContainerInfo{ID: "dead", FunctionName: "fn", HostPort: refusedRIEPort(t)}
			fresh := &engine.ContainerInfo{ID: "fresh", FunctionName: "fn", HostPort: dead.HostPort}
			if replaced {
				fresh.HostPort = strings.TrimPrefix(rie.URL, "http://127.0.0.1:")
			}
			info := dead
			var removed []*engine.ContainerInfo
			replacements := 0
			out, err := s.invokeWithRecovery(t.Context(), &info, &types.InvokeInput{FunctionName: "fn"}, false,
				func(c *engine.ContainerInfo) { removed = append(removed, c) },
				func() (*engine.ContainerInfo, error) {
					replacements++
					if replacements != 1 {
						t.Fatal("tried more than one replacement")
					}
					return fresh, nil
				})
			if replacements != 1 || len(removed) == 0 || removed[0] != dead {
				t.Fatalf("replacements=%d, removed=%v, want one replacement and old container removed", replacements, removed)
			}
			if replaced {
				if err != nil || out == nil || string(out.Payload) != `"ok"` || info != fresh || len(removed) != 1 || calls.Load()-before != 1 {
					t.Fatalf("output=%+v, error=%v, remaining=%v, removed=%v, calls=%d", out, err, info, removed, calls.Load()-before)
				}
			} else {
				var dialErr *engine.RIEConnectionError
				if !errors.As(err, &dialErr) || !dialErr.DialFailed || info != nil || len(removed) != 2 || removed[1] != fresh {
					t.Fatalf("error=%v, remaining=%v, removed=%v, want failed dial and both containers removed", err, info, removed)
				}
			}
		})
	}
}

func TestRIERefusedConnectionReportsReplacementFailure(t *testing.T) {
	s := &Service{invoker: engine.NewInvoker()}
	info := &engine.ContainerInfo{ID: "dead", FunctionName: "fn", HostPort: refusedRIEPort(t)}
	_, err := s.invokeWithRecovery(t.Context(), &info, &types.InvokeInput{FunctionName: "fn"}, false,
		func(*engine.ContainerInfo) {},
		func() (*engine.ContainerInfo, error) { return nil, errors.New("docker down") })
	var dialErr *engine.RIEConnectionError
	if !errors.As(err, &dialErr) || !dialErr.DialFailed || !strings.Contains(err.Error(), "docker down") || info != nil {
		t.Fatalf("error=%v, remaining=%v, want both failures and discarded container", err, info)
	}
}

func refusedRIEPort(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return strconv.Itoa(port)
}
