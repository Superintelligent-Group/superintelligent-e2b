//go:build linux

package networkusage

import (
	"encoding/csv"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestSpoolDafnyDecisions(t *testing.T) {
	f, e := os.Open("testdata/spool-model.csv")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	rows, e := csv.NewReader(f).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 480 {
		t.Fatal("incomplete oracle fixture")
	}
	for _, row := range rows {
		used, e := strconv.ParseInt(row[0], 10, 64)
		if e != nil {
			t.Fatal(e)
		}
		count, e := strconv.Atoi(row[1])
		if e != nil {
			t.Fatal(e)
		}
		bytes, e := strconv.ParseInt(row[2], 10, 64)
		if e != nil {
			t.Fatal(e)
		}
		fresh, e := strconv.ParseBool(row[3])
		if e != nil {
			t.Fatal(e)
		}
		want, e := strconv.ParseBool(row[4])
		if e != nil {
			t.Fatal(e)
		}
		if got := spoolAllows(used, count, bytes, fresh, SpoolOptions{MaxBytes: 10, SegmentBytes: 4, MaxSegments: 3}); got != want {
			t.Fatalf("oracle mismatch: %v", row)
		}
	}
}

func TestSpoolConcurrentBudget(t *testing.T) {
	s := testSpool(t, SpoolOptions{MaxBytes: 4000, SegmentBytes: 650, MaxSegments: 8})
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			j, e := OpenWithSpool(s, "concurrent")
			if e != nil {
				if !errors.Is(e, ErrSpoolBudget) {
					t.Error(e)
				}
				return
			}
			for range 5 {
				if e = j.Observe(1, 1); e != nil {
					if !errors.Is(e, ErrSpoolBudget) {
						t.Error(e)
					}
					break
				}
			}
			if e = j.Close(); e != nil && !errors.Is(e, ErrSpoolBudget) {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	assertBound(t, s)
}

func testSpool(t *testing.T, options SpoolOptions) *Spool {
	t.Helper()
	s, e := OpenSpool(t.TempDir(), options)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := s.Close(); e != nil {
			t.Error(e)
		}
	})
	return s
}
func assertBound(t *testing.T, s *Spool) {
	t.Helper()
	entries, e := os.ReadDir(s.dir)
	if e != nil {
		t.Fatal(e)
	}
	var bytes int64
	count := 0
	for _, v := range entries {
		f, e := v.Info()
		if e != nil {
			t.Fatal(e)
		}
		bytes += f.Size()
		if segmentName(v.Name()) {
			count++
		}
	}
	if bytes > s.options.MaxBytes || count > s.options.MaxSegments {
		t.Fatalf("unbounded: %d bytes, %d segments", bytes, count)
	}
}

func TestSpoolJournalRotationAndAcknowledgment(t *testing.T) {
	s := testSpool(t, SpoolOptions{MaxBytes: 10000, SegmentBytes: 650, MaxSegments: 20})
	j, e := OpenWithSpool(s, "test")
	if e != nil {
		t.Fatal(e)
	}
	for range 5 {
		if e = j.Observe(11, 7); e != nil {
			t.Fatal(e)
		}
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	segments, e := s.Segments()
	if e != nil || len(segments) < 2 {
		t.Fatalf("rotation: %+v %v", segments, e)
	}
	var tx, rx uint64
	seen := map[uint64]bool{}
	for _, v := range segments {
		data, e := os.ReadFile(filepath.Join(s.dir, v.Name))
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range records(t, data) {
			if seen[r.Sequence] || r.Complete {
				t.Fatal("duplicate sequence or complete claim")
			}
			seen[r.Sequence] = true
			tx += r.DeltaTxBytes
			rx += r.DeltaRxBytes
		}
		if e = s.Acknowledge(v.Name, strings.Repeat("0", 64)); e == nil {
			t.Fatal("accepted wrong digest")
		}
		if e = s.Acknowledge(v.Name, v.SHA256); e != nil {
			t.Fatal(e)
		}
	}
	if tx != 55 || rx != 35 || len(seen) != 7 {
		t.Fatal("lost or duplicated journal evidence")
	}
	if s.used != 1 || s.count != 0 {
		t.Fatal("retention accounting")
	}
	assertBound(t, s)
}

func TestSpoolBudgetStickyAndReclaim(t *testing.T) {
	for _, options := range []SpoolOptions{{MaxBytes: 1300, SegmentBytes: 600, MaxSegments: 20}, {MaxBytes: 10000, SegmentBytes: 600, MaxSegments: 2}} {
		s := testSpool(t, options)
		j, e := OpenWithSpool(s, "budget")
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 100; i++ {
			e = j.Observe(1, 1)
			if e != nil {
				break
			}
		}
		if !errors.Is(e, ErrSpoolBudget) {
			t.Fatalf("no backpressure: %v", e)
		}
		before := j.last
		if e = j.Observe(0, 0); !errors.Is(e, ErrSpoolBudget) || j.last != before {
			t.Fatal("failure not latched")
		}
		if e = j.Close(); !errors.Is(e, ErrSpoolBudget) {
			t.Fatal(e)
		}
		marker, e := os.ReadFile(filepath.Join(s.dir, ".incomplete"))
		if e != nil || string(marker) != "\x01" {
			t.Fatal("missing sticky exhaustion marker")
		}
		assertBound(t, s)
		segments, e := s.Segments()
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range segments {
			if e = s.Acknowledge(v.Name, v.SHA256); e != nil {
				t.Fatal(e)
			}
		}
		next, e := OpenWithSpool(s, "next")
		if e != nil {
			t.Fatal(e)
		}
		_ = next.Close()
		assertBound(t, s)
	}
}

func TestSpoolActiveProtectionAndSyncFailure(t *testing.T) {
	s := testSpool(t, SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10})
	j, e := OpenWithSpool(s, "fault")
	if e != nil {
		t.Fatal(e)
	}
	w := j.file.(*spoolWriter)
	if e = s.Acknowledge(w.name, strings.Repeat("0", 64)); e == nil {
		t.Fatal("deleted active writer")
	}
	if e = s.Close(); e == nil {
		t.Fatal("closed with active writer")
	}
	if other, e := OpenSpool(s.dir, s.options); e == nil {
		other.Close()
		t.Fatal("second owner accepted")
	}
	before := j.last
	_ = w.file.Close()
	if e = j.Observe(2, 3); e == nil || j.last != before {
		t.Fatal("I/O failure published")
	}
	if e = j.Close(); e == nil {
		t.Fatal("close hid failure")
	}
	assertBound(t, s)
}

func TestSpoolCrashRecovery(t *testing.T) {
	if directory := os.Getenv("NETWORK_SPOOL_CRASH_TEST"); directory != "" {
		s, e := OpenSpool(directory, SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10})
		if e != nil {
			os.Exit(2)
		}
		j, e := OpenWithSpool(s, "crash")
		if e != nil {
			os.Exit(3)
		}
		w := j.file.(*spoolWriter)
		// Simulate a process dying during its next append, before acknowledgment.
		_, _ = w.file.Write([]byte("{partial"))
		_ = w.file.Sync()
		os.Exit(0)
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpoolCrashRecovery$")
	cmd.Env = append(os.Environ(), "NETWORK_SPOOL_CRASH_TEST="+dir)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("crash helper: %v %s", e, out)
	}
	options := SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10}
	s, e := OpenSpool(dir, options)
	if e != nil {
		t.Fatal(e)
	}
	segments, e := s.Segments()
	if e != nil || len(segments) != 1 || !segments[0].Incomplete {
		t.Fatalf("recovery: %+v %v", segments, e)
	}
	v := segments[0]
	data, e := os.ReadFile(filepath.Join(dir, v.Name))
	if e != nil || !strings.HasSuffix(string(data), "{partial") {
		t.Fatal("partial evidence lost")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = OpenSpool(dir, options)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	again, e := s.Segments()
	if e != nil || len(again) != 1 || again[0] != v {
		t.Fatal("unstable replay identity")
	}
	if e = s.Acknowledge(v.Name, v.SHA256); e != nil {
		t.Fatal(e)
	}
	assertBound(t, s)
}

func TestSpoolRejectsCorruptControls(t *testing.T) {
	for _, data := range [][]byte{{}, {2}, {0, 0}} {
		dir := t.TempDir()
		if e := os.WriteFile(filepath.Join(dir, ".incomplete"), data, 0600); e != nil {
			t.Fatal(e)
		}
		if s, e := OpenSpool(dir, SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10}); e == nil {
			s.Close()
			t.Fatal("accepted corrupt marker")
		}
	}
	for _, name := range []string{".lock", ".incomplete"} {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "target")
		if e := os.WriteFile(target, []byte{0}, 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(target, filepath.Join(dir, name)); e != nil {
			t.Fatal(e)
		}
		if s, e := OpenSpool(dir, SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10}); e == nil {
			s.Close()
			t.Fatal("accepted symlink control")
		}
	}
}

func TestSpoolDeletionSyncFailureBlocksReuse(t *testing.T) {
	s := testSpool(t, SpoolOptions{MaxBytes: 10000, SegmentBytes: 1000, MaxSegments: 10})
	j, e := OpenWithSpool(s, "delete")
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	segments, e := s.Segments()
	if e != nil {
		t.Fatal(e)
	}
	v := segments[0]
	s.syncDirectory = func() error { return errors.New("injected directory sync failure") }
	if e = s.Acknowledge(v.Name, v.SHA256); e == nil {
		t.Fatal("ack hid durability failure")
	}
	if j, e := OpenWithSpool(s, "unsafe-reuse"); e == nil {
		j.Close()
		t.Fatal("reused uncertain deletion capacity")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenSpool(s.dir, s.options)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	assertBound(t, reopened)
	// A missing segment is explicitly ambiguous, never a fabricated durable ack.
	if e = reopened.Acknowledge(v.Name, v.SHA256); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("missing evidence must be explicit")
	}
}
