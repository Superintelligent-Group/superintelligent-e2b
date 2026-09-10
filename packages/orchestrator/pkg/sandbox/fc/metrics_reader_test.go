//go:build linux

package fc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
)

type readerTestJournal struct {
	mu       sync.Mutex
	gaps     []string
	closed   bool
	closeErr error
}

func (j *readerTestJournal) Gap(reason string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return errors.New("gap after close")
	}
	j.gaps = append(j.gaps, reason)
	return nil
}
func (j *readerTestJournal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return errors.New("journal closed twice")
	}
	j.closed = true
	return j.closeErr
}
func (j *readerTestJournal) hasGap(reason string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, gap := range j.gaps {
		if gap == reason {
			return true
		}
	}
	return false
}
func fifoForTest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "metrics.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func joinReader(t *testing.T, s *metricsReader) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	return s.Join(ctx)
}
func awaitReader(t *testing.T, s *metricsReader) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("reader did not end")
	}
}
func noopMetricsFlush(context.Context) error { return nil }

func TestMetricsReaderOpenReturnsFIFOErrors(t *testing.T) {
	for _, kind := range []string{"missing", "regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "metrics")
			if kind == "regular" {
				if err := os.WriteFile(path, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink" {
				if err := os.Symlink(fifoForTest(t), path); err != nil {
					t.Fatal(err)
				}
			}
			j := &readerTestJournal{}
			s, err := openMetricsReader(t.Context(), path, j, func([]byte) error { return nil }, noopMetricsFlush, time.Hour, nil)
			if err == nil || s != nil || !j.closed || !j.hasGap("fifo_open_failed") {
				t.Fatalf("open did not fail/close: %v", err)
			}
		})
	}
}

func TestMetricsReaderJoinWaitsForConsumedFrameAndJournalClose(t *testing.T) {
	path := fifoForTest(t)
	j := &readerTestJournal{}
	entered, release := make(chan struct{}), make(chan struct{})
	s, err := openMetricsReader(t.Context(), path, j, func(line []byte) error {
		if !bytes.Equal(line, []byte(`{"net":{}}`)) {
			t.Errorf("unexpected frame: %q", line)
		}
		close(entered)
		<-release
		return nil
	}, noopMetricsFlush, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.WriteString("{\"net\":{}}\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	<-entered
	joined := make(chan error, 1)
	go func() { joined <- joinReader(t, s) }()
	select {
	case err := <-joined:
		t.Fatalf("join returned before consumer: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-joined; err != nil {
		t.Fatal(err)
	}
	if !j.closed {
		t.Fatal("successful join without journal close")
	}
	if err := joinReader(t, s); err != nil {
		t.Fatal("repeat join", err)
	}
}

func TestMetricsReaderAbortWithoutExitAndSurvivingWriter(t *testing.T) {
	path := fifoForTest(t)
	j := &readerTestJournal{}
	s, err := openMetricsReader(t.Context(), path, j, func([]byte) error { return nil }, noopMetricsFlush, time.Hour, make(chan struct{}))
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	p := &Process{metrics: s}
	if err := p.abortMetricsReader(); err == nil {
		t.Fatal("abort did not report incomplete")
	}
	awaitReader(t, s)
	if !j.closed || !j.hasGap("metrics_startup_aborted") {
		t.Fatal("abort was not durably closed")
	}
	if !p.metricsStopped {
		t.Fatal("startup can restart after abort")
	}
}

func TestMetricsReaderCanceledJoinForcesReadButDoesNotClaimPersistence(t *testing.T) {
	path := fifoForTest(t)
	j := &readerTestJournal{}
	s, err := openMetricsReader(t.Context(), path, j, func([]byte) error { return nil }, noopMetricsFlush, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Join(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	awaitReader(t, s)
	if !j.closed || !j.hasGap("metrics_join_interrupted") {
		t.Fatal("forced gap/close missing")
	}
	if err := joinReader(t, s); err == nil {
		t.Fatal("incomplete join became success")
	}
}

func TestMetricsReaderFlushCanceledAndJoinedBeforeClose(t *testing.T) {
	path := fifoForTest(t)
	j := &readerTestJournal{}
	entered, exited := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s, err := openMetricsReader(t.Context(), path, j, func([]byte) error { return nil }, func(ctx context.Context) error {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		select {
		case <-exited:
		default:
			close(exited)
		}
		return nil
	}, 10*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := joinReader(t, s); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("flusher not joined")
	}
	if !j.closed {
		t.Fatal("journal not closed")
	}
}

func TestMetricsReaderPartialTailRejectedAndSizeBoundary(t *testing.T) {
	for _, test := range []struct {
		name      string
		data      []byte
		wantFrame bool
		wantError bool
	}{
		{"partial", []byte(`{"net":{}}`), false, true},
		{"max", append(bytes.Repeat([]byte{'x'}, 1024*1024), '\n'), true, false},
		{"oversize", append(bytes.Repeat([]byte{'x'}, 1024*1024+1), '\n'), false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := fifoForTest(t)
			j := &readerTestJournal{}
			seen := false
			s, err := openMetricsReader(t.Context(), path, j, func([]byte) error { seen = true; return nil }, noopMetricsFlush, time.Hour, nil)
			if err != nil {
				t.Fatal(err)
			}
			w, err := os.OpenFile(path, os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			// Write in another goroutine because a full-size frame exceeds FIFO capacity.
			written := make(chan struct{})
			go func() { defer close(written); _, _ = w.Write(test.data); _ = w.Close() }()
			err = joinReader(t, s)
			<-written
			if (err != nil) != test.wantError || seen != test.wantFrame {
				t.Fatalf("error=%v frame=%v", err, seen)
			}
			if test.name == "partial" && !j.hasGap("partial_metrics_frame") {
				t.Fatal("partial tail gap missing")
			}
		})
	}
}

func TestMetricsReaderReportsJournalCloseFailure(t *testing.T) {
	want := errors.New("sync close failed")
	j := &readerTestJournal{closeErr: want}
	s, err := openMetricsReader(t.Context(), fifoForTest(t), j, func([]byte) error { return nil }, noopMetricsFlush, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := joinReader(t, s); !errors.Is(err, want) {
		t.Fatal(err)
	}
}

func TestMetricsReaderRealJournalPartialTailDurable(t *testing.T) {
	dir := t.TempDir()
	j, err := networkusage.Open(dir, "sandbox-test")
	if err != nil {
		t.Fatal(err)
	}
	path := fifoForTest(t)
	s, err := openMetricsReader(t.Context(), path, j, func([]byte) error { return j.Observe(3, 4) }, noopMetricsFlush, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.WriteString("complete\npartial"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err := joinReader(t, s); !errors.Is(err, errMetricsPartialTail) {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var records []networkusage.Record
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var record networkusage.Record
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if len(records) != 4 || records[1].DeltaTxBytes != 3 || records[2].Reason != "partial_metrics_frame" || records[3].Kind != "closed" || records[3].Valid || records[3].Complete {
		t.Fatalf("unexpected durable records: %+v", records)
	}
}

func TestMetricsReaderTimeoutDoesNotWaitForBlockedPersistence(t *testing.T) {
	path := fifoForTest(t)
	j := &readerTestJournal{}
	entered, release := make(chan struct{}), make(chan struct{})
	s, err := openMetricsReader(t.Context(), path, j, func([]byte) error { close(entered); <-release; return nil }, noopMetricsFlush, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("frame\n")
	w.Close()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.Join(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-s.done:
		t.Fatal("claimed joined while persistence blocked")
	default:
	}
	close(release)
	awaitReader(t, s)
	if !j.hasGap("metrics_join_interrupted") || !j.closed {
		t.Fatal("eventual gap and close missing")
	}
}

type blockingCloseJournal struct {
	readerTestJournal
	entered, release chan struct{}
}

func (j *blockingCloseJournal) Close() error {
	close(j.entered)
	<-j.release
	return j.readerTestJournal.Close()
}

func TestMetricsReaderCanceledSealingWaitDoesNotRewriteDurableOutcome(t *testing.T) {
	j := &blockingCloseJournal{entered: make(chan struct{}), release: make(chan struct{})}
	s, err := openMetricsReader(t.Context(), fifoForTest(t), j, func([]byte) error { return nil }, noopMetricsFlush, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.beginDrain()
	<-j.entered // actual journal.Close is blocked; input and flusher are drained
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Join(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if j.hasGap("metrics_join_interrupted") {
		t.Fatal("caller wait changed already-drained stream")
	}
	close(j.release)
	if err := joinReader(t, s); err != nil {
		t.Fatal("later join must report actual durable outcome", err)
	}
	for i := 0; i < 100; i++ {
		if err := s.Join(ctx); err != nil {
			t.Fatal("completed result changed for canceled caller", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != metricsCompleted || s.forcedReason != "" || s.err != nil {
		t.Fatal("completed state mutated")
	}
}
