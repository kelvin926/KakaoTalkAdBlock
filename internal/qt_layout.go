//go:build windows

package internal

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
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
	var paths struct {
		Node           string `json:"node"`
		FridaModule    string `json:"fridaModule"`
		StateDirectory string `json:"stateDirectory"`
	}
	if data, err := os.ReadFile(filepath.Join(directory, "paths.json")); err == nil {
		if err := json.Unmarshal(data, &paths); err != nil {
			return qtLayoutRuntime{}, err
		}
	}
	resolvePath := func(value string) string {
		if value != "" && !filepath.IsAbs(value) {
			return filepath.Join(directory, value)
		}
		return value
	}
	node := os.Getenv("KAKAOTALK_ADBLOCK_NODE")
	if node == "" {
		node = resolvePath(paths.Node)
	}
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
		module = resolvePath(paths.FridaModule)
	}
	if module == "" {
		module = filepath.Join(directory, "node_modules", "frida", "build", "src", "frida.js")
	}
	if !regularFile(node) || !regularFile(module) {
		return qtLayoutRuntime{}, fmt.Errorf("Qt layout helper runtime is incomplete; see docs/qt6-layout.md")
	}
	state := os.Getenv("KAKAOTALK_ADBLOCK_STATE_DIR")
	if state == "" {
		state = resolvePath(paths.StateDirectory)
	}
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

type qtHelperProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	done    chan error
}

func (helper *qtHelperProcess) stop() {
	helper.input.Close() // EOF restores the layout and detaches before exit.
	select {
	case <-helper.done:
	case <-time.After(5 * time.Second):
		helper.command.Process.Kill()
		<-helper.done
	}
}

func startQtLayoutHelper() (*qtHelperProcess, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	runtime, err := resolveQtLayoutRuntime(filepath.Dir(executable))
	if err != nil {
		return nil, err // Native hiding remains available without the optional runtime.
	}
	configuration, err := json.Marshal(map[string]string{"fridaModule": runtime.fridaModule})
	if err != nil || os.MkdirAll(runtime.stateDirectory, 0700) != nil {
		return nil, fmt.Errorf("cannot prepare Qt layout helper")
	}
	for name, data := range map[string][]byte{"agent.js": qtLayoutAgent, "helper.mjs": qtLayoutHelper, "config.json": configuration} {
		file := filepath.Join(runtime.stateDirectory, name)
		previous, _ := os.ReadFile(file)
		if !bytes.Equal(previous, data) {
			if err := os.WriteFile(file, data, 0600); err != nil {
				return nil, err
			}
		}
	}
	command := exec.Command(runtime.node, filepath.Join(runtime.stateDirectory, "helper.mjs"), filepath.Join(runtime.stateDirectory, "config.json"))
	command.Dir = runtime.stateDirectory
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err = command.Start(); err != nil {
		input.Close()
		return nil, err
	}
	helper := &qtHelperProcess{command, input, make(chan error, 1)}
	go func() { helper.done <- command.Wait() }()
	return helper, nil
}

func runQtLayoutHelper(ctx context.Context, targets <-chan []uint32, changes <-chan struct{}) {
	superviseQtLayoutHelper(ctx, targets, changes, startQtLayoutHelper)
}

func superviseQtLayoutHelper(ctx context.Context, targets <-chan []uint32, changes <-chan struct{}, start func() (*qtHelperProcess, error)) {
	var helper *qtHelperProcess
	var done <-chan error
	var retry <-chan time.Time
	var ids []uint32
	defer func() {
		if helper != nil {
			helper.stop()
		}
	}()
	for {
		if len(ids) != 0 && helper == nil && retry == nil {
			var err error
			helper, err = start()
			if err != nil {
				retry = time.After(10 * time.Second)
			} else {
				done = helper.done
			}
			if helper != nil {
				_ = json.NewEncoder(helper.input).Encode(map[string]any{"type": "targets", "pids": ids})
			}
		}
		select {
		case <-ctx.Done():
			return
		case ids = <-targets:
			if len(ids) == 0 {
				if helper != nil {
					helper.stop()
					helper = nil
					done = nil
				}
				retry = nil
			} else if helper != nil {
				_ = json.NewEncoder(helper.input).Encode(map[string]any{"type": "targets", "pids": ids})
			}
		case <-changes:
			if helper != nil {
				_ = json.NewEncoder(helper.input).Encode(map[string]string{"type": "ensure"})
			}
		case <-done:
			helper.input.Close()
			helper, done = nil, nil
			if len(ids) != 0 {
				retry = time.After(10 * time.Second)
			}
		case <-retry:
			retry = nil
		}
	}
}
