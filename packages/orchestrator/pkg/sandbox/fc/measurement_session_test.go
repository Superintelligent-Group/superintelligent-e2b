//go:build linux

package fc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/cfg"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
	"github.com/e2b-dev/infra/packages/shared/pkg/utils"
)

// Uses the production Process reader, actual Unix HTTP and Linux FIFO, and the
// real bounded spool. The server models one response lost after frame emission.
func TestMeasurementProcessFIFOExactRetryAndTerminalJoin(t *testing.T) {
	dir := t.TempDir()
	service, err := networkusage.OpenService(dir, networkusage.SpoolOptions{MaxBytes: 8 << 20, SegmentBytes: 2 << 20, MaxSegments: 16})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "api.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	p := &Process{config: cfg.BuilderConfig{NetworkUsageCorrelated: true}, files: &storage.SandboxFiles{SandboxID: "session-test"}, metricsPath: fifoForTest(t), firecrackerSocketPath: socket, Exit: utils.NewErrorOnce()}
	p.SetNetworkUsageService(service)
	if err = p.startMetricsReader(t.Context()); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var writer *os.File
	var first networkusage.ProducerRequest
	var firstReceipt []byte
	var attempt uint64
	var calls []measurementAction
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodPut {
			t.Error("unexpected method", r.Method)
			http.Error(w, "method", 400)
			return
		}
		if r.URL.Path == "/metrics" {
			var config measurementConfig
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				t.Error(err)
			}
			if config.Path != p.metricsPath || config.Incarnation != p.measurement.collector.Incarnation() {
				t.Error("incorrect producer binding", config)
			}
			writer, err = os.OpenFile(config.Path, os.O_WRONLY, 0)
			if err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/actions" {
			t.Error("unexpected path", r.URL.Path)
		}
		var action measurementAction
		if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
			t.Error(err)
		}
		calls = append(calls, action)
		if action.Action != "FlushMeasurement" && action.Action != "FinalizeMeasurement" {
			t.Error("wrong action", action.Action)
		}
		if firstReceipt != nil && action.Measurement == first {
			w.Write(firstReceipt)
			return
		}
		attempt++
		request := action.Measurement
		header := networkusage.ProducerHeader{Schema: networkusage.ProducerSchema, Incarnation: request.Incarnation, Attempt: attempt, RequestSequence: &request.Sequence, RequestID: &request.ID}
		metrics := map[string]any{"net": map[string]uint64{"tx_bytes_count": 0, "rx_bytes_count": 0}}
		if request.ID != "baseline" {
			metrics["net_eth0"] = map[string]uint64{"tx_bytes_count": 3, "rx_bytes_count": 4}
			metrics["net"] = metrics["net_eth0"]
			metrics["balloon"] = map[string]uint64{"free_page_hint_count": 1}
		}
		frame := map[string]any{"measurement": header, "metrics": metrics}
		var receipt any = header
		if action.Action == "FinalizeMeasurement" {
			frame["terminal_cutoff"] = networkusage.TerminalCutoff
			receipt = map[string]any{"schema": networkusage.TerminalSchema, "cutoff": networkusage.TerminalCutoff, "measurement": header}
		}
		raw, _ := json.Marshal(frame)
		raw = append(raw, '\n')
		if _, err := writer.Write(raw); err != nil {
			t.Error(err)
		}
		response, _ := json.Marshal(receipt)
		if request.ID == "baseline" {
			first = request
			firstReceipt = response
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		w.Write(response)
	})}
	go server.Serve(listener)
	t.Cleanup(func() {
		server.Close()
		mu.Lock()
		if writer != nil {
			writer.Close()
		}
		mu.Unlock()
		p.abortMetricsReader()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		service.Close(ctx)
	})
	if err = p.configureMetrics(t.Context()); err != nil {
		t.Fatal(err)
	}
	started := false
	if err = p.startMeasuredActivity(t.Context(), func() error { started = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("activity not invoked")
	}
	balloon, err := p.FlushAndReadBalloonMetrics(t.Context())
	if err != nil || balloon.HintCount != 1 {
		t.Fatal(balloon, err)
	}
	if err = p.FinalizeMeasurement(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = p.FinalizeMeasurement(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = p.FlushMetrics(t.Context()); err == nil {
		t.Fatal("freshness claimed after terminal cutoff")
	}
	mu.Lock()
	writer.Close()
	writer = nil
	if len(calls) != 4 || calls[0].Measurement != calls[1].Measurement || calls[3].Action != "FinalizeMeasurement" {
		t.Errorf("unexpected exact retry/action sequence: %+v", calls)
	}
	mu.Unlock()
	p.Exit.SetError(nil)
	if err = p.JoinMetrics(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = service.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	spool, err := networkusage.OpenSpool(dir, networkusage.SpoolOptions{MaxBytes: 8 << 20, SegmentBytes: 2 << 20, MaxSegments: 16})
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	segments, err := spool.Segments()
	if err != nil || len(segments) == 0 {
		t.Fatal(segments, err)
	}
	for _, segment := range segments {
		if segment.Incomplete {
			t.Fatal("unexpected raw tail", segment)
		}
	}
}

func TestMeasurementFailureIsolationAndProcessSupervision(t *testing.T) {
	service, err := networkusage.OpenService(t.TempDir(), networkusage.SpoolOptions{MaxBytes: 8 << 20, SegmentBytes: 2 << 20, MaxSegments: 16})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		_ = service.Close(ctx)
	})
	makeProcess := func(id string) *Process {
		cmd := exec.Command("sleep", "60")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		p := &Process{cmd: cmd, config: cfg.BuilderConfig{NetworkUsageCorrelated: true}, files: &storage.SandboxFiles{SandboxID: id}, metricsPath: fifoForTest(t), Exit: utils.NewErrorOnce()}
		go func() { p.Exit.SetError(cmd.Wait()) }()
		p.SetNetworkUsageService(service)
		if err := p.startMetricsReader(t.Context()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Stop(context.Background()); _ = p.JoinMetrics(context.Background()) })
		return p
	}
	a, b := makeProcess("cancel-one"), makeProcess("keep-running")
	a.measurementSession().fail(context.Canceled)
	select {
	case <-a.Exit.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("local failure did not stop its process")
	}
	if err := a.JoinMetrics(t.Context()); err == nil {
		t.Fatal("canceled process silently complete")
	}
	if err := service.Err(); err != nil {
		t.Fatal("local cancellation poisoned host", err)
	}
	select {
	case <-b.Exit.Done():
		t.Fatal("unrelated process stopped")
	default:
	}
	if err := b.MeasurementError(); err != nil {
		t.Fatal(err)
	}
	late, err := service.NewCollector("still-admitted")
	if err != nil {
		t.Fatal(err)
	}
	if err := late.Close(); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("shared durable storage unavailable")
	service.Fail(cause)
	select {
	case <-b.Exit.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("shared failure did not stop remaining process")
	}
	if _, err := service.NewCollector("rejected"); !errors.Is(err, cause) {
		t.Fatal("host failure allowed admission", err)
	}
	if err := b.JoinMetrics(t.Context()); err == nil {
		t.Fatal("failed host silently complete")
	}
}

func TestMeasurementPeriodicTickSkipsBusyActivityGate(t *testing.T) {
	service, err := networkusage.OpenService(t.TempDir(), networkusage.SpoolOptions{MaxBytes: 8 << 20, SegmentBytes: 2 << 20, MaxSegments: 16})
	if err != nil {
		t.Fatal(err)
	}
	session, err := newMeasurementSession(service, "slow-load", "unused")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close(); _ = service.Close(context.Background()) }()
	session.active.Store(true)
	if err := session.lock(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer session.unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := session.periodicSample(ctx); err != nil {
		t.Fatal("busy periodic tick should be skipped", err)
	}
	if err := session.Err(); err != nil {
		t.Fatal("slow activity incorrectly failed", err)
	}
	if err := service.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMeasurementNoDeviceFrameAtActivityBoundaryFailsLocally(t *testing.T) {
	service, err := networkusage.OpenService(t.TempDir(), networkusage.SpoolOptions{MaxBytes: 8 << 20, SegmentBytes: 2 << 20, MaxSegments: 16})
	if err != nil {
		t.Fatal(err)
	}
	session, err := newMeasurementSession(service, "activity-boundary", "unused")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close(); _ = service.Close(context.Background()) }()
	fixture := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join("..", "networkusage", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		raw = bytes.SplitAfter(raw, []byte{'\n'})[0]
		return bytes.ReplaceAll(raw, []byte("sup909_local_boot"), []byte(session.collector.Incarnation()))
	}
	baseline := fixture("sup909-producer.jsonl")
	consume := func([]byte) error { return nil }
	if err := session.observe(baseline, consume); err != nil {
		t.Fatal(err)
	}
	if err := session.collector.ObserveReceipt(fixture("sup909-receipts.jsonl")); err != nil {
		t.Fatal(err)
	}
	periodic := bytes.Replace(baseline, []byte(`"attempt_sequence":1`), []byte(`"attempt_sequence":2`), 1)
	periodic = bytes.Replace(periodic, []byte(`"request_sequence":1`), []byte(`"request_sequence":null`), 1)
	periodic = bytes.Replace(periodic, []byte(`"request_id":"baseline"`), []byte(`"request_id":null`), 1)
	if err := session.observe(periodic, consume); err != nil {
		t.Fatal("preboot periodic frame rejected", err)
	}
	periodic = bytes.Replace(periodic, []byte(`"attempt_sequence":2`), []byte(`"attempt_sequence":3`), 1)
	// Once intent precedes startVM/loadSnapshot, an unexpected no-device frame
	// is unknown coverage, never an observed zero. Abort just this incarnation.
	err = session.startActivity(t.Context(), func() error { return session.observe(periodic, consume) })
	if err == nil || session.Err() == nil {
		t.Fatal("unknown activity coverage accepted")
	}
	if err := service.Err(); err != nil {
		t.Fatal("session gap poisoned host", err)
	}
}

func TestMeasurementFailedHostNeverStartsAndMissingTerminalFails(t *testing.T) {
	service, err := networkusage.OpenService(t.TempDir(), networkusage.SpoolOptions{MaxBytes: 1 << 20, SegmentBytes: 128 << 10, MaxSegments: 8})
	if err != nil {
		t.Fatal(err)
	}
	session, err := newMeasurementSession(service, "failed-host", "unused")
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("disk failed")
	service.Fail(cause)
	called := false
	if err = session.startActivity(t.Context(), func() error { called = true; return nil }); !errors.Is(err, cause) || called {
		t.Fatal("failed host admitted work", called, err)
	}
	if err = session.Close(); err == nil {
		t.Fatal("missing terminal silently closed")
	}
	if err = service.Close(t.Context()); err == nil {
		t.Fatal("failure not preserved")
	}
}

func TestMeasurementClientRejectsWrongStatusAndOversizedReceipt(t *testing.T) {
	for _, body := range []string{"wrong-status", fmt.Sprintf("%02049d", 1)} {
		t.Run(body[:8], func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "api.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if body == "wrong-status" {
					w.WriteHeader(204)
				} else {
					w.Write([]byte(body))
				}
			})}
			go server.Serve(listener)
			defer server.Close()
			_, err = newMeasurementClient(socket).request(t.Context(), networkusage.ProducerRequest{Incarnation: "i", Sequence: 1, ID: "x"}, false)
			if err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}
