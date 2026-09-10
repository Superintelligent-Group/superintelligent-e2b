# Correlated Firecracker measurement producer (SUP-908)

This directory carries an additive, opt-in Rust patch against the exact upstream
source named in `source.json`. It does not select a production Firecracker version,
upload binaries, change templates, or activate collection. The upstream repository
is an input only; no upstream fork or publication is needed.

The patch gives a collector an incarnation-bound frame sequence, an exact explicit
flush receipt, bounded retry of the last request, and sticky loss after failed
serialization or emission. Legacy configuration retains the existing API and frame
format. See [the protocol specification](../../specs/firecracker-measurement.md).

## Local build and evidence

With Docker running and a local clone containing the pinned upstream commit:

```powershell
node third_party/firecracker-measurement/build.mjs C:/Github/upstream-e2b-firecracker C:/existing-artifacts/SUP908-build-1
```

The output directory must not exist. The script checks the upstream tree and Cargo
lock, checks the reviewed patch hash, archives the exact upstream commit, and builds
inside the digest-pinned upstream Linux development image. Local uncommitted source
edits do not enter this build. It runs formatting and focused actual Rust writer,
legacy metrics, request parser, and HTTP response tests, then builds the Linux x86-64
musl release binary with `--locked`. Dependency downloads require network access.
Build output is streamed to `build.log` in the output directory. Immutable inputs
and writable artifacts use separate mounts; the container cannot write the input
archive or patch through its artifact directory.

`receipt.json` is written only after success and binds source, patch, builder image,
build script, archive, binary and log hashes. Raw test logs and `validation.json`
remain in `artifacts/` alongside the binary. This build receipt is not a signed repository proof;
the reviewed E2B commit also needs the governed local proof and source review.

The container receives no host credentials, Docker socket, or KVM device. These are
local unit/build checks, not boot, snapshot, network calibration, or deployment
acceptance. This recipe targets x86-64 only. A production change requires separately
identified binary selection, runtime compatibility tests and governed rollout.

## Remaining boundaries

An emitted frame is not a collector fsync, protected remote version, network cutoff,
or complete lifetime receipt. SUP-909 owns a terminal device-observation cutoff;
SUP-907 owns start readiness, durable joined collection and protected custody.
Any missing sequence, loss, forced exit or failed persistence keeps the window
incomplete. Existing network counters do not equal external/provider wire bytes or
billable bytes. No pricing or historical settlement authority is introduced.

The upstream source and patch carry Apache-2.0 licensing; upstream copyright and
license headers are preserved in the archived source and patch.
