package admin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	infrasvc "github.com/aircwo-systems/tarn/internal/infrastructure"
	"github.com/aircwo-systems/tarn/internal/redisviewer"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
)

func startMockRedisForAdmin(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	store := map[string]string{"session:1": "active"}

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				r := bufio.NewReader(conn)
				w := bufio.NewWriter(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						if err != io.EOF {
							return
						}
						return
					}
					if !strings.HasPrefix(line, "*") {
						return
					}
					n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
					args := make([]string, 0, n)
					for i := 0; i < n; i++ {
						szLine, err := r.ReadString('\n')
						if err != nil {
							return
						}
						sz, _ := strconv.Atoi(strings.TrimSpace(szLine[1:]))
						buf := make([]byte, sz+2)
						if _, err := io.ReadFull(r, buf); err != nil {
							return
						}
						args = append(args, string(buf[:sz]))
					}
					if len(args) == 0 {
						continue
					}
					switch strings.ToUpper(args[0]) {
					case "INFO":
						info := "# Server\r\nredis_version:7.4.1\r\nredis_mode:standalone\r\n# Clients\r\nconnected_clients:2\r\n# Memory\r\nused_memory:4096\r\nused_memory_human:4.00K\r\n# Keyspace\r\ndb0:keys=1,expires=0,avg_ttl=0\r\n"
						_, _ = fmt.Fprintf(w, "$%d\r\n%s\r\n", len(info), info)
					case "DBSIZE":
						_, _ = fmt.Fprintf(w, ":%d\r\n", len(store))
					case "SCAN":
						_, _ = w.WriteString("*2\r\n$1\r\n0\r\n*1\r\n$9\r\nsession:1\r\n")
					case "TYPE":
						if _, ok := store[args[1]]; ok {
							_, _ = w.WriteString("+string\r\n")
						} else {
							_, _ = w.WriteString("+none\r\n")
						}
					case "PTTL":
						_, _ = w.WriteString(":-1\r\n")
					case "OBJECT":
						_, _ = w.WriteString("$6\r\nembstr\r\n")
					case "MEMORY":
						_, _ = w.WriteString(":48\r\n")
					case "STRLEN":
						_, _ = fmt.Fprintf(w, ":%d\r\n", len(store[args[1]]))
					case "GETRANGE":
						v := store[args[1]]
						_, _ = fmt.Fprintf(w, "$%d\r\n%s\r\n", len(v), v)
					case "PING":
						_, _ = w.WriteString("+PONG\r\n")
					default:
						_, _ = w.WriteString("+OK\r\n")
					}
					_ = w.Flush()
				}
			}(c)
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port
}

func TestRedisViewerSettingsAndGating(t *testing.T) {
	port := startMockRedisForAdmin(t)
	h := newTestHandler(t)
	h.infra = infrasvc.NewService("", false)
	h.infra.SetResult(infrasvc.ProbeResult{
		Name:   "Redis",
		Kind:   "redis",
		Host:   "127.0.0.1",
		Port:   port,
		Status: "connected",
	})
	h.SetRedisViewer(redisviewer.NewService(false))

	targetID := fmt.Sprintf("redis-127.0.0.1-%d", port)

	// Disabled by default -> target detail returns 403
	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/redis/targets/"+targetID, nil)
	req.SetPathValue("id", targetID)
	w := httptest.NewRecorder()
	h.RedisTargetDetail(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when disabled, got %d: %s", w.Code, w.Body.String())
	}

	// Enable at runtime via settings endpoint
	w = httptest.NewRecorder()
	h.SetRedisViewerSettings(w, httptest.NewRequest(http.MethodPut, "/_tarn/admin/redis/settings", bytes.NewBufferString(`{"enabled":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("set settings failed: %d %s", w.Code, w.Body.String())
	}

	// Overview reflects enabled state immediately (cache invalidated)
	w = httptest.NewRecorder()
	h.Overview(w, httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil))
	var ov overviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &ov); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if !ov.Config.RedisViewerEnabled {
		t.Fatal("expected overview.config.redisViewerEnabled=true")
	}

	// Unknown target rejected (prevents arbitrary SSRF)
	badReq := httptest.NewRequest(http.MethodGet, "/_tarn/admin/redis/targets/redis-169.254.169.254-6379", nil)
	badReq.SetPathValue("id", "redis-169.254.169.254-6379")
	w = httptest.NewRecorder()
	h.RedisTargetDetail(w, badReq)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unregistered target, got %d", w.Code)
	}

	// Seed a redis span trace so integrations picks it up
	h.traceStore = tracesvc.OpenStore(t.TempDir())
	t.Cleanup(func() { _ = h.traceStore.Close() })
	h.traceStore.Add(&tracesvc.Trace{
		ID:         "tr-redis-1",
		StartedAt:  time.Now().UTC(),
		DurationMs: 12,
		Status:     200,
		Method:     "GET",
		Path:       "/cache",
		Spans: []tracesvc.Span{
			{Kind: "redis", Name: "Redis", DurationMs: 2, Status: "ok"},
		},
	})

	// Valid target returns INFO + integrations
	req = httptest.NewRequest(http.MethodGet, "/_tarn/admin/redis/targets/"+targetID, nil)
	req.SetPathValue("id", targetID)
	w = httptest.NewRecorder()
	h.RedisTargetDetail(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", w.Code, w.Body.String())
	}
	var detail redisTargetDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Info == nil || detail.Info.Version != "7.4.1" {
		t.Fatalf("expected live redis INFO 7.4.1, got %+v (err=%s)", detail.Info, detail.InfoError)
	}
	if len(detail.Integrations.Traces) != 1 || detail.Integrations.Traces[0].ID != "tr-redis-1" {
		t.Fatalf("expected redis trace integration, got %+v", detail.Integrations.Traces)
	}

	// Scan keys
	scanReq := httptest.NewRequest(http.MethodPost, "/_tarn/admin/redis/targets/"+targetID+"/scan", bytes.NewBufferString(`{"db":0,"pattern":"*"}`))
	scanReq.SetPathValue("id", targetID)
	w = httptest.NewRecorder()
	h.RedisScanKeys(w, scanReq)
	if w.Code != http.StatusOK {
		t.Fatalf("scan status=%d body=%s", w.Code, w.Body.String())
	}
	var scanRes redisviewer.ScanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &scanRes); err != nil {
		t.Fatalf("decode scan: %v", err)
	}
	if len(scanRes.Keys) != 1 || scanRes.Keys[0].Key != "session:1" {
		t.Fatalf("unexpected scan keys: %+v", scanRes)
	}
}
