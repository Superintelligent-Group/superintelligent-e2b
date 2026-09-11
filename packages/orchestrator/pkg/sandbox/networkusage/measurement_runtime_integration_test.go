//go:build linux

package networkusage_test

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

// Opt-in real Process -> protected service -> closing -> consumer acceptance.
// Run only inside the pinned disposable nested-KVM environment; no live AWS.
func TestRuntimeProtectedClosingConsumerCreateAndUFFDResume(t *testing.T) {
	if os.Getenv("SUP922_KVM_ACCEPTANCE") != "1" {
		t.Skip("explicit SUP922_KVM_ACCEPTANCE=1 required")
	}
	identity, err := networkusage.ConsumerBuildIdentity()
	require.NoError(t, err)
	require.Len(t, os.Getenv("SUP922_EXPECTED_SOURCE"), 40)
	require.Equal(t, os.Getenv("SUP922_EXPECTED_SOURCE"), identity.SourceRevision)
	require.Equal(t, "linker_embedded_source_declaration", identity.Provenance)
	fixtures := os.Getenv("SUP922_ACCEPTANCE_DIR")
	require.True(t, filepath.IsAbs(fixtures))
	base := filepath.Join(fixtures, "runtime-consumer")
	require.NoError(t, os.Mkdir(base, 0700))
	input, err := os.Open(filepath.Join(fixtures, "rootfs.ext4"))
	require.NoError(t, err)
	output, err := os.Create(filepath.Join(base, "rootfs.ext4"))
	require.NoError(t, err)
	_, err = io.Copy(output, input)
	require.NoError(t, errors.Join(err, output.Close(), input.Close()))
	_, toolErr := exec.LookPath("python3")
	require.NoError(t, toolErr, "acceptance requires Python for its raw ICMP probe")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
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
	rootfsSHA := os.Getenv("SUP922_ROOTFS_SHA256")
	require.Len(t, rootfsSHA, 64, "pin the derived ext4 from the environment build receipt")
	require.Equal(t, rootfsSHA, hashFile(filepath.Join(base, "rootfs.ext4")))
	executable, err := os.Executable()
	require.NoError(t, err)
	testBinarySHA := hashFile(executable)
	run := func(args ...string) {
		output, e := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
		require.NoError(t, e, "ip %v: %s", args, output)
	}
	run("netns", "add", "ns-922")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, exec.CommandContext(cleanupCtx, "ip", "netns", "del", "ns-922").Run())
	})
	run("netns", "exec", "ns-922", "ip", "link", "set", "lo", "up")
	run("netns", "exec", "ns-922", "ip", "tuntap", "add", "tap0", "mode", "tap")
	run("netns", "exec", "ns-922", "ip", "addr", "add", "169.254.0.22/30", "dev", "tap0")
	run("netns", "exec", "ns-922", "ip", "link", "set", "tap0", "up")
	for _, dir := range []string{"cache", "spool", "vm"} {
		require.NoError(t, os.MkdirAll(filepath.Join(base, dir), 0700))
	}
	spoolOptions := networkusage.SpoolOptions{MaxBytes: 32 << 20, SegmentBytes: 4 << 20, MaxSegments: 16}
	backendStore, deliveryConfig, e := newRetainedBackend(filepath.Join(base, "retained"))
	require.NoError(t, e)
	writer := &retainedWriter{backend: backendStore}
	service, e := networkusage.OpenProtectedServiceForTest(filepath.Join(base, "spool"), spoolOptions, deliveryConfig, writer)
	require.NoError(t, e)
	var consumerReceipts []*networkusage.NonMonetaryReceipt
	deliveryJoined := false
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
				if err := p.JoinMetrics(closeCtx); err != nil {
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
		if !deliveryJoined {
			if err := service.Close(closeCtx); err != nil && !errors.Is(err, networkusage.ErrClosingPending) {
				t.Error(err)
			}
		}
		if accepted && !t.Failed() {
			result := map[string]any{"passed": true, "binary_sha256": binarySHA, "test_binary_sha256": testBinarySHA, "verifier": identity, "rootfs_sha256_before_run": rootfsSHA,
				"spool_options": spoolOptions, "delivery_config": deliveryConfig, "consumer_options": runtimeConsumerOptions(),
				"create_pre_icmp": 3, "create_post_icmp": 0, "resume_pre_icmp": 3, "resume_post_icmp": 0,
				"receipts": consumerReceipts, "rootfs_sha256_after_run": hashFile(filepath.Join(base, "rootfs.ext4")),
				"scope": "actual local Process and protected closing/consumer composition; simulated storage, no live AWS retention, cgroup containment or SIG session mapping"}
			encoded, err := json.MarshalIndent(result, "", "  ")
			require.NoError(t, err)
			require.NoError(t, writeExclusive(filepath.Join(base, "result.json"), encoded))
		}

	})
	config := cfg.BuilderConfig{FirecrackerVersionsDir: fixtures, HostKernelsDir: fixtures, SandboxDir: filepath.Join(base, "vm"), StorageConfig: storage.Config{SandboxCacheDir: filepath.Join(base, "cache")}, NetworkUsageCorrelated: true, NetworkUsageBinarySHA256: binarySHA, NetworkUsageSpoolDir: filepath.Join(base, "spool")}
	slot, e := network.NewSlot("sup922", 922, network.Config{}, nil)
	require.NoError(t, e)
	disabled := fc.RateLimiterConfig{Ops: fc.TokenBucketConfig{BucketSize: -1}, Bandwidth: fc.TokenBucketConfig{BucketSize: -1}}
	metadata := sbxlogger.SandboxMetadata{SandboxID: "sup922-create", TemplateID: "sup922-local", TeamID: "sup922-local"}
	makeProcess := func(id string) (*fc.Process, *storage.SandboxFiles) {
		files := (storage.CachePaths{}).NewSandboxFiles(id)
		p, e := fc.NewProcess(ctx, ctx, config, slot, files, fc.Config{KernelVersion: "kernel", FirecrackerVersion: "fc"}, &acceptanceRootfs{path: filepath.Join(base, "rootfs.ext4")}, fc.ConstantRootfsPaths)
		require.NoError(t, e)
		p.SetNetworkUsageService(service)
		p.SetMeasurementWorkload(networkusage.WorkloadBinding{SandboxID: id, ExecutionID: "execution-" + id, LifecycleID: "lifecycle-" + id, TemplateID: metadata.TemplateID, TeamID: metadata.TeamID, BuildID: "sup922-local-build"})
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
		return exec.CommandContext(ctx, "ip", "netns", "exec", "ns-922", "python3", "-c", acceptanceICMPProbe, fmt.Sprint(count), fmt.Sprint(expected)).CombinedOutput()
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
	restored, restoredFiles := makeProcess("sup922-resume")
	backend = uffd.New(&acceptanceMemory{source: memory}, restoredFiles.SandboxUffdSocketPath())
	require.NoError(t, backend.Start(ctx, "sup922-resume"))
	metadata.SandboxID = "sup922-resume"
	require.NoError(t, restored.Resume(ctx, metadata, restoredFiles.SandboxUffdSocketPath(), snapshot, backend.Ready(), nil, cgroup.NoCgroupFD, false, disabled, disabled))
	alive(restored)
	ping(true)
	require.NoError(t, restored.FinalizeMeasurement(ctx))
	require.NoError(t, restored.FinalizeMeasurement(ctx))
	alive(restored)
	paused(restoredFiles)
	ping(false)
	alive(restored)
	require.NoError(t, restored.Stop(ctx))
	require.NoError(t, restored.JoinMetrics(ctx))
	require.NoError(t, service.Err())
	consumerReceipts = verifyRuntimeConsumer(t, ctx, base, backendStore, service, spoolOptions, deliveryConfig)
	deliveryJoined = true
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
