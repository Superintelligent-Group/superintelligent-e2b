//go:build linux

package fc_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/fc"
	"github.com/stretchr/testify/require"
)

//go:embed measurement_calibration_test.py
var calibrationPython string

func runFiniteCalibration(t *testing.T, ctx context.Context, process *fc.Process, base, phase string) {
	t.Helper()
	dir := filepath.Join(base, "calibration-"+phase)
	require.NoError(t, os.Mkdir(dir, 0700))
	pid, err := process.Pid()
	require.NoError(t, err)
	// Process.Pid identifies the launcher. Locate the exact producer only within
	// that bounded descendant tree, never by a host-wide name match.
	queue := []int{pid}
	visited := 0
	seen := map[int]bool{}
	producerPIDs := []int{}
	for len(queue) > 0 {
		visited++
		require.LessOrEqual(t, visited, 16, "unexpected launcher descendant count")
		candidate := queue[0]
		require.False(t, seen[candidate], "cycle in launcher descendant tree")
		seen[candidate] = true
		queue = queue[1:]
		raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/exe", candidate))
		if e == nil && fmt.Sprintf("%x", sha256.Sum256(raw)) == "bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7" {
			producerPIDs = append(producerPIDs, candidate)
		}
		children, e := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", candidate, candidate))
		require.NoError(t, e)
		for _, child := range strings.Fields(string(children)) {
			id, e := strconv.Atoi(child)
			require.NoError(t, e)
			queue = append(queue, id)
		}
		require.LessOrEqual(t, len(queue), 16, "unexpected launcher process tree")
	}
	require.Len(t, producerPIDs, 1, "exact producer identity required")
	pid = producerPIDs[0]
	fds := map[string]string{}
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	require.NoError(t, err)
	for _, entry := range entries {
		link := fmt.Sprintf("/proc/%d/fd/%s", pid, entry.Name())
		target, e := os.Readlink(link)
		require.NoError(t, e)
		if target == "/dev/net/tun" {
			require.Empty(t, fds["tap"], "ambiguous TAP")
			fds["tap"] = entry.Name()
		}
		if strings.Contains(target, "metrics") && filepath.IsAbs(target) {
			info, e := os.Stat(link)
			require.NoError(t, e)
			if info.Mode()&os.ModeNamedPipe != 0 {
				require.Empty(t, fds["metrics"], "ambiguous FIFO")
				fds["metrics"] = entry.Name()
			}
		}
	}
	require.NotEmpty(t, fds["tap"])
	require.NotEmpty(t, fds["metrics"])
	writeJSON := func(name string, value any) {
		raw, e := json.MarshalIndent(value, "", "  ")
		require.NoError(t, e)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), raw, 0600))
	}
	writeJSON("fds.json", fds)
	capture := exec.CommandContext(ctx, "ip", "netns", "exec", "ns-912", "python3", "-c", calibrationPython, "capture", dir, phase)
	captureError, err := os.Create(filepath.Join(dir, "capture-stderr.txt"))
	require.NoError(t, err)
	defer captureError.Close()
	capture.Stderr = captureError
	captureInput, err := capture.StdinPipe()
	require.NoError(t, err)
	captureOutput, err := capture.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, capture.Start())
	captureWaited := false
	defer func() {
		_ = captureInput.Close()
		if !captureWaited {
			_ = capture.Process.Kill()
			_ = capture.Wait()
		}
	}()
	captureLines := bufio.NewScanner(captureOutput)
	require.True(t, captureLines.Scan(), "capture readiness missing")
	require.Equal(t, "ready", captureLines.Text())
	stderr, err := os.Create(filepath.Join(dir, "strace-stderr.txt"))
	require.NoError(t, err)
	defer stderr.Close()
	// A hard per-file 32MiB limit terminates capture on overflow; the result then
	// fails rather than accepting a truncated trace or exhausting the host disk.
	tracer := exec.Command("sh", "-c", "ulimit -f 65536; exec \"$@\"", "sh", "strace", "-ff", "-qq", "-yy", "-xx", "-v", "-s", "1048577", "-e", "trace=read,write,readv,writev", "-o", filepath.Join(dir, "trace"), "-p", strconv.Itoa(pid))
	tracer.Stderr = stderr
	require.NoError(t, tracer.Start())
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = tracer.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- tracer.Wait() }()
		select {
		case e := <-done:
			if e != nil {
				exit, ok := e.(*exec.ExitError)
				require.True(t, ok)
				status, ok := exit.Sys().(syscall.WaitStatus)
				require.True(t, ok && status.Signaled() && status.Signal() == syscall.SIGINT, "unexpected tracer failure: %v", e)
			}
		case <-time.After(5 * time.Second):
			_ = tracer.Process.Kill()
			<-done
			t.Fatal("tracer did not detach")
		}
	}
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tasks, e := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
		require.NoError(t, e)
		attached := len(tasks) > 0
		for _, task := range tasks {
			raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/status", pid, task.Name()))
			require.NoError(t, e)
			attached = attached && strings.Contains(string(raw), fmt.Sprintf("TracerPid:\t%d\n", tracer.Process.Pid))
		}
		if attached {
			break
		}
		require.True(t, time.Now().Before(deadline), "tracer attachment timeout")
		time.Sleep(10 * time.Millisecond)
	}
	start, err := fc.CalibrationSample(process, ctx)
	require.NoError(t, err)
	writeJSON("start.json", start)
	// Observe both directions independently while generating finite tagged traffic.
	_, err = fmt.Fprintln(captureInput, "traffic")
	require.NoError(t, err)
	require.True(t, captureLines.Scan(), "traffic completion missing")
	require.Equal(t, "traffic-complete", captureLines.Text())
	end, err := fc.CalibrationSample(process, ctx)
	require.NoError(t, err)
	writeJSON("end.json", end)
	stop()
	_, err = fmt.Fprintln(captureInput, "stop")
	require.NoError(t, err)
	err = capture.Wait()
	captureWaited = true
	require.NoError(t, err)
}

func verifyFiniteCalibration(t *testing.T, base string) {
	t.Helper()
	output, err := exec.Command("python3", "-c", calibrationPython, "verify", base).CombinedOutput()
	require.NoError(t, os.WriteFile(filepath.Join(base, "calibration-verifier.txt"), output, 0600))
	require.NoError(t, err, "%s", output)
}
