package redisviewer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// startFakeRedis spins up a lightweight in-memory RESP2 TCP server for tests.
func startFakeRedis(t *testing.T) (string, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var mu sync.Mutex
	stringsStore := map[string]string{
		"user:1":     `{"name":"Ada"}`,
		"bin:sample": string([]byte{0xff, 0xfe, 0x01}),
	}
	hashesStore := map[string]map[string]string{
		"session:abc": {"role": "admin", "tenant": "t1"},
	}
	listsStore := map[string][]string{
		"jobs:pending": {"j1", "j2"},
	}
	setsStore := map[string][]string{
		"tags:live": {"alpha", "beta"},
	}
	zsetsStore := map[string][]ZSetMember{
		"scores": {{Member: "alice", Score: 10.5}, {Member: "bob", Score: 22}},
	}
	streamsStore := map[string][]StreamEntry{
		"events:orders": {{ID: "1700000000000-0", Fields: map[string]string{"order": "42"}}},
	}
	ttls := map[string]int64{
		"user:1": 120000,
	}

	writeBulk := func(w *bufio.Writer, s string) {
		_, _ = fmt.Fprintf(w, "$%d\r\n%s\r\n", len(s), s)
	}

	keyTypeOf := func(k string) string {
		if _, ok := stringsStore[k]; ok {
			return "string"
		}
		if _, ok := hashesStore[k]; ok {
			return "hash"
		}
		if _, ok := listsStore[k]; ok {
			return "list"
		}
		if _, ok := setsStore[k]; ok {
			return "set"
		}
		if _, ok := zsetsStore[k]; ok {
			return "zset"
		}
		if _, ok := streamsStore[k]; ok {
			return "stream"
		}
		return "none"
	}

	allKeys := func() []string {
		var keys []string
		for k := range stringsStore {
			keys = append(keys, k)
		}
		for k := range hashesStore {
			keys = append(keys, k)
		}
		for k := range listsStore {
			keys = append(keys, k)
		}
		for k := range setsStore {
			keys = append(keys, k)
		}
		for k := range zsetsStore {
			keys = append(keys, k)
		}
		for k := range streamsStore {
			keys = append(keys, k)
		}
		return keys
	}

	handle := func(c net.Conn) {
		defer func() { _ = c.Close() }()
		r := bufio.NewReader(c)
		w := bufio.NewWriter(c)
		for {
			val, err := readRESP(r, 0)
			if err != nil {
				if err != io.EOF {
					return
				}
				return
			}
			if val.kind != respArray || len(val.arr) == 0 {
				_, _ = w.WriteString("-ERR bad request\r\n")
				_ = w.Flush()
				continue
			}
			args := make([]string, len(val.arr))
			for i, a := range val.arr {
				args[i] = a.str
			}
			cmd := strings.ToUpper(args[0])

			mu.Lock()
			switch cmd {
			case "PING":
				_, _ = w.WriteString("+PONG\r\n")
			case "AUTH":
				if len(args) >= 2 && args[len(args)-1] == "secret" {
					_, _ = w.WriteString("+OK\r\n")
				} else {
					_, _ = w.WriteString("-WRONGPASS invalid credentials\r\n")
				}
			case "SELECT", "MULTI":
				_, _ = w.WriteString("+OK\r\n")
			case "EXEC":
				_, _ = w.WriteString("*0\r\n")
			case "INFO":
				info := "# Server\r\nredis_version:7.2.5\r\nredis_mode:standalone\r\nos:Darwin\r\nuptime_in_seconds:3600\r\n" +
					"# Clients\r\nconnected_clients:3\r\n" +
					"# Memory\r\nused_memory:1048576\r\nused_memory_human:1.00M\r\nused_memory_peak_human:2.00M\r\nmaxmemory:0\r\nmaxmemory_human:0B\r\n" +
					"# Stats\r\ninstantaneous_ops_per_sec:42\r\nkeyspace_hits:90\r\nkeyspace_misses:10\r\ntotal_commands_processed:500\r\n" +
					"# Replication\r\nrole:master\r\n" +
					"# Keyspace\r\ndb0:keys=6,expires=1,avg_ttl=120000\r\n"
				writeBulk(w, info)
			case "DBSIZE":
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(allKeys()))
			case "SCAN":
				match := "*"
				typeFilter := ""
				for i := 2; i+1 < len(args); i += 2 {
					switch strings.ToUpper(args[i]) {
					case "MATCH":
						match = args[i+1]
					case "TYPE":
						typeFilter = strings.ToLower(args[i+1])
					}
				}
				var matched []string
				for _, k := range allKeys() {
					if typeFilter != "" && keyTypeOf(k) != typeFilter {
						continue
					}
					if match != "*" && !strings.HasPrefix(k, strings.TrimSuffix(match, "*")) {
						continue
					}
					matched = append(matched, k)
				}
				_, _ = w.WriteString("*2\r\n$1\r\n0\r\n")
				_, _ = fmt.Fprintf(w, "*%d\r\n", len(matched))
				for _, k := range matched {
					writeBulk(w, k)
				}
			case "TYPE":
				_, _ = fmt.Fprintf(w, "+%s\r\n", keyTypeOf(args[1]))
			case "PTTL":
				if keyTypeOf(args[1]) == "none" {
					_, _ = w.WriteString(":-2\r\n")
				} else if ttl, ok := ttls[args[1]]; ok {
					_, _ = fmt.Fprintf(w, ":%d\r\n", ttl)
				} else {
					_, _ = w.WriteString(":-1\r\n")
				}
			case "OBJECT":
				writeBulk(w, "embstr")
			case "MEMORY":
				_, _ = w.WriteString(":64\r\n")
			case "STRLEN":
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(stringsStore[args[1]]))
			case "GET":
				if v, ok := stringsStore[args[1]]; ok {
					writeBulk(w, v)
				} else {
					_, _ = w.WriteString("$-1\r\n")
				}
			case "GETRANGE":
				v := stringsStore[args[1]]
				end, _ := strconv.Atoi(args[3])
				if end >= len(v) {
					end = len(v) - 1
				}
				if end < 0 || len(v) == 0 {
					writeBulk(w, "")
				} else {
					writeBulk(w, v[:end+1])
				}
			case "SET":
				stringsStore[args[1]] = args[2]
				for i := 3; i+1 < len(args); i++ {
					if strings.EqualFold(args[i], "EX") {
						sec, _ := strconv.ParseInt(args[i+1], 10, 64)
						ttls[args[1]] = sec * 1000
					}
				}
				_, _ = w.WriteString("+OK\r\n")
			case "EXPIRE":
				sec, _ := strconv.ParseInt(args[2], 10, 64)
				ttls[args[1]] = sec * 1000
				_, _ = w.WriteString(":1\r\n")
			case "PERSIST":
				delete(ttls, args[1])
				_, _ = w.WriteString(":1\r\n")
			case "DEL":
				var deleted int64
				for _, k := range args[1:] {
					if keyTypeOf(k) != "none" {
						deleted++
					}
					delete(stringsStore, k)
					delete(hashesStore, k)
					delete(listsStore, k)
					delete(setsStore, k)
					delete(zsetsStore, k)
					delete(streamsStore, k)
					delete(ttls, k)
				}
				_, _ = fmt.Fprintf(w, ":%d\r\n", deleted)
			case "HLEN":
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(hashesStore[args[1]]))
			case "HSCAN":
				m := hashesStore[args[1]]
				_, _ = w.WriteString("*2\r\n$1\r\n0\r\n")
				_, _ = fmt.Fprintf(w, "*%d\r\n", len(m)*2)
				for k, v := range m {
					writeBulk(w, k)
					writeBulk(w, v)
				}
			case "HSET":
				m := hashesStore[args[1]]
				if m == nil {
					m = map[string]string{}
					hashesStore[args[1]] = m
				}
				for i := 2; i+1 < len(args); i += 2 {
					m[args[i]] = args[i+1]
				}
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(m))
			case "HDEL":
				delete(hashesStore[args[1]], args[2])
				_, _ = w.WriteString(":1\r\n")
			case "LLEN":
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(listsStore[args[1]]))
			case "LRANGE":
				items := listsStore[args[1]]
				start, _ := strconv.Atoi(args[2])
				stop, _ := strconv.Atoi(args[3])
				if start < 0 {
					start = 0
				}
				if stop >= len(items) {
					stop = len(items) - 1
				}
				if start > stop || len(items) == 0 {
					_, _ = w.WriteString("*0\r\n")
				} else {
					slice := items[start : stop+1]
					_, _ = fmt.Fprintf(w, "*%d\r\n", len(slice))
					for _, item := range slice {
						writeBulk(w, item)
					}
				}
			case "LSET":
				idx, _ := strconv.Atoi(args[2])
				listsStore[args[1]][idx] = args[3]
				_, _ = w.WriteString("+OK\r\n")
			case "LREM":
				target := args[3]
				var kept []string
				removed := false
				for _, v := range listsStore[args[1]] {
					if !removed && v == target {
						removed = true
						continue
					}
					kept = append(kept, v)
				}
				listsStore[args[1]] = kept
				_, _ = w.WriteString(":1\r\n")
			case "RPUSH":
				listsStore[args[1]] = append(listsStore[args[1]], args[2:]...)
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(listsStore[args[1]]))
			case "SCARD":
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(setsStore[args[1]]))
			case "SSCAN":
				members := setsStore[args[1]]
				_, _ = w.WriteString("*2\r\n$1\r\n0\r\n")
				_, _ = fmt.Fprintf(w, "*%d\r\n", len(members))
				for _, m := range members {
					writeBulk(w, m)
				}
			case "SADD":
				setsStore[args[1]] = append(setsStore[args[1]], args[2:]...)
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(args[2:]))
			case "SREM":
				var kept []string
				for _, m := range setsStore[args[1]] {
					if m != args[2] {
						kept = append(kept, m)
					}
				}
				setsStore[args[1]] = kept
				_, _ = w.WriteString(":1\r\n")
			case "ZCARD":
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(zsetsStore[args[1]]))
			case "ZRANGE":
				members := zsetsStore[args[1]]
				start, _ := strconv.Atoi(args[2])
				stop, _ := strconv.Atoi(args[3])
				if start < 0 {
					start = 0
				}
				if stop >= len(members) {
					stop = len(members) - 1
				}
				if start > stop || len(members) == 0 {
					_, _ = w.WriteString("*0\r\n")
				} else {
					slice := members[start : stop+1]
					_, _ = fmt.Fprintf(w, "*%d\r\n", len(slice)*2)
					for _, m := range slice {
						writeBulk(w, m.Member)
						writeBulk(w, strconv.FormatFloat(m.Score, 'f', -1, 64))
					}
				}
			case "ZADD":
				for i := 2; i+1 < len(args); i += 2 {
					score, _ := strconv.ParseFloat(args[i], 64)
					zsetsStore[args[1]] = append(zsetsStore[args[1]], ZSetMember{Member: args[i+1], Score: score})
				}
				_, _ = w.WriteString(":1\r\n")
			case "ZREM":
				var kept []ZSetMember
				for _, m := range zsetsStore[args[1]] {
					if m.Member != args[2] {
						kept = append(kept, m)
					}
				}
				zsetsStore[args[1]] = kept
				_, _ = w.WriteString(":1\r\n")
			case "XLEN":
				_, _ = fmt.Fprintf(w, ":%d\r\n", len(streamsStore[args[1]]))
			case "XREVRANGE":
				entries := streamsStore[args[1]]
				_, _ = fmt.Fprintf(w, "*%d\r\n", len(entries))
				for _, e := range entries {
					_, _ = w.WriteString("*2\r\n")
					writeBulk(w, e.ID)
					_, _ = fmt.Fprintf(w, "*%d\r\n", len(e.Fields)*2)
					for fk, fv := range e.Fields {
						writeBulk(w, fk)
						writeBulk(w, fv)
					}
				}
			case "XADD":
				fields := map[string]string{}
				for i := 3; i+1 < len(args); i += 2 {
					fields[args[i]] = args[i+1]
				}
				id := args[2]
				if id == "*" {
					id = "1700000000001-0"
				}
				streamsStore[args[1]] = append(streamsStore[args[1]], StreamEntry{ID: id, Fields: fields})
				writeBulk(w, id)
			case "XDEL":
				var kept []StreamEntry
				for _, e := range streamsStore[args[1]] {
					if e.ID != args[2] {
						kept = append(kept, e)
					}
				}
				streamsStore[args[1]] = kept
				_, _ = w.WriteString(":1\r\n")
			default:
				_, _ = fmt.Fprintf(w, "-ERR unknown command %s\r\n", cmd)
			}
			mu.Unlock()
			_ = w.Flush()
		}
	}

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c)
		}
	}()

	tcpAddr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", tcpAddr.Port
}

func TestRedisViewerClientOperations(t *testing.T) {
	host, port := startFakeRedis(t)
	cfg := ConnConfig{Host: host, Port: port, DB: 0}
	ctx := context.Background()

	info, err := FetchServerInfo(ctx, cfg)
	if err != nil {
		t.Fatalf("FetchServerInfo: %v", err)
	}
	if info.Version != "7.2.5" || info.ConnectedClients != 3 || len(info.Databases) != 1 || info.Databases[0].Keys != 6 {
		t.Fatalf("unexpected info: %+v", info)
	}

	scan, err := ScanKeys(ctx, cfg, ScanRequest{Pattern: "user:*", Count: 50})
	if err != nil {
		t.Fatalf("ScanKeys: %v", err)
	}
	if len(scan.Keys) != 1 || scan.Keys[0].Key != "user:1" || scan.Keys[0].Type != "string" {
		t.Fatalf("unexpected scan result: %+v", scan)
	}

	// Inspect every data type
	strDetail, err := InspectKey(ctx, cfg, "user:1", 50)
	if err != nil || !strDetail.Found || strDetail.StringValue != `{"name":"Ada"}` || strDetail.TTLMs != 120000 {
		t.Fatalf("unexpected string detail: %+v, err=%v", strDetail, err)
	}

	binDetail, err := InspectKey(ctx, cfg, "bin:sample", 50)
	if err != nil || !binDetail.IsBinary {
		t.Fatalf("expected binary flag on invalid utf8 string: %+v, err=%v", binDetail, err)
	}

	hashDetail, err := InspectKey(ctx, cfg, "session:abc", 50)
	if err != nil || len(hashDetail.HashEntries) != 2 {
		t.Fatalf("unexpected hash detail: %+v, err=%v", hashDetail, err)
	}

	listDetail, err := InspectKey(ctx, cfg, "jobs:pending", 50)
	if err != nil || len(listDetail.ListItems) != 2 {
		t.Fatalf("unexpected list detail: %+v, err=%v", listDetail, err)
	}

	setDetail, err := InspectKey(ctx, cfg, "tags:live", 50)
	if err != nil || len(setDetail.SetMembers) != 2 {
		t.Fatalf("unexpected set detail: %+v, err=%v", setDetail, err)
	}

	zsetDetail, err := InspectKey(ctx, cfg, "scores", 50)
	if err != nil || len(zsetDetail.ZSetMembers) != 2 || zsetDetail.ZSetMembers[0].Score != 10.5 {
		t.Fatalf("unexpected zset detail: %+v, err=%v", zsetDetail, err)
	}

	streamDetail, err := InspectKey(ctx, cfg, "events:orders", 50)
	if err != nil || len(streamDetail.StreamEntries) != 1 || streamDetail.StreamEntries[0].Fields["order"] != "42" {
		t.Fatalf("unexpected stream detail: %+v, err=%v", streamDetail, err)
	}

	// Create, update, and delete
	ttl := int64(60)
	if err := SetKey(ctx, cfg, SetKeyRequest{Key: "new:key", Type: "string", StringValue: "hello", TTLSeconds: &ttl}); err != nil {
		t.Fatalf("SetKey string: %v", err)
	}
	if err := SetKey(ctx, cfg, SetKeyRequest{Key: "new:hash", Type: "hash", HashEntries: []HashEntry{{Field: "a", Value: "1"}}}); err != nil {
		t.Fatalf("SetKey hash: %v", err)
	}
	deleted, err := DeleteKeys(ctx, cfg, []string{"new:key", "new:hash"})
	if err != nil || deleted != 2 {
		t.Fatalf("DeleteKeys: deleted=%d err=%v", deleted, err)
	}

	// Guarded CLI
	cmdRes, err := ExecCommand(ctx, cfg, `SET "quoted key" "hello world"`)
	if err != nil || cmdRes.ResultType != "ok" {
		t.Fatalf("ExecCommand SET: %+v, err=%v", cmdRes, err)
	}
	if _, err := ExecCommand(ctx, cfg, "MONITOR"); err == nil {
		t.Fatal("expected MONITOR to be blocked")
	}
	if _, err := ExecCommand(ctx, cfg, "CONFIG SET dir /tmp"); err == nil {
		t.Fatal("expected CONFIG SET to be blocked")
	}

	// Incremental pagination & item-level mutations
	page1, err := InspectKeyPage(ctx, cfg, InspectOptions{Key: "jobs:pending", Limit: 1, Offset: 0})
	if err != nil || !page1.HasMore || page1.NextOffset != 1 || len(page1.ListItems) != 1 || page1.ListItems[0] != "j1" {
		t.Fatalf("unexpected list page1: %+v, err=%v", page1, err)
	}
	page2, err := InspectKeyPage(ctx, cfg, InspectOptions{Key: "jobs:pending", Limit: 1, Offset: page1.NextOffset})
	if err != nil || page2.HasMore || len(page2.ListItems) != 1 || page2.ListItems[0] != "j2" {
		t.Fatalf("unexpected list page2: %+v, err=%v", page2, err)
	}

	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "session:abc", Action: "hset", Field: "env", Value: "local"}); err != nil {
		t.Fatalf("MutateKey hset: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "session:abc", Action: "hdel", Field: "tenant"}); err != nil {
		t.Fatalf("MutateKey hdel: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "jobs:pending", Action: "rpush", Value: "j3"}); err != nil {
		t.Fatalf("MutateKey rpush: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "jobs:pending", Action: "lset", Index: 0, Value: "j1-updated"}); err != nil {
		t.Fatalf("MutateKey lset: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "jobs:pending", Action: "lrem", Value: "j2"}); err != nil {
		t.Fatalf("MutateKey lrem: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "tags:live", Action: "sadd", Member: "gamma"}); err != nil {
		t.Fatalf("MutateKey sadd: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "tags:live", Action: "srem", Member: "alpha"}); err != nil {
		t.Fatalf("MutateKey srem: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "scores", Action: "zadd", Member: "charlie", Score: 30}); err != nil {
		t.Fatalf("MutateKey zadd: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "scores", Action: "zrem", Member: "alice"}); err != nil {
		t.Fatalf("MutateKey zrem: %v", err)
	}
	newTTL := int64(300)
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "user:1", Action: "ttl", TTLSeconds: &newTTL}); err != nil {
		t.Fatalf("MutateKey ttl: %v", err)
	}
	if err := MutateKey(ctx, cfg, MutateKeyRequest{Key: "events:orders", Action: "xdel", StreamID: "1700000000000-0"}); err != nil {
		t.Fatalf("MutateKey xdel: %v", err)
	}
}

func TestServiceStatePersistenceAndOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "redis-viewer.json")

	svc := NewService(false)
	if err := svc.LoadState(path, false); err != nil {
		t.Fatalf("LoadState initial: %v", err)
	}
	if svc.Enabled() {
		t.Fatal("expected disabled by default")
	}
	if err := svc.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	// Without explicit CLI override, persisted preference restores.
	reloaded := NewService(false)
	if err := reloaded.LoadState(path, false); err != nil {
		t.Fatalf("LoadState reload: %v", err)
	}
	if !reloaded.Enabled() {
		t.Fatal("expected persisted enabled=true to load")
	}

	// Explicit CLI override wins over persisted file.
	overridden := NewService(false)
	if err := overridden.LoadState(path, true); err != nil {
		t.Fatalf("LoadState override: %v", err)
	}
	if overridden.Enabled() {
		t.Fatal("expected explicit override=false to win over persisted file")
	}
}
