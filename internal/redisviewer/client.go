package redisviewer

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	dialTimeout          = 2500 * time.Millisecond
	opTimeout            = 5 * time.Second
	maxBulkReadBytes     = 1 << 20 // 1 MiB safety cap per bulk string
	maxArrayElements     = 10000
	maxRespDepth         = 8
	defaultScanCount     = 100
	maxScanCount         = 500
	defaultInspectLimit  = 100
	maxInspectLimit      = 500
	maxStringDisplaySize = 256 << 10 // 256 KiB
)

// ConnConfig describes how to connect to a probed Redis instance.
type ConnConfig struct {
	Host     string
	Port     int
	DB       int
	Username string
	Password string
	TLS      bool
}

// DBKeyspace summarizes a single Redis database index from INFO keyspace.
type DBKeyspace struct {
	DB      int   `json:"db"`
	Keys    int64 `json:"keys"`
	Expires int64 `json:"expires"`
	AvgTTL  int64 `json:"avgTtlMs"`
}

// ServerInfo captures parsed Redis INFO metrics and keyspace metadata.
type ServerInfo struct {
	Version              string       `json:"version"`
	Engine               string       `json:"engine"` // "redis" | "valkey"
	Mode                 string       `json:"mode"`
	OS                   string       `json:"os"`
	Role                 string       `json:"role"`
	UptimeSeconds        int64        `json:"uptimeSeconds"`
	ConnectedClients     int64        `json:"connectedClients"`
	UsedMemoryBytes      int64        `json:"usedMemoryBytes"`
	UsedMemoryHuman      string       `json:"usedMemoryHuman"`
	UsedMemoryPeakHuman  string       `json:"usedMemoryPeakHuman"`
	MaxMemoryBytes       int64        `json:"maxMemoryBytes"`
	MaxMemoryHuman       string       `json:"maxMemoryHuman"`
	OpsPerSec            int64        `json:"opsPerSec"`
	KeyspaceHits         int64        `json:"keyspaceHits"`
	KeyspaceMisses       int64        `json:"keyspaceMisses"`
	TotalKeys            int64        `json:"totalKeys"`
	SelectedDB           int          `json:"selectedDb"`
	SelectedDBKeys       int64        `json:"selectedDbKeys"`
	Databases            []DBKeyspace `json:"databases"`
	TotalCommandsHandled int64        `json:"totalCommandsHandled"`
}

// KeySummary describes one scanned key in a Redis database.
type KeySummary struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	TTLMs       int64  `json:"ttlMs"` // -1 no expiry, -2 missing
	MemoryBytes int64  `json:"memoryBytes,omitempty"`
}

// ScanRequest configures a cursor-based SCAN call.
type ScanRequest struct {
	Pattern string `json:"pattern"`
	Type    string `json:"type"`
	Cursor  string `json:"cursor"`
	Count   int    `json:"count"`
}

// ScanResponse returns one page of scanned keys.
type ScanResponse struct {
	Cursor       string       `json:"cursor"`
	HasMore      bool         `json:"hasMore"`
	Keys         []KeySummary `json:"keys"`
	ScannedCount int          `json:"scannedCount"`
	DBSize       int64        `json:"dbSize"`
}

// HashEntry represents a field/value pair in a Redis hash.
type HashEntry struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// ZSetMember represents a scored member in a Redis sorted set.
type ZSetMember struct {
	Member string  `json:"member"`
	Score  float64 `json:"score"`
}

// StreamEntry represents one Redis stream entry.
type StreamEntry struct {
	ID     string            `json:"id"`
	Fields map[string]string `json:"fields"`
}

// KeyDetail holds the full inspected value and metadata for one key.
type KeyDetail struct {
	Found         bool          `json:"found"`
	Key           string        `json:"key"`
	Type          string        `json:"type"`
	TTLMs         int64         `json:"ttlMs"`
	Encoding      string        `json:"encoding,omitempty"`
	MemoryBytes   int64         `json:"memoryBytes,omitempty"`
	Length        int64         `json:"length"`
	Truncated     bool          `json:"truncated,omitempty"`
	IsBinary      bool          `json:"isBinary,omitempty"`
	StringValue   string        `json:"stringValue,omitempty"`
	HashEntries   []HashEntry   `json:"hashEntries,omitempty"`
	ListItems     []string      `json:"listItems,omitempty"`
	SetMembers    []string      `json:"setMembers,omitempty"`
	ZSetMembers   []ZSetMember  `json:"zsetMembers,omitempty"`
	StreamEntries []StreamEntry `json:"streamEntries,omitempty"`
}

// SetKeyRequest creates or replaces a Redis key value and optional TTL.
type SetKeyRequest struct {
	Key          string            `json:"key"`
	Type         string            `json:"type"` // string, hash, list, set, zset, stream
	StringValue  string            `json:"stringValue,omitempty"`
	HashEntries  []HashEntry       `json:"hashEntries,omitempty"`
	ListItems    []string          `json:"listItems,omitempty"`
	SetMembers   []string          `json:"setMembers,omitempty"`
	ZSetMembers  []ZSetMember      `json:"zsetMembers,omitempty"`
	StreamFields map[string]string `json:"streamFields,omitempty"`
	StreamID     string            `json:"streamId,omitempty"`
	TTLSeconds   *int64            `json:"ttlSeconds,omitempty"` // nil = keep/default, <=0 = persist, >0 = expire
	KeepTTL      bool              `json:"keepTtl,omitempty"`
}

// CommandResult is the outcome of a guarded CLI command execution.
type CommandResult struct {
	Command    string `json:"command"`
	ResultType string `json:"resultType"` // string, integer, array, nil, ok
	Result     any    `json:"result"`
	Formatted  string `json:"formatted"`
	DurationMs int64  `json:"durationMs"`
}

// respKind identifies a RESP2 wire value type.
type respKind byte

const (
	respSimple respKind = '+'
	respError  respKind = '-'
	respInt    respKind = ':'
	respBulk   respKind = '$'
	respArray  respKind = '*'
)

type respValue struct {
	kind  respKind
	str   string
	num   int64
	isNil bool
	arr   []respValue
}

func (v respValue) err() error {
	if v.kind == respError {
		return errors.New(v.str)
	}
	return nil
}

type respConn struct {
	raw net.Conn
	br  *bufio.Reader
	bw  *bufio.Writer
}

func dial(ctx context.Context, cfg ConnConfig) (*respConn, error) {
	if cfg.Host == "" || cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid redis target %s:%d", cfg.Host, cfg.Port)
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: dialTimeout}

	var (
		raw net.Conn
		err error
	)
	if cfg.TLS {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12},
		}
		raw, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		raw, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(opTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = raw.SetDeadline(deadline)

	rc := &respConn{
		raw: raw,
		br:  bufio.NewReaderSize(raw, 16*1024),
		bw:  bufio.NewWriterSize(raw, 8*1024),
	}

	if cfg.Password != "" {
		var authArgs []string
		if cfg.Username != "" {
			authArgs = []string{"AUTH", cfg.Username, cfg.Password}
		} else {
			authArgs = []string{"AUTH", cfg.Password}
		}
		if _, err := rc.do(authArgs...); err != nil {
			_ = raw.Close()
			return nil, fmt.Errorf("redis auth failed: %w", err)
		}
	}

	if cfg.DB < 0 || cfg.DB > 15 {
		_ = raw.Close()
		return nil, fmt.Errorf("redis db index %d out of range (0-15)", cfg.DB)
	}
	if cfg.DB > 0 {
		if _, err := rc.do("SELECT", strconv.Itoa(cfg.DB)); err != nil {
			_ = raw.Close()
			return nil, fmt.Errorf("redis SELECT %d failed: %w", cfg.DB, err)
		}
	}

	return rc, nil
}

func (c *respConn) Close() error {
	return c.raw.Close()
}

func (c *respConn) writeCmd(args ...string) error {
	if _, err := fmt.Fprintf(c.bw, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(c.bw, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

func (c *respConn) do(args ...string) (respValue, error) {
	if err := c.writeCmd(args...); err != nil {
		return respValue{}, err
	}
	if err := c.bw.Flush(); err != nil {
		return respValue{}, err
	}
	val, err := readRESP(c.br, 0)
	if err != nil {
		return respValue{}, err
	}
	if err := val.err(); err != nil {
		return val, err
	}
	return val, nil
}

func (c *respConn) pipeline(cmds [][]string) ([]respValue, error) {
	if len(cmds) == 0 {
		return nil, nil
	}
	for _, cmd := range cmds {
		if err := c.writeCmd(cmd...); err != nil {
			return nil, err
		}
	}
	if err := c.bw.Flush(); err != nil {
		return nil, err
	}
	out := make([]respValue, len(cmds))
	for i := range cmds {
		v, err := readRESP(c.br, 0)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func readRESP(r *bufio.Reader, depth int) (respValue, error) {
	if depth > maxRespDepth {
		return respValue{}, errors.New("redis response nesting limit exceeded")
	}
	line, err := readLine(r)
	if err != nil {
		return respValue{}, err
	}
	if len(line) == 0 {
		return respValue{}, errors.New("empty redis response line")
	}
	prefix := respKind(line[0])
	rest := line[1:]

	switch prefix {
	case respSimple, respError:
		return respValue{kind: prefix, str: rest}, nil
	case respInt:
		n, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return respValue{}, fmt.Errorf("invalid redis integer %q: %w", rest, err)
		}
		return respValue{kind: respInt, num: n}, nil
	case respBulk:
		size, err := strconv.Atoi(rest)
		if err != nil {
			return respValue{}, fmt.Errorf("invalid bulk length %q: %w", rest, err)
		}
		if size < 0 {
			return respValue{kind: respBulk, isNil: true}, nil
		}
		if size > maxBulkReadBytes {
			// Drain to keep stream aligned? Safer to abort connection.
			return respValue{}, fmt.Errorf("redis bulk value too large (%d bytes > %d max)", size, maxBulkReadBytes)
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return respValue{}, err
		}
		return respValue{kind: respBulk, str: string(buf[:size])}, nil
	case respArray:
		count, err := strconv.Atoi(rest)
		if err != nil {
			return respValue{}, fmt.Errorf("invalid array length %q: %w", rest, err)
		}
		if count < 0 {
			return respValue{kind: respArray, isNil: true}, nil
		}
		if count > maxArrayElements {
			return respValue{}, fmt.Errorf("redis array too large (%d elements)", count)
		}
		arr := make([]respValue, count)
		for i := 0; i < count; i++ {
			elem, err := readRESP(r, depth+1)
			if err != nil {
				return respValue{}, err
			}
			arr[i] = elem
		}
		return respValue{kind: respArray, arr: arr}, nil
	default:
		return respValue{}, fmt.Errorf("unexpected redis RESP prefix %q", line[0])
	}
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// FetchServerInfo connects to Redis and returns parsed INFO and keyspace details.
func FetchServerInfo(ctx context.Context, cfg ConnConfig) (*ServerInfo, error) {
	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	res, err := conn.pipeline([][]string{
		{"INFO"},
		{"DBSIZE"},
	})
	if err != nil {
		return nil, err
	}
	if err := res[0].err(); err != nil {
		return nil, err
	}

	info := parseInfoOutput(res[0].str)
	info.SelectedDB = cfg.DB
	if res[1].kind == respInt {
		info.SelectedDBKeys = res[1].num
	}
	return info, nil
}

func parseInfoOutput(raw string) *ServerInfo {
	info := &ServerInfo{
		Engine:    "redis",
		Databases: []DBKeyspace{},
	}
	section := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "#")))
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch k {
		case "valkey_version":
			if v != "" {
				info.Engine = "valkey"
				info.Version = v
			}
		case "redis_version":
			if info.Version == "" {
				info.Version = v
			}
		case "redis_mode", "server_mode":
			if info.Mode == "" {
				info.Mode = v
			}
		case "os":
			info.OS = v
		case "role":
			info.Role = v
		case "uptime_in_seconds":
			info.UptimeSeconds, _ = strconv.ParseInt(v, 10, 64)
		case "connected_clients":
			info.ConnectedClients, _ = strconv.ParseInt(v, 10, 64)
		case "used_memory":
			info.UsedMemoryBytes, _ = strconv.ParseInt(v, 10, 64)
		case "used_memory_human":
			info.UsedMemoryHuman = v
		case "used_memory_peak_human":
			info.UsedMemoryPeakHuman = v
		case "maxmemory":
			info.MaxMemoryBytes, _ = strconv.ParseInt(v, 10, 64)
		case "maxmemory_human":
			info.MaxMemoryHuman = v
		case "instantaneous_ops_per_sec":
			info.OpsPerSec, _ = strconv.ParseInt(v, 10, 64)
		case "keyspace_hits":
			info.KeyspaceHits, _ = strconv.ParseInt(v, 10, 64)
		case "keyspace_misses":
			info.KeyspaceMisses, _ = strconv.ParseInt(v, 10, 64)
		case "total_commands_processed":
			info.TotalCommandsHandled, _ = strconv.ParseInt(v, 10, 64)
		default:
			if section == "keyspace" && strings.HasPrefix(k, "db") {
				if idx, err := strconv.Atoi(strings.TrimPrefix(k, "db")); err == nil && idx >= 0 {
					ks := parseDBKeyspace(idx, v)
					info.Databases = append(info.Databases, ks)
					info.TotalKeys += ks.Keys
				}
			}
		}
	}
	return info
}

func parseDBKeyspace(db int, raw string) DBKeyspace {
	ks := DBKeyspace{DB: db}
	for _, part := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		switch k {
		case "keys":
			ks.Keys = n
		case "expires":
			ks.Expires = n
		case "avg_ttl":
			ks.AvgTTL = n
		}
	}
	return ks
}

// ScanKeys performs cursor-based SCAN and pipelines TYPE, PTTL, and MEMORY USAGE.
func ScanKeys(ctx context.Context, cfg ConnConfig, req ScanRequest) (*ScanResponse, error) {
	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	cursor := strings.TrimSpace(req.Cursor)
	if cursor == "" {
		cursor = "0"
	}
	if _, err := strconv.ParseUint(cursor, 10, 64); err != nil {
		return nil, fmt.Errorf("invalid scan cursor %q", cursor)
	}
	count := req.Count
	if count <= 0 {
		count = defaultScanCount
	}
	if count > maxScanCount {
		count = maxScanCount
	}
	pattern := strings.TrimSpace(req.Pattern)
	if pattern == "" {
		pattern = "*"
	}
	keyType := strings.ToLower(strings.TrimSpace(req.Type))
	if keyType == "all" {
		keyType = ""
	}

	buildScanArgs := func(includeType bool) []string {
		args := []string{"SCAN", cursor, "MATCH", pattern, "COUNT", strconv.Itoa(count)}
		if includeType && keyType != "" {
			args = append(args, "TYPE", keyType)
		}
		return args
	}

	filterClientSide := false
	scanVal, err := conn.do(buildScanArgs(true)...)
	if err != nil && keyType != "" && strings.Contains(strings.ToLower(err.Error()), "syntax") {
		// Older Redis (<6.0) lacks SCAN TYPE; fall back to client-side filter.
		filterClientSide = true
		scanVal, err = conn.do(buildScanArgs(false)...)
	}
	if err != nil {
		return nil, err
	}
	if scanVal.kind != respArray || len(scanVal.arr) != 2 {
		return nil, errors.New("unexpected SCAN response shape")
	}

	nextCursor := scanVal.arr[0].str
	rawKeys := scanVal.arr[1].arr
	scannedCount := len(rawKeys)

	// Fetch DBSIZE alongside per-key metadata in one pipelined batch.
	cmds := make([][]string, 0, 1+len(rawKeys)*3)
	cmds = append(cmds, []string{"DBSIZE"})
	for _, kv := range rawKeys {
		cmds = append(cmds,
			[]string{"TYPE", kv.str},
			[]string{"PTTL", kv.str},
			[]string{"MEMORY", "USAGE", kv.str, "SAMPLES", "0"},
		)
	}
	metaVals, err := conn.pipeline(cmds)
	if err != nil {
		return nil, err
	}

	var dbSize int64
	if len(metaVals) > 0 && metaVals[0].kind == respInt {
		dbSize = metaVals[0].num
	}

	keys := make([]KeySummary, 0, len(rawKeys))
	for i, kv := range rawKeys {
		base := 1 + i*3
		t := metaVals[base].str
		if t == "" || t == "none" {
			continue
		}
		if filterClientSide && keyType != "" && !strings.EqualFold(t, keyType) {
			continue
		}
		ttlMs := int64(-1)
		if metaVals[base+1].kind == respInt {
			ttlMs = metaVals[base+1].num
		}
		var mem int64
		if metaVals[base+2].kind == respInt && !metaVals[base+2].isNil {
			mem = metaVals[base+2].num
		}
		keys = append(keys, KeySummary{
			Key:         kv.str,
			Type:        t,
			TTLMs:       ttlMs,
			MemoryBytes: mem,
		})
	}

	return &ScanResponse{
		Cursor:       nextCursor,
		HasMore:      nextCursor != "" && nextCursor != "0",
		Keys:         keys,
		ScannedCount: scannedCount,
		DBSize:       dbSize,
	}, nil
}

// InspectKey fetches metadata and a bounded, type-aware value snapshot for one key.
func InspectKey(ctx context.Context, cfg ConnConfig, key string, limit int) (*KeyDetail, error) {
	if key == "" {
		return nil, errors.New("key is required")
	}
	if limit <= 0 {
		limit = defaultInspectLimit
	}
	if limit > maxInspectLimit {
		limit = maxInspectLimit
	}

	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	meta, err := conn.pipeline([][]string{
		{"TYPE", key},
		{"PTTL", key},
		{"OBJECT", "ENCODING", key},
		{"MEMORY", "USAGE", key, "SAMPLES", "0"},
	})
	if err != nil {
		return nil, err
	}
	if err := meta[0].err(); err != nil {
		return nil, err
	}

	keyType := meta[0].str
	if keyType == "" || keyType == "none" {
		return &KeyDetail{Found: false, Key: key, Type: "none", TTLMs: -2}, nil
	}

	detail := &KeyDetail{
		Found: true,
		Key:   key,
		Type:  keyType,
		TTLMs: -1,
	}
	if meta[1].kind == respInt {
		detail.TTLMs = meta[1].num
	}
	if meta[2].kind == respBulk && !meta[2].isNil {
		detail.Encoding = meta[2].str
	}
	if meta[3].kind == respInt && !meta[3].isNil {
		detail.MemoryBytes = meta[3].num
	}

	switch keyType {
	case "string":
		vals, err := conn.pipeline([][]string{
			{"STRLEN", key},
			{"GETRANGE", key, "0", strconv.Itoa(maxStringDisplaySize - 1)},
		})
		if err != nil {
			return nil, err
		}
		if vals[0].kind == respInt {
			detail.Length = vals[0].num
		}
		if err := vals[1].err(); err != nil {
			return nil, err
		}
		raw := vals[1].str
		if detail.Length > int64(len(raw)) {
			detail.Truncated = true
		}
		if !utf8.ValidString(raw) {
			detail.IsBinary = true
			detail.StringValue = base64.StdEncoding.EncodeToString([]byte(raw))
		} else {
			detail.StringValue = raw
		}

	case "hash":
		vals, err := conn.pipeline([][]string{
			{"HLEN", key},
			{"HSCAN", key, "0", "COUNT", strconv.Itoa(limit)},
		})
		if err != nil {
			return nil, err
		}
		if vals[0].kind == respInt {
			detail.Length = vals[0].num
		}
		if err := vals[1].err(); err != nil {
			return nil, err
		}
		entries := []HashEntry{}
		if vals[1].kind == respArray && len(vals[1].arr) == 2 {
			pairs := vals[1].arr[1].arr
			for i := 0; i+1 < len(pairs) && len(entries) < limit; i += 2 {
				entries = append(entries, HashEntry{
					Field: safeDisplayString(pairs[i].str),
					Value: safeDisplayString(pairs[i+1].str),
				})
			}
			if vals[1].arr[0].str != "0" || detail.Length > int64(len(entries)) {
				detail.Truncated = true
			}
		}
		detail.HashEntries = entries

	case "list":
		vals, err := conn.pipeline([][]string{
			{"LLEN", key},
			{"LRANGE", key, "0", strconv.Itoa(limit - 1)},
		})
		if err != nil {
			return nil, err
		}
		if vals[0].kind == respInt {
			detail.Length = vals[0].num
		}
		if err := vals[1].err(); err != nil {
			return nil, err
		}
		items := make([]string, 0, len(vals[1].arr))
		for _, elem := range vals[1].arr {
			items = append(items, safeDisplayString(elem.str))
		}
		detail.ListItems = items
		detail.Truncated = detail.Length > int64(len(items))

	case "set":
		vals, err := conn.pipeline([][]string{
			{"SCARD", key},
			{"SSCAN", key, "0", "COUNT", strconv.Itoa(limit)},
		})
		if err != nil {
			return nil, err
		}
		if vals[0].kind == respInt {
			detail.Length = vals[0].num
		}
		if err := vals[1].err(); err != nil {
			return nil, err
		}
		members := []string{}
		if vals[1].kind == respArray && len(vals[1].arr) == 2 {
			for _, elem := range vals[1].arr[1].arr {
				if len(members) >= limit {
					break
				}
				members = append(members, safeDisplayString(elem.str))
			}
			if vals[1].arr[0].str != "0" || detail.Length > int64(len(members)) {
				detail.Truncated = true
			}
		}
		detail.SetMembers = members

	case "zset":
		vals, err := conn.pipeline([][]string{
			{"ZCARD", key},
			{"ZRANGE", key, "0", strconv.Itoa(limit - 1), "WITHSCORES"},
		})
		if err != nil {
			return nil, err
		}
		if vals[0].kind == respInt {
			detail.Length = vals[0].num
		}
		if err := vals[1].err(); err != nil {
			return nil, err
		}
		members := make([]ZSetMember, 0, len(vals[1].arr)/2)
		for i := 0; i+1 < len(vals[1].arr); i += 2 {
			score, _ := strconv.ParseFloat(vals[1].arr[i+1].str, 64)
			members = append(members, ZSetMember{
				Member: safeDisplayString(vals[1].arr[i].str),
				Score:  score,
			})
		}
		detail.ZSetMembers = members
		detail.Truncated = detail.Length > int64(len(members))

	case "stream":
		vals, err := conn.pipeline([][]string{
			{"XLEN", key},
			{"XREVRANGE", key, "+", "-", "COUNT", strconv.Itoa(limit)},
		})
		if err != nil {
			return nil, err
		}
		if vals[0].kind == respInt {
			detail.Length = vals[0].num
		}
		if err := vals[1].err(); err != nil {
			return nil, err
		}
		entries := make([]StreamEntry, 0, len(vals[1].arr))
		for _, entryVal := range vals[1].arr {
			if entryVal.kind != respArray || len(entryVal.arr) != 2 {
				continue
			}
			fields := map[string]string{}
			pairArr := entryVal.arr[1].arr
			for i := 0; i+1 < len(pairArr); i += 2 {
				fields[safeDisplayString(pairArr[i].str)] = safeDisplayString(pairArr[i+1].str)
			}
			entries = append(entries, StreamEntry{
				ID:     entryVal.arr[0].str,
				Fields: fields,
			})
		}
		detail.StreamEntries = entries
		detail.Truncated = detail.Length > int64(len(entries))
	}

	return detail, nil
}

func safeDisplayString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(s))
}

// SetKey creates or updates a key with type-specific structure and optional TTL.
func SetKey(ctx context.Context, cfg ConnConfig, req SetKeyRequest) error {
	if req.Key == "" {
		return errors.New("key is required")
	}
	keyType := strings.ToLower(strings.TrimSpace(req.Type))
	if keyType == "" {
		keyType = "string"
	}

	conn, err := dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	// Preserve existing TTL when requested on non-string replacements.
	var existingPTTL int64 = -1
	if req.KeepTTL && keyType != "string" {
		if v, err := conn.do("PTTL", req.Key); err == nil && v.kind == respInt && v.num > 0 {
			existingPTTL = v.num
		}
	}

	switch keyType {
	case "string":
		args := []string{"SET", req.Key, req.StringValue}
		if req.KeepTTL && req.TTLSeconds == nil {
			args = append(args, "KEEPTTL")
		} else if req.TTLSeconds != nil && *req.TTLSeconds > 0 {
			args = append(args, "EX", strconv.FormatInt(*req.TTLSeconds, 10))
		}
		if _, err := conn.do(args...); err != nil {
			return err
		}
		if req.TTLSeconds != nil && *req.TTLSeconds <= 0 {
			_, _ = conn.do("PERSIST", req.Key)
		}
		return nil

	case "hash":
		if len(req.HashEntries) == 0 {
			return errors.New("hash requires at least one field/value pair")
		}
		cmds := [][]string{{"DEL", req.Key}}
		hset := make([]string, 0, 2+len(req.HashEntries)*2)
		hset = append(hset, "HSET", req.Key)
		for _, e := range req.HashEntries {
			hset = append(hset, e.Field, e.Value)
		}
		cmds = append(cmds, hset)
		if err := runAtomicPipeline(conn, cmds); err != nil {
			return err
		}

	case "list":
		if len(req.ListItems) == 0 {
			return errors.New("list requires at least one item")
		}
		cmds := [][]string{{"DEL", req.Key}}
		rpush := make([]string, 0, 2+len(req.ListItems))
		rpush = append(rpush, "RPUSH", req.Key)
		rpush = append(rpush, req.ListItems...)
		cmds = append(cmds, rpush)
		if err := runAtomicPipeline(conn, cmds); err != nil {
			return err
		}

	case "set":
		if len(req.SetMembers) == 0 {
			return errors.New("set requires at least one member")
		}
		cmds := [][]string{{"DEL", req.Key}}
		sadd := make([]string, 0, 2+len(req.SetMembers))
		sadd = append(sadd, "SADD", req.Key)
		sadd = append(sadd, req.SetMembers...)
		cmds = append(cmds, sadd)
		if err := runAtomicPipeline(conn, cmds); err != nil {
			return err
		}

	case "zset":
		if len(req.ZSetMembers) == 0 {
			return errors.New("sorted set requires at least one member")
		}
		cmds := [][]string{{"DEL", req.Key}}
		zadd := make([]string, 0, 2+len(req.ZSetMembers)*2)
		zadd = append(zadd, "ZADD", req.Key)
		for _, m := range req.ZSetMembers {
			zadd = append(zadd, strconv.FormatFloat(m.Score, 'f', -1, 64), m.Member)
		}
		cmds = append(cmds, zadd)
		if err := runAtomicPipeline(conn, cmds); err != nil {
			return err
		}

	case "stream":
		if len(req.StreamFields) == 0 {
			return errors.New("stream entry requires at least one field")
		}
		streamID := strings.TrimSpace(req.StreamID)
		if streamID == "" {
			streamID = "*"
		}
		xadd := make([]string, 0, 3+len(req.StreamFields)*2)
		xadd = append(xadd, "XADD", req.Key, streamID)
		for k, v := range req.StreamFields {
			xadd = append(xadd, k, v)
		}
		if _, err := conn.do(xadd...); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsupported redis key type %q", req.Type)
	}

	if req.TTLSeconds != nil {
		if *req.TTLSeconds > 0 {
			if _, err := conn.do("EXPIRE", req.Key, strconv.FormatInt(*req.TTLSeconds, 10)); err != nil {
				return err
			}
		} else {
			_, _ = conn.do("PERSIST", req.Key)
		}
	} else if existingPTTL > 0 {
		_, _ = conn.do("PEXPIRE", req.Key, strconv.FormatInt(existingPTTL, 10))
	}

	return nil
}

func runAtomicPipeline(conn *respConn, cmds [][]string) error {
	wrapped := make([][]string, 0, len(cmds)+2)
	wrapped = append(wrapped, []string{"MULTI"})
	wrapped = append(wrapped, cmds...)
	wrapped = append(wrapped, []string{"EXEC"})
	vals, err := conn.pipeline(wrapped)
	if err != nil {
		return err
	}
	for _, v := range vals {
		if err := v.err(); err != nil {
			return err
		}
	}
	return nil
}

// DeleteKeys removes one or more keys and returns the number deleted.
func DeleteKeys(ctx context.Context, cfg ConnConfig, keys []string) (int64, error) {
	clean := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "" {
			clean = append(clean, k)
		}
	}
	if len(clean) == 0 {
		return 0, errors.New("at least one key is required")
	}
	conn, err := dial(ctx, cfg)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()

	args := append([]string{"DEL"}, clean...)
	v, err := conn.do(args...)
	if err != nil {
		return 0, err
	}
	return v.num, nil
}

// blockedCommands prevent blocking/connection-hijacking or destructive daemon ops from the web CLI.
var blockedCommands = map[string]bool{
	"MONITOR":       true,
	"SUBSCRIBE":     true,
	"PSUBSCRIBE":    true,
	"SSUBSCRIBE":    true,
	"BLPOP":         true,
	"BRPOP":         true,
	"BRPOPLPUSH":    true,
	"BLMOVE":        true,
	"BLMPOP":        true,
	"BZPOPMIN":      true,
	"BZPOPMAX":      true,
	"BZMPOP":        true,
	"WAIT":          true,
	"WAITAOF":       true,
	"SHUTDOWN":      true,
	"DEBUG":         true,
	"SLAVEOF":       true,
	"REPLICAOF":     true,
	"MODULE":        true,
	"MIGRATE":       true,
	"SYNC":          true,
	"PSYNC":         true,
	"FAILOVER":      true,
	"CLUSTER":       true,
	"ACL":           true,
	"BGSAVE":        true,
	"BGREWRITEAOF":  true,
	"SAVE":          true,
}

// ExecCommand parses and runs a single guarded Redis command and formats its RESP reply.
func ExecCommand(ctx context.Context, cfg ConnConfig, rawCommand string) (*CommandResult, error) {
	args, err := splitCommandArgs(rawCommand)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, errors.New("command is required")
	}
	verb := strings.ToUpper(args[0])
	if blockedCommands[verb] {
		return nil, fmt.Errorf("command %s is blocked in the web CLI", verb)
	}
	if verb == "CONFIG" && len(args) > 1 && strings.EqualFold(args[1], "SET") {
		return nil, errors.New("CONFIG SET is blocked in the web CLI")
	}

	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	start := time.Now()
	val, err := conn.do(args...)
	dur := time.Since(start).Milliseconds()
	if err != nil {
		return nil, err
	}

	resType, native := respToJSON(val)
	return &CommandResult{
		Command:    strings.TrimSpace(rawCommand),
		ResultType: resType,
		Result:     native,
		Formatted:  formatRESP(val, 0),
		DurationMs: dur,
	}, nil
}

func respToJSON(v respValue) (string, any) {
	if v.isNil {
		return "nil", nil
	}
	switch v.kind {
	case respSimple:
		if v.str == "OK" {
			return "ok", "OK"
		}
		return "string", v.str
	case respInt:
		return "integer", v.num
	case respBulk:
		return "string", safeDisplayString(v.str)
	case respArray:
		out := make([]any, len(v.arr))
		for i, elem := range v.arr {
			_, out[i] = respToJSON(elem)
		}
		return "array", out
	default:
		return "string", v.str
	}
}

func formatRESP(v respValue, indent int) string {
	pad := strings.Repeat("  ", indent)
	if v.isNil {
		return pad + "(nil)"
	}
	switch v.kind {
	case respSimple:
		return pad + v.str
	case respInt:
		return fmt.Sprintf("%s(integer) %d", pad, v.num)
	case respBulk:
		return pad + safeDisplayString(v.str)
	case respArray:
		if len(v.arr) == 0 {
			return pad + "(empty array)"
		}
		var b strings.Builder
		for i, elem := range v.arr {
			if i > 0 {
				b.WriteByte('\n')
			}
			if elem.kind == respArray {
				fmt.Fprintf(&b, "%s%d)\n%s", pad, i+1, formatRESP(elem, indent+1))
			} else {
				fmt.Fprintf(&b, "%s%d) %s", pad, i+1, strings.TrimLeft(formatRESP(elem, 0), " "))
			}
		}
		return b.String()
	default:
		return pad + v.str
	}
}

// splitCommandArgs tokenizes a Redis CLI command line supporting single/double quotes and escapes.
func splitCommandArgs(input string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inToken := false
	quote := byte(0)

	for i := 0; i < len(input); i++ {
		ch := input[i]
		if quote != 0 {
			if ch == '\\' && quote == '"' && i+1 < len(input) {
				i++
				switch input[i] {
				case 'n':
					cur.WriteByte('\n')
				case 'r':
					cur.WriteByte('\r')
				case 't':
					cur.WriteByte('\t')
				default:
					cur.WriteByte(input[i])
				}
				continue
			}
			if ch == quote {
				quote = 0
				continue
			}
			cur.WriteByte(ch)
			continue
		}

		switch ch {
		case ' ', '\t', '\r', '\n':
			if inToken {
				args = append(args, cur.String())
				cur.Reset()
				inToken = false
			}
		case '"', '\'':
			quote = ch
			inToken = true
		case '\\':
			if i+1 < len(input) {
				i++
				cur.WriteByte(input[i])
				inToken = true
			}
		default:
			cur.WriteByte(ch)
			inToken = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in command")
	}
	if inToken {
		args = append(args, cur.String())
	}
	return args, nil
}
