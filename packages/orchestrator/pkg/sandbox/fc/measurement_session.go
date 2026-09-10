//go:build linux

package fc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
)

const measurementRequestTimeout = 10 * time.Second

type measurementSession struct {
	collector *networkusage.Correlator
	service   *networkusage.Service
	wire      *measurementClient
	gate      chan struct{}
	changed   chan struct{}
	frameMu   sync.Mutex // a matched balloon fence includes its telemetry consumption
	failureMu sync.Mutex
	failure   error
	failed    chan struct{}
	active    atomic.Bool
	ending    atomic.Bool
	terminal  atomic.Bool
	sequence  uint64 // action gate owned
}

func newMeasurementSession(service *networkusage.Service, sandboxID, socket string) (*measurementSession, error) {
	if service == nil {
		return nil, errors.New("correlated measurement requires host spool service")
	}
	c, err := service.NewCollector(sandboxID)
	if err != nil {
		return nil, err
	}
	s := &measurementSession{collector: c, service: service, wire: newMeasurementClient(socket), gate: make(chan struct{}, 1), changed: make(chan struct{}, 1), failed: make(chan struct{})}
	// Intent precedes configuring the producer, including its own periodic writer.
	if _, err = c.Begin("baseline", networkusage.BaselineScope); err != nil {
		return nil, errors.Join(err, c.Close())
	}
	return s, nil
}
func (s *measurementSession) fail(err error) error {
	if err != nil {
		s.failureMu.Lock()
		if s.failure == nil {
			s.failure = err
			close(s.failed)
		}
		s.failureMu.Unlock()
	}
	return err
}

// Protocol, transport and caller failures belong to this incarnation. Actual
// spool IO failures are broadcast separately by the service's file wrapper.
func (s *measurementSession) Err() error {
	s.failureMu.Lock()
	err := s.failure
	s.failureMu.Unlock()
	return errors.Join(err, s.service.Err())
}
func (s *measurementSession) lock(ctx context.Context) error {
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.service.Failed():
		return s.service.Err()
	case <-s.failed:
		return s.Err()
	}
	if err := errors.Join(ctx.Err(), s.Err()); err != nil {
		<-s.gate
		return err
	}
	return nil
}
func (s *measurementSession) unlock() { <-s.gate }
func (s *measurementSession) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}
func (s *measurementSession) observe(raw []byte, consume func([]byte) error) error {
	s.frameMu.Lock()
	defer s.frameMu.Unlock()
	if err := s.collector.ObserveFrame(raw); err != nil {
		return s.fail(err)
	}
	frame, err := networkusage.DecodeProducerFrame(raw)
	if err == nil {
		err = consume(frame.Metrics)
	}
	s.notify()
	return s.fail(err)
}
func (s *measurementSession) Gap(reason string) error {
	return s.fail(errors.Join(fmt.Errorf("measurement evidence incomplete: %s", reason), s.collector.Gap(reason)))
}
func (s *measurementSession) Close() error {
	var err error
	if !s.terminal.Load() {
		err = s.Gap("terminal_fence_missing")
	}
	return s.fail(errors.Join(err, s.collector.Close()))
}

// request holds the action gate. Transport ambiguity reuses the exact tuple;
// a successful HTTP response alone never authorizes activity or local sealing.
func (s *measurementSession) request(ctx context.Context, id string, scope networkusage.RequestScope) error {
	request, err := s.collector.Begin(id, scope)
	if err != nil {
		return s.fail(err)
	}
	var raw []byte
	for attempt := 0; attempt < 2; attempt++ {
		raw, err = s.wire.request(ctx, request, scope == networkusage.TerminalScope)
		if err == nil {
			break
		}
		var status *measurementHTTPError
		if errors.As(err, &status) || ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return s.fail(errors.Join(err, s.collector.Gap("measurement_response_unavailable")))
	}
	if err = s.collector.ObserveReceipt(raw); err != nil {
		return s.fail(err)
	}
	for {
		if err = s.Err(); err != nil {
			return err
		}
		s.frameMu.Lock()
		fence, fenceErr := s.collector.Fence()
		s.frameMu.Unlock()
		if fenceErr != nil {
			return s.fail(fenceErr)
		}
		if fence != nil && fence.Request == request {
			if scope == networkusage.TerminalScope {
				s.terminal.Store(true)
			}
			return s.Err()
		}
		select {
		case <-s.changed:
		case <-s.service.Failed():
			return s.service.Err()
		case <-s.failed:
			return s.Err()
		case <-ctx.Done():
			return s.fail(errors.Join(ctx.Err(), s.collector.Gap("measurement_fence_timeout")))
		}
	}
}
func (s *measurementSession) configure(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, measurementRequestTimeout)
	defer cancel()
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	if err := s.wire.configure(ctx, path, s.collector.Incarnation()); err != nil {
		return s.fail(err)
	}
	return s.request(ctx, "baseline", networkusage.BaselineScope)
}
func (s *measurementSession) startActivity(ctx context.Context, start func() error) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	if s.ending.Load() {
		return errors.New("measurement process stopping")
	}
	if err := s.collector.BeginActivity(); err != nil {
		return s.fail(err)
	}
	s.active.Store(true) // intent covers even an ambiguous failed start/load response
	if err := s.Err(); err != nil {
		return err
	}
	if err := start(); err != nil {
		return s.fail(errors.Join(err, s.collector.Gap("activity_start_failed")))
	}
	return s.Err()
}
func (s *measurementSession) sample(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, measurementRequestTimeout)
	defer cancel()
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	return s.sampleLocked(ctx, false)
}

// A periodic tick must not time out behind a slow snapshot load or another
// measurement request. Explicit freshness requests still wait for their fence.
func (s *measurementSession) periodicSample(ctx context.Context) error {
	if !s.active.Load() || s.ending.Load() {
		return nil
	}
	select {
	case s.gate <- struct{}{}:
		defer s.unlock()
	default:
		return nil
	}
	if s.ending.Load() {
		return nil
	}
	return s.sampleLocked(ctx, true)
}
func (s *measurementSession) sampleLocked(ctx context.Context, periodic bool) error {
	if err := errors.Join(ctx.Err(), s.Err()); err != nil {
		return err
	}
	if !s.active.Load() || s.ending.Load() {
		if periodic {
			return nil
		}
		return errors.New("fresh measurement unavailable before activity or during shutdown")
	}
	s.sequence++
	return s.request(ctx, fmt.Sprintf("sample_%d", s.sequence), networkusage.SampleScope)
}
func (s *measurementSession) finalize(ctx context.Context) error {
	s.ending.Store(true)
	ctx, cancel := context.WithTimeout(ctx, measurementRequestTimeout)
	defer cancel()
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	if s.terminal.Load() {
		return s.Err()
	}
	if !s.active.Load() {
		return s.Gap("activity_not_started")
	}
	return s.request(ctx, "terminal", networkusage.TerminalScope)
}

// SetNetworkUsageService is an initialization-only dependency setter, before Create/Resume.
func (p *Process) SetNetworkUsageService(service *networkusage.Service) {
	p.measurementService = service
}
func (p *Process) FinalizeMeasurement(ctx context.Context) error {
	measurement := p.measurementSession()
	if measurement == nil {
		return nil
	}
	return measurement.finalize(ctx)
}
func (p *Process) configureMetrics(ctx context.Context) error {
	if measurement := p.measurementSession(); measurement != nil {
		return measurement.configure(ctx, p.metricsPath)
	}
	return p.client.setMetrics(ctx, p.metricsPath)
}
func (p *Process) startMeasuredActivity(ctx context.Context, start func() error) error {
	if measurement := p.measurementSession(); measurement != nil {
		return measurement.startActivity(ctx, start)
	}
	return start()
}
func (p *Process) measurementSession() *measurementSession {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	return p.measurement
}
func (p *Process) MeasurementFailed() <-chan struct{} {
	if measurement := p.measurementSession(); measurement != nil {
		return measurement.failed
	}
	return nil
}
func (p *Process) MeasurementError() error {
	if measurement := p.measurementSession(); measurement != nil {
		return measurement.Err()
	}
	return nil
}

// Start only after configure has published the child process. Do not join the
// reader here: it can be the producer of the failure notification.
func (p *Process) superviseMeasurement(ctx context.Context, measurement *measurementSession) {
	if p.cmd == nil || p.cmd.Process == nil {
		return // reader-only callers have no child to terminate
	}
	go func() {
		select {
		case <-measurement.failed:
		case <-measurement.service.Failed():
		case <-p.Exit.Done():
			return
		}
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), measurementRequestTimeout)
		defer cancel()
		_ = p.Stop(stopCtx)
	}()
}
func (p *Process) verifyMeasurementBinary() error {
	if !p.config.NetworkUsageCorrelated {
		return nil
	}
	if p.measurementService == nil {
		return errors.New("correlated measurement has no host service")
	}
	if err := p.measurementService.Err(); err != nil {
		return err
	}
	if len(p.config.NetworkUsageBinarySHA256) != 64 {
		return errors.New("missing measurement binary SHA256")
	}
	f, err := os.Open(p.binaryPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("measurement binary is not regular")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != p.config.NetworkUsageBinarySHA256 {
		return errors.New("selected Firecracker binary does not match measurement SHA256")
	}
	return nil
}
