// Package reqlog lets a handler add fields to the one /tarn/api line the
// server writes for each request. The server's logging middleware attaches a
// Fields value to the request context; handlers deep in a call add key/value
// pairs to it, and the middleware appends them to the line it writes.
package reqlog

import (
	"context"
	"strconv"
	"strings"
	"sync"
)

type contextKey struct{}

// Fields holds ordered key/value pairs for one request's log line.
type Fields struct {
	mu    sync.Mutex
	pairs [][2]string
}

// WithFields returns a context carrying a new, empty Fields value.
func WithFields(ctx context.Context) (context.Context, *Fields) {
	f := &Fields{}
	return context.WithValue(ctx, contextKey{}, f), f
}

// FromContext returns the request's Fields, or nil when the request did not
// come through the logging middleware.
func FromContext(ctx context.Context) *Fields {
	f, _ := ctx.Value(contextKey{}).(*Fields)
	return f
}

// Add records key=value on the request's log line. It is a no-op when ctx
// carries no Fields.
func Add(ctx context.Context, key, value string) {
	if f := FromContext(ctx); f != nil {
		f.Add(key, value)
	}
}

// Add records key=value. A later value for the same key replaces the earlier
// one, keeping its position.
func (f *Fields) Add(key, value string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.pairs {
		if f.pairs[i][0] == key {
			f.pairs[i][1] = value
			return
		}
	}
	f.pairs = append(f.pairs, [2]string{key, value})
}

// String renders the fields as " key=value key2=value2", quoting values that
// contain spaces, quotes or are empty. It returns "" when there are none.
func (f *Fields) String() string {
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	for _, p := range f.pairs {
		b.WriteByte(' ')
		b.WriteString(p[0])
		b.WriteByte('=')
		if p[1] == "" || strings.ContainsAny(p[1], " \t\"=") {
			b.WriteString(strconv.Quote(p[1]))
		} else {
			b.WriteString(p[1])
		}
	}
	return b.String()
}
