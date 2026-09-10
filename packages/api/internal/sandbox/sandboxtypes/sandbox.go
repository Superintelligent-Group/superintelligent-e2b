package sandboxtypes

import (
	"time"

	"github.com/google/uuid"

	"github.com/e2b-dev/infra/packages/api/internal/api"
	"github.com/e2b-dev/infra/packages/db/pkg/types"
	"github.com/e2b-dev/infra/packages/shared/pkg/consts"
	"github.com/e2b-dev/infra/packages/shared/pkg/id"
	sbxlogger "github.com/e2b-dev/infra/packages/shared/pkg/logger/sandbox"
)

func NewSandbox(
	sandboxID string,
	templateID string,
	clientID string,
	alias *string,
	executionID string,
	teamID uuid.UUID,
	buildID uuid.UUID,
	metadata map[string]string,
	maxInstanceLength time.Duration,
	startTime time.Time,
	endTime time.Time,
	vcpu int64,
	totalDiskSizeMB int64,
	ramMB int64,
	kernelVersion string,
	firecrackerVersion string,
	envdVersion string,
	nodeID string,
	clusterID uuid.UUID,
	autoPause bool,
	autoPauseFilesystemOnly bool,
	autoResume *types.SandboxAutoResumeConfig,
	envdAccessToken *string,
	allowInternetAccess *bool,
	baseTemplateID string,
	domain *string,
	network *types.SandboxNetworkConfig,
	trafficAccessToken *string,
	mounts []*types.SandboxVolumeMountConfig,
	iam *types.SandboxIam,
) Sandbox {
	return Sandbox{
		SandboxID:  sandboxID,
		TemplateID: templateID,
		ClientID:   clientID,
		Alias:      alias,
		Domain:     domain,

		ExecutionID:             executionID,
		TeamID:                  teamID,
		BuildID:                 buildID,
		Metadata:                metadata,
		MaxInstanceLength:       maxInstanceLength,
		StartTime:               startTime,
		EndTime:                 endTime,
		VCpu:                    vcpu,
		TotalDiskSizeMB:         totalDiskSizeMB,
		RamMB:                   ramMB,
		KernelVersion:           kernelVersion,
		FirecrackerVersion:      firecrackerVersion,
		EnvdVersion:             envdVersion,
		EnvdAccessToken:         envdAccessToken,
		TrafficAccessToken:      trafficAccessToken,
		AllowInternetAccess:     allowInternetAccess,
		NodeID:                  nodeID,
		ClusterID:               clusterID,
		AutoPause:               autoPause,
		AutoPauseFilesystemOnly: autoPauseFilesystemOnly,
		AutoResume:              autoResume,
		State:                   StateRunning,
		BaseTemplateID:          baseTemplateID,
		Network:                 network,
		VolumeMounts:            mounts,
		Iam:                     iam,
	}
}

type Sandbox struct {
	SandboxID  string  `json:"sandboxID"`
	TemplateID string  `json:"templateID"`
	ClientID   string  `json:"clientID"`
	Alias      *string `json:"alias,omitempty"`
	Domain     *string `json:"domain,omitempty"`

	ExecutionID         string            `json:"executionID"`
	TeamID              uuid.UUID         `json:"teamID"`
	BuildID             uuid.UUID         `json:"buildID"`
	BaseTemplateID      string            `json:"baseTemplateID"`
	Metadata            map[string]string `json:"metadata"`
	MaxInstanceLength   time.Duration     `json:"maxInstanceLength"`
	StartTime           time.Time         `json:"startTime"`
	EndTime             time.Time         `json:"endTime"`
	VCpu                int64             `json:"vCpu"`
	TotalDiskSizeMB     int64             `json:"totalDiskSizeMB"`
	RamMB               int64             `json:"ramMB"`
	KernelVersion       string            `json:"kernelVersion"`
	FirecrackerVersion  string            `json:"firecrackerVersion"`
	EnvdVersion         string            `json:"envdVersion"`
	EnvdAccessToken     *string           `json:"envdAccessToken,omitempty"`
	TrafficAccessToken  *string           `json:"trafficAccessToken"`
	AllowInternetAccess *bool             `json:"allowInternetAccess,omitempty"`
	NodeID              string            `json:"nodeID"`
	ClusterID           uuid.UUID         `json:"clusterID"`
	// Set only after successful server placement or trusted orchestrator resync.
	// Missing historical JSON must not turn a zero ClusterID into known local placement.
	AllocationIdentityContext *AllocationIdentityContext `json:"allocationIdentityContext,omitempty"`
	AutoPause                 bool                       `json:"autoPause"`
	// AutoPauseFilesystemOnly makes a timeout auto-pause take a filesystem-only
	// snapshot (no memory) instead of a full memory snapshot. Only consulted when
	// AutoPause is true; read by the evictor at pause time.
	AutoPauseFilesystemOnly bool                              `json:"autoPauseFilesystemOnly,omitempty"`
	AutoResume              *types.SandboxAutoResumeConfig    `json:"autoResume,omitempty"`
	Network                 *types.SandboxNetworkConfig       `json:"network"`
	VolumeMounts            []*types.SandboxVolumeMountConfig `json:"volumeMounts"`
	// Iam records the sandbox workload identity configuration. Persisted so it
	// survives re-sync and is carried into the paused snapshot.
	Iam *types.SandboxIam `json:"iam,omitempty"`

	State State `json:"state"`
}

// ClusterID is a pointer so missing historical or partial context cannot be
// confused with the explicitly selected zero UUID used by the local cluster.
type AllocationIdentityContext struct {
	Provenance api.SandboxAllocationIdentityProvenance `json:"provenance"`
	ClusterID  *uuid.UUID                              `json:"clusterID"`
}

func (s Sandbox) ToAPISandbox() *api.Sandbox {
	return &api.Sandbox{
		AllocationIdentity: s.allocationIdentity(),
		SandboxID:          s.SandboxID,
		TemplateID:         s.BaseTemplateID,
		ClientID:           s.ClientID,
		Alias:              s.Alias,
		EnvdVersion:        s.EnvdVersion,
		EnvdAccessToken:    s.EnvdAccessToken,
		TrafficAccessToken: s.TrafficAccessToken,
		Domain:             s.Domain,
	}
}

// WithAllocationIdentity marks context established by the API's successful
// placement or existing trusted node-resync boundary. Never use caller Metadata.
// This marker is persisted with the server model, not inferred on JSON recovery.
func (s Sandbox) WithAllocationIdentity(provenance api.SandboxAllocationIdentityProvenance) Sandbox {
	cluster := s.ClusterID
	s.AllocationIdentityContext = &AllocationIdentityContext{Provenance: provenance, ClusterID: &cluster}
	return s
}

func (s Sandbox) allocationIdentity() *api.SandboxAllocationIdentity {
	identity := &api.SandboxAllocationIdentity{
		Schema:     api.E2bAllocationV1,
		Status:     api.SandboxAllocationIdentityStatusUnavailable,
		Provenance: api.SandboxAllocationIdentityProvenanceUnavailable,
	}
	context := s.AllocationIdentityContext
	if context == nil || context.ClusterID == nil || *context.ClusterID != s.ClusterID ||
		(context.Provenance != api.SandboxAllocationIdentityProvenanceServerAllocation &&
			context.Provenance != api.SandboxAllocationIdentityProvenanceOrchestratorResync) {
		return identity
	}
	execution, err := uuid.Parse(s.ExecutionID)
	if err != nil || execution == uuid.Nil || execution.String() != s.ExecutionID || s.TeamID == uuid.Nil || s.NodeID == "" || id.ValidateSandboxID(s.SandboxID) != nil {
		return identity
	}
	clusterKind := api.Cluster
	if s.ClusterID == consts.LocalClusterID {
		clusterKind = api.Local
	}
	identity.Status = api.SandboxAllocationIdentityStatusAvailable
	identity.Provenance = context.Provenance
	identity.SandboxID = &s.SandboxID
	identity.ExecutionID = &execution
	identity.TeamID = &s.TeamID
	identity.ClusterID = &s.ClusterID
	identity.ClusterKind = &clusterKind
	return identity
}

func (s Sandbox) LoggerMetadata() sbxlogger.SandboxMetadata {
	return sbxlogger.SandboxMetadata{
		SandboxID:  s.SandboxID,
		TemplateID: s.TemplateID,
		TeamID:     s.TeamID.String(),
	}
}

func (s Sandbox) IsExpired(now time.Time) bool {
	return now.After(s.EndTime)
}
