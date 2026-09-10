# SUP-911: durable producer correlation

This inactive, production-callable API connects the pinned SUP-908/909 wire
protocol to the existing [journal](network-usage-journal.md) and
[bounded spool](network-usage-spool.md). It does not select a Firecracker binary,
configure metrics, start work, make HTTP requests, deliver remote custody, or
change prices. Every journal row retains `complete=false`.

## Owner integration contract

1. Open a non-disabled fresh `Journal` or `OpenWithSpool`; pass it and a fresh
   producer token to `NewCorrelator`. This writes/syncs a producer binding and
   exclusively claims the journal. Producer incarnation is separate from its
   random local journal incarnation. Reattaching a previous journal is unsupported.
2. `Begin(id, BaselineScope)` durably records the request tuple before returning
   it to the transport. Configure the same producer incarnation and send the
   returned tuple with `FlushMeasurement`. Begin must precede any expected frame.
3. Pass exact newline-terminated FIFO bytes to `ObserveFrame` and successful
   response JSON to `ObserveReceipt`. Either may arrive first. `Fence` returns
   nil while either half is missing; only a matched, written and synced
   `producer_correlation` record makes a locally durable fence available.
   A successful callback alone is not a matched-fence acknowledgment.
4. Immediately before `startVM` or `loadSnapshot`, call `BeginActivity`. It
   requires a matched baseline and persists activity intent; it does not claim
   that the guest actually started. During slow preboot configuration, periodic
   no-device frames remain explicitly scoped `preboot_no_devices`, even after
   baseline acknowledgment. They do not count absence or aggregate zero as
   observed device traffic. Device requests require this activity transition.
5. `Begin(id, SampleScope/TerminalScope)` selects the next consecutive request;
   TerminalScope uses `FinalizeMeasurement`. There is one outstanding request.
   Calling Begin again with its same ID/scope returns the exact existing tuple,
   including after a lost response. Do not issue another requested action until
   resolved. The latest completed identity also replays without writing again.
   IDs may recur after intervening requests: identity includes sequence, and no
   unbounded global ID set is maintained.
6. Reader/transport failures use `Correlator.Gap`; its `Close` records unresolved
   requests and closes the journal. Direct legacy Observe/Gap are rejected on an
   owned journal. External Journal.Close remains safe, and subsequently prevents
   a correlator fence. Terminal frames prohibit further frames immediately,
   including when the terminal HTTP response is still outstanding.

The lifecycle owner must enforce deadlines, consume the FIFO, serialize actual
requested transports, and keep identities stable while retrying. This module
has no goroutine, timer, or network client. It does not recover an in-flight
request into a new process. Recovery preserves existing spool bytes as evidence;
a new producer process needs a new journal and incarnation.

## Wire and persistence boundaries

`DecodeProducerFrame` accepts JSON of at most 1 MiB plus exactly one terminal
newline; transport scanners must allow that extra byte. Receipt JSON is bounded
at 2048 bytes. UTF-8, paired Unicode escapes, nesting depth (64), duplicate keys
at every level, required header fields (including explicit false/null), unsigned
integer bounds, token syntax, schema and cutoff are validated. Unknown protocol
envelope/header fields are rejected; unknown metrics fields are preserved for
telemetry compatibility. Per-device `net_*` objects and aggregate `net` counters
must contain typed TX/RX fields when present. Network byte totals use aggregate
per-flush deltas once, without subtracting successive frames or converting via
floating point.

All frames must have consecutive attempt sequences starting at one and the
configured incarnation. A requested frame must match the pending request tuple;
receipts must match every header field and terminal cutoff. Loss flags, malformed
or partial frames, foreign/duplicate/reordered frames, counter or sequence
exhaustion, postterminal data, mismatches, or persistence failure make subsequent
fences unavailable. Bounded rejected raw bytes remain in a gap record where the
journal can still write; an oversized input records its length without pretending
all bytes were preserved. A failing journal cannot claim a durable gap receipt.

Raw frames retain their newline and are encoded as base64 in the journal. Each
has its exact byte length and SHA-256. Correlation records contain raw receipt
bytes with their own hash/length and reference the frame's local journal sequence,
hash and length. Header producer sequences never replace local journal sequences.
The existing full-write + file Sync protocol is the only publication boundary;
clock regression remains invalid. Spool limits charge the actual JSON/base64
encoded bytes, so operators must budget for the expansion. A single request,
its header, and one bounded receipt are held in memory; frames are not cached in
an unbounded map.

`DurableFence` means locally persisted correlation only. It does not establish
complete stream closure, protected custody, provider-billable bytes or settlement.
An invalid later observation makes future Fence calls fail; previously returned
references remain historical evidence, not an authority to set complete=true.

## Evidence and tests

Fixtures in `networkusage/testdata/sup909-*` originate from the actual local
SUP-909 booted KVM/TAP run. Frames are byte-for-byte raw capture. Successful
receipt objects are extracted from the captured API log and re-encoded without
changing values; original HTTP formatting is not claimed. The provenance JSON
records raw/API hashes, producer binary hash, harness hash and run timestamps.
No credentials or guest content are included.

| Invariant | Executable tests |
|---|---|
| Exact raw preservation, all-header receipt matching, either arrival order, no duplicate replay accounting | `TestProducerPinnedFramesBothOrdersAndDurableRaw`, `TestProducerConcurrentCallbacksAndFenceIsolation` |
| Explicit baseline/activity gates and exclusive ownership | `TestProducerRequestOwnershipAndExplicitActivity` |
| Terminal frame closes stream before response | `TestProducerTerminalFrameClosesStreamBeforeResponse` |
| No fence before full write/Sync, including intent/frame/correlation/activity failures | `TestProducerDurabilityFailuresNeverAcknowledge` |
| Exact u64, missing/null/duplicate/foreign/loss/partial data and size limits | `TestProducerParsingRejectsAmbiguityAndRequiredFields`, `TestProducerBoundsUnknownFieldsAndAbsentPreboot`, `TestProducerGapsPreserveBoundedRawAndInvalidate` |
| Overflow, mismatches, close and clock invalidity are sticky | `TestProducerReceiptConflictsAndCounterOverflow`, `TestProducerCloseAndExternalJournalFailureInvalidateFence` |
| Existing spool byte budget includes base64 evidence | `TestProducerSpoolBudgetAndRawRoundTrip` |

Run `GOWORK=off go test -race -count=1 -mod=readonly ./pkg/sandbox/networkusage`
from `packages/orchestrator` under pinned Go 1.26.5. The linked
[abstract model](network-usage-correlation.dfy) verifies the correlation kernel's
publication/barrier invariants; it assumes truthful durability outcomes and is
not a universal Go refinement proof. Production runtime activation belongs to
the separately owned reader/lifecycle and transport integration.
