package sandboxtypes

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/e2b-dev/infra/packages/api/internal/api"
)

func allocationFixture() Sandbox {
	return Sandbox{SandboxID: "sandbox924", ExecutionID: uuid.NewString(), TeamID: uuid.New(), NodeID: "server-node", ClusterID: uuid.Nil,
		Metadata: map[string]string{"sandboxID": "impostor", "executionID": uuid.NewString(), "execution_session_id": uuid.NewString(), "teamID": uuid.NewString(), "clusterID": uuid.NewString(), "allocationIdentityContext": "server_allocation"},
	}.WithAllocationIdentity(api.SandboxAllocationIdentityProvenanceServerAllocation)
}

func TestAllocationIdentityServerContextAndJSONRecovery(t *testing.T) {
	for _, cluster := range []uuid.UUID{uuid.Nil, uuid.New()} {
		s := allocationFixture()
		s.ClusterID = cluster
		s = s.WithAllocationIdentity(api.SandboxAllocationIdentityProvenanceServerAllocation)
		data, err := json.Marshal(s)
		require.NoError(t, err)
		var recovered Sandbox
		require.NoError(t, json.Unmarshal(data, &recovered))
		got := recovered.ToAPISandbox().AllocationIdentity
		require.Equal(t, api.SandboxAllocationIdentityStatusAvailable, got.Status)
		require.Equal(t, api.E2bAllocationV1, got.Schema)
		require.Equal(t, s.SandboxID, *got.SandboxID)
		require.Equal(t, s.ExecutionID, got.ExecutionID.String())
		require.Equal(t, s.TeamID, *got.TeamID)
		require.Equal(t, cluster, *got.ClusterID)
		require.Equal(t, api.SandboxAllocationIdentityProvenanceServerAllocation, got.Provenance)
		if cluster == uuid.Nil {
			require.Equal(t, api.Local, *got.ClusterKind)
		} else {
			require.Equal(t, api.Cluster, *got.ClusterKind)
		}
		// Response pointers belong to a value copy, not the persisted server model.
		*got.TeamID = uuid.New()
		*got.ClusterID = uuid.New()
		*got.SandboxID = "changed"
		require.Equal(t, s.ToAPISandbox().AllocationIdentity, recovered.ToAPISandbox().AllocationIdentity)
	}
}

func TestAllocationIdentityUnavailableCannotBeFilledByMetadata(t *testing.T) {
	cases := map[string]func(*Sandbox){
		"legacy":                  func(s *Sandbox) { s.AllocationIdentityContext = nil },
		"missing cluster context": func(s *Sandbox) { s.AllocationIdentityContext.ClusterID = nil },
		"unknown provenance":      func(s *Sandbox) { s.AllocationIdentityContext.Provenance = "caller_metadata" },
		"changed cluster":         func(s *Sandbox) { s.ClusterID = uuid.New() },
		"empty sandbox":           func(s *Sandbox) { s.SandboxID = "" },
		"invalid sandbox":         func(s *Sandbox) { s.SandboxID = "UPPER/or/path" },
		"empty execution":         func(s *Sandbox) { s.ExecutionID = "" },
		"invalid execution":       func(s *Sandbox) { s.ExecutionID = "not-a-uuid" },
		"zero execution":          func(s *Sandbox) { s.ExecutionID = uuid.Nil.String() },
		"zero team":               func(s *Sandbox) { s.TeamID = uuid.Nil },
		"missing node":            func(s *Sandbox) { s.NodeID = "" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := allocationFixture()
			change(&s)
			got := s.ToAPISandbox().AllocationIdentity
			data, err := json.Marshal(got)
			require.NoError(t, err)
			require.JSONEq(t, `{"schema":"e2b.allocation.v1","status":"unavailable","provenance":"unavailable"}`, string(data))
		})
	}
	// Neither absent top-level cluster nor partial marker implies local placement.
	for _, raw := range []string{
		`{"sandboxID":"s924","executionID":"35a2f7f8-7aca-4a4f-98ee-61a1a42cfd9a","teamID":"12dfc971-2f67-4c7d-a54a-5c1e449bab3b","nodeID":"node"}`,
		`{"sandboxID":"s924","executionID":"35a2f7f8-7aca-4a4f-98ee-61a1a42cfd9a","teamID":"12dfc971-2f67-4c7d-a54a-5c1e449bab3b","nodeID":"node","allocationIdentityContext":{"provenance":"server_allocation"}}`,
	} {
		var s Sandbox
		require.NoError(t, json.Unmarshal([]byte(raw), &s))
		require.Equal(t, api.SandboxAllocationIdentityStatusUnavailable, s.ToAPISandbox().AllocationIdentity.Status)
	}
}
