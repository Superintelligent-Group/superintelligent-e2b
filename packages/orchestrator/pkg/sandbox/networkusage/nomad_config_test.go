package networkusage

import (
	"os"
	"testing"
)

// The actual Terraform -> Nomad parser test compares Env to this same fixture.
// Validate it with the production strict decoder, without creating any service.
func TestNomadProtectedConfigFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../scripts/local-proof/fixtures/orchestrator-protected-env.json")
	if err != nil {
		t.Fatal(err)
	}
	config, err := ParseProtectedDeliveryConfig(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if config.Custody.Destination.Bucket != "test-only-evidence" || config.PassLimit != 1 {
		t.Fatal("unexpected fixture configuration")
	}
}
