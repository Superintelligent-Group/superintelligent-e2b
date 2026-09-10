//go:build linux

package cfg

import (
	"errors"
	"path/filepath"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage"
)

// CorrelatedProducerSHA256 identifies the SUP-909 reviewed musl build. Updating
// this pin requires a new identified producer build and runtime acceptance.
const CorrelatedProducerSHA256 = "bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7"

func (c BuilderConfig) NetworkUsageSpoolOptions() networkusage.SpoolOptions {
	return networkusage.SpoolOptions{MaxBytes: c.NetworkUsageMaxBytes, SegmentBytes: c.NetworkUsageSegmentBytes, MaxSegments: c.NetworkUsageMaxSegments}
}

// ValidateNetworkUsage runs before path normalization: a relative spool path
// must not accidentally select evidence storage from the process working dir.
func (c BuilderConfig) ValidateNetworkUsage() error {
	if !c.NetworkUsageCorrelated {
		return nil
	}
	if !filepath.IsAbs(c.NetworkUsageSpoolDir) || filepath.Clean(c.NetworkUsageSpoolDir) == string(filepath.Separator) {
		return errors.New("correlated network usage requires an explicit absolute spool directory")
	}
	if c.NetworkUsageJournalDir != "" {
		return errors.New("correlated network usage cannot also configure the legacy journal directory")
	}
	if c.NetworkUsageBinarySHA256 != CorrelatedProducerSHA256 {
		return errors.New("correlated network usage requires the reviewed producer binary SHA256")
	}
	// A max-sized producer frame expands under JSON base64 encoding, then gains
	// bounded journal/header metadata. Each segment must hold that single record.
	if c.NetworkUsageSegmentBytes < 2*1024*1024 || c.NetworkUsageMaxBytes <= c.NetworkUsageSegmentBytes || c.NetworkUsageMaxSegments < 1 {
		return errors.New("invalid correlated network usage spool budget: segment must hold a full encoded frame")
	}
	return nil
}
