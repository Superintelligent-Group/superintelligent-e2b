# Correlated producer emission (SUP-908)

## Scope and wire contract

This protocol identifies attempted metric frames from one configured Firecracker
process. It proves successful emission to its writer, not collector persistence,
network cutoff, complete lifetime coverage, or billing authority. The source and
build inputs are pinned in [the producer package](../third_party/firecracker-measurement/README.md).

`PUT /metrics` accepts optional `measurement_incarnation`. Omission preserves the
legacy unwrapped format. An incarnation is a caller-selected token of 1–128 ASCII
letters, digits, underscores or hyphens. The collector must select a fresh token
for every process and persist initialization evidence before admitting measured
device activity. The producer validates token syntax, not global uniqueness or
caller authority. Metrics configuration remains one-time per process.

An explicit request is:

```json
{"action_type":"FlushMeasurement","measurement":{"incarnation":"epoch_1","request_sequence":1,"request_id":"flush_1"}}
```

It is allowed before boot for baseline capture and after boot for observations.
Unconfigured or legacy-mode metrics reject it explicitly. The incarnation must
match. Request IDs follow the same token syntax; request sequences start at one
and advance exactly by one. Unknown fields and explicit null request payloads are
invalid. Legacy actions reject the measurement field.

On successful emission, HTTP 200 returns:

```json
{"schema":"sig.fc-measurement.v1","incarnation":"epoch_1","attempt_sequence":1,"request_sequence":1,"request_id":"flush_1","loss_detected":false}
```

The newline-delimited output frame wraps that identical header and the existing
metrics payload:

```json
{"measurement":{"schema":"sig.fc-measurement.v1","incarnation":"epoch_1","attempt_sequence":1,"request_sequence":1,"request_id":"flush_1","loss_detected":false},"metrics":{"...":"existing Firecracker fields"}}
```

Periodic writes and legacy `FlushMetrics` in opt-in mode use the same envelope and
attempt sequence with null request sequence and ID. All producers serialize under
the existing writer mutex. A collector must support the envelope before opt-in;
the current orchestrator reader is not implicitly compatible.

## State, retries and loss

Each attempted serialization first allocates a checked, strictly increasing
attempt sequence starting at one. Explicit request ordering is independent of
periodic attempts. Only the most recent explicit request and its outcome are
cached. Retrying that exact tuple returns its original receipt or error without
writing or consuming another delta, even if periodic frames intervened. Older,
conflicting, wrong-incarnation or nonconsecutive requests are rejected without
metric serialization. Callers must not advance their request sequence until they
have handled the previous outcome. The cache is in-memory and never spans a
process restart.

Serialization happens once into a byte vector, then `write_all` emits the complete
frame and newline, and `flush` completes before returning the receipt. The
serialized frame is limited to one MiB before its newline. That limit bounds
accepted frame size, not temporary serialization allocation. Existing dynamic
metric/device configuration determines allocation size.

Existing `SharedIncMetric` baselines advance during serialization. A serialization,
oversize, partial-write or flush failure therefore permanently sets the process
loss flag. This patch does not recover consumed deltas. Subsequent successful
frames expose `loss_detected:true`; failed explicit requests cache their failure.
Attempt overflow never wraps and permanently fails further emission. Request
sequence exhaustion rejects further new requests while exact last-request replay
remains available. Invalid caller requests do not themselves consume observations.
Consumers must preserve unsigned 64-bit sequence and counter values without lossy
floating-point conversion. Existing metric-counter arithmetic is unchanged; the
checked-overflow guarantee applies to measurement sequences only.

A flush failure may leave a syntactically complete frame in the destination, but
there is no successful explicit receipt for that attempt. A partial write may
leave raw bytes preceding another frame. Readers must preserve and reject malformed
or missing data, not resynchronize and declare complete. Abrupt process loss may
prevent any later sticky-loss frame: a missing terminal handshake remains
incomplete regardless of earlier apparently valid frames.

## Required downstream proof

SUP-909 must establish a terminal device cutoff before requesting the final frame.
SUP-907 must bind every contiguous frame to the incarnation, match the exact final
receipt, join FIFO consumption, sync and seal raw evidence, and verify protected
custody. HTTP success and EOF alone do not establish those properties. A reader
cannot declare a complete window from this protocol alone.

At the pinned source, network TX counts TAP-accepted frames including virtio-net
headers and excludes MMDS detours. RX includes TAP and MMDS reads, before some
rate-limited guest delivery. Pending unread RX and unsent TX need explicit cutoff
semantics. These measurements do not establish external host/provider wire bytes,
prices, or historical liabilities.

## Verification

The package runs actual Rust writer and API tests covering periodic interleaving,
concurrent emission, exact replay, conflicting requests, partial writes, flush and
serialization failures, oversize frames, checked overflow, uninitialized mode,
legacy behavior and HTTP response conversion. Its clean build checks the pinned
Cargo lock and produces an x86-64 musl release binary plus hash-bound local evidence.
KVM boot, snapshot and live traffic validation remain separate gates.

The [Dafny model](firecracker-measurement.dfy) proves bounded sequence transitions,
sticky failure, periodic preservation of the last requested outcome and replay as
an identity transition. It assumes the sink mutex's serialization and validated
request ordering; it is not a machine-checked refinement of Rust, token uniqueness,
counter conservation, persistence or lifetime completeness. Actual writer tests
exercise those implementation transitions, including failure after delta baseline
consumption and concurrent periodic/requested emission.
