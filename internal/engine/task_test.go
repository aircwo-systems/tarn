package engine

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
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
	exposed, bindings := buildTaskPortBindings([]int{8080, 9090}, nil)

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

// TestBuildTaskPortBindingsHonorsFixedHostPorts covers a task definition
// PortMapping with a non-zero HostPort (internal/ecs/runner.go's
// startContainer maps this into FixedHostPorts): that port must be bound to
// the exact host port requested, while a sibling port with no fixed entry
// keeps the ephemeral behavior.
func TestBuildTaskPortBindingsHonorsFixedHostPorts(t *testing.T) {
	exposed, bindings := buildTaskPortBindings([]int{8080, 9090}, map[int]int{8080: 9999})

	if len(exposed) != 2 {
		t.Fatalf("expected 2 exposed ports, got %d", len(exposed))
	}

	fixed := bindings[nat.Port("8080/tcp")]
	if len(fixed) != 1 {
		t.Fatalf("expected one binding for 8080/tcp, got %d", len(fixed))
	}
	if fixed[0].HostPort != "9999" {
		t.Fatalf("expected fixed HostPort 9999 for 8080/tcp, got %q", fixed[0].HostPort)
	}
	if fixed[0].HostIP != "127.0.0.1" {
		t.Fatalf("expected HostIP 127.0.0.1 for 8080/tcp, got %q", fixed[0].HostIP)
	}

	ephemeral := bindings[nat.Port("9090/tcp")]
	if len(ephemeral) != 1 {
		t.Fatalf("expected one binding for 9090/tcp, got %d", len(ephemeral))
	}
	if ephemeral[0].HostPort != "" {
		t.Fatalf("expected empty (ephemeral) HostPort for 9090/tcp, got %q", ephemeral[0].HostPort)
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

func TestBuildTaskContainerConfigMapsDriftAvoidanceFields(t *testing.T) {
	containerCfg, hostCfg, err := buildTaskContainerConfig(TaskContainerSpec{
		Image:                  "example/task:latest",
		WorkingDirectory:       "/srv/app",
		User:                   "1000:1000",
		Hostname:               "app-host",
		Interactive:            true,
		PseudoTerminal:         true,
		Labels:                 map[string]string{"tarn.account": "000000000000"},
		DockerLabels:           map[string]string{"team": "platform", "tarn.account": "should-not-win"},
		Ulimits:                []types.Ulimit{{Name: "nofile", SoftLimit: 1024, HardLimit: 2048}},
		ReadonlyRootFilesystem: true,
		Privileged:             true,
		InitProcessEnabled:     true,
		CapAdd:                 []string{"NET_ADMIN"},
		CapDrop:                []string{"MKNOD"},
		ShmSize:                64 * 1024 * 1024,
		Tmpfs:                  map[string]string{"/tmp": "rw,size=32m"},
		DNSServers:             []string{"1.1.1.1"},
		ExtraHosts:             []string{"db.local:10.0.0.5"},
		VolumesFrom:            []string{"tarn-ecs-abc-sidecar:ro"},
		Binds:                  []string{"tarn-ecs-task-abc-shared:/data:ro"},
	}, 4566)
	if err != nil {
		t.Fatalf("buildTaskContainerConfig: %v", err)
	}

	if containerCfg.WorkingDir != "/srv/app" {
		t.Fatalf("WorkingDir = %q, want /srv/app", containerCfg.WorkingDir)
	}
	if containerCfg.User != "1000:1000" {
		t.Fatalf("User = %q, want 1000:1000", containerCfg.User)
	}
	if containerCfg.Hostname != "app-host" {
		t.Fatalf("Hostname = %q, want app-host", containerCfg.Hostname)
	}
	if !containerCfg.OpenStdin || !containerCfg.Tty {
		t.Fatalf("OpenStdin/Tty = %v/%v, want true/true", containerCfg.OpenStdin, containerCfg.Tty)
	}
	// Tarn's own label must win over a same-keyed dockerLabels entry.
	if containerCfg.Labels["tarn.account"] != "000000000000" {
		t.Fatalf("tarn.account label = %q, want 000000000000 (Tarn's own label must win)", containerCfg.Labels["tarn.account"])
	}
	if containerCfg.Labels["team"] != "platform" {
		t.Fatalf("team label = %q, want platform", containerCfg.Labels["team"])
	}
	if len(hostCfg.Ulimits) != 1 || hostCfg.Ulimits[0].Name != "nofile" || hostCfg.Ulimits[0].Soft != 1024 || hostCfg.Ulimits[0].Hard != 2048 {
		t.Fatalf("Ulimits = %+v, want one nofile 1024/2048", hostCfg.Ulimits)
	}
	if !hostCfg.ReadonlyRootfs {
		t.Fatalf("ReadonlyRootfs = false, want true")
	}
	if !hostCfg.Privileged {
		t.Fatalf("Privileged = false, want true")
	}
	if hostCfg.Init == nil || !*hostCfg.Init {
		t.Fatalf("Init = %v, want true", hostCfg.Init)
	}
	if len(hostCfg.CapAdd) != 1 || hostCfg.CapAdd[0] != "NET_ADMIN" {
		t.Fatalf("CapAdd = %v, want [NET_ADMIN]", hostCfg.CapAdd)
	}
	if len(hostCfg.CapDrop) != 1 || hostCfg.CapDrop[0] != "MKNOD" {
		t.Fatalf("CapDrop = %v, want [MKNOD]", hostCfg.CapDrop)
	}
	if hostCfg.ShmSize != 64*1024*1024 {
		t.Fatalf("ShmSize = %d, want %d", hostCfg.ShmSize, 64*1024*1024)
	}
	if hostCfg.Tmpfs["/tmp"] != "rw,size=32m" {
		t.Fatalf("Tmpfs[/tmp] = %q, want rw,size=32m", hostCfg.Tmpfs["/tmp"])
	}
	if len(hostCfg.DNS) != 1 || hostCfg.DNS[0] != "1.1.1.1" {
		t.Fatalf("DNS = %v, want [1.1.1.1]", hostCfg.DNS)
	}
	if len(hostCfg.VolumesFrom) != 1 || hostCfg.VolumesFrom[0] != "tarn-ecs-abc-sidecar:ro" {
		t.Fatalf("VolumesFrom = %v, want [tarn-ecs-abc-sidecar:ro]", hostCfg.VolumesFrom)
	}
	// The fixed host.docker.internal entry must survive alongside a spec
	// ExtraHosts entry (append, not overwrite).
	wantHosts := []string{taskHostGateway, "db.local:10.0.0.5"}
	if len(hostCfg.ExtraHosts) != len(wantHosts) || hostCfg.ExtraHosts[0] != wantHosts[0] || hostCfg.ExtraHosts[1] != wantHosts[1] {
		t.Fatalf("ExtraHosts = %v, want %v", hostCfg.ExtraHosts, wantHosts)
	}
	// A spec.Binds entry must survive alongside the payload bind (none here,
	// so it must be the only bind, not silently dropped).
	if len(hostCfg.Binds) != 1 || hostCfg.Binds[0] != "tarn-ecs-task-abc-shared:/data:ro" {
		t.Fatalf("Binds = %v, want [tarn-ecs-task-abc-shared:/data:ro]", hostCfg.Binds)
	}
}

func TestBuildTaskContainerConfigAppendsBindsAlongsideEventPayload(t *testing.T) {
	_, hostCfg, err := buildTaskContainerConfig(TaskContainerSpec{
		Image:            "example/task:latest",
		EventPayloadFile: "/tmp/payload.json",
		Binds:            []string{"tarn-ecs-task-abc-shared:/data"},
	}, 4566)
	if err != nil {
		t.Fatalf("buildTaskContainerConfig: %v", err)
	}
	want := map[string]bool{
		"/tmp/payload.json:" + taskPayloadPath + ":ro": false,
		"tarn-ecs-task-abc-shared:/data":               false,
	}
	if len(hostCfg.Binds) != len(want) {
		t.Fatalf("Binds = %v, want both the payload mount and the spec bind", hostCfg.Binds)
	}
	for _, b := range hostCfg.Binds {
		if _, ok := want[b]; !ok {
			t.Fatalf("unexpected bind %q", b)
		}
		want[b] = true
	}
	for b, seen := range want {
		if !seen {
			t.Fatalf("missing expected bind %q, got %v", b, hostCfg.Binds)
		}
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

	// Streams are drained concurrently, so only per-stream order is
	// guaranteed; cross-stream interleaving is arrival-ordered.
	var gotOut, gotErr []string
	for i, line := range got {
		if gotStderr[i] {
			gotErr = append(gotErr, line)
		} else {
			gotOut = append(gotOut, line)
		}
	}
	wantOut := []string{"hello stdout", "second stdout line"}
	wantErr := []string{"hello stderr"}
	if !slices.Equal(gotOut, wantOut) {
		t.Fatalf("stdout lines: want %v, got %v (full: %v)", wantOut, gotOut, got)
	}
	if !slices.Equal(gotErr, wantErr) {
		t.Fatalf("stderr lines: want %v, got %v (full: %v)", wantErr, gotErr, got)
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
