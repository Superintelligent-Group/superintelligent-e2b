package networkusage

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"unicode/utf8"
)

var ErrServiceClosing = errors.New("network evidence service is closing")
var ErrCollectorLimit = errors.New("network evidence active collector limit reached")

// Service owns one host spool and its admission lifetime. Remote delivery and
// reclaim are intentionally not exposed here. All collectors must close before
// the spool lock is released; a canceled Close keeps ownership and rejects admission.
type Service struct {
	mu                  sync.Mutex
	spool               *Spool
	maxCollectors       int
	active              int
	failed              error
	failedCh, closingCh chan struct{}
	drained             chan struct{}
	closing, closed     bool
	closeErr            error
}

// OpenService completes existing spool recovery and obtains its OS lock before
// admission. Configuration chooses whether to enable it; an empty path is an error.
func OpenService(directory string, options SpoolOptions) (*Service, error) {
	spool, err := OpenSpool(directory, options)
	if err != nil {
		return nil, err
	}
	drained := make(chan struct{})
	close(drained)
	return &Service{spool: spool, maxCollectors: options.MaxSegments, failedCh: make(chan struct{}), closingCh: make(chan struct{}), drained: drained}, nil
}

// NewCollector reserves a bounded lifetime before doing persistence I/O. It
// never returns a collector after admission has closed or failed concurrently.
func (s *Service) NewCollector(sandboxID string) (*Correlator, error) {
	if sandboxID == "" || len(sandboxID) > 256 || !utf8.ValidString(sandboxID) {
		return nil, errors.New("invalid sandbox identity")
	}
	s.mu.Lock()
	if s.failed != nil {
		err := s.failed
		s.mu.Unlock()
		return nil, err
	}
	if s.closing {
		s.mu.Unlock()
		return nil, ErrServiceClosing
	}
	if s.active >= s.maxCollectors {
		s.mu.Unlock()
		return nil, ErrCollectorLimit
	}
	if s.active == 0 {
		s.drained = make(chan struct{})
	}
	s.active++
	s.mu.Unlock()
	journal, err := OpenWithSpool(s.spool, sandboxID)
	if err != nil {
		s.Fail(err)
		s.release()
		return nil, err
	}
	// Correlator.Close -> Journal.Close -> wrapper.Close seals the writer first.
	// Failure broadcasts only notify; they never join a collector from its callback.
	journal.file = &serviceFile{durableFile: journal.file, service: s}
	collector, err := NewCorrelator(journal, rand.Text())
	if err != nil {
		s.Fail(err)
		return nil, errors.Join(err, journal.Close())
	}
	s.mu.Lock()
	closed := s.closing
	failure := s.failed
	s.mu.Unlock()
	if failure != nil || closed {
		if failure == nil {
			failure = ErrServiceClosing
		}
		return nil, errors.Join(failure, collector.Close())
	}
	return collector, nil
}

// Incarnation is immutable and safe to pass directly into metrics configuration.
func (c *Correlator) Incarnation() string { return c.incarnation }

// Failed is closed exactly once on the first host persistence/failure report.
// Supervisors must stop work asynchronously, rather than joining from reader callbacks.
func (s *Service) Failed() <-chan struct{} { return s.failedCh }

// Closing is closed as soon as host shutdown stops admission.
func (s *Service) Closing() <-chan struct{} { return s.closingCh }

// Err returns the first shared failure; ordinary shutdown has a separate observable.
func (s *Service) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.failed }

// Fail irreversibly closes admission and broadcasts without calling collectors.
func (s *Service) Fail(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = fmt.Errorf("host network evidence failure: %w", err)
		close(s.failedCh)
	}
}
func (s *Service) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	if s.active == 0 {
		close(s.drained)
	}
}

// Close waits for all in-flight construction and collector Close calls. A context
// error leaves the spool locked; retry Close after the remaining lifetimes drain.
// It never discards collectors or acknowledges/reclaims their evidence.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closing {
		s.closing = true
		close(s.closingCh)
	}
	drained := s.drained
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	select {
	case <-drained:
	case <-ctx.Done():
		return errors.Join(ctx.Err(), s.Err())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	// No writer can be constructed after closing; active releases only after seal.
	s.closeErr = errors.Join(s.failed, s.spool.Close())
	s.closed = true
	return s.closeErr
}

type serviceFile struct {
	durableFile
	service   *Service
	closeOnce sync.Once
	closeErr  error
}

func (f *serviceFile) Write(p []byte) (int, error) {
	if err := f.service.Err(); err != nil {
		return 0, err
	}
	n, err := f.durableFile.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		f.service.Fail(err)
	}
	return n, err
}
func (f *serviceFile) Sync() error {
	err := f.durableFile.Sync()
	if err != nil {
		f.service.Fail(err)
	}
	return errors.Join(err, f.service.Err())
}
func (f *serviceFile) Close() error {
	f.closeOnce.Do(func() {
		f.closeErr = f.durableFile.Close()
		if f.closeErr != nil {
			f.service.Fail(f.closeErr)
		}
		f.closeErr = errors.Join(f.closeErr, f.service.Err())
		f.service.release()
	})
	return f.closeErr
}
