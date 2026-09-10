# SUP-912: correlated measurement runtime

This slice connects the existing producer, durable correlator and owned FIFO
reader to actual Firecracker Process Create and UFFD Resume. It is disabled by
default and does not deploy the producer or authorize billing.

## Configuration and ownership

`NETWORK_USAGE_CORRELATED=true` requires an explicit absolute
`NETWORK_USAGE_SPOOL_DIR`, an empty legacy `NETWORK_USAGE_JOURNAL_DIR`, and
`NETWORK_USAGE_BINARY_SHA256` equal to the reviewed SUP-909 musl binary:
`bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7`.
The executable path captured while rendering the launch script is hashed before
launch. Updating the accepted pin requires identified build and runtime evidence.

Host budgets default to 1 GiB total, 4 MiB per segment and 1024 segments through
`NETWORK_USAGE_MAX_BYTES`, `NETWORK_USAGE_SEGMENT_BYTES` and
`NETWORK_USAGE_MAX_SEGMENTS`. Configuration rejects segments smaller than 2 MiB
because maximum-size raw frames expand when recorded as base64. Total bytes must
exceed segment bytes. Active collector count is also bounded by segment count.

One host Service obtains the spool lock and recovers retained files before serving
requests. Each collector has a fresh random incarnation. Close stops admission,
waits for every constructor and collector closure, then releases the spool lock.
A canceled close retains ownership and can be retried. There is no uploader or
acknowledgment/reclaim path in this Service. Capacity exhaustion is a hard failure,
not permission to delete evidence.

## Start, sample and stop

1. Validate the selected binary before launching Firecracker. Open the collector
   and FIFO reader synchronously. Persist baseline intent before configuring the
   producer's incarnation or requesting its baseline.
2. Use typed Unix HTTP requests for `FlushMeasurement` and
   `FinalizeMeasurement`. On an ambiguous transport failure retry at most once
   with the same incarnation, sequence and request ID. A 200 response is
   insufficient: the matching raw frame, receipt and correlation must be durable.
3. Persist activity intent before cold `InstanceStart` or `loadSnapshot`.
   Loading the snapshot can itself activate devices. A failed or canceled start
   remains incomplete and cannot later be reused for activity.
4. Serialize explicit samples, activity and terminal requests. Periodic ticks
   skip a busy action gate, preventing a slow load from becoming a spurious
   measurement timeout. Explicit balloon freshness waits for its exact frame
   and for inner telemetry consumption; it rejects requests during shutdown.
5. Normal Sandbox Stop obtains the terminal fence before signaling Firecracker,
   killing its cgroup, waiting for exit and joining reader persistence. Snapshot
   paths serialize pause/flush/snapshot with terminal initiation. They release
   that lock before ExportDiff, which can invoke Stop itself.

Raw newline-terminated producer frames are recorded before their inner metrics
feed existing OTEL and balloon accumulation. Producer attempts and journal
sequences remain distinct. EOF, partial frames, startup abort, missing terminal,
crash and interrupted joins cannot become complete evidence.

## Failure scope

Protocol, transport, guest and caller failures latch on one session. A separate
Process supervisor signals its child without joining from the FIFO callback;
the Sandbox supervisor performs the normal resource shutdown. These failures do
not poison other collectors or host admission. Actual spool write, short-write,
sync, seal and capacity failures broadcast through the shared Service. Existing
process supervisors stop their children, new collectors are rejected, and the
host shutdown select observes the persistent failed channel even if failure
occurred before that select began. Filesystem stalls cannot be made cancelable;
an interrupted persistence wait is explicitly incomplete.

## Verification and limits

The Linux race suite covers actual Unix HTTP/FIFO transport, exact retry after a
lost response, fenced balloon consumption, terminal sealing and spool reopen;
process failure isolation; shared failure supervision and admission rejection;
and spool budgets, concurrent ownership, recovery and injected IO failures.

The `TestMeasurementRuntime*` tests are opt-in through
`SUP912_KVM_ACCEPTANCE=1` and `SUP912_ACCEPTANCE_DIR`. A skipped invocation is not
runtime acceptance. The harness uses the pinned binary, kernel and local ext4
fixture in a disposable Linux KVM/TUN container, invokes actual Process Create,
exports guest memory, loads it through actual UFFD Resume, and checks guest ICMP
before and after terminal cutoff while the VMM remains alive and paused.
Create/terminal and actual tiny-spool exhaustion are separately named tests, so
their results cannot stand in for Resume acceptance. The budget scenario starts
guest traffic, fills an actual 64 KiB spool, observes process termination and
zero subsequent ICMP replies, checks rejected admission, then reopens retained
segments without claiming a terminal fence. Preserve input hashes, test output
and the reopened local evidence alongside the exact source archive used to build
the test binary.

The pinned producer requires UFFD `EVENT_REMOVE | MISSING_HUGETLBFS | WP_ASYNC`.
Successful `userfaultfd()` alone is insufficient. The local WSL kernel
`6.6.87.2-microsoft-standard-WSL2` reports supported features `0x7fff`; requesting
the producer's `0x8018` returns `EINVAL`. Actual snapshot Resume consequently
fails at UFFD object creation on that host. This is an open acceptance gate,
not permission to remove the producer requirement or to count a skipped test.

Reproduction helpers live in `scripts/network-usage-runtime/`. Prepare the rootfs
with `prepare-rootfs.sh` using the pinned SUP-909 assets mounted read-only at
`/assets` and a fresh writable directory at `/out`. Build `fc.test` from the exact
source archive with Linux Go and `GOWORK=off go test -mod=readonly -c
-o /output/fc.test ./pkg/sandbox/fc`. The runtime helper expects read-only mounts
at `/scripts` (the helpers), `/assets` (kernel), `/binary` (reviewed producer),
`/derived` (prepared rootfs), `/build` (test binary), and a fresh writable
`/output`. Use the pinned SUP-909 `fcuvm` image, `--network none`, explicit KVM/TUN
devices, `SYS_ADMIN`, `SYS_PTRACE`, `NET_ADMIN` and `seccomp=unconfined` for this
disposable test container. It adds a dummy default route only in that container
for the existing network package's initialization. It changes no host sysctl.
The helper records the UFFD probe, input hashes and test log. An explicit
`SUP912_TEST_PATTERN` can select individual scenarios; retain that selection in
the proof and do not represent it as the complete suite.

This is local device-observation evidence. It does not prove production cgroup
containment, authenticated remote producer custody, protected retention,
scheduled delivery, provider byte calibration, complete billable windows,
prices, settlement, or useful governed-pod acceptance. No record is upgraded to
`Complete=true`, and no infrastructure configuration is activated by this slice.
