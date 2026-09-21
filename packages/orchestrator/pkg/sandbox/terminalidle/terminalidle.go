// Package terminalidle records provider-owned lifecycle activity for one
// sandbox execution and derives the final idle interval at teardown.
package terminalidle

import (
	"errors"
	"time"
)

const provenance = "e2b.provider.lifecycle"

var (
	ErrActivityAfterTerminal = errors.New("activity recorded after terminal close")
	ErrActivityOutOfOrder    = errors.New("activity timestamp precedes the previous activity")
)

// Measurement is the provider-owned terminal idle result. A result is either
// measured or explicitly unavailable; consumers must not coerce unavailable
// results to zero.
type Measurement struct {
	Status     string
	Seconds    uint64
	Reason     string
	Provenance string
	ObservedAt time.Time
}

// AsMap returns the JSON-native terminal receipt representation.
func (m Measurement) AsMap() map[string]any {
	result := map[string]any{
		"status":      m.Status,
		"provenance":  m.Provenance,
		"observed_at": m.ObservedAt.UTC().Format(time.RFC3339Nano),
	}
	if m.Status == "measured" {
		result["seconds"] = m.Seconds
	} else {
		result["reason"] = m.Reason
	}

	return result
}

// Tracker is the single-owner activity state for one provider execution.
// Callers serialize Observe and Close through the sandbox lifecycle lock.
type Tracker struct {
	lastActivity time.Time
	closed       bool
}

// New starts a tracker at the provider's execution-start boundary. A zero
// timestamp intentionally produces an unavailable terminal result.
func New(startedAt time.Time) *Tracker {
	return &Tracker{lastActivity: startedAt}
}

// Observe records a provider lifecycle activity transition.
func (t *Tracker) Observe(at time.Time) error {
	if t == nil || t.closed {
		return ErrActivityAfterTerminal
	}
	if t.lastActivity.IsZero() || at.Before(t.lastActivity) {
		return ErrActivityOutOfOrder
	}
	t.lastActivity = at

	return nil
}

// Close derives the idle interval since the last provider activity.
func (t *Tracker) Close(at time.Time) Measurement {
	result := Measurement{
		Status:     "unavailable",
		Reason:     "provider_lifecycle_activity_unavailable",
		Provenance: provenance,
		ObservedAt: at,
	}
	if t == nil {
		return result
	}
	if t.closed {
		result.Reason = "provider_lifecycle_already_terminal"
		return result
	}
	t.closed = true
	if t.lastActivity.IsZero() || at.Before(t.lastActivity) {
		result.Reason = "provider_lifecycle_timestamp_out_of_order"
		return result
	}

	result.Status = "measured"
	result.Seconds = uint64(at.Sub(t.lastActivity) / time.Second)
	return result
}
