# SUP-906: protected raw custody prerequisite

This optional source slice extends [SUP-903 custody](network-usage-custody.md).
It is inactive: no startup configuration, scheduler, AWS resource, bucket lock,
or policy was activated. `network_evidence` defaults to null. There is no
production retention default and no claim of complete measurement or pricing.

## Contract and trust boundary

`ProtectedCustodyConfig` requires an explicit account, region, bucket, prefix,
producer role ARN and immutable role ID, retention days, and operator-approved
canonical policy SHA-256. `NewProtectedAWSCustody` checks STS account, role and
EC2-form session identity, then checks the live policy hash, enabled versioning,
and the exact configured GOVERNANCE default retention. Every operation rechecks
bucket evidence. An arbitrary policy hash supplied by an untrusted caller is
not an authorization proof: operators must approve the reviewed module output.

The optional dedicated-bucket Terraform module enforces conditional creates,
the producer's `${aws:userid}` namespace, and presence of `ec2:SourceInstanceARN`.
STS session spelling alone is insufficient. This identifies the origin of EC2
role credentials; it does not prove physical execution location or protect
against theft of those credentials. Cross-namespace, non-EC2 and ambient-policy
widening attempts are explicitly denied for producer and reader roles. Neither
role can change bucket configuration, delete versions, shorten retention, or
bypass governance. Separately privileged account administrators remain a trust
boundary and can change policy; this is not protection against an account admin.

Retention days and later lifecycle expiration days must both be chosen
explicitly. HEAD must return a non-null version, GOVERNANCE mode, and an unexpired
retain-until date at least the configured duration after Last-Modified. Exact
length, checksum or bounded raw readback, and producer metadata must match before
a receipt can authorize local acknowledgment. Existing restart/lost-response
delivery replay uses conditional create and verified custody before local deletion.
Raw incomplete tails remain incomplete evidence after upload.

`NewProtectedCustodyReader` uses a distinct reader role and immutable role ID,
with an explicitly expected producer UserId. It exposes only `VerifyVersion`:
non-null version is pinned through HEAD and fallback GET, alongside expected
owner, namespace, metadata, raw bytes and retention. IAM restricts body reads to
`GetObjectVersion`; AWS does not support `s3:VersionId` conditions on
`GetObjectRetention`. Retention metadata permission therefore covers the prefix
without that condition; the consumer still pins its HEAD/GET requests. This
metadata permission does not grant unversioned object-body reads.

## Evidence

- `packages/shared/pkg/storage/custody_protected_test.go`: identity substitution,
  config/policy mismatch, suspended versioning, missing/short/expired retention,
  and exact-version response failures.
- `custody_reader_test.go` in the same directory: independent constructor with
  current rendered policy evidence, producer namespace binding, exact-version
  HEAD/GET, and wrong version, producer, bytes, or expired retention rejection.
- `scripts/local-proof/network-producer-policy.test.mjs`: evaluates the actual
  policy template's supported IAM subset, including explicit-deny precedence
  against ambient grants. This is not an AWS IAM simulator or live canary.
- `iac/provider-aws/modules/network-evidence/tests/policy.tftest.hcl`: mocked
  Terraform plans, explicit duration/account and distinct-role rejection.
- Independent AWS Access Analyzer validation of the corrected rendered policy
  reported zero errors and one intentional literal `if-none-match` equality
  warning. The external review receipt is
  `.proof/SUP906-policy-review-corrected.json` in the framework worktree, SHA-256
  `efa4d6db573c861aacecb4a76bb2725e199cff159989aad70cb6d871a24aaf1c`.
  Static policy validation does not prove effective deployed permissions.

## Next completion gate

SUP-907 owns the orchestrator/runtime closing barrier: initialize the metrics
sink before running, own flushing and periodic serialization, quiesce network
activity, establish an exact final successful frame, sync/seal it, and record
explicit terminal incompleteness on failure. The pinned Firecracker source can
discard an uninitialized metrics-write result and advance per-field deltas
before a whole frame/newline succeeds; HTTP success is not a final-frame proof.
The runtime scheduler must also act on spool budget exhaustion and drain bounded
delivery passes. This prerequisite alone does not stop network activity.

Before activation, an authorized operator must select identities and durations,
review/apply protected infrastructure, and run a live negative canary: a second
host cannot write the first host's prefix, non-EC2 sessions fail, reader mutation
and latest-body reads fail, and retained versions survive denied deletion and
are readable by exact version after producer restart. Only the runtime barrier,
durable custody and independent ingestion together can establish an identified
measured window. Provider-billable pricing still requires separate artifact and
provider evidence.
