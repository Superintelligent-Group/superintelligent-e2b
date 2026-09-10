//go:build linux

package fc_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/cfg"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/block"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/cgroup"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/fc"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/network"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/uffd"
	sbxlogger "github.com/e2b-dev/infra/packages/shared/pkg/logger/sandbox"
	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
	"github.com/e2b-dev/infra/packages/shared/pkg/storage/header"
	"github.com/e2b-dev/infra/packages/shared/pkg/utils"
	"github.com/stretchr/testify/require"
)

// This opt-in acceptance test needs an isolated Linux container with KVM/TUN,
// SYS_ADMIN, NET_ADMIN and SYS_PTRACE. It creates only ns-912 inside that
// container. It intentionally does not claim sandbox cgroup containment.
// SUP912_ACCEPTANCE_DIR contains kernel/vmlinux.bin, fc/firecracker and a
// writable ext4 rootfs with /sup912-init that configures eth0 and stays alive.
func TestMeasurementRuntimeCreateAndUFFDResume(t *testing.T) {
	runMeasurementRuntime(t, "resume")
}
func TestMeasurementRuntimeCreateAndTerminal(t *testing.T) {
	runMeasurementRuntime(t, "create")
}
func TestMeasurementRuntimeSpoolExhaustion(t *testing.T) {
	runMeasurementRuntime(t, "budget")
}
func TestMeasurementRuntimeFiniteCalibration(t *testing.T) {
	if os.Getenv("SUP916_CALIBRATION") != "1" {
		t.Skip("explicit SUP916_CALIBRATION=1 required")
	}
	runMeasurementRuntime(t, "calibration")
}
func runMeasurementRuntime(t *testing.T, mode string) {
	if os.Getenv("SUP912_KVM_ACCEPTANCE") != "1" {
		t.Skip("explicit SUP912_KVM_ACCEPTANCE=1 required")
	}
	fixtures := os.Getenv("SUP912_ACCEPTANCE_DIR")
	require.True(t, filepath.IsAbs(fixtures))
	base := filepath.Join(fixtures, mode)
	require.NoError(t, os.Mkdir(base, 0700))
	input, err := os.Open(filepath.Join(fixtures, "rootfs.ext4"))
	require.NoError(t, err)
	output, err := os.Create(filepath.Join(base, "rootfs.ext4"))
	require.NoError(t, err)
	_, err = io.Copy(output, input)
	require.NoError(t, errors.Join(err, output.Close(), input.Close()))
	_, toolErr := exec.LookPath("python3")
	require.NoError(t, toolErr, "acceptance requires Python for its raw ICMP probe")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	const binarySHA = "bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7"
	hashFile := func(path string) string {
		f, e := os.Open(path)
		require.NoError(t, e)
		defer f.Close()
		h := sha256.New()
		_, e = io.Copy(h, f)
		require.NoError(t, e)
		return hex.EncodeToString(h.Sum(nil))
	}
	require.Equal(t, binarySHA, hashFile(filepath.Join(fixtures, "fc/firecracker")))
	require.Equal(t, "643096c1fabf0fbbda1d03c100b9e86b4e6965fd5a01d3c6fb9e8c0ecb7fbfc9", hashFile(filepath.Join(fixtures, "kernel/vmlinux.bin")))
	run := func(args ...string) {
		output, e := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
		require.NoError(t, e, "ip %v: %s", args, output)
	}
	run("netns", "add", "ns-912")
	t.Cleanup(func() { require.NoError(t, exec.Command("ip", "netns", "del", "ns-912").Run()) })
	run("netns", "exec", "ns-912", "ip", "link", "set", "lo", "up")
	run("netns", "exec", "ns-912", "ip", "tuntap", "add", "tap0", "mode", "tap")
	run("netns", "exec", "ns-912", "ip", "addr", "add", "169.254.0.22/30", "dev", "tap0")
	run("netns", "exec", "ns-912", "ip", "link", "set", "tap0", "up")
	for _, dir := range []string{"cache", "spool", "vm"} {
		require.NoError(t, os.MkdirAll(filepath.Join(base, dir), 0700))
	}
	spoolOptions := networkusage.SpoolOptions{MaxBytes: 32 << 20, SegmentBytes: 4 << 20, MaxSegments: 16}
	if mode == "budget" {
		// Deliberately tiny test dependency; production config enforces a larger
		// per-record minimum. Exercise actual capacity failure after guest traffic.
		spoolOptions = networkusage.SpoolOptions{MaxBytes: 64 << 10, SegmentBytes: 32 << 10, MaxSegments: 8}
	}
	service, e := networkusage.OpenService(filepath.Join(base, "spool"), spoolOptions)
	require.NoError(t, e)
	var processes []*fc.Process
	var backend *uffd.Uffd
	var memory block.DiffSource
	var serial *os.File
	accepted := false
	t.Cleanup(func() {
		closeCtx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		for _, p := range processes {
			if _, err := p.Pid(); err == nil {
				if err := p.Stop(closeCtx); err != nil {
					t.Error(err)
				}
				select {
				case <-p.Exit.Done():
				case <-closeCtx.Done():
					t.Error("process exit not observed")
				}
				if err := p.JoinMetrics(closeCtx); err != nil && mode != "budget" {
					t.Error(err)
				}
			}
		}
		if backend != nil {
			if err := backend.Stop(); err != nil {
				t.Error(err)
			}
		}
		if memory != nil {
			if err := memory.Close(); err != nil {
				t.Error(err)
			}
		}
		if serial != nil {
			_ = serial.Close()
		}
		if err := service.Close(closeCtx); err != nil && mode != "budget" {
			t.Error(err)
		}
		if accepted && !t.Failed() {
			spool, err := networkusage.OpenSpool(filepath.Join(base, "spool"), spoolOptions)
			require.NoError(t, err)
			defer spool.Close()
			segments, err := spool.Segments()
			require.NoError(t, err)
			require.NotEmpty(t, segments)
			correlations := map[string]map[networkusage.RequestScope]int{}
			var tx, rx uint64
			for _, segment := range segments {
				path := filepath.Join(base, "spool", segment.Name)
				require.Equal(t, segment.SHA256, hashFile(path))
				f, err := os.Open(path)
				require.NoError(t, err)
				decoder := json.NewDecoder(f)
				for {
					var record networkusage.Record
					err := decoder.Decode(&record)
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					require.False(t, record.Complete, "local runtime must not publish complete coverage")
					if mode != "budget" {
						require.True(t, record.Valid, "invalid durable record: %+v", record)
					}
					if record.Kind == "producer_frame" {
						tx += record.DeltaTxBytes
						rx += record.DeltaRxBytes
					}
					if record.Kind == "producer_correlation" {
						require.NotNil(t, record.Producer)
						if correlations[record.SandboxID] == nil {
							correlations[record.SandboxID] = map[networkusage.RequestScope]int{}
						}
						correlations[record.SandboxID][record.Producer.Scope]++
					}
				}
				require.NoError(t, f.Close())
			}
			require.Positive(t, tx, "no durable guest TX")
			require.Positive(t, rx, "no durable guest RX")
			ids := []string{"sup912-create"}
			if mode == "resume" || mode == "calibration" {
				ids = append(ids, "sup912-resume")
			}
			for _, id := range ids {
				require.Equal(t, 1, correlations[id][networkusage.BaselineScope])
				terminals := 1
				if mode == "budget" {
					terminals = 0
				}
				require.Equal(t, terminals, correlations[id][networkusage.TerminalScope])
			}
			result := map[string]any{"passed": true, "mode": mode, "create_pre_icmp": 3, "create_post_icmp": 0, "binary_sha256": binarySHA, "durable_correlations": correlations, "durable_tx": tx, "durable_rx": rx, "segments": segments, "rootfs_sha256_after_run": hashFile(filepath.Join(base, "rootfs.ext4")), "scope": "local Process acceptance only; no cgroup containment or remote custody claim"}
			if mode == "resume" || mode == "calibration" {
				result["resume_pre_icmp"], result["resume_post_icmp"] = 3, 0
			}
			encoded, err := json.MarshalIndent(result, "", "  ")
			require.NoError(t, err)
			if mode == "calibration" {
				verifyFiniteCalibration(t, base)
			}
			require.NoError(t, os.WriteFile(filepath.Join(base, "result.json"), encoded, 0600))
		}
	})
	config := cfg.BuilderConfig{FirecrackerVersionsDir: fixtures, HostKernelsDir: fixtures, SandboxDir: filepath.Join(base, "vm"), StorageConfig: storage.Config{SandboxCacheDir: filepath.Join(base, "cache")}, NetworkUsageCorrelated: true, NetworkUsageBinarySHA256: binarySHA, NetworkUsageSpoolDir: filepath.Join(base, "spool")}
	slot, e := network.NewSlot("sup912", 912, network.Config{}, nil)
	require.NoError(t, e)
	disabled := fc.RateLimiterConfig{Ops: fc.TokenBucketConfig{BucketSize: -1}, Bandwidth: fc.TokenBucketConfig{BucketSize: -1}}
	metadata := sbxlogger.SandboxMetadata{SandboxID: "sup912-create", TemplateID: "sup912-local", TeamID: "sup912-local"}
	makeProcess := func(id string) (*fc.Process, *storage.SandboxFiles) {
		files := (storage.CachePaths{}).NewSandboxFiles(id)
		p, e := fc.NewProcess(ctx, ctx, config, slot, files, fc.Config{KernelVersion: "kernel", FirecrackerVersion: "fc"}, &acceptanceRootfs{path: filepath.Join(base, "rootfs.ext4")}, fc.ConstantRootfsPaths)
		require.NoError(t, e)
		p.SetNetworkUsageService(service)
		processes = append(processes, p)
		return p, files
	}
	alive := func(p *fc.Process) {
		select {
		case <-p.Exit.Done():
			t.Fatal("Firecracker exited during acceptance")
		default:
		}
	}
	probe := func(count, expected int) ([]byte, error) {
		return exec.CommandContext(ctx, "ip", "netns", "exec", "ns-912", "python3", "-c", acceptanceICMPProbe, fmt.Sprint(count), fmt.Sprint(expected)).CombinedOutput()
	}
	ping := func(wantSuccess bool) {
		expected := 0
		if wantSuccess {
			expected = 3
		}
		output, e := probe(3, expected)
		t.Log(string(output))
		require.NoError(t, e, string(output))
	}
	paused := func(files *storage.SandboxFiles) {
		client := http.Client{Transport: &http.Transport{DialContext: func(c context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(c, "unix", files.SandboxFirecrackerSocketPath())
		}}, Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		response, e := client.Get("http://localhost/")
		require.NoError(t, e)
		defer response.Body.Close()
		require.Equal(t, 200, response.StatusCode)
		var body map[string]any
		require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
		require.Equal(t, "Paused", body["state"])
	}
	source, sourceFiles := makeProcess(metadata.SandboxID)
	serial, e = os.Create(filepath.Join(base, "create-serial.log"))
	require.NoError(t, e)
	require.NoError(t, source.Create(ctx, metadata, 1, 256, false, false, false, fc.ProcessOptions{InitScriptPath: "/sup912-init", KernelLogs: true, Stdout: serial, Stderr: serial}, disabled, disabled, cgroup.NoCgroupFD))
	// Boot completion is established by actual guest ICMP, not Create returning.
	var lastProbe []byte
	booted := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline) && ctx.Err() == nil; {
		var err error
		lastProbe, err = probe(1, 1)
		if err == nil {
			booted = true
			break
		}
	}
	require.True(t, booted, "guest boot ICMP failed: %s", lastProbe)
	alive(source)
	ping(true)
	if mode == "calibration" {
		runFiniteCalibration(t, ctx, source, base, "create")
	}
	if mode == "budget" {
		for i := 0; i < 32 && service.Err() == nil; i++ {
			_ = source.FlushMetrics(ctx)
		}
		require.ErrorIs(t, service.Err(), networkusage.ErrSpoolBudget)
		select {
		case <-source.Exit.Done():
		case <-time.After(15 * time.Second):
			t.Fatal("spool exhaustion did not stop guest process")
		}
		ping(false)
		require.Error(t, source.JoinMetrics(ctx))
		_, err := service.NewCollector("must-be-rejected")
		require.ErrorIs(t, err, networkusage.ErrSpoolBudget)
		accepted = true
		return
	}
	require.NoError(t, source.Pause(ctx))
	snapshot := acceptanceSnapshot(filepath.Join(base, "snapshot.bin"))
	require.NoError(t, source.CreateSnapshot(ctx, snapshot.Path()))
	pages := roaring.New()
	pages.AddRange(0, (256<<20)/4096)
	metaOut := utils.NewSetOnce[*header.DiffMetadata]()
	memory, e = source.ExportMemory(ctx, pages, filepath.Join(base, "memory.bin"), 4096, nil, false, nil, false, false, block.DedupBudget{}, roaring.New(), metaOut, false)
	require.NoError(t, e)
	require.NoError(t, source.FinalizeMeasurement(ctx))
	require.NoError(t, source.FinalizeMeasurement(ctx))
	alive(source)
	paused(sourceFiles)
	ping(false)
	alive(source)
	require.NoError(t, source.Stop(ctx))
	require.NoError(t, source.JoinMetrics(ctx))
	if mode == "create" {
		accepted = true
		return
	}
	restored, restoredFiles := makeProcess("sup912-resume")
	backend = uffd.New(&acceptanceMemory{source: memory}, restoredFiles.SandboxUffdSocketPath())
	require.NoError(t, backend.Start(ctx, "sup912-resume"))
	metadata.SandboxID = "sup912-resume"
	require.NoError(t, restored.Resume(ctx, metadata, restoredFiles.SandboxUffdSocketPath(), snapshot, backend.Ready(), nil, cgroup.NoCgroupFD, false, disabled, disabled))
	alive(restored)
	ping(true)
	if mode == "calibration" {
		runFiniteCalibration(t, ctx, restored, base, "resume")
	}
	require.NoError(t, restored.FinalizeMeasurement(ctx))
	require.NoError(t, restored.FinalizeMeasurement(ctx))
	alive(restored)
	paused(restoredFiles)
	ping(false)
	alive(restored)
	require.NoError(t, restored.Stop(ctx))
	require.NoError(t, restored.JoinMetrics(ctx))
	require.NoError(t, service.Err())
	accepted = true
}

// Runs inside the disposable test network namespace. Match source, identifier,
// sequence and payload so unrelated traffic cannot satisfy either control.
const acceptanceICMPProbe = `import json, os, socket, struct, sys, time
count, expected = map(int, sys.argv[1:])
identifier = os.getpid() & 65535
payload = b'SUP912_LOCAL_ACCEPTANCE'
replies = []
with socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_ICMP) as sock:
    sock.settimeout(.1)
    for seq in range(count):
        packet = struct.pack('!BBHHH', 8, 0, 0, identifier, seq) + payload
        padded = packet + b'\0' * (len(packet) % 2)
        total = sum(struct.unpack('!' + str(len(padded)//2) + 'H', padded))
        total = (total >> 16) + (total & 65535)
        total += total >> 16
        packet = packet[:2] + struct.pack('!H', (~total) & 65535) + packet[4:]
        sock.sendto(packet, ('169.254.0.21', 0))
        deadline = time.monotonic() + 1
        while time.monotonic() < deadline:
            try: response, addr = sock.recvfrom(65536)
            except socket.timeout: continue
            offset = (response[0] & 15) * 4
            if len(response) < offset + 8: continue
            typ, code, _, rid, rseq = struct.unpack('!BBHHH', response[offset:offset+8])
            if (typ, code, rid, rseq, addr[0], response[offset+8:]) == (0, 0, identifier, seq, '169.254.0.21', payload):
                replies.append(seq)
                break
print(json.dumps({'identifier': identifier, 'sent': count, 'reply_sequences': replies}))
sys.exit(0 if len(replies) == expected else 1)
`

type acceptanceRootfs struct{ path string }

func (r *acceptanceRootfs) Start(context.Context) error { return nil }
func (r *acceptanceRootfs) Close(context.Context) error { return nil }
func (r *acceptanceRootfs) Path() (string, error)       { return r.path, nil }
func (r *acceptanceRootfs) ExportDiff(context.Context, *os.File, func(context.Context) error) (*header.DiffMetadata, error) {
	return nil, fmt.Errorf("rootfs diff export outside acceptance scope")
}

type acceptanceSnapshot string

func (s acceptanceSnapshot) Path() string { return string(s) }
func (s acceptanceSnapshot) Close() error { return nil }

type acceptanceMemory struct{ source block.DiffSource }

func (m *acceptanceMemory) ReadAt(_ context.Context, p []byte, off int64) (int, error) {
	return m.source.ReadAt(p, off)
}
func (m *acceptanceMemory) Slice(_ context.Context, off, length int64) ([]byte, error) {
	return m.source.Slice(off, length)
}
func (m *acceptanceMemory) Size(context.Context) (int64, error) { return m.source.Size() }
func (m *acceptanceMemory) BlockSize() int64                    { return 4096 }
func (m *acceptanceMemory) Header() *header.Header              { return nil }
func (m *acceptanceMemory) SwapHeader(*header.Header)           {}
func (m *acceptanceMemory) Close() error                        { return nil } // The exporting test owns the cache lifetime.
