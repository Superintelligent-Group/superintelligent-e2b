package telemetry

import (
	"context"
	"testing"
)

func TestNewMeterExporterWithoutCollectorUsesNoop(t *testing.T) {
	t.Parallel()

	if otelCollectorGRPCEndpoint != "" {
		t.Skip("collector endpoint configured in test environment")
	}

	exporter, err := NewMeterExporter(context.Background())
	if err != nil {
		t.Fatalf("NewMeterExporter() error = %v", err)
	}
	if _, ok := exporter.(noopMetricExporter); !ok {
		t.Fatalf("NewMeterExporter() type = %T, want noopMetricExporter", exporter)
	}
}
