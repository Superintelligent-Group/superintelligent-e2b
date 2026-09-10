//go:build linux

package fc

import (
	"context"
	"errors"
	"fmt"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
)

// CalibrationSample uses the production sample path while retaining the gate
// through the fence copy, so a periodic sample cannot replace its identity.
// This symbol exists only in the fc test binary.
func CalibrationSample(p *Process, ctx context.Context) (networkusage.DurableFence, error) {
	s := p.measurementSession()
	if s == nil {
		return networkusage.DurableFence{}, errors.New("calibration requires owned measurement session")
	}
	ctx, cancel := context.WithTimeout(ctx, measurementRequestTimeout)
	defer cancel()
	if err := s.lock(ctx); err != nil {
		return networkusage.DurableFence{}, err
	}
	defer s.unlock()
	if err := s.sampleLocked(ctx, false); err != nil {
		return networkusage.DurableFence{}, err
	}
	fence, err := s.collector.Fence()
	if err != nil {
		return networkusage.DurableFence{}, err
	}
	if fence == nil {
		return networkusage.DurableFence{}, errors.New("sample has no durable fence")
	}
	if fence.Scope != networkusage.SampleScope || fence.Request.ID != fmt.Sprintf("sample_%d", s.sequence) || fence.Header.RequestID == nil || *fence.Header.RequestID != fence.Request.ID || fence.Header.RequestSequence == nil || *fence.Header.RequestSequence != fence.Request.Sequence {
		return networkusage.DurableFence{}, errors.New("sample fence identity mismatch")
	}
	return *fence, nil
}

// CalibrationTerminal calls the actual production deadline/ending/action path.
// Only its immutable, matched terminal reference is exposed to the test caller.
func CalibrationTerminal(p *Process, ctx context.Context) (networkusage.DurableFence, error) {
	if err := p.FinalizeMeasurement(ctx); err != nil {
		return networkusage.DurableFence{}, err
	}
	s := p.measurementSession()
	if s == nil {
		return networkusage.DurableFence{}, errors.New("terminal requires owned measurement session")
	}
	ctx, cancel := context.WithTimeout(ctx, measurementRequestTimeout)
	defer cancel()
	if err := s.lock(ctx); err != nil {
		return networkusage.DurableFence{}, err
	}
	defer s.unlock()
	if err := s.Err(); err != nil {
		return networkusage.DurableFence{}, err
	}
	fence, err := s.collector.Fence()
	if err != nil {
		return networkusage.DurableFence{}, err
	}
	if !s.ending.Load() || !s.terminal.Load() || fence == nil || fence.Scope != networkusage.TerminalScope || fence.Request.ID != "terminal" || fence.Header.RequestID == nil || *fence.Header.RequestID != fence.Request.ID || fence.Header.RequestSequence == nil || *fence.Header.RequestSequence != fence.Request.Sequence {
		return networkusage.DurableFence{}, errors.New("terminal fence identity mismatch")
	}
	return *fence, nil
}
