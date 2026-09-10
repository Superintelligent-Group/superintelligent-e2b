package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocationIdentityGeneratedSchemaIsAdditive(t *testing.T) {
	spec, err := GetSpec()
	require.NoError(t, err)
	sandbox := spec.Components.Schemas["Sandbox"].Value
	require.NotContains(t, sandbox.Required, "allocationIdentity")
	require.Equal(t, "#/components/schemas/SandboxAllocationIdentity", sandbox.Properties["allocationIdentity"].Ref)
	require.NotContains(t, spec.Components.Schemas["SandboxDetail"].Value.Properties, "allocationIdentity")
	identity := spec.Components.Schemas["SandboxAllocationIdentity"].Value
	require.ElementsMatch(t, []string{"schema", "status", "provenance"}, identity.Required)
	for _, id := range []string{"executionID", "teamID", "clusterID"} {
		require.Equal(t, "uuid", identity.Properties[id].Value.Format)
	}
	// Existing response JSON remains decodable, with absence distinguishable
	// from the new server's explicit unavailable response.
	var old Sandbox
	require.NoError(t, json.Unmarshal([]byte(`{"sandboxID":"old","templateID":"t","clientID":"c","envdVersion":"0.1"}`), &old))
	require.Nil(t, old.AllocationIdentity)
	for _, status := range []SandboxAllocationIdentityStatus{SandboxAllocationIdentityStatusAvailable, SandboxAllocationIdentityStatusUnavailable} {
		require.True(t, status.Valid())
	}
}
