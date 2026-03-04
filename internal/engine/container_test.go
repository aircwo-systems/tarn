package engine

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/pkg/stdcopy"
)

func TestReadContainerLogStreamPreservesInterleaving(t *testing.T) {
	var mux bytes.Buffer
	stdout := stdcopy.NewStdWriter(&mux, stdcopy.Stdout)
	stderr := stdcopy.NewStdWriter(&mux, stdcopy.Stderr)

	records := []struct {
		writer io.Writer
		line   string
	}{
		{writer: stderr, line: "START RequestId: req-1\n"},
		{writer: stdout, line: "{\"connected\":true}\n"},
		{writer: stderr, line: "END RequestId: req-1\n"},
		{writer: stderr, line: "REPORT RequestId: req-1 Duration: 10 ms\n"},
	}

	var want strings.Builder
	for _, record := range records {
		if _, err := record.writer.Write([]byte(record.line)); err != nil {
			t.Fatalf("write multiplexed log record: %v", err)
		}
		want.WriteString(record.line)
	}

	got, err := readContainerLogStream(bytes.NewReader(mux.Bytes()))
	if err != nil {
		t.Fatalf("read container log stream: %v", err)
	}

	if got != want.String() {
		t.Fatalf("log stream mismatch\nwant:\n%s\ngot:\n%s", want.String(), got)
	}
}
