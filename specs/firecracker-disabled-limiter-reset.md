# SUP-925: replace saved disabled rate-limit buckets

`RateLimiterConfig` is the current desired configuration at the production Resume
callers in `fc/process.go`. A negative `BucketSize` documents a disabled bucket.
Previously the PATCH helpers used the initial-creation builder, omitting each
disabled bucket and sending `{}` when both were disabled. That preserves a saved
bucket in the pinned producer instead of clearing it.

The pinned source is `e2b-dev/firecracker` commit
`431f1fc7d47f4cfe1dfe42437b9394f20972b65d` plus the unchanged measurement patch.
`src/vmm/src/vmm_config/mod.rs:80-126` defines `ops` and `bandwidth` and converts
absent buckets to `BucketUpdate::None`; explicit zero size/refill converts to
`Disabled`. `rate_limiter/mod.rs:485-495` leaves `None` untouched. Network updates
use that conversion at `rpc_interface.rs:1001-1013`; drive updates use the same
conversion at `:979-993`. No VM reproduction has yet been performed for this leaf.

The correction is PATCH-specific: normalize each negative bucket independently
to a zero-valued bucket, then use the existing generated model builder. Clear
stale burst/refill values as part of disabled normalization. Preserve every
nonnegative configured bucket, caller values, initial-creation omission semantics,
RX settings, drive paths, admission guards, budgets and defaults.

`TestRateLimiterPatchGeneratedWire` calls both production helpers over a local
Unix socket through the actual generated SDK. It checks exact request methods,
paths, device identities and serialized independent buckets for both disabled,
both enabled, each mixed direction and explicit zero. The creation regression
checks that omitted buckets continue to be omitted. These are wire-contract
tests, not a substitute for observing a saved limiter reset in the pinned VM.

Before claiming runtime acceptance, a separately authorized serialized fixture
must save nonzero TX and drive limits, load that exact snapshot, use the actual
Resume configuration, and independently read back the resulting buckets for
disabled and mixed cases. Bind source/binary/snapshot/requests/readback and retain
failures. Nested-guest instability currently keeps that VM gate open. No cloud
or economic policy mutation is part of this change.
