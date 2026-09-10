package nodemanager

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/e2b-dev/infra/packages/api/internal/api"
	"github.com/e2b-dev/infra/packages/db/pkg/types"
	"github.com/e2b-dev/infra/packages/shared/pkg/grpc/orchestrator"
)

// mockSandboxListClient implements orchestrator.SandboxServiceClient and returns
// a canned List response, so GetSandboxes' proto->Sandbox reconstruction can be
// tested without a live orchestrator.
type mockSandboxListClient struct {
	orchestrator.SandboxServiceClient

	resp *orchestrator.SandboxListResponse
}

func (m *mockSandboxListClient) List(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*orchestrator.SandboxListResponse, error) {
	return m.resp, nil
}

// TestGetSandboxes_RestoresAutoPauseFilesystemOnly verifies that the auto-pause
// snapshot-kind policy round-trips through the orchestrator's SandboxConfig when
// the API re-syncs its sandbox list (e.g. after a restart). The proto field
// exists for exactly this path, so without it the policy would silently revert
// to a memory auto-pause.
func TestGetSandboxes_RestoresAutoPauseFilesystemOnly(t *testing.T) {
	t.Parallel()

	now := time.Now()
	runningSandbox := func(id string, autoPauseFilesystemOnly bool) *orchestrator.RunningSandbox {
		return &orchestrator.RunningSandbox{
			StartTime: timestamppb.New(now),
			EndTime:   timestamppb.New(now.Add(time.Hour)),
			Config: &orchestrator.SandboxConfig{
				SandboxId:               id,
				TemplateId:              "tmpl",
				BaseTemplateId:          "tmpl",
				TeamId:                  uuid.NewString(),
				BuildId:                 uuid.NewString(),
				ExecutionId:             uuid.NewString(),
				AutoPause:               true,
				AutoPauseFilesystemOnly: autoPauseFilesystemOnly,
			},
		}
	}

	node := NewTestNode("test-node", api.NodeStatusReady, 0, 4)
	node.SetSandboxClient(&mockSandboxListClient{
		resp: &orchestrator.SandboxListResponse{
			Sandboxes: []*orchestrator.RunningSandbox{
				runningSandbox("fs-only", true),
				runningSandbox("memory", false),
			},
		},
	})

	sandboxes, err := node.GetSandboxes(t.Context())
	require.NoError(t, err)
	require.Len(t, sandboxes, 2)

	got := make(map[string]bool, len(sandboxes))
	for _, sbx := range sandboxes {
		got[sbx.SandboxID] = sbx.AutoPauseFilesystemOnly
	}

	assert.True(t, got["fs-only"], "filesystem-only auto-pause policy must survive an orchestrator re-sync")
	assert.False(t, got["memory"], "memory auto-pause policy must survive an orchestrator re-sync")
}

// TestGetSandboxes_RestoresIamTokens verifies that the named workload-token
// definitions round-trip through the orchestrator's stored SandboxConfig when
// the API re-syncs its sandbox list (e.g. after a restart), and that a config
// without an iam message decodes as no definitions.
func TestGetSandboxes_RestoresIamTokens(t *testing.T) {
	t.Parallel()

	now := time.Now()
	runningSandbox := func(id string, iam *orchestrator.SandboxIam) *orchestrator.RunningSandbox {
		return &orchestrator.RunningSandbox{
			StartTime: timestamppb.New(now),
			EndTime:   timestamppb.New(now.Add(time.Hour)),
			Config: &orchestrator.SandboxConfig{
				SandboxId:      id,
				TemplateId:     "tmpl",
				BaseTemplateId: "tmpl",
				TeamId:         uuid.NewString(),
				BuildId:        uuid.NewString(),
				ExecutionId:    uuid.NewString(),
				Iam:            iam,
			},
		}
	}

	node := NewTestNode("test-node", api.NodeStatusReady, 0, 4)
	node.SetSandboxClient(&mockSandboxListClient{
		resp: &orchestrator.SandboxListResponse{
			Sandboxes: []*orchestrator.RunningSandbox{
				runningSandbox("with-iam", &orchestrator.SandboxIam{Tokens: map[string]*orchestrator.SandboxIamToken{
					"aws": {Audience: "sts.amazonaws.com", TokenType: "JWT-SVID"},
				}}),
				runningSandbox("no-iam", nil),
			},
		},
	})

	sandboxes, err := node.GetSandboxes(t.Context())
	require.NoError(t, err)
	require.Len(t, sandboxes, 2)

	got := make(map[string]*types.SandboxIam, len(sandboxes))
	for _, sbx := range sandboxes {
		got[sbx.SandboxID] = sbx.Iam
	}

	assert.Equal(t, &types.SandboxIam{Tokens: map[string]types.SandboxIamToken{"aws": {Audience: "sts.amazonaws.com", TokenType: "JWT-SVID"}}}, got["with-iam"],
		"named token definitions must survive an orchestrator re-sync")
	assert.Nil(t, got["no-iam"], "a config without an iam message must decode as no configuration")
}

func TestGetSandboxesAllocationIdentityUsesNodeAndServerConfig(t *testing.T) {
	team, execution := uuid.New(), uuid.NewString()
	for _, cluster := range []uuid.UUID{uuid.Nil, uuid.New()} {
		node := NewTestNode("node924", api.NodeStatusReady, 0, 4)
		node.ClusterID = cluster
		node.SetSandboxClient(&mockSandboxListClient{resp: &orchestrator.SandboxListResponse{Sandboxes: []*orchestrator.RunningSandbox{{
			StartTime: timestamppb.Now(), EndTime: timestamppb.Now(), Config: &orchestrator.SandboxConfig{
				SandboxId: "sandbox924", TeamId: team.String(), ExecutionId: execution, BuildId: uuid.NewString(),
				Metadata: map[string]string{"teamID": uuid.NewString(), "executionID": uuid.NewString(), "clusterID": uuid.NewString()},
			},
		}}}})
		sandboxes, err := node.GetSandboxes(t.Context())
		require.NoError(t, err)
		require.Len(t, sandboxes, 1)
		got := sandboxes[0].ToAPISandbox().AllocationIdentity
		require.Equal(t, api.SandboxAllocationIdentityStatusAvailable, got.Status)
		require.Equal(t, api.SandboxAllocationIdentityProvenanceOrchestratorResync, got.Provenance)
		require.Equal(t, execution, got.ExecutionID.String())
		require.Equal(t, team, *got.TeamID)
		require.Equal(t, cluster, *got.ClusterID)
	}
}
