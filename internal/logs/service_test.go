package logs

import (
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
)

func TestLogAPIRequestNamesJSONTarget(t *testing.T) {
	s := NewService(config.Default())
	s.LogAPIRequest("POST", "/", 400, time.Millisecond, "TrentService.Decrypt")
	s.LogAPIRequest("GET", "/2015-03-31/functions", 200, time.Millisecond, "")

	events, _, _ := s.GetLogEvents("/tarn/api", nil)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if got, want := events[0].Message, "POST / 400 1ms target=TrentService.Decrypt"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if events[0].Level != LevelWARN {
		t.Fatalf("level = %q, want WARN", events[0].Level)
	}
	if got, want := events[1].Message, "GET /2015-03-31/functions 200 1ms"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
