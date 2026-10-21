package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func clearQtRuntimeOverrides(t *testing.T) {
	t.Helper()
	for _, name := range []string{"KAKAOTALK_ADBLOCK_RUNTIME_DIR", "KAKAOTALK_ADBLOCK_NODE", "KAKAOTALK_ADBLOCK_FRIDA_MODULE", "KAKAOTALK_ADBLOCK_STATE_DIR"} {
		t.Setenv(name, "")
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
