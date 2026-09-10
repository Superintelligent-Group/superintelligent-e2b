package orchestrator

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/e2b-dev/infra/packages/api/internal/api"
	"github.com/e2b-dev/infra/packages/api/internal/sandbox"
	orchgrpc "github.com/e2b-dev/infra/packages/shared/pkg/grpc/orchestrator"
)

type allocationRecorder struct {
	orchgrpc.SandboxServiceClient
	mu      sync.Mutex
	creates []*orchgrpc.SandboxConfig
	updates int
}

func (r *allocationRecorder) Create(_ context.Context, req *orchgrpc.SandboxCreateRequest, _ ...grpc.CallOption) (*orchgrpc.SandboxCreateResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates = append(r.creates, proto.Clone(req.GetSandbox()).(*orchgrpc.SandboxConfig))
	return &orchgrpc.SandboxCreateResponse{}, nil
}
func (r *allocationRecorder) Update(context.Context, *orchgrpc.SandboxUpdateRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates++
	return &emptypb.Empty{}, nil
}

func TestAllocationIdentitySuccessfulPlacementReplayResumeAndKeepAlive(t *testing.T) {
	o := newCreateSandboxTestOrchestrator(t)
	node, ok := o.nodes.Get("node-1")
	require.True(t, ok)
	recorder := &allocationRecorder{}
	node.SetSandboxClient(recorder)
	team := testTeam()
	build := testBuild()
	fetch := func(context.Context) (SandboxMetadata, *api.APIError) {
		return SandboxMetadata{
			TemplateID: "template924", BaseTemplateID: "base924", Build: build,
			Metadata: map[string]string{"executionID": uuid.NewString(), "teamID": uuid.NewString(), "clusterID": uuid.NewString(), "execution_session_id": uuid.NewString()},
		}, nil
	}
	create := func(id, execution string, resume bool) sandbox.Sandbox {
		now := time.Now()
		s, err := o.CreateSandbox(t.Context(), id, execution, team, fetch, now, now.Add(time.Hour), time.Hour, resume, sandbox.CreationMetadata{IsResume: resume})
		require.Nil(t, err)
		return s
	}
	first := create("sandbox924", uuid.NewString(), false)
	identity := first.ToAPISandbox().AllocationIdentity
	require.Equal(t, api.SandboxAllocationIdentityStatusAvailable, identity.Status)
	require.Equal(t, api.Local, *identity.ClusterKind)
	require.Equal(t, uuid.Nil, *identity.ClusterID)
	require.Equal(t, team.ID, *identity.TeamID)
	recorder.mu.Lock()
	sent := proto.Clone(recorder.creates[0]).(*orchgrpc.SandboxConfig)
	recorder.mu.Unlock()
	require.Equal(t, sent.GetExecutionId(), identity.ExecutionID.String())
	require.Equal(t, sent.GetSandboxId(), *identity.SandboxID)
	require.Equal(t, sent.GetTeamId(), identity.TeamID.String())
	require.NotEqual(t, sent.GetMetadata()["executionID"], identity.ExecutionID.String())

	duplicate := create("sandbox924", uuid.NewString(), false)
	require.Equal(t, identity, duplicate.ToAPISandbox().AllocationIdentity, "joined existing allocation must retain its actual execution")
	kept, apiErr := o.KeepAliveFor(t.Context(), team.ID, first.SandboxID, 2*time.Hour, false)
	require.Nil(t, apiErr)
	require.Equal(t, identity, kept.ToAPISandbox().AllocationIdentity)
	foreign, apiErr := o.KeepAliveFor(t.Context(), uuid.New(), first.SandboxID, time.Hour, false)
	require.Nil(t, foreign)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusNotFound, apiErr.Code)
	recorder.mu.Lock()
	require.Len(t, recorder.creates, 1)
	require.Equal(t, 1, recorder.updates)
	recorder.mu.Unlock()

	// Model the existing external handler's new execution ID after its preceding
	// allocation has been removed. The real API placement and Redis paths run.
	o.sandboxStore.Remove(t.Context(), team.ID, first.SandboxID)
	resumed := create(first.SandboxID, uuid.NewString(), true)
	got := resumed.ToAPISandbox().AllocationIdentity
	require.Equal(t, api.SandboxAllocationIdentityStatusAvailable, got.Status)
	require.NotEqual(t, identity.ExecutionID, got.ExecutionID)
	require.Equal(t, identity.SandboxID, got.SandboxID)
	recorder.mu.Lock()
	last := proto.Clone(recorder.creates[1]).(*orchgrpc.SandboxConfig)
	recorder.mu.Unlock()
	require.Equal(t, last.GetExecutionId(), got.ExecutionID.String())
}
