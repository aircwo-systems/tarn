package engine

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

func TestBuildTaskContainerEnvIncludesEndpointAndSpecEnv(t *testing.T) {
	spec := TaskContainerSpec{
		Region: "us-east-1",
		Env:    map[string]string{"FOO": "bar"},
	}

	env := buildTaskContainerEnv(spec, 4566)

	want := map[string]bool{
		"AWS_ENDPOINT_URL=http://host.docker.internal:4566": false,
		"AWS_REGION=us-east-1":                              false,
		"AWS_DEFAULT_REGION=us-east-1":                      false,
		"AWS_ACCESS_KEY_ID=" + defaultTaskAccountID:         false,
		"AWS_SECRET_ACCESS_KEY=test":                        false,
		"FOO=bar":                                           false,
	}
	for _, e := range env {
		if _, ok := want[e]; !ok {
			t.Fatalf("unexpected env entry %q", e)
		}
		want[e] = true
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("missing expected env entry %q, got %v", k, env)
		}
	}
}

// TestBuildTaskContainerEnvUsesSpecAccountID verifies the owning account's ID
// is injected as AWS_ACCESS_KEY_ID (see internal/account: SigV4 requests
// with a 12-digit AKID resolve to that account), so a container launched for
// account 222222222222 doesn't silently attribute its SDK calls to the
// default account.
func TestBuildTaskContainerEnvUsesSpecAccountID(t *testing.T) {
	spec := TaskContainerSpec{
		Region:    "us-east-1",
		AccountID: "222222222222",
	}

	env := buildTaskContainerEnv(spec, 4566)

	if !containsString(env, "AWS_ACCESS_KEY_ID=222222222222") {
		t.Fatalf("expected AWS_ACCESS_KEY_ID=222222222222, got %v", env)
	}
}

// TestBuildTaskContainerEnvUserOverrideWinsForAccessKey verifies a task
// definition's own AWS_ACCESS_KEY_ID (via spec.Env) still overrides the
// injected account default, since spec.Env is applied after the defaults.
func TestBuildTaskContainerEnvUserOverrideWinsForAccessKey(t *testing.T) {
	spec := TaskContainerSpec{
		Region:    "us-east-1",
		AccountID: "222222222222",
		Env:       map[string]string{"AWS_ACCESS_KEY_ID": "custom-key"},
	}

	env := buildTaskContainerEnv(spec, 4566)

	if !containsString(env, "AWS_ACCESS_KEY_ID=custom-key") {
		t.Fatalf("expected user override AWS_ACCESS_KEY_ID=custom-key to win, got %v", env)
	}
	if containsString(env, "AWS_ACCESS_KEY_ID=222222222222") {
		t.Fatalf("account default should not also be present, got %v", env)
	}
}

// TestBuildTaskContainerEnvIncludesCorrelationID verifies the task's
// correlation ID is exposed as TARN_CORRELATION_ID when set, and omitted
// entirely when not (no trace store wired up / none assigned).
func TestBuildTaskContainerEnvIncludesCorrelationID(t *testing.T) {
	env := buildTaskContainerEnv(TaskContainerSpec{
		Region:        "us-east-1",
		CorrelationID: "corr-123",
	}, 4566)
	if !containsString(env, "TARN_CORRELATION_ID=corr-123") {
		t.Fatalf("expected TARN_CORRELATION_ID=corr-123, got %v", env)
	}

	envNoCorr := buildTaskContainerEnv(TaskContainerSpec{Region: "us-east-1"}, 4566)
	for _, e := range envNoCorr {
		if strings.HasPrefix(e, "TARN_CORRELATION_ID=") {
			t.Fatalf("expected no TARN_CORRELATION_ID entry, got %v", envNoCorr)
		}
	}
}

// TestBuildTaskContainerEnvCorrelationIDUserOverrideWins verifies a task
// definition's own TARN_CORRELATION_ID (via spec.Env) overrides the one the
// runner assigned.
func TestBuildTaskContainerEnvCorrelationIDUserOverrideWins(t *testing.T) {
	env := buildTaskContainerEnv(TaskContainerSpec{
		Region:        "us-east-1",
		CorrelationID: "corr-123",
		Env:           map[string]string{"TARN_CORRELATION_ID": "user-corr"},
	}, 4566)
	if !containsString(env, "TARN_CORRELATION_ID=user-corr") {
		t.Fatalf("expected user override TARN_CORRELATION_ID=user-corr to win, got %v", env)
	}
}

func TestBuildTaskContainerEnvUsesFileForLargeEventPayload(t *testing.T) {
	payload := make([]byte, maxTaskPayloadEnv+1)
	for i := range payload {
		payload[i] = 'x'
	}

	env := buildTaskContainerEnv(TaskContainerSpec{
		Region:           "us-east-1",
		EventPayload:     payload,
		EventPayloadFile: "/host/payload.json",
	}, 4566)

	for _, entry := range env {
		if len(entry) > len("EVENT_PAYLOAD=") && entry[:len("EVENT_PAYLOAD=")] == "EVENT_PAYLOAD=" {
			t.Fatalf("large event payload was placed in environment: %d bytes", len(entry))
		}
	}
	if !containsString(env, "EVENT_PAYLOAD_FILE=/tmp/tarn/event-payload.json") {
		t.Fatalf("expected EVENT_PAYLOAD_FILE, got %v", env)
	}
	_, hostCfg, err := buildTaskContainerConfig(TaskContainerSpec{
		Image:            "example/task:latest",
		EventPayload:     payload,
		EventPayloadFile: "/host/payload.json",
	}, 4566)
	if err != nil {
		t.Fatalf("buildTaskContainerConfig: %v", err)
	}
	if len(hostCfg.Binds) != 1 || hostCfg.Binds[0] != "/host/payload.json:/tmp/tarn/event-payload.json:ro" {
		t.Fatalf("unexpected payload bind: %v", hostCfg.Binds)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestBuildTaskPortBindingsPublishesEphemeralHostPorts(t *testing.T) {
	exposed, bindings := buildTaskPortBindings([]int{8080, 9090})

	if len(exposed) != 2 {
		t.Fatalf("expected 2 exposed ports, got %d", len(exposed))
	}
	if _, ok := exposed[nat.Port("8080/tcp")]; !ok {
		t.Fatalf("expected 8080/tcp exposed, got %v", exposed)
	}
	if _, ok := exposed[nat.Port("9090/tcp")]; !ok {
		t.Fatalf("expected 9090/tcp exposed, got %v", exposed)
	}

	for port, bs := range bindings {
		if len(bs) != 1 {
			t.Fatalf("expected one binding for %s, got %d", port, len(bs))
		}
		if bs[0].HostPort != "" {
			t.Fatalf("expected empty HostPort (ephemeral) for %s, got %q", port, bs[0].HostPort)
		}
		if bs[0].HostIP != "127.0.0.1" {
			t.Fatalf("expected HostIP 127.0.0.1 for %s, got %q", port, bs[0].HostIP)
		}
	}
}

func TestBuildTaskContainerConfigMapsResourcesToDocker(t *testing.T) {
	containerCfg, hostCfg, err := buildTaskContainerConfig(TaskContainerSpec{
		Image:             "example/task:latest",
		CPU:               512,
		Memory:            256,
		MemoryReservation: 128,
		Ports:             []int{8080},
	}, 4566)
	if err != nil {
		t.Fatalf("buildTaskContainerConfig: %v", err)
	}

	if containerCfg.Image != "example/task:latest" {
		t.Fatalf("container image = %q, want example/task:latest", containerCfg.Image)
	}
	if hostCfg.CPUShares != 512 {
		t.Fatalf("CPU shares = %d, want 512", hostCfg.CPUShares)
	}
	if hostCfg.NanoCPUs != 500_000_000 {
		t.Fatalf("NanoCPUs = %d, want 500000000", hostCfg.NanoCPUs)
	}
	if hostCfg.Memory != 256*1024*1024 {
		t.Fatalf("memory = %d, want %d", hostCfg.Memory, 256*1024*1024)
	}
	if hostCfg.MemoryReservation != 128*1024*1024 {
		t.Fatalf("memory reservation = %d, want %d", hostCfg.MemoryReservation, 128*1024*1024)
	}
}

func TestBuildTaskContainerConfigMapsSupportedNetworkModes(t *testing.T) {
	tests := []struct {
		name          string
		mode          string
		wantDocker    container.NetworkMode
		wantPorts     bool
		wantHostEntry bool
	}{
		{name: "default", wantDocker: "", wantPorts: true, wantHostEntry: true},
		{name: "bridge", mode: "bridge", wantDocker: "bridge", wantPorts: true, wantHostEntry: true},
		{name: "awsvpc", mode: "awsvpc", wantDocker: "", wantPorts: true, wantHostEntry: true},
		{name: "host", mode: "host", wantDocker: "host", wantHostEntry: true},
		{name: "none", mode: "none", wantDocker: "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports := []int(nil)
			if tt.wantPorts {
				ports = []int{8080}
			}
			_, hostCfg, err := buildTaskContainerConfig(TaskContainerSpec{
				Image:       "example/task:latest",
				NetworkMode: tt.mode,
				Ports:       ports,
			}, 4566)
			if err != nil {
				t.Fatalf("buildTaskContainerConfig: %v", err)
			}
			if hostCfg.NetworkMode != tt.wantDocker {
				t.Fatalf("Docker network mode = %q, want %q", hostCfg.NetworkMode, tt.wantDocker)
			}
			if tt.wantPorts && len(hostCfg.PortBindings) != 1 {
				t.Fatalf("port bindings = %v, want one published port", hostCfg.PortBindings)
			}
			if !tt.wantPorts && len(hostCfg.PortBindings) != 0 {
				t.Fatalf("port bindings = %v, want none", hostCfg.PortBindings)
			}
			if tt.wantHostEntry {
				if len(hostCfg.ExtraHosts) != 1 || hostCfg.ExtraHosts[0] != taskHostGateway {
					t.Fatalf("extra hosts = %v, want [%q]", hostCfg.ExtraHosts, taskHostGateway)
				}
			} else if len(hostCfg.ExtraHosts) != 0 {
				t.Fatalf("extra hosts = %v, want none", hostCfg.ExtraHosts)
			}
		})
	}
}

func TestBuildTaskContainerConfigRejectsInvalidNetworkCombinations(t *testing.T) {
	tests := []struct {
		name string
		spec TaskContainerSpec
	}{
		{name: "unsupported mode", spec: TaskContainerSpec{Image: "example/task:latest", NetworkMode: "overlay"}},
		{name: "host publishes ports", spec: TaskContainerSpec{Image: "example/task:latest", NetworkMode: "host", Ports: []int{8080}}},
		{name: "none publishes ports", spec: TaskContainerSpec{Image: "example/task:latest", NetworkMode: "none", Ports: []int{8080}}},
		{name: "negative cpu", spec: TaskContainerSpec{Image: "example/task:latest", CPU: -1}},
		{name: "negative memory", spec: TaskContainerSpec{Image: "example/task:latest", Memory: -1}},
		{name: "reservation above memory", spec: TaskContainerSpec{Image: "example/task:latest", Memory: 128, MemoryReservation: 256}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := buildTaskContainerConfig(tt.spec, 4566); err == nil {
				t.Fatal("expected invalid task container spec error")
			}
		})
	}
}

func TestDecodeNetworkBindingsReadsAssignedHostPorts(t *testing.T) {
	ports := nat.PortMap{
		nat.Port("8080/tcp"): []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: "54321"},
		},
		nat.Port("9090/tcp"): []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: ""}, // not yet assigned, should be skipped
		},
	}

	bindings := decodeNetworkBindings(ports)

	if len(bindings) != 1 {
		t.Fatalf("expected 1 decoded binding (unassigned skipped), got %d: %+v", len(bindings), bindings)
	}
	b := bindings[0]
	if b.ContainerPort != 8080 || b.HostPort != 54321 || b.Protocol != "tcp" || b.BindIP != "127.0.0.1" {
		t.Fatalf("unexpected binding: %+v", b)
	}
}

func TestBuildLabelFilterArgsMatchesEveryPair(t *testing.T) {
	args := buildLabelFilterArgs(map[string]string{
		"tarn.account": "acct-1",
		"tarn.cluster": "default",
	})

	got := args.Get("label")
	sort.Strings(got)
	want := []string{"tarn.account=acct-1", "tarn.cluster=default"}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("expected %d label filters, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("label filter mismatch: want %v, got %v", want, got)
		}
	}
}

func TestScanDemuxedLinesDeliversStdoutAndStderrLines(t *testing.T) {
	var mux bytes.Buffer
	stdout := stdcopy.NewStdWriter(&mux, stdcopy.Stdout)
	stderr := stdcopy.NewStdWriter(&mux, stdcopy.Stderr)

	if _, err := stdout.Write([]byte("hello stdout\n")); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	if _, err := stderr.Write([]byte("hello stderr\n")); err != nil {
		t.Fatalf("write stderr: %v", err)
	}
	if _, err := stdout.Write([]byte("second stdout line\n")); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	var got []string
	var gotStderr []bool
	err := scanDemuxedLines(bytes.NewReader(mux.Bytes()), func(line string, stderr bool) {
		got = append(got, line)
		gotStderr = append(gotStderr, stderr)
	})
	if err != nil {
		t.Fatalf("scanDemuxedLines: %v", err)
	}

	want := []string{"hello stdout", "hello stderr", "second stdout line"}
	wantStderr := []bool{false, true, false}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d mismatch: want %q, got %q (full: %v)", i, want[i], got[i], got)
		}
		if gotStderr[i] != wantStderr[i] {
			t.Fatalf("line %d stderr flag: want %v, got %v (full: %v)", i, wantStderr[i], gotStderr[i], gotStderr)
		}
	}
}

// failingReader returns an error after n bytes are consumed via zero reads,
// so scanDemuxedLines can be exercised on a stream that errors mid-frame.
type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestScanDemuxedLinesPropagatesStreamError(t *testing.T) {
	boom := errors.New("boom")
	err := scanDemuxedLines(&failingReader{err: boom}, func(string, bool) {})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
}

var _ io.Reader = (*failingReader)(nil)
