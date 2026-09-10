//go:build linux

package sandbox

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/cfg"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/fc"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/network"
	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
	"github.com/stretchr/testify/require"
)

// Exercise the actual Process Unix client and the helpers called by Sandbox
// Pause/Shutdown/Stop. No VM or cgroup is needed to check snapshot/API ordering.
func TestMeasurementSnapshotCompletesBeforeConcurrentStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	dir := t.TempDir()
	for _, file := range []string{"fc/firecracker", "kernel/vmlinux.bin"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), nil, 0600))
	}
	files := (storage.CachePaths{}).NewSandboxFiles("snapshot-order")
	slot, err := network.NewSlot("snapshot-order", 1, network.Config{}, nil)
	require.NoError(t, err)
	config := cfg.BuilderConfig{FirecrackerVersionsDir: dir, HostKernelsDir: dir, SandboxDir: dir}
	process, err := fc.NewProcess(ctx, ctx, config, slot, files, fc.Config{FirecrackerVersion: "fc", KernelVersion: "kernel"}, nil, fc.ConstantRootfsPaths)
	require.NoError(t, err)
	sbx := &Sandbox{process: process, config: config}
	listener, err := net.Listen("unix", files.SandboxFirecrackerSocketPath())
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var mu sync.Mutex
	var calls []string
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/snapshot/create" {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})}
	go server.Serve(listener)
	defer server.Close()
	snapshotDone, stopDone := make(chan error, 1), make(chan error, 1)
	go func() { snapshotDone <- sbx.snapshotBeforeMeasurementTerminal(ctx, "/test.snapshot") }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("snapshot API not reached", ctx.Err())
	}
	go func() { stopDone <- sbx.finalizeMeasurementBeforeStop(ctx) }()
	select {
	case err := <-stopDone:
		t.Fatal("stop overtook pending snapshot", err)
	case <-time.After(25 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	select {
	case err := <-snapshotDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("snapshot did not finish")
	}
	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("stop deadlocked after snapshot")
	}
	require.ErrorContains(t, sbx.snapshotBeforeMeasurementTerminal(ctx, "/late.snapshot"), "sandbox stopping")
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"PATCH /vm", "PUT /actions", "PUT /snapshot/create"}, calls)
}
