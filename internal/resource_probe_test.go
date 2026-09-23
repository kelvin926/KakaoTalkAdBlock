package internal

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"testing"
	"time"
	"unsafe"
)

// Opt-in, read-only discovery probe. It never starts the ad-removal worker.
// Run with KAKAOTALK_RESOURCE_PROBE=1 go test ./internal -run TestWatchResourceProbe -v -count=1.
func TestWatchResourceProbe(t *testing.T) {
	if os.Getenv("KAKAOTALK_RESOURCE_PROBE") != "1" {
		t.Skip("opt-in resource probe")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	handlesProc := kernel.NewProc("GetProcessHandleCount")
	measure := func() (uint32, uint64) {
		var handles uint32
		ret, _, err := handlesProc.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&handles)))
		if ret == 0 {
			t.Fatal(err)
		}
		var created, exited, kernelTime, userTime windows.Filetime
		if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernelTime, &userTime); err != nil {
			t.Fatal(err)
		}
		cpu := (uint64(kernelTime.HighDateTime)<<32 | uint64(kernelTime.LowDateTime)) + (uint64(userTime.HighDateTime)<<32 | uint64(userTime.LowDateTime))
		return handles, cpu
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { watch(ctx); close(done) }()
	time.Sleep(time.Second)
	h0, c0 := measure()
	start := time.Now()
	time.Sleep(12 * time.Second)
	h1, c1 := measure()
	elapsed := time.Since(start)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop")
	}
	h2, _ := measure()
	cpuMs := float64(c1-c0) / 10000
	t.Logf("wall=%.3fs cpu=%.3fms one_core=%.4f%% handles=%d->%d after_stop=%d", elapsed.Seconds(), cpuMs, cpuMs/(elapsed.Seconds()*10), h0, h1, h2)
}
