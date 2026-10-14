package lambda

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/engine"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func functionsZIP(tb testing.TB, files map[string]string) []byte {
	tb.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0755)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			tb.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		tb.Fatal(err)
	}
	return buffer.Bytes()
}

func regressionService(t *testing.T) (*Service, *config.Config) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, store, nil, nil, nil)
	t.Cleanup(svc.Close)
	return svc, cfg
}

func createRegressionFunction(t *testing.T, svc *Service, name string, code []byte) *types.FunctionConfig {
	t.Helper()
	fn, err := svc.CreateFunction(context.Background(), &types.FunctionConfig{FunctionName: name, Runtime: types.RuntimeNodeJS20, Handler: "index.handler", Environment: map[string]string{"KEY": "original"}}, code)
	if err != nil {
		t.Fatal(err)
	}
	return fn
}

func TestConcurrentFunctionMutationsPreserveSuccessfulChanges(t *testing.T) {
	svc, _ := regressionService(t)
	createRegressionFunction(t, svc, "fn", functionsZIP(t, map[string]string{"index.js": "v1"}))
	const count = 32
	start := make(chan struct{})
	results := make(chan error, count+2)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- svc.TagResource("fn", map[string]string{fmt.Sprint(i): "value"})
		}()
	}
	code := functionsZIP(t, map[string]string{"index.js": "v2"})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := svc.UpdateFunctionCode(context.Background(), "fn", code)
		results <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := svc.UpdateFunctionConfiguration(context.Background(), "fn", &types.FunctionConfigUpdate{MemorySize: 256, Environment: &types.EnvVars{Variables: map[string]string{"KEY": "updated"}}})
		results <- err
	}()
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	fn, err := svc.GetFunction("fn")
	if err != nil {
		t.Fatal(err)
	}
	if len(fn.Tags) != count || fn.MemorySize != 256 || fn.Environment["KEY"] != "updated" || fn.CodeSHA256 != codeHash(code) {
		t.Fatalf("successful mutations lost: %+v", fn)
	}
	dir, err := svc.store.ExtractCode("fn")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "v2" {
		t.Fatalf("code metadata/content mismatch: %s", contents)
	}
}

func TestConcurrentLayerPublishKeepsEveryVersion(t *testing.T) {
	svc, cfg := regressionService(t)
	const count = 24
	packages := make([][]byte, count)
	for i := range count {
		packages[i] = functionsZIP(t, map[string]string{"value": fmt.Sprint(i)})
	}
	start := make(chan struct{})
	result := make(chan *types.LayerConfig, count)
	failures := make(chan error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			layer, err := svc.PublishLayerVersion("shared", fmt.Sprint(i), nil, packages[i])
			if err != nil {
				failures <- err
			} else {
				result <- layer
			}
		}()
	}
	close(start)
	wg.Wait()
	close(result)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	seen := make(map[int64]bool)
	for layer := range result {
		if seen[layer.VersionNumber] {
			t.Fatalf("duplicate version %d", layer.VersionNumber)
		}
		seen[layer.VersionNumber] = true
		dir, err := svc.store.ExtractLayer("shared", layer.VersionNumber)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "value"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != layer.Description {
			t.Fatalf("version overwritten: %+v contents=%s", layer, data)
		}
	}
	if len(seen) != count {
		t.Fatalf("persisted %d versions, want %d", len(seen), count)
	}
	if err := svc.DeleteLayerVersion("shared", count); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(cfg, NewStore(cfg), nil, nil, nil)
	t.Cleanup(restarted.Close)
	next, err := restarted.PublishLayerVersion("shared", "next", nil, packages[0])
	if err != nil {
		t.Fatal(err)
	}
	if next.VersionNumber != count+1 {
		t.Fatalf("deleted version number reused: %d", next.VersionNumber)
	}
	if _, err := svc.PublishLayerVersion("shared", "bad", nil, []byte("invalid")); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("invalid publish: %v", err)
	}
	versions, err := svc.ListLayerVersions("shared")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != count {
		t.Fatalf("failed publish changed versions: %d", len(versions))
	}
}

func TestResourcePathsRejectTraversalAndSymlinks(t *testing.T) {
	svc, cfg := regressionService(t)
	for _, name := range []string{"../outside", "a/../b", "/absolute", "..", "a\\b", "", strings.Repeat("x", 65)} {
		if err := svc.store.SaveFunction(&types.FunctionConfig{FunctionName: name}); !errors.Is(err, ErrInvalidParameter) {
			t.Errorf("SaveFunction(%q): %v", name, err)
		}
		if _, err := svc.store.SaveCode(name, nil); !errors.Is(err, ErrInvalidParameter) {
			t.Errorf("SaveCode(%q): %v", name, err)
		}
		if _, err := svc.store.GetFunction(name); !errors.Is(err, ErrInvalidParameter) {
			t.Errorf("GetFunction(%q): %v", name, err)
		}
		if err := svc.store.DeleteFunction(name); !errors.Is(err, ErrInvalidParameter) {
			t.Errorf("DeleteFunction(%q): %v", name, err)
		}
	}
	if _, err := svc.PublishLayerVersion("../outside", "", nil, nil); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("layer traversal: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cfg.FunctionsDir(), "linked")); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SaveFunction(&types.FunctionConfig{FunctionName: "linked"}); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("symlink write: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("storage escaped via symlink")
	}
	if _, _, err := ParseFunctionReference("arn:aws:s3:us-east-1:000000000000:function:fn", ""); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("invalid ARN accepted: %v", err)
	}
	if _, _, err := ParseFunctionReference("fn:1", "2"); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("conflicting qualifiers: %v", err)
	}
}

func TestVersionsAliasesPoliciesAndConcurrencySurviveRestart(t *testing.T) {
	svc, cfg := regressionService(t)
	code1 := functionsZIP(t, map[string]string{"index.js": "v1"})
	created := createRegressionFunction(t, svc, "fn", code1)
	created.Environment["KEY"] = "caller mutation"
	version1, err := svc.PublishVersion(context.Background(), "fn", "first")
	if err != nil {
		t.Fatal(err)
	}
	if version1.Version != "1" {
		t.Fatal(version1.Version)
	}
	alias, err := svc.CreateAlias("fn", Alias{Name: "live", FunctionVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	alias.FunctionVersion = "999"
	permission := Permission{StatementID: "one", Action: "lambda:InvokeFunction", Principal: "s3.amazonaws.com", SourceARN: "arn:aws:s3:::bucket", SourceAccount: cfg.AccountID}
	if err := svc.AddPermission("fn", permission); err != nil {
		t.Fatal(err)
	}
	if err := svc.PutFunctionConcurrency("fn", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateFunctionCode(context.Background(), "fn", functionsZIP(t, map[string]string{"index.js": "v2"})); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateFunctionConfiguration(context.Background(), "fn", &types.FunctionConfigUpdate{Environment: &types.EnvVars{Variables: map[string]string{"KEY": "v2"}}}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(cfg, store, nil, nil, nil)
	t.Cleanup(restarted.Close)
	for _, reference := range []string{"fn:1", "fn:live", fmt.Sprintf("arn:aws:lambda:%s:%s:function:fn:live", cfg.Region, cfg.AccountID), cfg.AccountID + ":function:fn:1"} {
		fn, err := restarted.GetFunction(reference)
		if err != nil {
			t.Fatalf("%s: %v", reference, err)
		}
		if fn.Version != "1" || fn.CodeSHA256 != codeHash(code1) || fn.Environment["KEY"] != "original" {
			t.Fatalf("version changed: %+v", fn)
		}
		fn.Environment["KEY"] = "changed"
	}
	policies, err := restarted.GetPolicy("fn")
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0] != permission {
		t.Fatalf("policy lost: %+v", policies)
	}
	policies[0].Action = "changed"
	limit, err := restarted.GetFunctionConcurrency("fn")
	if err != nil || limit == nil || *limit != 2 {
		t.Fatalf("concurrency lost: %v %v", limit, err)
	}
	*limit = 0
	versions, err := restarted.ListVersions("fn")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatal(len(versions))
	}
	second, err := restarted.PublishVersion(context.Background(), "fn", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != "2" {
		t.Fatalf("version sequence reset: %s", second.Version)
	}
	if err := restarted.DeleteFunction(context.Background(), "fn:1"); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("alias-target version deleted: %v", err)
	}
	if _, err := restarted.UpdateFunctionCode(context.Background(), "fn:1", code1); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("published version mutated: %v", err)
	}
}

func TestDeploymentLeaseSurvivesUpdateAndDelete(t *testing.T) {
	svc, _ := regressionService(t)
	createRegressionFunction(t, svc, "fn", functionsZIP(t, map[string]string{"index.js": "v1"}))
	lease, err := svc.store.acquireDeployment("fn", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	if _, err := svc.UpdateFunctionCode(context.Background(), "fn", functionsZIP(t, map[string]string{"index.js": "v2"})); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(lease.CodeDir, "index.js"))
	if err != nil || string(data) != "v1" {
		t.Fatalf("acquired code changed: %s %v", data, err)
	}
	if err := svc.DeleteFunction(context.Background(), "fn"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(lease.CodeDir, "index.js"))
	if err != nil || string(data) != "v1" {
		t.Fatalf("delete removed leased code: %s %v", data, err)
	}
	lease.release()
	if _, err := os.Stat(lease.CodeDir); !os.IsNotExist(err) {
		t.Fatalf("released old deployment not retired: %v", err)
	}
}

func TestFailedDeploymentRetainsLastWorkingCode(t *testing.T) {
	svc, _ := regressionService(t)
	code := functionsZIP(t, map[string]string{"index.js": "v1"})
	createRegressionFunction(t, svc, "fn", code)
	for _, operation := range []func() error{
		func() error {
			_, err := svc.UpdateFunctionCode(context.Background(), "fn", []byte("bad ZIP"))
			return err
		},
		func() error {
			_, err := svc.CreateFunction(context.Background(), &types.FunctionConfig{FunctionName: "fn", Runtime: types.RuntimeNodeJS20, Handler: "index.handler"}, []byte("bad ZIP"))
			return err
		},
		func() error {
			_, err := svc.UpdateFunctionConfiguration(context.Background(), "fn", &types.FunctionConfigUpdate{MemorySize: -1})
			return err
		},
	} {
		if err := operation(); !errors.Is(err, ErrInvalidParameter) {
			t.Fatalf("invalid deployment returned %v", err)
		}
		fn, err := svc.GetFunction("fn")
		if err != nil {
			t.Fatal(err)
		}
		if fn.CodeSHA256 != codeHash(code) || fn.Environment["KEY"] != "original" {
			t.Fatalf("failed update committed: %+v", fn)
		}
		dir, err := svc.store.ExtractCode("fn")
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "index.js"))
		if err != nil || string(data) != "v1" {
			t.Fatalf("previous code lost: %s %v", data, err)
		}
	}
}

func TestCachedMetadataObservesExternalFileEdits(t *testing.T) {
	svc, cfg := regressionService(t)
	fn := createRegressionFunction(t, svc, "fn", functionsZIP(t, map[string]string{"index.js": "v1"}))
	lease, err := svc.store.acquireDeployment("fn", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	fn.MemorySize = 512
	data, err := json.Marshal(fn)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.FunctionsDir(), "fn", configFileName), data, 0600); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.GetFunction("fn")
	if err != nil {
		t.Fatal(err)
	}
	if updated.MemorySize != 512 {
		t.Fatal("metadata cache hid an external edit")
	}
	if err := os.WriteFile(svc.store.GetCodePath("fn"), functionsZIP(t, map[string]string{"index.js": "external"}), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := svc.store.acquireDeployment("fn", "")
	if err != nil {
		t.Fatal(err)
	}
	defer next.release()
	data, err = os.ReadFile(filepath.Join(next.CodeDir, "index.js"))
	if err != nil || string(data) != "external" {
		t.Fatalf("source ZIP edit hidden: %s %v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(lease.CodeDir, "index.js"))
	if err != nil || string(data) != "v1" {
		t.Fatalf("external edit changed acquired code: %s %v", data, err)
	}
}

func TestMergedLayersKeepOrderPermissionsAndAttachedContent(t *testing.T) {
	svc, _ := regressionService(t)
	one, err := svc.PublishLayerVersion("one", "", nil, functionsZIP(t, map[string]string{"one": "one", "shared": "first", "bin/run": "executable"}))
	if err != nil {
		t.Fatal(err)
	}
	two, err := svc.PublishLayerVersion("two", "", nil, functionsZIP(t, map[string]string{"two": "two", "shared": "second"}))
	if err != nil {
		t.Fatal(err)
	}
	fn := &types.FunctionConfig{FunctionName: "fn", Runtime: types.RuntimeNodeJS20, Handler: "index.handler", Layers: []string{one.LayerVersionArn, two.LayerVersionArn}}
	if _, err := svc.CreateFunction(context.Background(), fn, functionsZIP(t, map[string]string{"index.js": "v1"})); err != nil {
		t.Fatal(err)
	}
	lease, err := svc.store.acquireDeployment("fn", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	if len(lease.LayerDirs) != 1 {
		t.Fatalf("expected one merged mount, got %v", lease.LayerDirs)
	}
	for name, want := range map[string]string{"one": "one", "two": "two", "shared": "second"} {
		data, err := os.ReadFile(filepath.Join(lease.LayerDirs[0], name))
		if err != nil || string(data) != want {
			t.Fatalf("merged %s = %s, %v", name, data, err)
		}
	}
	info, err := os.Stat(filepath.Join(lease.LayerDirs[0], "bin/run"))
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("executable mode lost: %v %v", info, err)
	}
	if err := svc.DeleteLayerVersion("two", two.VersionNumber); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateFunctionConfiguration(context.Background(), "fn", &types.FunctionConfigUpdate{MemorySize: 256}); err != nil {
		t.Fatalf("deleting layer broke attached function: %v", err)
	}
	if _, err := svc.UpdateFunctionConfiguration(context.Background(), "fn", &types.FunctionConfigUpdate{Layers: []string{two.LayerVersionArn, one.LayerVersionArn}}); err == nil {
		t.Fatal("new attachment of deleted layer succeeded")
	}
	// Restore the deleted layer under a new version, then reverse the order.
	replacement, err := svc.PublishLayerVersion("two", "", nil, functionsZIP(t, map[string]string{"two": "two", "shared": "second"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateFunctionConfiguration(context.Background(), "fn", &types.FunctionConfigUpdate{Layers: []string{replacement.LayerVersionArn, one.LayerVersionArn}}); err != nil {
		t.Fatal(err)
	}
	reversed, err := svc.store.acquireDeployment("fn", "")
	if err != nil {
		t.Fatal(err)
	}
	defer reversed.release()
	data, err := os.ReadFile(filepath.Join(reversed.LayerDirs[0], "shared"))
	if err != nil || string(data) != "first" {
		t.Fatalf("merge order ignored: %s %v", data, err)
	}
}

func TestColdStartGateCancellation(t *testing.T) {
	svc, cfg := regressionService(t)
	fn := createRegressionFunction(t, svc, "fn", functionsZIP(t, map[string]string{"index.js": "v1"}))
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	svc.engine = eng
	gate := svc.startGate("fn")
	if err := gate.Acquire(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	defer gate.Release(1)
	for _, preCancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if preCancelled {
			cancel()
		}
		done := make(chan error, 1)
		go func() { _, _, err := svc.acquireContainer(ctx, fn, "", nil, ""); done <- err }()
		if !preCancelled {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("queued acquire: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancelled acquisition stayed blocked on the gate")
		}
		cancel()
	}
}

func TestStableFunctionLockAcrossDeleteAndRecreate(t *testing.T) {
	svc, _ := regressionService(t)
	createRegressionFunction(t, svc, "fn", nil)
	before := svc.store.fnMu("fn")
	if err := svc.DeleteFunction(context.Background(), "fn"); err != nil {
		t.Fatal(err)
	}
	createRegressionFunction(t, svc, "fn", nil)
	if before != svc.store.fnMu("fn") {
		t.Fatal("lock replaced while queued operations can retain it")
	}
}

func TestDeleteLifecycleFinishesBeforeRecreation(t *testing.T) {
	svc, _ := regressionService(t)
	createRegressionFunction(t, svc, "fn", nil)
	entered, resume := make(chan struct{}), make(chan struct{})
	deleted, created := make(chan error, 1), make(chan error, 1)
	go func() { deleted <- svc.store.deleteFunction("fn", func() { close(entered); <-resume }) }()
	<-entered
	go func() {
		_, err := svc.CreateFunction(context.Background(), &types.FunctionConfig{FunctionName: "fn", Runtime: types.RuntimeNodeJS20, Handler: "index.handler"}, nil)
		created <- err
	}()
	early := false
	select {
	case err := <-created:
		early = true
		t.Errorf("recreation overtook deletion lifecycle: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(resume)
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	if !early {
		if err := <-created; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.GetFunction("fn"); err != nil {
		t.Fatal(err)
	}
}

func TestOldInvocationCannotRecordMetricsOnRecreatedFunction(t *testing.T) {
	svc, _ := regressionService(t)
	createRegressionFunction(t, svc, "fn", functionsZIP(t, map[string]string{"index.js": "v1"}))
	old, err := svc.store.acquireDeployment("fn", "")
	if err != nil {
		t.Fatal(err)
	}
	defer old.release()
	if err := svc.DeleteFunction(context.Background(), "fn"); err != nil {
		t.Fatal(err)
	}
	createRegressionFunction(t, svc, "fn", functionsZIP(t, map[string]string{"index.js": "v2"}))
	svc.recordInvocationInstance("fn", old.InstanceID, []byte(`{"Records":[{}]}`))
	if metrics := svc.GetFunctionMetrics("fn"); metrics.Invocations != 0 || metrics.MessagesProcessed != 0 {
		t.Fatalf("old invocation changed new function's metrics: %+v", metrics)
	}
}

func TestStartupPrunesAbandonedDeploymentsAndKeepsPublishedContent(t *testing.T) {
	svc, cfg := regressionService(t)
	createRegressionFunction(t, svc, "fn", functionsZIP(t, map[string]string{"index.js": "published"}))
	if _, err := svc.PublishVersion(context.Background(), "fn", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateFunctionCode(context.Background(), "fn", functionsZIP(t, map[string]string{"index.js": "current"})); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cfg.DataDir, "lambda-deployments", "fn")
	for _, directory := range []string{strings.Repeat("a", 64), ".pending-abandoned"} {
		path := filepath.Join(root, directory)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "uncommitted"), []byte("debris"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{strings.Repeat("a", 64), ".pending-abandoned"} {
		if _, err := os.Stat(filepath.Join(root, directory)); !os.IsNotExist(err) {
			t.Fatalf("startup retained abandoned directory %s: %v", directory, err)
		}
	}
	for reference, want := range map[string]string{"fn": "current", "fn:1": "published"} {
		lease, err := store.acquireDeployment(reference, "")
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := os.ReadFile(filepath.Join(lease.CodeDir, "index.js"))
		lease.release()
		if readErr != nil || string(data) != want {
			t.Fatalf("startup lost %s content: %s %v", reference, data, readErr)
		}
	}
}
