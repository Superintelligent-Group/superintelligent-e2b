//go:build linux

package cfg

import "testing"

func TestNetworkUsageExplicitActivation(t *testing.T) {
	valid := BuilderConfig{NetworkUsageCorrelated: true, NetworkUsageBinarySHA256: CorrelatedProducerSHA256, NetworkUsageSpoolDir: "/evidence", NetworkUsageMaxBytes: 8 * 1024 * 1024, NetworkUsageSegmentBytes: 2 * 1024 * 1024, NetworkUsageMaxSegments: 4}
	if err := valid.ValidateNetworkUsage(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BuilderConfig){
		func(c *BuilderConfig) { c.NetworkUsageSpoolDir = "" },
		func(c *BuilderConfig) { c.NetworkUsageSpoolDir = "relative" },
		func(c *BuilderConfig) { c.NetworkUsageSpoolDir = "/" },
		func(c *BuilderConfig) { c.NetworkUsageSpoolDir = "/tmp/../" },
		func(c *BuilderConfig) { c.NetworkUsageBinarySHA256 = "" },
		func(c *BuilderConfig) {
			c.NetworkUsageBinarySHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		},
		func(c *BuilderConfig) { c.NetworkUsageJournalDir = "/legacy" },
		func(c *BuilderConfig) { c.NetworkUsageSegmentBytes = 1024 },
		func(c *BuilderConfig) { c.NetworkUsageMaxBytes = c.NetworkUsageSegmentBytes },
		func(c *BuilderConfig) { c.NetworkUsageMaxSegments = 0 },
	} {
		c := valid
		mutate(&c)
		if c.ValidateNetworkUsage() == nil {
			t.Fatalf("accepted invalid config: %+v", c)
		}
	}
	if err := (BuilderConfig{}).ValidateNetworkUsage(); err != nil {
		t.Fatal("legacy disabled configuration changed", err)
	}
}
