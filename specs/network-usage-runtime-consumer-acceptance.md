# SUP-922 actual runtime to retained consumer acceptance

`TestRuntimeProtectedClosingConsumerCreateAndUFFDResume` is an opt-in Linux
acceptance in the external `networkusage_test` package. It imports `fc`; two
constructors in `networkusage/export_test.go` expose existing private constructors
only to the test binary. Production construction still requires its authenticated
writer and reader. There is no deployment configuration or credential bypass.

The test creates the real protected Service before Process startup, with closing
enabled and explicit workload bindings. Real Create produces a preboot baseline,
guest ICMP positive control, snapshot and exported memory. Real terminal requests
(including replay) precede live/Paused and post-terminal negative ICMP checks,
Stop and JoinMetrics. A second Process uses the actual UFFD backend over that
exported memory, then repeats guest traffic and terminal shutdown. The two
processes have distinct execution, lifecycle and producer-incarnation identities.

JoinMetrics invokes the production journal close hooks. Service.Close owns the
actual delivery/closing passes. A bounded maximum of three passes, using unchanged
configuration and Close/reopen on ErrClosingPending, must produce two actual
terminal claim files. No test writes registration, intent, seal, membership, part,
final manifest or terminal claim controls. The test requires raw reclamation and
inventory retirement, then consumes each unchanged terminal claim through the
production consumer traversal and correlation replay. It requires a sealed result,
no gaps, verified baseline and terminal fences, positive per-device TX/RX, exact
workload metadata, Complete=false and SessionMapping=unresolved.

The retained backend is deliberately simulated. Separate writer and reader types
share an immutable local object directory. Writes retain exact object bytes and
receipts with exclusive creation; existing keys cannot change. The reader checks
the saved receipt, exact version, configured destination/key, file length and
content hash independently of the writer's verification methods and in-memory
index. A local unit test clears the writer index to exercise this independence.
An append-only transcript records operations and exact claims/versions. It never
contains credentials. This is not AWS role-separation, retention-policy or Object
Lock proof, and does not assert that the test backend survives power loss.

For each actual owner, missing and mismatched final-object and raw-object responses
must fail in fresh consumer directories without a receipt or output claim. These
eight faults change only read responses; all original objects remain preserved.
The successful consumer is closed and reopened with the reader unavailable.
Historical replay must return byte-identical serialized receipts with no new read
calls and unchanged input claim files. That historical replay does not renew
remote retention or authorize financial settlement.

Explicit bounds are: raw spool 32 MiB, 4 MiB segments and 16 segments; retained
objects 64 MiB/64 objects; transcript 1024 operations; inventory 4 MiB/128 entries;
closing controls 4 MiB/128 entries, 32 KiB parts/16 parts; consumer 64 objects,
4 MiB per object, 64 MiB total reads, 2 MiB records, 8 devices, 4 MiB metadata,
4 MiB receipt, 16 MiB output/64 entries and 10 seconds per traversal. The runtime
context is three minutes, cleanup has a separate bounded context, and the outer
isolated VM must still have a maximum ten-minute timeout, 4 vCPU and 4 GiB RAM.

## Build and invocation

Use the immutable exact-commit archive with the pinned Go image. Bind the archive,
command, source declaration and resulting binary hashes in the local proof:

```sh
go test -mod=readonly -c \
  -ldflags "-X github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/networkusage.consumerBuildRevision=$COMMIT" \
  -o /output/networkusage.test ./pkg/sandbox/networkusage
```

The test constructor calls the real `ConsumerBuildIdentity`. The runtime requires
its linker declaration to match `SUP922_EXPECTED_SOURCE`; this declaration is
not itself an attestation. The separate governed proof binds it to the source.

Inside the already proven disposable nested-KVM environment:

```sh
SUP922_KVM_ACCEPTANCE=1 SUP922_EXPECTED_SOURCE="$COMMIT" \
SUP922_ACCEPTANCE_DIR=/work/fixtures SUP922_ROOTFS_SHA256="$DERIVED_EXT4_SHA256" \
  /inputs/networkusage.test \
  -test.run '^TestRuntimeProtectedClosingConsumerCreateAndUFFDResume$' \
  -test.v -test.timeout 5m
```

Fixture layout follows the existing SUP-912 harness: `fc/firecracker`,
`kernel/vmlinux.bin`, and a derived `rootfs.ext4` containing `/sup912-init`.
The producer is pinned to SHA256
`bfe0e5276dcdc8614316335887d7c8555eb6544d707b359070e420f67f33f9f7` and guest
kernel to `643096c1fabf0fbbda1d03c100b9e86b4e6965fd5a01d3c6fb9e8c0ecb7fbfc9`.
Pin the ext4 hash from the retained derivation receipt. Runtime evidence is written
under a new `runtime-consumer` directory, whose pre-existence is an error. Inputs
must be mounted read-only with separate local guest work storage/output. Use the
existing complete userspace, KVM/TUN, required namespace privileges and exact UFFD
feature gate; do not substitute NoopMemory, change the producer or change host
sysctls. This test uses only `ns-922` inside the disposable environment. Package
initialization requires a default route even though the VM has no external
network access. Serialize this VM with SUP-921 and other runtime acceptances.

Retain every failed run, including its exit status and partial files. Final
acceptance requires the process exit code and Go test PASS in addition to
`result.json`, retained objects/receipts/transcript, original closing claims,
consumer output controls, negative-control errors, `consumer-checks.json`, input
hashes and the pinned host/L1/image identities. Ordinary local Go tests explicitly
skip the runtime when its opt-in is absent; that skip is not runtime proof.

This acceptance does not prove cgroup containment, live AWS enforcement, provider
billing equivalence, SIG session mapping, production deployment, scheduled handoff
or the useful-pod parent objective.
