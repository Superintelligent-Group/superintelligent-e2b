# SUP-919: explicit retained evidence consumer

This optional API consumes one explicitly supplied SUP-914 host-local terminal
claim using the distinct authenticated SUP-918 byte reader. It performs no bucket
discovery, scheduled handoff, database operation, pricing, usage publication,
retention mutation or deployment. `Complete` is always false and `SessionMapping`
is always `unresolved`. Factory ExecutionID and LifecycleID remain E2B metadata;
they are never interpreted as SIG execution_session_id.

## Construction and source provenance

`networkusage.OpenProtectedEvidenceConsumer(ctx, outputDirectory, options,
readerConfig)` validates explicit budgets and constructs only
`storage.NewProtectedCustodyReader`. The existing protected-reader account,
distinct role, expected producer, policy and retention checks remain required.
The test-only package-private interface supplies deterministic exact-version
objects; it is not a production producer-credential fallback.

The verifier identity comes from `ConsumerBuildIdentity()`. Clean Go VCS build
metadata is supported, but is not assumed to exist in an archive build. The
supported archive build path embeds the exact archived commit with the linker:

```sh
go build -mod=readonly \
  -ldflags "-X github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage.consumerBuildRevision=$SOURCE_COMMIT" \
  ./path/to/the/calling/program
```

The calling program must import and use the consumer API. This repository adds
no CLI or production startup wiring. The source value is a build declaration,
not a runtime config field. The receipt labels its provenance
`linker_embedded_source_declaration`, never a verified source attestation. A
governed build must separately bind the source archive, linker argument and
compiled artifact. Without an embedded declaration, a full clean VCS revision is
required and labeled `go_build_vcs_metadata`. Neither label authenticates the
deployment or proves operator approval.

The exact-source test gate sets the same linker symbol and
`SUP919_EMBEDDED_PROVENANCE=1`, with `SUP919_EXPECTED_SOURCE` equal to the archived
commit. `TestConsumerEmbeddedBuildDeclaration` calls the same public identity
function used by the constructor. Ordinary unconfigured test runs explicitly
skip that gate; they are not embedded-build proof.

The caller supplies a dedicated existing host-owned output directory and calls
`Consume(ctx, terminalClaimBytes)`. Only a successful return acknowledges durable
output. `Close(ctx)` joins any active call before releasing the exclusive output
lock; a canceled close retains ownership for retry.

## Exact bounded traversal

Traversal is claim -> exact final manifest -> indexed exact manifest parts ->
exact raw segment versions. Every returned destination, key, claim name, SHA256,
length and version must equal its parent reference and the configured reader.
Null, empty and `latest` versions are rejected. Object keys cannot repeat across
the traversal; raw membership cannot overlap, duplicate or change sequence order.
Frozen gaps, workload/producer identities and bounded baseline/terminal summaries
must agree with actual membership and independently described raw bytes.

`ConsumerOptions` independently bounds claim input bytes, per-object bytes,
aggregate object count/bytes, metadata payload bytes, per-record bytes, device
count, receipt bytes, retained output bytes/entries and operation duration.
Final/part claim lengths are reserved against metadata capacity before reads or
decoding. Retained object receipt payloads are charged too. Counts and string
lengths are separately constrained. Metadata limits describe logical JSON
payload, not an exact Go heap bound: decoded structures/parser overhead are
additional, bounded by the payload, field counts, nesting and object limits.

One raw object is held at a time. Before even summary decoding, newline-delimited
record lengths, partial-tail length and context are checked. The record cap also
applies to unidentified evidence, even when semantic replay cannot begin. The
verifier retains bounded manifest/receipt metadata and one comparison record,
one pending requested-frame reference, and bounded per-device totals. It does
not accumulate all journal records or a request history.

Strict JSON validation rejects duplicate/unknown fields, missing required
fields (including false/zero scalars), invalid UTF-8/surrogate escapes, unsupported
schemas, null scalar substitutions, floating integer encodings and noncanonical
journal decimal strings. Integer values never pass through float64. Existing
`DecodeProducerFrame` and `DecodeProducerReceipt` preserve the producer protocol
checks and exact raw hashes/lengths.

## Replay, deltas and uncertainty

A one-record comparison sink lets the existing Correlator and Journal regenerate
each successful retained transition, including counters and all request/header/
frame-reference fields. The retained timestamp supplies the replay clock; all
generated fields must match. API receipt arrival before its frame has no separate
journal record: the exact receipt bytes are retained at correlation, after the
frame. Replaying those bytes at correlation verifies the same durable state
without inventing API arrival timing. Tests generate frame-first and receipt-first
paths through the actual SUP-914 Service.

Per-flush `net_*` device deltas are decoded as uint64, checked for overflow, and
summed once per device. Their sum must equal the frame's `net` aggregate; the
aggregate is never added again. Journal cumulative and delta fields are checked
by production replay. Pre-activity frames do not contribute to activity totals.

`Prefix` describes only the verified contiguous activity prefix. Its lower
baseline, first/last counted record and optional upper terminal identify the
accounting boundary. At the first gap, missing sequence, invalid record or partial
tail, totals stop permanently. Later exact raw objects remain evidence, but
cannot extend totals or become verified prefix fences. `RawBaseline` and
`RawTerminal` are independently identified raw correlations; they are deliberately
separate from verified prefix fences. Structural identity, timestamp, device
budget and raw reference checks continue after a gap.

Missing terminal, missing closed record, unknown/failed seal, unidentified
registration and partial tails remain explicit gaps. A claimed successful seal
with an observed predecessor must bind the actual pre-closed intent sequence.
`SealOutcome` preserves the manifest claim; gaps expose missing supporting raw
records. An unidentified registration never acquires workload authority or
device totals from guessed association. The named terminal cutoff remains
`last_completed_device_operation`, not a statement of provider-billable coverage.

## Durable output and restart

The output slot hashes stable account/region/bucket/producer namespace and final
object key. It excludes operational object caps. An immutable `.binding` in that
slot binds the full exact root claim and verifier identity. A different root
version/hash, destination configuration or verifier source in the same slot is
an explicit conflict requiring separate migration; it cannot choose a new slot
to hide replacement.

The deterministic `.receipt` contains verifier identity, exact root/object
identities, workload/producer metadata, prefix/fences, decimal-string device
totals, cutoff/outcome, gaps and unresolved mapping. It contains no current-time
field that would change exact replay. A third immutable `.claim` binds its exact
serialized hash and length. Only after that claim is synced can Consume return
the receipt. Mutating any saved total, object identity, gap or fence therefore
fails historical replay.

Each control uses exclusive temporary creation, full write, file sync, rename
and directory sync under one owner. Any local failure, including a filesystem
timeout, is absorbing for that consumer instance; it cannot create more controls
or acknowledge visible-but-unsynced output. Recovery validates bounded regular
controls, removes only recognized temporary files and syncs the directory before
replay. A receipt without its output claim must be reconstructed from exact
remote objects and compared before a new claim can acknowledge it. A complete
existing claim returns the historical receipt without claiming refreshed remote
retention. Source objects are never deleted or changed.

Retained binding/receipt/claim contents consume separate output quotas. No
automatic eviction occurs. Directory metadata overhead is bounded by entries,
not counted as logical payload bytes. Read cancellation and cooperative deadline
checks prevent acknowledgment; synchronous filesystem IO and JSON decoding are
not falsely advertised as interruptible. A returned receipt is freshly decoded,
so caller mutation cannot affect stored bytes or later results.

## Proof limits

Tests consume actual SUP-914-generated chains through a fake exact-version
reader, then rebuild parent hashes around hostile raw data to exercise semantic
verification. They cover durable replay/source conflicts, receipt mutation,
missing fences, partial tails, gap-stopped totals, duplicates/reordering/loss,
exact integers/device overflow, read/metadata/record/output bounds, cancellation
and output persistence faults. Live reader authorization/retention remains the
SUP-918 and activation gate; a fake reader does not establish it.

The adjacent Dafny model abstracts exact identity, checked deltas, context and
successful durability boundaries. It proves ordering, prefix and budget
properties of that model, not the Go implementation, filesystem, build metadata,
provider retention, calibration or complete-window coverage. Exact-source race
tests and independent review remain necessary. No hosted Actions are proof.
