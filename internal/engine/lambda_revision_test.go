package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestRevisionAcquisitionAndReleaseNotification(t *testing.T) {
	e := &Engine{containers: map[string][]*ContainerInfo{poolKey("a", "fn"): {
		{ID: "first", AccountID: "a", FunctionName: "fn", State: "running", Revision: "one"},
		{ID: "second", AccountID: "a", FunctionName: "fn", State: "running", Revision: "two"},
	}}}
	info, ok := e.AcquireIdleRevision("a", "fn", "two")
	if !ok || info.ID != "second" {
		t.Fatalf("wrong deployment acquired: %v", info)
	}
	if _, ok := e.AcquireIdleRevision("a", "fn", "one", 1); ok {
		t.Fatal("acquisition exceeded the shared function concurrency limit")
	}
	if _, ok := e.AcquireIdleRevision("a", "fn", "missing"); ok {
		t.Fatal("acquired wrong revision")
	}
	before := e.Availability("a", "fn")
	otherAccount := e.Availability("b", "fn")
	e.Release(info)
	// Subscribe before checking/acquiring, then wait after release. The wakeup
	// must remain observable even though there was no waiter at release time.
	select {
	case <-before:
	default:
		t.Fatal("release wakeup lost")
	}
	select {
	case <-otherAccount:
		t.Fatal("another account was notified")
	default:
	}
	select {
	case <-e.Availability("a", "fn"):
		t.Fatal("notification channel wasn't renewed")
	default:
	}
}

func BenchmarkLambdaPoolBurstWait(b *testing.B) {
	for _, size := range []int{1, 8} {
		b.Run(fmt.Sprintf("Environments=%d", size), func(b *testing.B) {
			e := &Engine{containers: map[string][]*ContainerInfo{}}
			for i := range size {
				e.containers[poolKey("a", "fn")] = append(e.containers[poolKey("a", "fn")], &ContainerInfo{ID: fmt.Sprint(i), AccountID: "a", FunctionName: "fn", State: "running", Revision: "one"})
			}
			b.SetParallelism(4)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					for {
						available := e.Availability("a", "fn")
						if info, ok := e.AcquireIdleRevision("a", "fn", "one", size); ok {
							runtime.Gosched()
							e.Release(info)
							break
						}
						<-available
					}
				}
			})
		})
	}
}

func TestRetiredBusyContainerKeepsDeploymentUntilDockerRemoval(t *testing.T) {
	e, fake := newFakeEngine(t, 4566)
	var releases atomic.Int32
	released := make(chan struct{})
	info := &ContainerInfo{ID: "retired-busy", AccountID: "a", FunctionName: "fn", Busy: true, State: "running", Revision: "old:$LATEST", onRemove: func() { releases.Add(1); close(released) }}
	e.containers[poolKey("a", "fn")] = []*ContainerInfo{info}
	e.RetireLatestRevision("a", "fn", "new:$LATEST")
	if !info.Retired || fake.removedCount() != 0 || releases.Load() != 0 {
		t.Fatal("running invocation interrupted or deployment released early")
	}
	e.Release(info)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("retired environment not removed")
	}
	if e.CountContainers("a", "fn") != 0 || releases.Load() != 1 {
		t.Fatal("retirement didn't release exactly one lease")
	}
	if err := e.RemoveContainer(context.Background(), info.ID); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 1 {
		t.Fatal("deployment released twice")
	}
}

func TestImageContainerUsesPinnedImageAndImageDefaults(t *testing.T) {
	e, fake := newFakeEngine(t, 4566)
	fn := &types.FunctionConfig{FunctionName: "image", PackageType: "Image", ImageURI: "local:latest", ResolvedImageURI: "sha256:pinned", MemorySize: 128, ImageConfig: &types.FunctionImageConfig{EntryPoint: []string{"/custom"}, Command: []string{"run"}, WorkingDirectory: "/app"}}
	if _, err := e.CreateContainer(context.Background(), fn, "", nil, "a"); err != nil {
		t.Fatal(err)
	}
	if fake.created.Image != "sha256:pinned" || fake.created.WorkingDir != "/app" || len(fake.created.Cmd) != 1 || fake.created.Cmd[0] != "run" || fake.created.Entrypoint[0] != "/custom" {
		t.Fatalf("image defaults lost: %+v", fake.created)
	}
	if len(fake.host.Binds) != 0 {
		t.Fatalf("image code overwritten by ZIP mount: %v", fake.host.Binds)
	}
	fn.ImageConfig = nil
	if _, err := e.CreateContainer(context.Background(), fn, "", nil, "a"); err != nil {
		t.Fatal(err)
	}
	if len(fake.created.Cmd) != 0 || len(fake.created.Entrypoint) != 0 {
		t.Fatalf("embedded image defaults overridden: %+v", fake.created)
	}
}

func TestAlreadyReadyPortDoesNotWaitForPollingTick(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := NewInvoker().WaitForReady(ctx, strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)); err != nil {
		t.Fatalf("ready port waited for first tick: %v", err)
	}
}

func TestImagePullWaitIsCancellable(t *testing.T) {
	e, _ := newFakeEngine(t, 4566)
	gate := e.runtimePullMu(types.RuntimeNodeJS20)
	if err := gate.Acquire(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	defer gate.Release(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := e.EnsureImage(ctx, types.RuntimeNodeJS20); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("image gate ignored deadline: %v", err)
	}
}
