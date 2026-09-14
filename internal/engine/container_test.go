package engine

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/docker/docker/pkg/stdcopy"
)

// TestBuildLambdaContainerEnvUsesAccountID verifies the owning function's
// account ID is injected as AWS_ACCESS_KEY_ID, so a Lambda container invoked
// for account 222222222222 is attributed to that account (via SigV4 in
// internal/account) rather than the default account.
func TestBuildLambdaContainerEnvUsesAccountID(t *testing.T) {
	fn := &types.FunctionConfig{
		FunctionName: "my-fn",
		Version:      "1",
		Handler:      "index.handler",
	}

	env := buildLambdaContainerEnv(fn, "222222222222", "us-east-1", 4566, nil)

	if !containsString(env, "AWS_ACCESS_KEY_ID=222222222222") {
		t.Fatalf("expected AWS_ACCESS_KEY_ID=222222222222, got %v", env)
	}
}

// TestBuildLambdaContainerEnvUserOverrideWinsForAccessKey verifies a
// function's own AWS_ACCESS_KEY_ID environment variable overrides the
// injected account default, since fn.Environment is applied last.
func TestBuildLambdaContainerEnvUserOverrideWinsForAccessKey(t *testing.T) {
	fn := &types.FunctionConfig{
		FunctionName: "my-fn",
		Version:      "1",
		Handler:      "index.handler",
		Environment:  map[string]string{"AWS_ACCESS_KEY_ID": "custom-key"},
	}

	env := buildLambdaContainerEnv(fn, "222222222222", "us-east-1", 4566, nil)

	// fn.Environment is appended after the account default, and Docker
	// resolves duplicate env keys by taking the last occurrence, so the
	// user's value must come after the default in the slice to win.
	defaultIdx, overrideIdx := -1, -1
	for i, e := range env {
		switch e {
		case "AWS_ACCESS_KEY_ID=222222222222":
			defaultIdx = i
		case "AWS_ACCESS_KEY_ID=custom-key":
			overrideIdx = i
		}
	}
	if overrideIdx == -1 {
		t.Fatalf("expected user override AWS_ACCESS_KEY_ID=custom-key present, got %v", env)
	}
	if defaultIdx != -1 && defaultIdx > overrideIdx {
		t.Fatalf("account default must not come after the user override, got %v", env)
	}
}

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
