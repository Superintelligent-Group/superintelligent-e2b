//go:build linux

package fc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

const metricsJoinTimeout = 5 * time.Second

var errMetricsPartialTail = errors.New("metrics FIFO ended without a newline")

type metricsReaderPhase uint8

const (
	metricsReading metricsReaderPhase = iota
	metricsSealing
	metricsCompleted
)

type metricsJournal interface {
	Gap(string) error
	Close() error
}

// metricsReader owns both FIFO descriptors and all workers. A successful Join
// means the reader and flusher ended and journal.Close completed, not coverage.
type metricsReader struct {
	reader, keeper  *os.File
	journal         metricsJournal
	cancel          context.CancelFunc
	flushDone, done chan struct{}
	drainOnce       sync.Once
	mu              sync.Mutex
	err             error
	forcedReason    string
	phase           metricsReaderPhase
}

func openMetricsReader(ctx context.Context, path string, journal metricsJournal, consume func([]byte) error, flush func(context.Context) error, interval time.Duration, exit <-chan struct{}) (*metricsReader, error) {
	// Nonblocking opens avoid waiting on an absent producer. O_NOFOLLOW and
	// fstat ensure a replaced path cannot silently become a regular file.
	open := func(flags int) (*os.File, error) {
		fd, err := syscall.Open(path, flags|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(fd), path)
		info, err := file.Stat()
		if err == nil && info.Mode()&os.ModeNamedPipe == 0 {
			err = errors.New("metrics path is not a FIFO")
		}
		if err != nil {
			return nil, errors.Join(err, file.Close())
		}
		return file, nil
	}
	keeper, err := open(syscall.O_RDWR)
	if err != nil {
		return nil, errors.Join(err, journal.Gap("fifo_open_failed"), journal.Close())
	}
	reader, err := open(syscall.O_RDONLY)
	if err != nil {
		return nil, errors.Join(err, keeper.Close(), journal.Gap("fifo_read_open_failed"), journal.Close())
	}
	keeperInfo, keeperErr := keeper.Stat()
	readerInfo, readerErr := reader.Stat()
	if keeperErr != nil || readerErr != nil || !os.SameFile(keeperInfo, readerInfo) {
		return nil, errors.Join(errors.New("metrics FIFO changed during open"), keeperErr, readerErr, reader.Close(), keeper.Close(), journal.Gap("fifo_open_failed"), journal.Close())
	}
	lifetime, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &metricsReader{reader: reader, keeper: keeper, journal: journal, cancel: cancel, flushDone: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.flushDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-lifetime.Done():
				return
			case <-ticker.C:
				if lifetime.Err() != nil {
					return
				}
				requestCtx, requestCancel := context.WithTimeout(lifetime, interval)
				err := flush(requestCtx)
				requestCancel()
				if err != nil {
					// Cancellation while stopping is intentional, not a new gap.
					if lifetime.Err() != nil {
						return
					}
					s.addError(errors.Join(fmt.Errorf("flush metrics: %w", err), journal.Gap("flush_request_failed")))
				}
			}
		}
	}()
	go func() {
		defer func() {
			s.mu.Lock()
			s.phase = metricsCompleted
			close(s.done)
			s.mu.Unlock()
		}()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, metricsReaderBufSize), metricsReaderBufSize)
		scanner.Split(metricsLines)
		for scanner.Scan() {
			s.addError(consume(scanner.Bytes()))
		}
		if err := scanner.Err(); err != nil {
			reason := "metrics_reader_failed"
			if errors.Is(err, errMetricsPartialTail) {
				reason = "partial_metrics_frame"
			}
			s.addError(errors.Join(err, journal.Gap(reason)))
		}
		s.cancel()
		<-s.flushDone // no writer can race journal.Close
		s.mu.Lock()
		s.phase = metricsSealing
		reason := s.forcedReason
		s.mu.Unlock()
		if reason != "" {
			s.addError(journal.Gap(reason))
		}
		s.beginDrain()
		if err := reader.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			s.addError(err)
		}
		s.addError(journal.Close())
	}()
	go func() {
		select {
		case <-exit:
			s.beginDrain()
		case <-s.done:
		}
	}()
	return s, nil
}

func metricsLines(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) != 0 {
		return 0, nil, errMetricsPartialTail
	}
	return 0, nil, nil
}

func (s *metricsReader) addError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Bound error storage even if malformed frames continue arriving.
	if s.err == nil {
		s.err = err
	}
}

func (s *metricsReader) beginDrain() {
	s.cancel()
	s.drainOnce.Do(func() { s.addError(s.keeper.Close()) })
}

// Abort does not depend on an Exit notification, including pre-start failures.
func (s *metricsReader) Abort(ctx context.Context) error {
	s.force("metrics_startup_aborted", errors.New("metrics startup aborted"))
	return s.Join(ctx)
}

func (s *metricsReader) force(reason string, err error) (completed bool, result error) {
	s.mu.Lock()
	if s.phase == metricsCompleted {
		result = s.err
		s.mu.Unlock()
		return true, result
	}
	if s.phase == metricsSealing {
		// Input is drained. Interrupt only this caller's wait: filesystem
		// Close cannot be canceled or retroactively accept a new gap.
		s.mu.Unlock()
		return false, nil
	}
	if s.forcedReason == "" {
		s.forcedReason = reason
	}
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
	s.beginDrain()
	_ = s.reader.Close() // wake pollable FIFO reads even if a writer survives
	return false, nil
}

func (s *metricsReader) Join(ctx context.Context) error {
	s.beginDrain()
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.err
	default:
	}
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.err
	case <-ctx.Done():
		if completed, result := s.force("metrics_join_interrupted", ctx.Err()); completed {
			return result
		}
		return fmt.Errorf("metrics persistence join incomplete: %w", ctx.Err())
	}
}

// JoinMetrics is called after terminating the process and sandbox cgroup.
// A nil result guarantees journal closure and no remaining worker. An interrupted
// result is explicitly NOT a completed persistence join: forced FIFO close wakes
// the reader, which still owns its final gap/Close if filesystem IO is stalled.
func (p *Process) JoinMetrics(ctx context.Context) error {
	p.metricsMu.Lock()
	session := p.metrics
	p.metricsMu.Unlock()
	if session == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, metricsJoinTimeout)
	defer cancel()
	return session.Join(ctx)
}

func (p *Process) abortMetricsReader() error {
	p.metricsMu.Lock()
	p.metricsStopped = true
	session := p.metrics
	p.metricsMu.Unlock()
	if session == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), metricsJoinTimeout)
	defer cancel()
	return session.Abort(ctx)
}
