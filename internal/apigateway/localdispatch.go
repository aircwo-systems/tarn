package apigateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/reqlog"
)

// maxLocalDispatchDepth stops an integration whose URL leads back to the same
// gateway from recursing in process forever.
const maxLocalDispatchDepth = 8

var errLocalDispatchLoop = errors.New("in-process request loop: integration URL leads back to the gateway")

type localDepthKey struct{}

// LocalDispatcher sends HTTP_PROXY requests addressed to Tarn's own listener
// to Tarn's handler in process, without opening a socket. One dispatcher is
// shared by every account's API Gateway service; the server sets its handler
// once it has built its routes.
type LocalDispatcher struct {
	handler atomic.Pointer[http.Handler]
	port    int
	hosts   map[string]bool
}

// NewLocalDispatcher recognises 127.0.0.1, localhost, ::1,
// host.docker.internal and cfg.Host, each on cfg.Port, as Tarn itself.
func NewLocalDispatcher(cfg *config.Config) *LocalDispatcher {
	d := &LocalDispatcher{
		port: cfg.Port,
		hosts: map[string]bool{
			"127.0.0.1":            true,
			"localhost":            true,
			"::1":                  true,
			"host.docker.internal": true,
		},
	}
	if h := strings.Trim(strings.ToLower(cfg.Host), "[]"); h != "" && h != "0.0.0.0" && h != "::" {
		d.hosts[h] = true
	}
	return d
}

// SetHandler installs the handler local requests are served by. It should be
// Tarn's routes without the logging middleware.
func (d *LocalDispatcher) SetHandler(h http.Handler) {
	if d != nil {
		d.handler.Store(&h)
	}
}

// Matches reports whether u addresses Tarn's own listener and a handler is
// installed to serve it.
func (d *LocalDispatcher) Matches(u *url.URL) bool {
	if d == nil || d.handler.Load() == nil || u.Scheme != "http" {
		return false
	}
	port := 80
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return false
		}
		port = n
	}
	return port == d.port && d.hosts[strings.ToLower(u.Hostname())]
}

// RoundTrip serves req with Tarn's handler. It returns when the handler sends
// its response headers, or with the context's error if the context ends first,
// as a network round trip does. The body streams while the handler runs.
func (d *LocalDispatcher) RoundTrip(req *http.Request) (*http.Response, error) {
	hp := d.handler.Load()
	if hp == nil {
		return nil, errors.New("in-process handler not set")
	}
	ctx := req.Context()
	depth, _ := ctx.Value(localDepthKey{}).(int)
	if depth >= maxLocalDispatchDepth {
		return nil, errLocalDispatchLoop
	}
	ctx = context.WithValue(ctx, localDepthKey{}, depth+1)
	// The inner request writes no /tarn/api line of its own and must not add
	// fields to the outer request's line.
	ctx, _ = reqlog.WithFields(ctx)

	// Shape the request as the server would have received it.
	in := req.Clone(ctx)
	in.URL = &url.URL{Path: req.URL.Path, RawPath: req.URL.RawPath, RawQuery: req.URL.RawQuery}
	in.RequestURI = req.URL.RequestURI()
	in.RemoteAddr = "127.0.0.1:0"
	if in.Body == nil {
		in.Body = http.NoBody
	}

	pr, pw := io.Pipe()
	w := &pipeResponseWriter{header: http.Header{}, pw: pw, ready: make(chan struct{})}
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		defer func() {
			if p := recover(); p != nil {
				w.fail(fmt.Errorf("in-process handler panicked: %v", p))
				return
			}
			w.WriteHeader(http.StatusOK)
			// A handler that returns because its context ended has not sent
			// a whole response; the reader must see why, not a clean EOF.
			if err := ctx.Err(); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			_ = pw.Close()
		}()
		(*hp).ServeHTTP(w, in)
	}()

	select {
	case <-w.ready:
	case <-ctx.Done():
		_ = pr.CloseWithError(ctx.Err())
		return nil, ctx.Err()
	}
	if w.err != nil {
		_ = pr.Close()
		return nil, w.err
	}
	// The context bounds the body as well as the headers, as it does for a
	// network response: when it ends, a read blocked on a stalled handler
	// returns the context's error.
	go func() {
		select {
		case <-ctx.Done():
			_ = pr.CloseWithError(ctx.Err())
		case <-handlerDone:
		}
	}()
	contentLength := int64(-1)
	if cl := w.sent.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
			contentLength = n
		}
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", w.status, http.StatusText(w.status)),
		StatusCode:    w.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        w.sent,
		Body:          pr,
		ContentLength: contentLength,
		Request:       req,
	}, nil
}

// pipeResponseWriter hands the status and headers to RoundTrip on the first
// WriteHeader and streams the body through a pipe.
type pipeResponseWriter struct {
	header http.Header
	pw     *io.PipeWriter
	once   sync.Once
	ready  chan struct{}
	status int
	sent   http.Header
	err    error // set before ready closes when the handler fails early
}

func (w *pipeResponseWriter) Header() http.Header { return w.header }

func (w *pipeResponseWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		return // informational responses are not passed on
	}
	w.once.Do(func() {
		w.status = code
		w.sent = w.header.Clone()
		close(w.ready)
	})
}

func (w *pipeResponseWriter) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.pw.Write(b)
}

// Flush satisfies http.Flusher; the pipe has no buffer to flush.
func (w *pipeResponseWriter) Flush() {}

func (w *pipeResponseWriter) fail(err error) {
	w.once.Do(func() {
		w.err = err
		close(w.ready)
	})
	_ = w.pw.CloseWithError(err)
}
