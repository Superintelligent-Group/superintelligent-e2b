# SUP-913: host-owned protected delivery inventory

This source slice depends on SUP-912 and SUP-906. It does not activate a bucket,
choose retention, or establish a complete measurement window. SUP-914 owns the
subsequent retained closing manifest.

## Runtime ownership

`factories/run.go` optionally constructs `OpenProtectedService` before serving
measured work. The empty `NETWORK_USAGE_PROTECTED_DELIVERY` value disables this
path. A nonempty value is a strict JSON `ProtectedDeliveryConfig`: the complete
protected custody configuration and explicit inventory byte/count, pass-count,
and interval budgets. Duplicate/unknown fields are rejected. Account, region,
bucket, prefix, producer role ARN and immutable ID, approved policy hash,
retention days and object size are required. This code cannot establish that an
operator actually approved a caller-supplied policy hash. That remains an
identified activation gate.

The protected constructor checks live identity/policy/retention evidence using
the existing SUP-906 implementation. Service obtains the exclusive spool lock,
recovers custody state and starts one host delivery goroutine. No VM owns an
uploader. The scheduler processes at most the configured pass count (maximum
100) within a cooperative 30-second deadline. Transient network, service-limit,
conditional conflict and timeout failures retain raw data and retry with
exponential delay capped at one hour. Other errors fail host admission through
the existing persistent Service failure channel. Raw capacity exhaustion remains
sticky; upload success cannot repair the affected measurement session.

Retry classification distinguishes operation origin. Local fsync timeouts are
absorbing even when their error implements `net.Error`. Explicit cooperative
context checks during candidate reads and before raw acknowledgment carry a
separate marker: pass-budget expiry at those safe points retains data and retries
without killing healthy siblings. A joined local IO failure cannot be hidden by
that marker. Shared failure forbids all later same-owner delivery/reclaim,
including the final shutdown pass.

## Durable identities and reclaim order

Only protected-mode spool recovery recognizes the `.custody` directory. Its
immutable `binding.json` binds schema, resolved producer UserID and destination,
policy/retention configuration and control budgets. An unbound directory with
existing raw evidence is rejected; existing evidence cannot silently acquire a
new producer identity. Changed instance, role, destination or configuration
requires a separate explicit migration procedure. Disabling protected mode
cannot ignore an existing custody directory.

Each immutable raw segment has one `<segment-name>.receipt` containing exact
raw name/hash/length, destination/key/non-null version and lifecycle association.
The association includes journal incarnation and sequence bounds plus Factory
SandboxID, ExecutionID, TemplateID, BuildID and LifecycleID, the verified selected
producer SHA256, and fresh producer incarnation. TeamID is retained only as
best-effort metadata, as defined by RuntimeMetadata. It is not an authorization
decision. Provider allocation/reservation, price, artifact coverage and historical
settlement are absent rather than inferred from sandbox identity.

Workload context is written in correlated journal records before metrics are
configured. Segments interrupted before a durable binding or containing partial
tails are explicitly unidentified/incomplete. Raw bytes are unchanged. A receipt
never establishes continuity or upgrades `Complete`.

The delivery order is raw read/hash, protected conditional create plus exact
verification, immutable receipt temporary write, file sync, rename, custody
directory sync, and only then the existing hash-checked raw acknowledgment and
spool-directory sync. A returned upload receipt alone never permits deletion.
After restart, an existing receipt plus surviving raw segment requires a fresh
verification of that exact protected version before reclaim. If raw unlink
already happened, its receipt remains the inventory for SUP-914; directory
enumeration is not used to pretend that missing raw bytes are a new receipt.

Temporary receipt files cannot authorize reclaim. Recovery removes only bounded,
recognized regular temporary control files under the held spool lock, then syncs
their directory; unchanged raw evidence retries under the same identity. Invalid
committed controls, symlinks, extra filenames, mixed lifecycle identities or
conflicting receipt/raw identity fail closed. Local filesystem operations may
block despite context cancellation; no claim of interruptible fsync is made.

Inventory never shrinks in this leaf. `MaxInventoryBytes` covers binding plus
receipt contents; `MaxInventoryEntries` limits receipts, with one binding and at
most one additional temporary entry. A single control record is limited to 8192
bytes. Raw storage has its existing separate budget. Directory/block overhead is
bounded by file counts, not represented as logical content bytes. This deliberate
backpressure persists until SUP-914 can retire inventory after manifest custody.

## Closing and evidence

Service.Close closes admission, waits for collector sealing while delivery
continues, requests a bounded final pass, joins the scheduler, and only then
releases the spool lock. A canceled Close retains ownership for retry. A final
remote error is returned with retained backlog; it is not reported as successful
delivery. No remote listing, automatic receipt retirement, billing publication,
or closing-manifest assertion is introduced.

`protected_delivery_test.go` exercises actual Service scheduling/closing, two
collectors, lost upload response, receipt-directory sync failure, interruption
before unlink and after unlink, exact-version replay, replacement identity,
unbound backlog rejection, temporary-file recovery, inventory exhaustion,
non-versioned receipts, slow delivery and canceled ownership joins. The fake
custody provider supplies no AWS identity or retention proof. Existing SUP-906
HTTP tests cover the adapter boundary. The linked [model](network-usage-delivery-inventory.dfy)
proves bounded ordering properties assuming truthful protected verification and
filesystem durability, not universal Go refinement or cloud authorization.

Activation still requires approved identities/durations/policy and independent
live cross-host/non-EC2, reader-mutation, unversioned-body, deletion and retention
negative tests, plus exact-version reader verification after producer restart.
SUP-912 actual Resume acceptance and SUP-914 retained manifests remain separate.
