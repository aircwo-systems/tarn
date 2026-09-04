package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func isBindPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "operation not permitted") || strings.Contains(msg, "permission denied")
}

func mustListenLocalhost(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if isBindPermissionError(err) {
			t.Skipf("skipping test: local listen unavailable in this environment: %v", err)
		}
		t.Fatalf("listen failed: %v", err)
	}
	return listener
}

func TestInvokerInvoke(t *testing.T) {
	listener := mustListenLocalhost(t)
	defer func() { _ = listener.Close() }()

	port := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)

	mux := http.NewServeMux()
	mux.HandleFunc(rieInvokePath, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = fmt.Fprintf(w, `{"received":%s}`, string(body))
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	time.Sleep(50 * time.Millisecond)

	inv := NewInvoker()

	input := &types.InvokeInput{
		FunctionName: "test-func",
		Payload:      []byte(`{"key":"value"}`),
	}

	output, err := inv.Invoke(context.Background(), port, input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	if output.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", output.StatusCode)
	}

	expected := `{"received":{"key":"value"}}`
	if string(output.Payload) != expected {
		t.Fatalf("expected payload %q, got %q", expected, string(output.Payload))
	}
}

func TestInvokerInvokeWithFunctionError(t *testing.T) {
	listener := mustListenLocalhost(t)
	defer func() { _ = listener.Close() }()

	port := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)

	mux := http.NewServeMux()
	mux.HandleFunc(rieInvokePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Amz-Function-Error", "Unhandled")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, `{"errorMessage":"something broke","errorType":"Error"}`)
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	time.Sleep(50 * time.Millisecond)

	inv := NewInvoker()
	input := &types.InvokeInput{FunctionName: "err-func", Payload: []byte(`{}`)}

	output, err := inv.Invoke(context.Background(), port, input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if output.FunctionError != "Unhandled" {
		t.Fatalf("expected FunctionError 'Unhandled', got %q", output.FunctionError)
	}
}

func TestInvokerTimeout(t *testing.T) {
	listener := mustListenLocalhost(t)
	defer func() { _ = listener.Close() }()

	port := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)

	mux := http.NewServeMux()
	mux.HandleFunc(rieInvokePath, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, `{"result":"too slow"}`)
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	time.Sleep(50 * time.Millisecond)

	inv := NewInvoker()
	input := &types.InvokeInput{FunctionName: "slow-func", Payload: []byte(`{}`)}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	output, err := inv.Invoke(ctx, port, input)
	if err != nil {
		t.Fatalf("expected timeout to be handled gracefully, got error: %v", err)
	}
	if output.FunctionError != "Unhandled" {
		t.Fatalf("expected FunctionError 'Unhandled' for timeout, got %q", output.FunctionError)
	}
}

func TestWaitForReady(t *testing.T) {
	inv := NewInvoker()

	// Start a TCP listener with a delay to simulate container startup
	listener := mustListenLocalhost(t)
	port := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
	_ = listener.Close() // close — will reopen after delay

	go func() {
		time.Sleep(300 * time.Millisecond)

		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%s", port))
		if err != nil {
			return
		}
		defer func() { _ = l.Close() }()
		// Accept connections to keep the listener alive
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := inv.WaitForReady(ctx, port)
	if err != nil {
		t.Fatalf("expected WaitForReady to succeed, got: %v", err)
	}
}

func TestWaitForReadyTimeout(t *testing.T) {
	inv := NewInvoker()

	// Use a port that will never open
	port := "19999"

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := inv.WaitForReady(ctx, port)
	if err == nil {
		t.Fatal("expected WaitForReady to fail with timeout")
	}
}

func TestInvokerEmptyPayload(t *testing.T) {
	listener := mustListenLocalhost(t)
	defer func() { _ = listener.Close() }()

	port := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)

	mux := http.NewServeMux()
	mux.HandleFunc(rieInvokePath, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		// Echo back what was received
		_, _ = fmt.Fprintf(w, `{"received":%s}`, string(body))
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	time.Sleep(50 * time.Millisecond)

	inv := NewInvoker()

	// Invoke with nil payload — should default to {}
	input := &types.InvokeInput{
		FunctionName: "test-func",
		Payload:      nil,
	}

	output, err := inv.Invoke(context.Background(), port, input)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	expected := `{"received":{}}`
	if string(output.Payload) != expected {
		t.Fatalf("expected payload %q, got %q", expected, string(output.Payload))
	}
}

// TestInvokerFunctionErrorWithoutHeader covers RIE builds that return the error
// envelope in the body but omit X-Amz-Function-Error. Measured against
// public.ecr.aws/lambda/nodejs:20, which does exactly that: a thrown handler
// came back as HTTP 200 with no header, so every caller — the invoke handler,
// the trace store, and any AWS SDK reading FunctionError — treated a crashed
// invocation as a success.
func TestInvokerFunctionErrorWithoutHeader(t *testing.T) {
	listener := mustListenLocalhost(t)
	defer func() { _ = listener.Close() }()

	port := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)

	mux := http.NewServeMux()
	mux.HandleFunc(rieInvokePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"errorType":"TypeError","errorMessage":"Cannot read properties of undefined (reading 'reduce')","trace":["TypeError: ..."]}`)
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	time.Sleep(50 * time.Millisecond)

	output, err := NewInvoker().Invoke(context.Background(), port, &types.InvokeInput{
		FunctionName: "order-processor",
		Payload:      []byte(`{"orderId":"ord-991"}`),
	})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if output.FunctionError != "Unhandled" {
		t.Fatalf("FunctionError = %q, want %q", output.FunctionError, "Unhandled")
	}
}

// TestInvokerSuccessNotFlaggedAsError guards the payload sniff against false
// positives on ordinary handler return values.
func TestInvokerSuccessNotFlaggedAsError(t *testing.T) {
	listener := mustListenLocalhost(t)
	defer func() { _ = listener.Close() }()

	port := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)

	cases := map[string]string{
		"api gateway response": `{"statusCode":200,"body":"{\"ok\":true}"}`,
		"bare value":           `"hello"`,
		"null":                 `null`,
		"array":                `[1,2,3]`,
		"unrelated keys":       `{"error":"handled downstream","errorCode":42}`,
	}

	var body string
	mux := http.NewServeMux()
	mux.HandleFunc(rieInvokePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, body)
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	time.Sleep(50 * time.Millisecond)

	for name, payload := range cases {
		body = payload
		output, err := NewInvoker().Invoke(context.Background(), port, &types.InvokeInput{
			FunctionName: "order-processor",
			Payload:      []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("%s: invoke failed: %v", name, err)
		}
		if output.FunctionError != "" {
			t.Fatalf("%s: FunctionError = %q, want empty", name, output.FunctionError)
		}
	}
}
