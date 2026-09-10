# SUP-902: bounded host network evidence spool

Child of SUP-897; depends on the SUP-898 journal (E2B PR #62). This slice adds
host-local storage APIs under `networkusage/`. It does not configure the running
orchestrator, deliver evidence remotely, establish provider-billable network
semantics, or authorize pricing/settlement. Existing `Open` and default-off
configuration remain unchanged. `OpenWithSpool` exercises the existing Journal
writer and record format, including its latched failure and `complete=false`.

An operator provisions a dedicated Linux host directory, never guest-mounted.
One `OpenSpool` owns an exclusive nonblocking OS lock until all writers close.
An OS process exit releases the lock. Existing control files must be regular,
non-symlink files; the lock is empty and the sticky marker is exactly one byte,
0 or 1. Corrupt controls fail closed. This is not a defense against a privileged
host process altering the spool while its owner runs.

`MaxBytes` bounds logical data-file bytes plus the one-byte marker. `MaxSegments`
bounds all active, sealed and incomplete segment files. Filesystem block rounding,
directory entries and allocation overhead require separate operator headroom;
this API does not claim a physical-disk quota. The marker and lock add two fixed
directory entries. `SegmentBytes` must fit each serialized record. Admission of
an append reserves its full requested size before writing; actual short writes
consume only the bytes written and latch the Journal failure. No unacknowledged
evidence is discarded to make space. Exhaustion writes and syncs the sticky
marker and returns `ErrSpoolBudget`; failure to persist that marker is also
returned. Reclaimed capacity permits a new Journal; the failed incarnation and
global marker stay incomplete. The future caller must enforce workload
backpressure from these errors; no scheduler integration is claimed here.

Rotation syncs and closes the current file, renames it immutable, and syncs the
directory. A normal close retains the Journal's `terminal_coverage_unverified`
record. On restart, abandoned `.active` files become immutable `.incomplete`
segments without parsing, trimming or discarding a possible partial tail.
`Segments` returns bounded inventory with exact SHA-256 hashes and byte lengths;
it never returns active writers as eligible for deletion. Filenames are random
host identities, independent of guest-controlled sandbox names. The original
incarnation and sequence stay in each JSON record.

`Acknowledge(name, hash)` is a privileged retention API for a future trusted
delivery adapter. Its caller must first obtain durable remote custody of the
**entire exact file**, including raw incomplete tails, then supply its name/hash.
The method checks current content and only then removes it and syncs the
directory. A hash match is not authentication or proof of remote durability.
A failed directory sync latches the spool unavailable: uncertain reclaimed space
cannot be reused before restart reconstructs actual inventory. A missing file
returns `os.ErrNotExist`, not a fabricated delivery acknowledgment. After losing
a deletion response, the adapter must consult its durable remote receipt; local
absence alone cannot prove delivery. No local per-ack tombstones accumulate.

| Contract | Model obligation | Runtime evidence |
| --- | --- | --- |
| Full requested append and new segment fit both budgets before any write. Partial writes cannot exceed reservation. | `Allows`, `AcceptedPrefixBounded` in [model](network-usage-spool.dfy) | `spoolAllows` used by `spoolWriter.Write`; `TestSpoolDafnyDecisions` replays 480 executable oracle decisions; `TestSpoolBudgetStickyAndReclaim` exercises byte/count exhaustion. |
| Retention cannot underflow the data/segment inventory; refused appends preserve prior evidence. | `RetentionBounded`, `RefusalPreservesEvidence` | `TestSpoolJournalRotationAndAcknowledgment`, budget test and existing Journal persistence-fault tests. |
| Only exact immutable evidence can be deleted. Directory durability failure prevents reuse. | Filesystem/authentication assumptions outside arithmetic model. | `TestSpoolActiveProtectionAndSyncFailure`, `TestSpoolDeletionSyncFailureBlocksReuse`. |
| Restart preserves raw partial evidence and stable identity; only one owner; invalid controls rejected. | OS locking and recovery outside model. | `TestSpoolCrashRecovery` uses an actual abrupt subprocess exit; `TestSpoolRejectsCorruptControls`; active-protection test. |
| Original Journal quantities, sequences, and terminal incompleteness survive segmentation. | Existing SUP-898 model, not a universal Go refinement proof. | `TestSpoolJournalRotationAndAcknowledgment` and existing journal tests. |

Reproduce with Dafny 4.11.0 + .NET, Node, and Linux Go 1.26.5:

```sh
DAFNY=/path/to/dafny node scripts/local-proof/network-spool-model.mjs
DAFNY=/path/to/dafny node scripts/local-proof/network-journal-model.mjs
cd packages/orchestrator
GOWORK=off go test -race -count=1 ./pkg/sandbox/networkusage
```

The model verifier proves the arithmetic obligations; its executable output is
compared to a committed fixture before Go replays it against the actual admission
predicate. It does not prove universal Go/Dafny correspondence, OS behavior,
hardware persistence, remote durability, final flush coverage or provider costs.
The general local-proof script does not automatically run these slice checks.

Local verification on 2026-09-10: spool model **6 verified, 0 errors**, 480 oracle
decisions matched; existing journal model **12 verified, 0 errors**, 22 transitions
matched. Linux Go 1.26.5 `networkusage` race tests passed, including concurrent
multi-journal budget contention, abrupt-process recovery, control corruption and
deletion-sync fault injection. These are local API checks, not fleet activation.
