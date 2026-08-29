package telemetry

import (
	"context"
	"testing"
)

func TestNewWithoutCollectorUsesNoopClient(t *testing.T) {
	t.Parallel()

	previous := otelCollectorGRPCEndpoint
	otelCollectorGRPCEndpoint = ""
	t.Cleanup(func() { otelCollectorGRPCEndpoint = previous })

	client, err := New(context.Background(), "node", "service", "commit", "version", "instance")
	if err != nil {
		t.Fatalf("New returned an error without a collector endpoint: %v", err)
	}
	if client == nil {
		t.Fatal("New returned a nil client")
	}
	if _, ok := client.MetricExporter.(*noopMetricExporter); !ok {
		t.Fatalf("expected noop metric exporter, got %T", client.MetricExporter)
	}
	if _, ok := client.LogsProvider.(noopLogProvider); !ok {
		t.Fatalf("expected noop log provider, got %T", client.LogsProvider)
	}
}
