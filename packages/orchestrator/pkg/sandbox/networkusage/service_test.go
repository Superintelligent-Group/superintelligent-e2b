package networkusage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func serviceOptions() SpoolOptions {
	return SpoolOptions{MaxBytes: 2 * 1024 * 1024, SegmentBytes: 128 * 1024, MaxSegments: 64}
}
func openTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	s, e := OpenService(dir, serviceOptions())
	if e != nil {
		t.Fatal(e)
	}
	return s, dir
}

func TestServiceConcurrentCollectorsFreshIdentityAndDrain(t *testing.T) {
	s, dir := openTestService(t)
	var wg sync.WaitGroup
	collectors := make(chan *Correlator, 20)
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			c, e := s.NewCollector("sandbox")
			if e != nil {
				t.Error(e)
				return
			}
			collectors <- c
		})
	}
	wg.Wait()
	close(collectors)
	seen := map[string]bool{}
	var all []*Correlator
	for c := range collectors {
		if !tokenOK(c.Incarnation()) || seen[c.Incarnation()] {
			t.Fatal("nonfresh identity")
		}
		seen[c.Incarnation()] = true
		all = append(all, c)
	}
	result := make(chan error, 1)
	go func() { result <- s.Close(context.Background()) }()
	<-s.Closing()
	if _, e := s.NewCollector("late"); !errors.Is(e, ErrServiceClosing) {
		t.Fatal(e)
	}
	select {
	case <-result:
		t.Fatal("closed with active collectors")
	default:
	}
	for _, c := range all {
		wg.Go(func() {
			if e := c.Close(); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	// The OS lock is available only after all collector seals completed.
	reopened, e := OpenService(dir, serviceOptions())
	if e != nil {
		t.Fatal(e)
	}
	if e = reopened.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
}

func TestServiceRealSpoolBudgetBroadcastStopsAdmission(t *testing.T) {
	options := serviceOptions()
	options.SegmentBytes = 1200
	s, e := OpenService(t.TempDir(), options)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.NewCollector("a")
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.NewCollector("b")
	if e != nil {
		t.Fatal(e)
	}
	mustBegin(t, a, "baseline", BaselineScope)
	mustBegin(t, b, "baseline", BaselineScope)
	frames, _ := producerFixtures(t)
	raw := bytes.ReplaceAll(frames[0], []byte("sup909_local_boot"), []byte(a.Incarnation()))
	if e = a.ObserveFrame(raw); !errors.Is(e, ErrSpoolBudget) {
		t.Fatal(e)
	}
	select {
	case <-s.Failed():
	default:
		t.Fatal("no shared failure broadcast")
	}
	if !errors.Is(s.Err(), ErrSpoolBudget) {
		t.Fatal(s.Err())
	}
	if _, e = s.NewCollector("late"); e == nil {
		t.Fatal("admission after exhaustion")
	}
	raw = bytes.ReplaceAll(frames[0], []byte("sup909_local_boot"), []byte(b.Incarnation()))
	if e = b.ObserveFrame(raw); e == nil {
		t.Fatal("active collector acknowledged after host failure")
	}
	if e = a.Close(); e == nil {
		t.Fatal("failed collector close succeeded")
	}
	if e = b.Close(); e == nil {
		t.Fatal("affected collector close succeeded")
	}
	if e = s.Close(context.Background()); e == nil {
		t.Fatal("failed service close succeeded")
	}
}

type serviceFaultFile struct {
	durableFile
	writeErr, syncErr, closeErr error
	closeEntered, closeRelease  chan struct{}
}

func (f *serviceFaultFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.durableFile.Write(p)
}
func (f *serviceFaultFile) Sync() error { return errors.Join(f.durableFile.Sync(), f.syncErr) }
func (f *serviceFaultFile) Close() error {
	if f.closeEntered != nil {
		close(f.closeEntered)
		<-f.closeRelease
	}
	return errors.Join(f.durableFile.Close(), f.closeErr)
}

func TestServiceCloseCancellationRetainsLockUntilSeal(t *testing.T) {
	s, dir := openTestService(t)
	c, e := s.NewCollector("blocking")
	if e != nil {
		t.Fatal(e)
	}
	wrapper := c.journal.file.(*serviceFile)
	blocked := &serviceFaultFile{durableFile: wrapper.durableFile, closeEntered: make(chan struct{}), closeRelease: make(chan struct{})}
	wrapper.durableFile = blocked
	collectorClosed := make(chan error, 1)
	go func() { collectorClosed <- c.Close() }()
	<-blocked.closeEntered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = s.Close(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e = OpenService(dir, serviceOptions()); e == nil {
		t.Fatal("unsafe reopen during blocked writer close")
	}
	close(blocked.closeRelease)
	if e = <-collectorClosed; e != nil {
		t.Fatal(e)
	}
	if e = c.Close(); e != nil {
		t.Fatal("idempotent close", e)
	}
	if e = s.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenService(dir, serviceOptions())
	if e != nil {
		t.Fatal(e)
	}
	reopened.Close(context.Background())
}

func TestServicePersistenceFailuresBroadcastAndReleaseExactlyOnce(t *testing.T) {
	for _, kind := range []string{"write", "sync", "close"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := openTestService(t)
			c, e := s.NewCollector("fault")
			if e != nil {
				t.Fatal(e)
			}
			wrapper := c.journal.file.(*serviceFile)
			fault := &serviceFaultFile{durableFile: wrapper.durableFile}
			boom := errors.New("injected " + kind)
			switch kind {
			case "write":
				fault.writeErr = boom
			case "sync":
				fault.syncErr = boom
			case "close":
				fault.closeErr = boom
			}
			wrapper.durableFile = fault
			if kind != "close" {
				if _, e = c.Begin("baseline", BaselineScope); e == nil {
					t.Fatal("persistence failure acknowledged")
				}
			}
			if e = c.Close(); e == nil {
				t.Fatal("failed close returned success")
			}
			if e = c.Close(); e == nil {
				t.Fatal("repeat close erased failure")
			}
			select {
			case <-s.Failed():
			default:
				t.Fatal("missing failure")
			}
			if s.active != 0 {
				t.Fatal("collector not released after failed seal", s.active)
			}
			if e = s.Close(context.Background()); e == nil {
				t.Fatal("service erased failure")
			}
		})
	}
}

func TestServiceRecoveryPrecedesAdmissionAndPreservesTail(t *testing.T) {
	dir := t.TempDir()
	name := "0123456789abcdef0123456789abcdef.active"
	tail := []byte("{\"partial\":")
	if e := os.WriteFile(filepath.Join(dir, name), tail, 0600); e != nil {
		t.Fatal(e)
	}
	s, e := OpenService(dir, serviceOptions())
	if e != nil {
		t.Fatal(e)
	}
	preserved, e := os.ReadFile(filepath.Join(dir, "0123456789abcdef0123456789abcdef.incomplete"))
	if e != nil || !bytes.Equal(preserved, tail) {
		t.Fatal("recovery lost tail", e)
	}
	marker, e := os.ReadFile(filepath.Join(dir, ".incomplete"))
	if e != nil || !bytes.Equal(marker, []byte{1}) {
		t.Fatal(marker, e)
	}
	c, e := s.NewCollector("fresh")
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Close(); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	preserved, e = os.ReadFile(filepath.Join(dir, "0123456789abcdef0123456789abcdef.incomplete"))
	if e != nil || !bytes.Equal(preserved, tail) {
		t.Fatal("cleanup reclaimed evidence", e)
	}
}

func TestServiceInvalidConstructionAndFailureAreBounded(t *testing.T) {
	if _, e := OpenService("", serviceOptions()); e == nil {
		t.Fatal("disabled service accepted")
	}
	s, _ := openTestService(t)
	if _, e := s.NewCollector(""); e == nil {
		t.Fatal("empty sandbox")
	}
	if s.Err() != nil {
		t.Fatal("caller typo poisoned host")
	}
	first := errors.New("first failure")
	s.Fail(first)
	s.Fail(errors.New("later"))
	s.Fail(nil)
	if !errors.Is(s.Err(), first) {
		t.Fatal("first cause lost")
	}
	if _, e := s.NewCollector("no"); e == nil {
		t.Fatal("failed admission")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := s.Close(ctx); !errors.Is(e, first) {
		t.Fatal(e)
	}
}
