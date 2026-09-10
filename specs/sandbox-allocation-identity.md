# SUP-924 server-owned allocation response identity

The public `Sandbox` response adds an optional `allocationIdentity` property.
Existing fields and request schemas are unchanged. This server emits the property
with schema `e2b.allocation.v1` and status `available` or `unavailable`; a missing
property means an older server has not implemented this contract.

Available identity contains sandboxID, executionID, teamID, clusterID, clusterKind
and provenance. The sandbox ID passes the existing lowercase-alphanumeric
validator; execution is a canonical, nonzero UUID; team is nonzero. A selected
node must exist in the retained model. All fields come from server state, never
from request Metadata or a claimed SIG execution_session_id/run_id. The execution
passed in `SandboxCreateRequest` and authenticated team are the same fields used
to construct the response model after successful placement.

`clusterKind=local` includes the explicit zero UUID (`consts.LocalClusterID`).
It does not identify a cloud account or region. `clusterKind=cluster` carries the
nonzero cluster UUID already selected by the API. Neither value is inferred from
ambient credentials. Consumers requiring deployment/provider namespace authority
must establish that separately.

The internal running-sandbox model persists `allocationIdentityContext`, containing
provenance and an explicitly present cluster UUID. Only the successful placement
path (`server_allocation`) and existing trusted node resync (`orchestrator_resync`)
establish it. Resync uses the node's selected cluster plus the orchestrator's stored
SandboxConfig execution/team, not its Metadata map. It adds no new authentication
or permission to the existing node connection. Its provenance may differ from the
original response while referring to the same allocation execution.

Unmarked historical JSON, absent/null cluster context, unknown provenance,
context/model cluster mismatch, absent node or invalid sandbox/execution/team IDs
produce only schema/status/provenance=`unavailable` in the nested identity.
Caller metadata cannot complete or override those fields. The marker distinguishes
an explicitly selected local zero UUID from absent historical placement. Existing
JSON decoding still rejects malformed UUID-typed storage fields; no recovery path
manufactures replacements. The property is context, not an immutable historical
record or cryptographic attestation.

## Response coverage and execution boundaries

- Public Create, explicit Resume and Fork use `startSandbox` and `ToAPISandbox`
  after the successful allocation returned by the orchestrator. The existing
  external start/resume path generates an execution UUID. A joined/replayed request
  returns the already allocated model, not the unused candidate execution UUID.
- Live Connect uses `KeepAliveFor` with the authenticated team and preserves the
  existing identity while extending timeout. A different team receives the existing
  not-found response. Connect of a paused sandbox checks snapshot ownership and
  uses external resume, with a new execution.
- Internal checkpoint continues to preserve Runtime.ExecutionID and replace the
  Firecracker LifecycleID (`orchestrator/pkg/sandbox/sandbox.go`); this leaf does
  not expose LifecycleID, claim a new execution for checkpoint, or change that
  runtime behavior. Trusted resync projects the retained execution as received.
- `SandboxDetail`, list responses and internal proxy gRPC are separate schemas and
  are not covered by this extension.

Tests exercise hostile metadata, explicit local/nonlocal context, absent and
partial historical context, invalid IDs, JSON recovery, response pointer isolation,
actual node resync, and real API placement/store/KeepAlive paths against the
repository's local Redis fixture and recorded mock orchestrator calls. The placement
test supplies the execution candidate that the existing external handler generates;
it does not boot a VM or exercise checkpoint execution. Generated OpenAPI types and
embedded schema are regenerated using the repository's pinned generator, with
compatibility checks for old responses and the optional property.

This leaf does not create SIG association, billing records, provider account
attestation, lifecycle/incarnation custody, signed receipts or historical backfill.
Network evidence consumer SessionMapping remains unresolved and Complete remains
false. No deployment, cloud mutation or financial action is included.
