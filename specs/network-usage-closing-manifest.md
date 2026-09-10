# SUP-914: retained closing manifests

This slice extends the host-owned SUP-913 delivery service. It does not activate
protected infrastructure, grant read authority, establish a complete measured
window, or associate a sandbox with a provider reservation or price. Every
closing control, manifest part, final manifest and retained claim has
`Complete=false`, including a loss-free baseline-to-terminal local trace.

## Admission and actual reader ownership

`ProtectedDeliveryConfig.Closing` is an explicit `ClosingOptions` value. The zero
value disables manifests. Enabling it requires separate `MaxBytes`, `MaxEntries`,
`PartBytes` and `MaxParts` limits. The immutable original-producer custody binding
includes these values: existing configuration cannot be silently changed or old
unbound evidence adopted. No new provider read or mutation API is introduced.

`Service.NewCollector` writes a durable registration before returning the
collector. The registration binds the journal incarnation to Factory-supplied
SandboxID, ExecutionID, LifecycleID, TemplateID, BuildID, best-effort TeamID,
selected producer SHA256 and fresh producer incarnation. These are workload
metadata, not allocation proof or an authorization decision.

The actual Process path remains `Process.JoinMetrics` -> owned reader sealing
-> `Correlator.Close` -> `Journal.Close`. The journal first syncs immutable close
intent, then appends its closing record and seals its spool writer, then syncs
the sealing outcome. Only after both hooks return does it release the Service's
active-writer ownership. Failed intent/outcome persistence broadcasts shared
failure at that boundary, before waiting on any later filesystem operation.
Ordinary session gaps remain local when their evidence persists successfully.

A canceled Service.Close keeps the spool lock and can be retried. Cancellation
does not manufacture a completed seal. Filesystem sync/close can block; this
implementation does not claim interruptible filesystem IO. Remote operations
run outside the closing-ledger mutex so another collector can register or seal.
After reacquiring it, delivery checks shared failure before proceeding.

## Summary before raw reclaim

Each protected raw inventory receipt now carries `SummaryVersion=1` and at most
one baseline and one terminal correlation reference. A reference retains every
producer header field, scope and named cutoff, correlation/frame journal
sequences, and exact frame/receipt hashes and lengths. This fixed summary is
derived from the raw bytes before SUP-913's receipt-sync/raw-reclaim boundary;
closing therefore needs no later remote raw download.

Malformed or partial records, missing internal journal sequences, invalid
records and unidentified segments remain incomplete. Directory order is never
journal order. Frozen membership is sorted by journal sequence; overlaps,
conflicting workload/fence identities and changed exact versions fail closed.
Missing ranges, missing fence/frame references and unknown sealing become named
manifest gaps. A scope-specific reference must be stored under its matching
baseline or terminal field. Old summary version zero does not invent a fence.

## Durable progression and restart

The `.custody/closing` ledger uses immutable bounded controls:

1. `<journal>.open`: admitted registration, persisted before collector return.
2. `.intent`: pre-seal boundary, fixed fence references and uncertainty.
3. `.seal`: actual outcome (`sealed`, `seal_failed`, or recovered unknown).
4. `.part-N` and `.plan`: fixed membership and deterministic exact byte claims.
5. `.receipt-N`: protected exact versions of the manifest parts.
6. `.final`: immutable final bytes containing the plan and part receipts.
7. `.claim`: terminal retained claim for the verified exact final version.

Each new control uses exclusive temporary creation, write, file sync, rename
and directory sync. A local error, including a filesystem timeout, is absorbing
for that Service owner. It is never reclassified as a remote retry. Temporary
files cannot authorize reclaim. Recovery accepts only bounded regular controls,
validates their identities and schemas, removes recognized incomplete temporary
files under the exclusive spool lock, and syncs the directory.

An admitted owner missing its intent is recovered as
`owner_crashed_without_close_intent`; missing outcome is
`owner_crashed_seal_unknown`. An exact journal found in inventory without any
registration is explicitly unidentified, with empty workload registration and
unknown outcome. Raw contents cannot promote it to an admitted collector.
Unparseable raw evidence cannot invent even that journal identity and remains
retained inventory with explicit pending backlog. New inventory after a terminal
claim is rejected.

Membership freezes only after all currently sealed raw candidates have drained
to durable custody receipts. Active writers are excluded from that candidate
list. Parts have stable index-based names and exact hashes/lengths; retry uses
the same persisted bytes and exact protected versions. A failed/lost response
cannot change membership. The final manifest is conditionally created and its
exact version verified before the terminal claim is synced.

Only the durable terminal claim plus fresh verification of its exact final
version permits inventory retirement. Remaining receipts must equal members of
the frozen parts. Raw protected objects remain retained. Inventory unlinks are
directory-synced; temporary closing controls are then retired with `.seal` last,
so restart resumes interrupted retirement. The terminal `.claim` remains as the
local terminal artifact. It has no recursive receipt-of-receipt chain.

## Bounds and shutdown truth

Raw segments, SUP-913 receipt inventory, and closing controls have separate
logical byte/count budgets. Closing control payloads are each at most
`PartBytes` (8 KiB to 1 MiB and no larger than the protected object limit), with
at most 128 parts and 100,000 retained/control entries. `MaxBytes` and entry
limits include retained terminal claims. Temporary writes consume only the
reserved prospective control budget; there is at most one temporary control
under the ledger mutex. Filesystem metadata overhead is bounded by entry count,
not represented as content bytes. There is no automatic retention eviction.

The host scheduler handles at most one closing owner per cooperative bounded
pass, after its existing bounded raw pass. Remaining sealed raw or closing
owners return `ErrClosingPending`, which retries at the configured interval
without host failure. Final Service.Close reports pending backlog instead of
claiming successful delivery; it still joins the scheduler and releases the
spool lock once writers and that pass finish. Reopening continues the same
identities. Local budget exhaustion fails admission, preserving remaining
inventory and protected objects.

## Evidence and limits

`closing_test.go` exercises actual Service registration/close ownership with a
package-private exact-version store: correlated baseline/terminal summaries,
multi-segment ordering, independent final/part/raw exact-byte reads, lost
responses after raw deletion, unknown crash outcomes, final verification,
partial retirement restart, filesystem timeout origin, bounded backlog,
canceled ownership and concurrent sibling sealing. It uses the retained
SUP-909 producer fixture through the production Correlator. These tests do not
claim live AWS enforcement or a new booted guest acceptance run.

The adjacent Dafny model abstracts synced controls and exact protected version
truth as inputs. Its lemmas establish the retirement ordering and preservation
of incomplete claims. It does not prove the Go filesystem implementation,
provider retention/authorization, frame provenance, or workload truth. Existing
reader/Process runtime tests supply the linked lifecycle path; full window
coverage and production activation remain separately identified gates.
