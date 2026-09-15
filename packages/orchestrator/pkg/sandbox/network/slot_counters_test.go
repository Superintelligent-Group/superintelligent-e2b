//go:build linux

package network

import (
	"testing"

	"github.com/coreos/go-iptables/iptables"
)

func TestTerminalEgressBytesFromStatsMeasured(t *testing.T) {
	bytes, ok := terminalEgressBytesFromStats([]iptables.Stat{
		{Target: "ACCEPT", Input: "veth-1", Output: "eth0", Bytes: 4096},
	}, "veth-1", "eth0")
	if !ok || bytes != 4096 {
		t.Fatalf("expected measured egress bytes, got %d, ok=%v", bytes, ok)
	}
}

func TestTerminalEgressBytesFromStatsPreservesMeasuredZero(t *testing.T) {
	bytes, ok := terminalEgressBytesFromStats([]iptables.Stat{
		{Target: "ACCEPT", Input: "veth-1", Output: "eth0", Bytes: 0},
	}, "veth-1", "eth0")
	if !ok || bytes != 0 {
		t.Fatalf("expected measured zero, got %d, ok=%v", bytes, ok)
	}
}

func TestTerminalEgressBytesFromStatsUnavailableWhenRuleMissing(t *testing.T) {
	bytes, ok := terminalEgressBytesFromStats([]iptables.Stat{
		{Target: "ACCEPT", Input: "veth-other", Output: "eth0", Bytes: 12},
	}, "veth-1", "eth0")
	if ok || bytes != 0 {
		t.Fatalf("expected unavailable counter, got %d, ok=%v", bytes, ok)
	}
}
