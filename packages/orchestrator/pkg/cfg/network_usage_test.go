//go:build linux

package cfg

import (
	"encoding/json"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
	"github.com/e2b-dev/infra/packages/shared/pkg/storage"
	"strings"
	"testing"
)

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

func TestProtectedDeliveryConfigRequiresExplicitRuntimeAndBudgets(t *testing.T) {
	c := BuilderConfig{NetworkUsageCorrelated: true, NetworkUsageBinarySHA256: CorrelatedProducerSHA256, NetworkUsageSpoolDir: "/evidence", NetworkUsageMaxBytes: 8 << 20, NetworkUsageSegmentBytes: 2 << 20, NetworkUsageMaxSegments: 4}
	fixture := networkusage.ProtectedDeliveryConfig{Custody: storage.ProtectedCustodyConfig{Destination: storage.CustodyDestination{AccountID: "123456789012", Region: "us-east-1", Bucket: "test-only-evidence", Prefix: "network-usage/v1/test", MaxObjectBytes: 2 << 20}, ProducerRoleARN: "arn:aws:iam::123456789012:role/client", ProducerRoleID: "AROA12345678901234567", PolicySHA256: strings.Repeat("a", 64), RetentionDays: 30}, MaxInventoryBytes: 1 << 20, MaxInventoryEntries: 64, IntervalSeconds: 1, PassLimit: 10}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	c.NetworkUsageProtectedDelivery = string(data)
	if err = c.ValidateNetworkUsage(); err != nil {
		t.Fatal(err)
	}
	options, err := c.ProtectedDeliveryOptions()
	if err != nil || options == nil || *options != fixture {
		t.Fatal(options, err)
	}
	c.NetworkUsageCorrelated = false
	if c.ValidateNetworkUsage() == nil {
		t.Fatal("delivery without correlated runtime accepted")
	}
	c.NetworkUsageCorrelated = true
	c.NetworkUsageSegmentBytes = 4 << 20
	if c.ValidateNetworkUsage() == nil {
		t.Fatal("undersized custody object limit accepted")
	}
	c.NetworkUsageProtectedDelivery = ""
	if options, err = c.ProtectedDeliveryOptions(); err != nil || options != nil {
		t.Fatal("empty delivery config not disabled", err)
	}
}
