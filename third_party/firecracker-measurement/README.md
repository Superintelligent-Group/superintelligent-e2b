# Correlated and terminal Firecracker measurement producer (SUP-908 / SUP-909)

This directory carries an additive, opt-in Rust patch against the exact upstream
source named in `source.json`. It does not select a production Firecracker version,
upload binaries, change templates, or activate collection. The upstream repository
is an input only; no upstream fork or publication is needed.

The patch gives a collector an incarnation-bound frame sequence, an exact explicit
flush receipt, bounded retry of the last request, and sticky loss after failed
serialization or emission. Legacy configuration retains the existing API and frame
format. See [the protocol specification](../../specs/firecracker-measurement.md).
The cumulative patch also includes the [terminal extension](../../specs/firecracker-terminal-measurement.md):
an irreversible VMM cutoff, a distinct final receipt, and blocked device restart.

## Local build and evidence

With Docker running, local KVM/TUN device access, and a clone containing the pinned
upstream commit:

```powershell
node third_party/firecracker-measurement/build.mjs C:/Github/upstream-e2b-firecracker C:/existing-artifacts/SUP908-build-1
```

The output directory must not exist. The script checks the upstream tree and Cargo
lock, checks the reviewed patch hash, archives the exact upstream commit, and builds
inside the digest-pinned upstream Linux development image. Local uncommitted source
edits do not enter this build. It runs formatting and focused actual Rust writer,
legacy metrics, request parser, and HTTP response tests, then builds the Linux x86-64
musl release binary with `--locked`. It also runs actual terminal adapter tests in
the binary target and explicit KVM/TUN regressions. Dependency downloads require network access.
Build output is streamed to `build.log` in the output directory. Immutable inputs
and writable artifacts use separate mounts; the container cannot write the input
archive or patch through its artifact directory.

`receipt.json` is written only after success and binds source, patch, builder image,
build script, archive, binary and log hashes. Raw test logs and `validation.json`
remain in `artifacts/` alongside the binary. This build receipt is not a signed repository proof;
the reviewed E2B commit also needs the governed local proof and source review.

The container receives `/dev/kvm`, `/dev/net/tun` and `NET_ADMIN` for the runtime tests;
its TAP interfaces stay in its own network namespace. It receives no host credentials
or Docker socket. These are local unit/build checks, not booted-guest cutoff,
snapshot, provider network calibration, or deployment acceptance. This recipe targets
x86-64 only. A production change requires separately
identified binary selection, runtime compatibility tests and governed rollout.

## Remaining boundaries

An emitted frame is not a collector fsync, protected remote version, network cutoff,
or complete lifetime receipt. SUP-909 implements a terminal device-observation cutoff
as inactive source; one local booted-guest scenario has passed. SUP-907 owns
start readiness, durable joined collection and protected custody.
Any missing sequence, loss, forced exit or failed persistence keeps the window
incomplete. Existing network counters do not equal external/provider wire bytes or
billable bytes. No pricing or historical settlement authority is introduced.

The upstream source and patch carry Apache-2.0 licensing; upstream copyright and
license headers are preserved in the archived source and patch.

## Optional local guest acceptance

`boot_acceptance.py` runs separately from the build. It uses only Python's standard
library, the pinned builder image's `ip` tool, local KVM/TUN, and the exact binary
SHA-256. No SSH credentials or rootfs rewrite is needed. The guest starts a shell
from a read-only squashfs image; its TAP has no external route.

Fetch the two public objects under bucket `spec.ccfc.min`, prefix
`firecracker-ci/v1.14/x86_64/`, named in `boot-assets.json`. Keep response metadata
and require the exact listed VersionIds, lengths and SHA-256 hashes before copying
that manifest to the asset directory as `manifest.json`. Anonymous version-specific
GET may be denied; a current GET is acceptable only with a matching HEAD ETag
condition and returned VersionId, followed by the same byte/hash checks. Do not
silently replace the pinned assets. The download is approximately 147.5 MB.

Run the harness inside a disposable container with `--network none`,
`--device /dev/kvm --device /dev/net/tun --cap-add NET_ADMIN`, and the exact builder
image digest from `source.json`. Mount the harness as `/harness.py`, the asset
directory as `/assets`, and the built binary directory as `/binary`, all read-only.
Mount a separate writable evidence directory as `/evidence`; it must not expose
any input through a writable parent mount. Invoke:

```text
/opt/venv/bin/python3 /harness.py --binary /binary/firecracker --sha256 <receipt binary_sha256> --assets /assets --out /evidence/new-run
```

The run directory must be new. Retain `result.json`, `api.json`, `metrics.raw` and
`serial.log`. Success requires real guest execution and traffic, a matched terminal
frame/replay, exact latch errors, no post-cutoff replies or emissions, and a still
live paused process at the observation endpoint. The final raw stream is checked
again after process termination and reader joins. This does not replace the
orchestrator's durable collection or protected custody gates.
