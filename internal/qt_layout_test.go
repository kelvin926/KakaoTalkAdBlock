package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func clearQtRuntimeOverrides(t *testing.T) {
	t.Helper()
	for _, name := range []string{"KAKAOTALK_ADBLOCK_RUNTIME_DIR", "KAKAOTALK_ADBLOCK_NODE", "KAKAOTALK_ADBLOCK_FRIDA_MODULE", "KAKAOTALK_ADBLOCK_STATE_DIR"} {
		t.Setenv(name, "")
	}
}

func TestQtRuntimeSidecarAndEnvironmentPrecedence(t *testing.T) {
	clearQtRuntimeOverrides(t)
	directory := t.TempDir()
	node := filepath.Join(directory, "runtime", "shared", "node.exe")
	module := filepath.Join(directory, "runtime", "shared", "frida.js")
	runtimeFixtureFile(t, node)
	runtimeFixtureFile(t, module)
	data, _ := json.Marshal(map[string]string{"node": "shared/node.exe", "fridaModule": "shared/frida.js", "stateDirectory": "data"})
	if err := os.WriteFile(filepath.Join(directory, "runtime", "paths.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	runtime, err := resolveQtLayoutRuntime(directory)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.node != node || runtime.fridaModule != module || runtime.stateDirectory != filepath.Join(directory, "runtime", "data") {
		t.Fatalf("wrong sidecar paths: %+v", runtime)
	}
	t.Setenv("KAKAOTALK_ADBLOCK_STATE_DIR", filepath.Join(directory, "override"))
	runtime, err = resolveQtLayoutRuntime(directory)
	if err != nil || runtime.stateDirectory != filepath.Join(directory, "override") {
		t.Fatalf("environment override ignored: %+v %v", runtime, err)
	}
}

func TestQtHelperDoesNotStartWithoutTargets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	targets, changes := make(chan []uint32, 1), make(chan struct{}, 1)
	started, done := make(chan struct{}, 1), make(chan struct{})
	go func() {
		superviseQtLayoutHelper(ctx, targets, changes, func() (*qtHelperProcess, error) {
			started <- struct{}{}
			return nil, fmt.Errorf("test runtime unavailable")
		})
		close(done)
	}()
	targets <- []uint32{}
	changes <- struct{}{}
	select {
	case <-started:
		t.Fatal("helper started without Qt")
	case <-time.After(100 * time.Millisecond):
	}
	targets <- []uint32{123}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Qt target did not start helper")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop")
	}
}

func runtimeFixtureFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestQtRuntimePortableBundle(t *testing.T) {
	clearQtRuntimeOverrides(t)
	directory := t.TempDir()
	node := filepath.Join(directory, "runtime", "node.exe")
	module := filepath.Join(directory, "runtime", "node_modules", "frida", "build", "src", "frida.js")
	runtimeFixtureFile(t, node)
	runtimeFixtureFile(t, module)
	runtime, err := resolveQtLayoutRuntime(directory)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.node != node || runtime.fridaModule != module || runtime.stateDirectory != filepath.Join(directory, "runtime", "state") {
		t.Fatalf("portable bundle resolved outside its directory: %+v", runtime)
	}
}

func TestQtRuntimeOverridesAndMissingModule(t *testing.T) {
	clearQtRuntimeOverrides(t)
	directory := t.TempDir()
	node, module, state := filepath.Join(directory, "custom", "node.exe"), filepath.Join(directory, "custom", "frida.js"), filepath.Join(directory, "state")
	runtimeFixtureFile(t, node)
	t.Setenv("KAKAOTALK_ADBLOCK_NODE", node)
	t.Setenv("KAKAOTALK_ADBLOCK_FRIDA_MODULE", module)
	t.Setenv("KAKAOTALK_ADBLOCK_STATE_DIR", state)
	if _, err := resolveQtLayoutRuntime(directory); err == nil {
		t.Fatal("missing module was accepted")
	}
	runtimeFixtureFile(t, module)
	runtime, err := resolveQtLayoutRuntime(directory)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.node != node || runtime.fridaModule != module || runtime.stateDirectory != state {
		t.Fatalf("explicit runtime paths were ignored: %+v", runtime)
	}
}
