# Terminal device-observation cutoff (SUP-909)

This extends [correlated emission](firecracker-measurement.md) with an irreversible
producer cutoff. The cumulative pinned patch and clean build recipe live in
`third_party/firecracker-measurement`. Runtime binary selection remains unchanged.

## Finalization

`PUT /actions` accepts `FinalizeMeasurement` with the same incarnation, request
sequence and request-ID object as `FlushMeasurement`. Finalization is post-boot
only. It validates a fresh, consecutive request before changing runtime state: an
ordinary cached flush cannot be reinterpreted as terminal evidence.

Under the producer writer lock, the implementation reserves the terminal identity,
latches the VMM irreversibly and requests vCPU pause. It then emits the identified
frame only after pause acknowledgment. The frame includes
`terminal_cutoff: "last_completed_device_operation"`; the API returns a distinct
`sig.fc-terminal-measurement.v1` receipt containing the matching measurement header
and cutoff. Existing loss remains visible in that header.

The API adapter remains in its blocking API-only loop whenever the authoritative
VMM state is paused or terminal. It cannot return to the device EventManager merely
because a Resume request arrived. The VMM itself also rejects resume before
`kick_virtio_devices`, protecting direct callers as well as API dispatch.

Exact terminal request replay returns the original receipt or error. Other terminal
identities, ordinary flushes, periodic/signal writes and post-terminal mutations are
rejected. The narrow API allowlist permits only terminal retry and selected instance,
version and machine-configuration reads. Snapshots must be taken before finalization;
they are not allowed to reopen a terminal incarnation.

Any failure after the latch remains irreversible. Pause uncertainty, partial output
or writer failure cannot turn into a later successful retry. The runtime must stop
the process within its shutdown bounds, join and preserve collected evidence, and
record an incomplete window. A disconnected vCPU event channel returns an error
before fd access/signaling rather than panicking while the metrics lock is held.
That avoids recursive acquisition by the production panic hook on this path.

## Count scope and remaining gates

The cutoff is the last completed device operation before entering the API callback;
it does not drain queues. Rate-limited unsent TX and unread TAP RX are excluded.
Previously read/deferred RX has already been counted, even if guest delivery is
pending. These are the pinned producer's existing device-observation semantics,
including virtio headers and RX MMDS traffic; they do not equal provider wire bytes.

A terminal producer receipt alone does not prove collection began before activity,
that every preceding frame was persisted without gaps, or that evidence reached
protected custody. SUP-907 owns those runtime and collector conditions. A receipt
with sticky loss or missing durable evidence must remain incomplete. Neither this
patch nor its build changes pricing, historical holds or production configuration.

## Verification boundaries

Actual writer tests cover terminal freshness, exact success/error replay, failure
irreversibility, and concurrent periodic/finalization ordering. Production adapter
loop tests cover rejected Resume without returning to EventManager. Separate actual
KVM/TUN tests connect terminal VMM state to controller freezing and direct resume
rejection, including a queued-net positive control and disconnected VcpuHandle.
The latter tests run explicitly with `--ignored`; an ignored declaration is not
treated as a pass. The clean build requires those test executions to succeed.

The [terminal state model](firecracker-terminal-measurement.dfy) describes irreversible
latching and receipt outcomes. It assumes serialized requests and the stated pause/
dispatch contract; it is not a Rust refinement proof. The adapter and VMM tests are
complementary, not a single integrated same-epoll-batch test.

A separate local booted-guest acceptance run passed on 2026-09-10 using binary
`bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7`.
The [reproducible harness](../third_party/firecracker-measurement/boot_acceptance.py)
booted the pinned read-only Linux rootfs, captured baseline before boot, verified
guest traffic and three host ICMP replies, then finalized amid active traffic.
Exact terminal replay and latch-specific Resume/flush/snapshot rejection passed.
Post-cutoff probes received zero of three replies; Firecracker remained alive and
its allowed instance-info read confirmed `Paused` at the end of the observation.
After process shutdown and joined FIFO consumption, four contiguous, loss-free
frames remained, with the terminal frame last and unique.

This is one local producer acceptance scenario. Integrated same-epoll-batch race
coverage, production runtime persistence, protected custody and provider traffic
calibration remain separate gates. No production rollout is implied.
