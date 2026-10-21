//go:build windows

package internal

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed qt_layout/agent.js
var qtLayoutAgent []byte

//go:embed qt_layout/helper.mjs
var qtLayoutHelper []byte

type qtLayoutRuntime struct {
	node, fridaModule, stateDirectory string
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Runtime lookup never depends on a particular user's account or REA install.
func resolveQtLayoutRuntime(executableDirectory string) (qtLayoutRuntime, error) {
	directory := os.Getenv("KAKAOTALK_ADBLOCK_RUNTIME_DIR")
	if directory == "" {
		directory = filepath.Join(executableDirectory, "runtime")
	}
	node := os.Getenv("KAKAOTALK_ADBLOCK_NODE")
	if node == "" {
		node = filepath.Join(directory, "node.exe")
		if !regularFile(node) {
			var err error
			node, err = exec.LookPath("node.exe")
			if err != nil {
				return qtLayoutRuntime{}, fmt.Errorf("Qt layout helper needs Node.js: %w", err)
			}
		}
	}
	module := os.Getenv("KAKAOTALK_ADBLOCK_FRIDA_MODULE")
	if module == "" {
		module = filepath.Join(directory, "node_modules", "frida", "build", "src", "frida.js")
	}
	if !regularFile(node) || !regularFile(module) {
		return qtLayoutRuntime{}, fmt.Errorf("Qt layout helper runtime is incomplete; see docs/qt6-layout.md")
	}
	state := os.Getenv("KAKAOTALK_ADBLOCK_STATE_DIR")
	if state == "" {
		state = filepath.Join(directory, "state")
	}
	for _, value := range []*string{&node, &module, &state} {
		absolute, err := filepath.Abs(*value)
		if err != nil {
			return qtLayoutRuntime{}, err
		}
		*value = absolute
	}
	return qtLayoutRuntime{node, module, state}, nil
}

func runQtLayoutHelper(ctx context.Context) {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	runtime, err := resolveQtLayoutRuntime(filepath.Dir(executable))
	if err != nil {
		return // Native window hiding remains available without the optional runtime.
	}
	configuration, err := json.Marshal(map[string]string{"fridaModule": runtime.fridaModule})
	if err != nil || os.MkdirAll(runtime.stateDirectory, 0700) != nil {
		return
	}
	for name, data := range map[string][]byte{"agent.js": qtLayoutAgent, "helper.mjs": qtLayoutHelper, "config.json": configuration} {
		if os.WriteFile(filepath.Join(runtime.stateDirectory, name), data, 0600) != nil {
			return
		}
	}
	for ctx.Err() == nil {
		command := exec.Command(runtime.node, filepath.Join(runtime.stateDirectory, "helper.mjs"), filepath.Join(runtime.stateDirectory, "config.json"))
		command.Dir = runtime.stateDirectory
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		input, err := command.StdinPipe()
		if err != nil {
			return
		}
		if err = command.Start(); err == nil {
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			select {
			case <-ctx.Done():
				input.Close()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					command.Process.Kill()
					<-done
				}
				return
			case <-done:
			}
		}
		input.Close()
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}
