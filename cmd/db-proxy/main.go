// db-proxy is a lightweight transparent TCP proxy that runs inside Lambda containers.
// It sits between the Lambda function and its PostgreSQL database, forwarding all
// traffic unchanged while detecting query boundaries in the PostgreSQL wire protocol.
//
// When the server sends ReadyForQuery ('Z') — which marks the end of every query
// cycle — the proxy immediately reports a "postgres" span to the Tarn telemetry
// endpoint. This happens before the Lambda sends its HTTP response, so the span is
// always visible in the X-Ray trace regardless of whether the connection is short-lived
// or kept alive by a connection pool.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

func main() {
	listenPort := os.Getenv("TARN_DB_PROXY_PORT")
	if listenPort == "" {
		listenPort = "15432"
	}

	upstreamAddr := os.Getenv("TARN_DB_UPSTREAM")
	if upstreamAddr == "" {
		log.Fatal("[db-proxy] TARN_DB_UPSTREAM not set")
	}

	tarnEndpoint := os.Getenv("AWS_ENDPOINT_URL")
	if tarnEndpoint == "" {
		tarnEndpoint = "http://host.docker.internal:4566"
	}

	dbName := os.Getenv("TARN_DB_NAME")
	if dbName == "" {
		dbName = upstreamAddr
	}

	ln, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		log.Fatalf("[db-proxy] listen error: %v", err)
	}
	log.Printf("[db-proxy] listening on :%s, upstream: %s", listenPort, upstreamAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[db-proxy] accept error: %v", err)
			continue
		}
		go handleConn(conn, upstreamAddr, tarnEndpoint, dbName)
	}
}

func handleConn(client net.Conn, upstreamAddr, tarnEndpoint, dbName string) {
	defer client.Close()

	upstream, err := net.Dial("tcp", upstreamAddr)
	if err != nil {
		log.Printf("[db-proxy] dial %s error: %v", upstreamAddr, err)
		reportSpan(tarnEndpoint, dbName, 0, "error")
		return
	}
	defer upstream.Close()

	var (
		mu         sync.Mutex
		readyCount int       // number of ReadyForQuery messages seen
		queryStart time.Time // zero until the client sends a real query message
	)

	// Called when the client sends a query-initiating message ('Q', 'P', or 'B').
	// Sets queryStart only if not already set (first message of a pipeline wins).
	onQuery := func() {
		mu.Lock()
		defer mu.Unlock()
		if queryStart.IsZero() {
			queryStart = time.Now()
		}
	}

	// Called each time the server sends ReadyForQuery ('Z').
	// The first 'Z' is the startup complete signal — skip it.
	// Subsequent ones mark query completions; measure from when the client sent
	// the query (onQuery), not from when the last 'Z' was received, so idle time
	// between requests in a connection pool is excluded from the span duration.
	onReady := func(txStatus byte) {
		mu.Lock()
		defer mu.Unlock()
		readyCount++
		if readyCount == 1 {
			// Startup complete. queryStart intentionally left zero here;
			// it will be set by onQuery when the first real query arrives.
			return
		}
		if queryStart.IsZero() {
			// No query start recorded — skip (guards against unexpected 'Z').
			return
		}
		dur := time.Since(queryStart).Milliseconds()
		queryStart = time.Time{} // reset; next query sets it via onQuery
		status := "ok"
		if txStatus == 'E' { // error in transaction
			status = "error"
		}
		go reportSpan(tarnEndpoint, dbName, dur, status)
	}

	done := make(chan struct{}, 2)

	// client → upstream: parse client messages to record when a query starts.
	go func() {
		if err := parseClientQueries(client, upstream, onQuery); err != nil && err != io.EOF {
			log.Printf("[db-proxy] client parse error: %v", err)
		}
		done <- struct{}{}
	}()

	// upstream → client: protocol-aware copy that detects ReadyForQuery.
	go func() {
		if err := parseBackend(upstream, client, onReady); err != nil && err != io.EOF {
			log.Printf("[db-proxy] parse error: %v", err)
		}
		done <- struct{}{}
	}()

	<-done
}

// parseClientQueries forwards all bytes from src (client) to dst (upstream),
// calling onQuery whenever it sees a message that initiates a new query cycle:
//   - 'Q' (0x51) SimpleQuery — the entire query in one message
//   - 'P' (0x50) Parse       — first step of the extended query protocol
//   - 'B' (0x42) Bind        — covers re-execution of named prepared statements
//
// PostgreSQL client messages after the startup handshake are typed:
//
//	1-byte type | 4-byte big-endian length (includes itself) | body
//
// Startup-phase messages (SSLRequest and StartupMessage) are NOT typed — they
// begin with a 4-byte big-endian length whose most-significant byte is always 0
// (startup payloads are always < 16 MB). We use this to skip them transparently.
func parseClientQueries(src io.Reader, dst io.Writer, onQuery func()) error {
	hdr := make([]byte, 5) // type(1) + length(4) for typed; or first 4 bytes for startup
	body := make([]byte, 0, 4096)

	for {
		// Read first byte to decide: startup (0x00) vs typed message (non-zero).
		if _, err := io.ReadFull(src, hdr[:1]); err != nil {
			return err
		}

		if hdr[0] == 0 {
			// Startup-phase non-typed message (SSLRequest or StartupMessage).
			// The 4-byte length is the first 4 bytes of the message; we already
			// read the high byte (0x00), so read the remaining 3 to get the length.
			if _, err := io.ReadFull(src, hdr[1:4]); err != nil {
				return err
			}
			if _, err := dst.Write(hdr[:4]); err != nil {
				return err
			}
			msgLen := int(binary.BigEndian.Uint32(hdr[:4]))
			remaining := msgLen - 4
			if remaining > 0 {
				if cap(body) < remaining {
					body = make([]byte, remaining)
				}
				body = body[:remaining]
				if _, err := io.ReadFull(src, body); err != nil {
					return err
				}
				if _, err := dst.Write(body); err != nil {
					return err
				}
			}
			continue
		}

		// Typed message: hdr[0] is the type; read the 4-byte length.
		msgType := hdr[0]
		if _, err := io.ReadFull(src, hdr[1:5]); err != nil {
			return err
		}
		if _, err := dst.Write(hdr); err != nil {
			return err
		}

		msgLen := int(binary.BigEndian.Uint32(hdr[1:5]))
		bodyLen := msgLen - 4
		if bodyLen < 0 {
			// Malformed — fall back to transparent copy.
			_, err := io.Copy(dst, src)
			return err
		}
		if bodyLen > 0 {
			if cap(body) < bodyLen {
				body = make([]byte, bodyLen)
			}
			body = body[:bodyLen]
			if _, err := io.ReadFull(src, body); err != nil {
				return err
			}
			if _, err := dst.Write(body); err != nil {
				return err
			}
		}

		// Record query start for span timing.
		if msgType == 'Q' || msgType == 'P' || msgType == 'B' {
			onQuery()
		}
	}
}

// parseBackend forwards all bytes from src to dst while parsing PostgreSQL
// backend (server → client) messages to detect ReadyForQuery ('Z') signals.
//
// It handles the optional SSL negotiation byte that precedes the typed message
// stream: 'N' = no SSL (typed messages follow), 'S' = SSL accepted (falls back
// to transparent copy since we cannot parse through TLS).
func parseBackend(src io.Reader, dst io.Writer, onReady func(txStatus byte)) error {
	// Peek at the first byte — it may be an SSL negotiation response.
	var first [1]byte
	if _, err := io.ReadFull(src, first[:]); err != nil {
		return err
	}

	switch first[0] {
	case 'S':
		// Server accepted SSL — write the 'S' byte to the client, then fall back
		// to transparent copy for the TLS handshake (no span detection).
		if _, err := dst.Write(first[:]); err != nil {
			return err
		}
		_, err := io.Copy(dst, src)
		return err
	case 'N':
		// Server declined SSL — write the 'N' byte to the client, then start
		// parsing typed messages from the next read.
		if _, err := dst.Write(first[:]); err != nil {
			return err
		}
		return parseMessages(src, dst, onReady)
	default:
		// No SSL negotiation — the first byte is the message type byte of the
		// first typed message. Feed it back into parseMessages via MultiReader so
		// the full 5-byte header (type + length) is written to dst exactly once.
		return parseMessages(io.MultiReader(bytes.NewReader(first[:]), src), dst, onReady)
	}
}

// parseMessages reads a stream of PostgreSQL backend messages (each: 1-byte type +
// 4-byte big-endian length including itself + body) from src, forwards them to dst,
// and calls onReady whenever ReadyForQuery ('Z') is encountered.
func parseMessages(src io.Reader, dst io.Writer, onReady func(txStatus byte)) error {
	hdr := make([]byte, 5) // type(1) + length(4)
	body := make([]byte, 0, 4096)

	for {
		if _, err := io.ReadFull(src, hdr); err != nil {
			return err
		}
		if _, err := dst.Write(hdr); err != nil {
			return err
		}

		msgType := hdr[0]
		msgLen := int(binary.BigEndian.Uint32(hdr[1:5]))
		bodyLen := msgLen - 4
		if bodyLen < 0 {
			// Malformed length — fall back to transparent copy.
			_, err := io.Copy(dst, src)
			return err
		}

		if bodyLen > 0 {
			if cap(body) < bodyLen {
				body = make([]byte, bodyLen)
			}
			body = body[:bodyLen]
			if _, err := io.ReadFull(src, body); err != nil {
				return err
			}
			if _, err := dst.Write(body); err != nil {
				return err
			}
		}

		// ReadyForQuery: body[0] is the transaction status indicator.
		// 'I' = idle, 'T' = in transaction, 'E' = failed transaction.
		if msgType == 'Z' && bodyLen >= 1 {
			onReady(body[0])
		}
	}
}

func reportSpan(endpoint, dbName string, durationMs int64, status string) {
	body, _ := json.Marshal(map[string]interface{}{
		"name":       dbName,
		"durationMs": durationMs,
		"status":     status,
	})
	resp, err := http.Post(fmt.Sprintf("%s/_tarn/telemetry/db", endpoint), "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("[db-proxy] report span error: %v", err)
		return
	}
	resp.Body.Close()
}
