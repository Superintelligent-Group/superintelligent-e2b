package terminalidle

import (
	"errors"
	"testing"
	"time"
)

func TestTrackerMeasuresPositiveIdleAfterActivity(t *testing.T) {
	start := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	tracker := New(start)
	if err := tracker.Observe(start.Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	measurement := tracker.Close(start.Add(8 * time.Second))
	if measurement.Status != "measured" || measurement.Seconds != 5 {
		t.Fatalf("measurement = %#v, want measured 5 seconds", measurement)
	}
}

func TestTrackerPreservesMeasuredZero(t *testing.T) {
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	measurement := New(at).Close(at)
	if measurement.Status != "measured" || measurement.Seconds != 0 {
		t.Fatalf("measurement = %#v, want measured zero", measurement)
	}
}

func TestTrackerRejectsOutOfOrderActivityAndClose(t *testing.T) {
	start := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	tracker := New(start)
	if err := tracker.Observe(start.Add(-time.Second)); !errors.Is(err, ErrActivityOutOfOrder) {
		t.Fatalf("Observe error = %v, want ErrActivityOutOfOrder", err)
	}
	measurement := tracker.Close(start.Add(-time.Second))
	if measurement.Status != "unavailable" || measurement.Reason != "provider_lifecycle_timestamp_out_of_order" {
		t.Fatalf("measurement = %#v, want unavailable ordering result", measurement)
	}
}

func TestTrackerUnavailableWithoutStartAndAfterTerminal(t *testing.T) {
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	measurement := New(time.Time{}).Close(at)
	if measurement.Status != "unavailable" {
		t.Fatalf("measurement = %#v, want unavailable", measurement)
	}
	if err := New(at).Observe(at); err != nil {
		t.Fatal(err)
	}
	tracker := New(at)
	_ = tracker.Close(at)
	if err := tracker.Observe(at); !errors.Is(err, ErrActivityAfterTerminal) {
		t.Fatalf("Observe after Close error = %v, want ErrActivityAfterTerminal", err)
	}
}

func TestMeasurementAsMapKeepsUnavailableExplicit(t *testing.T) {
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	value := (Measurement{Status: "unavailable", Reason: "missing", Provenance: provenance, ObservedAt: at}).AsMap()
	if _, ok := value["seconds"]; ok {
		t.Fatal("unavailable measurement must not contain numeric seconds")
	}
	if value["reason"] != "missing" || value["provenance"] != provenance {
		t.Fatalf("value = %#v, missing unavailable evidence", value)
	}
}
