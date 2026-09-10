# SUP-903: exact raw segment custody and bounded delivery

Child of SUP-897; depends on SUP-902 (E2B PR #63). This adds optional storage
capability `ImmutableCustodyStore` and `Spool.DeliverOnce`. Existing `Blob.Put`,
snapshot storage, orchestrator startup and configuration remain unchanged.
There is no production activation, infrastructure mutation or new secret.

The AWS adapter uses the existing SDK credential chain. Its constructor calls
STS through the official regional endpoint and requires the configured 12-digit
account to match the returned caller account. Both S3 and STS clients override
ambient endpoint URLs; custom S3-compatible destinations are unsupported here.
Every object request specifies `ExpectedBucketOwner`. A destination explicitly
names account, region, bucket, a prefix beginning `network-usage/v1/`, an uploader
host claim, and a maximum raw object size (at most 64 MiB). Inputs reject path
separators/traversal in object identity. The stable key is:

`<prefix>/<account>/<claimed-host>/<segment-name>`

`ClaimedHostID` is an unverified uploader claim, not authenticated producer
provenance. STS account equality does not authenticate that claim or even require
an EC2 role. Journal bytes are never parsed or rewritten during delivery. SIG
must not accept these objects as authenticated per-host usage until a separate
host authorization and identity-binding gate exists.

`CreateExact` checks local length and SHA-256 before issuing conditional
`PutObject(IfNoneMatch="*")` with a full-object SHA-256 checksum. It then calls
`VerifyExact`; a 412 conflict also verifies existing custody. Other failures,
including 409 and lost responses, return no receipt. An SDK retry or later pass
uses the same key and must verify the existing exact bytes. Ordinary overwrite
is never used. A byte conflict under the same identity fails closed.

Verification checks raw HEAD length, required claim metadata and provider
SHA-256. Composite or mismatching SHA-256 is rejected. Metadata containing a hash
is not checksum evidence: absent provider SHA-256 triggers a bounded GET and
local SHA-256 calculation, pinned to HEAD version/ETag when available. The GET
must also retain matching claim metadata. The receipt binds destination, key,
claim, raw length/hash, and optional provider version. This is evidence of
custody at verification time, not proof of protected future retention.

`DeliverOnce` processes at most 100 immutable segments under a cooperative
30-second pass deadline. Candidate enumeration reads directory metadata in
16-entry chunks and hashes only selected files; it does not hold the Journal
writer mutex while reading the backlog. Local reads check cancellation between
32-KiB chunks. This cannot interrupt a blocked operating-system syscall.
It hashes each bounded local file to construct the exact claim, invokes
the configured custody implementation, validates every receipt identity field,
then rechecks the local hash in cancellation-aware `Spool.Acknowledge`. Active
segments are excluded. Error, cancellation,
identity mismatch, partial remote read or uncertain response retains local
evidence. Restart derives the same object key from the unchanged destination and
segment identity. If the local segment disappeared after an uncertain deletion,
`VerifyExact` provides the remote replay path; local absence is not a receipt.
No remote listing or local acknowledgment tombstone database is introduced.

Production remains gated on an authorized producer identity, an IAM-protected
prefix that prevents unauthorized writes/deletes, a receiver with authentic
source binding, raw-evidence retention policy, scheduling/backpressure and fleet
load evidence. Current broad template/build-cache bucket access does not supply
those guarantees. This code is not wired to those buckets or a background worker.
No raw counters become billable egress, artifact usage, terminal coverage or
price authority. Every original `complete=false` and partial tail survives.

| Requirement | Formal obligation | Executable evidence |
| --- | --- | --- |
| No successful local reclaim before exact remote custody verification. | `NeverDeleteWithoutCustody` in [model](network-usage-custody.dfy), assuming provider truth. | `TestDeliveryLostResponseRestartRawTail`, AWS conditional replay and corrupt-object tests. |
| Lost upload response preserves local evidence; restart reuses exact remote identity. | `LostUploadResponseRetainsLocal` | `TestDeliveryLostResponseRestartRawTail`, `TestAWSCustodyConditionalReplay`. |
| Mismatched receipt fields cannot authorize reclaim. | `Matches`, `ReceiptMismatchBlocksReclaim` | `TestCustodyDafnyReceiptIdentity` replays 16 executable oracle rows against the real receipt predicate; `TestDeliveryRejectsWrongReceiptAndCancellation`. |
| Delivery cannot establish terminal completeness. | `NeverEstablishesCompleteness` | Raw-tail byte equality in delivery restart test; existing Journal/spool invariants. |
| Endpoint/account/owner/key are pinned, checksums are verified, and conditional conflicts remain uncertain. | SDK/provider boundary outside model. | `TestAWSCustodyConstructorPinsEndpointsAndAccount`, `TestAWSCustodyRejectsInvalidInputBeforeRequest`, `TestAWSCustodyConditionalConflictRetainsUncertainty`, conditional replay, concurrent replay and corrupt-object tests. |
| A small delivery limit does not hash the full backlog; expired/canceled passes retain evidence. | Runtime resource boundary outside model. | `TestDeliveryBoundedReadsAndDeadline`, `TestDeliveryCancellationAfterCustodyRetainsLocal`. |

Reproduce with Dafny 4.11.0/.NET, Node and Linux Go 1.26.5:

```sh
DAFNY=/path/to/dafny node scripts/local-proof/network-custody-model.mjs
cd packages/shared
GOWORK=off go test -race -count=1 ./pkg/storage -run 'TestAWSCustody|TestCustodyDafny'
cd ../orchestrator
GOWORK=off go test -race -count=1 ./pkg/sandbox/networkusage
```

Tests use mock HTTP/fake custody only. No live AWS/S3 operations are part of
these tests. Formal proof covers the model, not universal Go refinement, AWS
truthfulness, cryptographic collision resistance or retained-object IAM policy.
The repository general local-proof verifier does not automatically run these
slice checks.

Local verification on 2026-09-10: Dafny **8 verified, 0 errors**, 16 receipt
identity decisions matched. Linux Go 1.26.5 race tests passed for the scoped
storage HTTP/identity cases and the entire `networkusage` package, including
bounded backlog reads, cancellation, raw-tail replay, and existing spool faults.
