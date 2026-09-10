# SUP-918: bounded protected evidence reads

`ProtectedCustodyReader.ReadVersion(ctx, claim, version)` returns verified bytes
and the exact custody receipt, or nil bytes and a zero receipt on every error.
The existing distinct-reader constructor still binds account, reader role,
expected producer namespace and explicit protected bucket policy/retention.
`VerifyVersion` is unchanged; neither method grants write or latest-read access.

The new path validates the claim against the configured per-object byte limit
(at most 64 MiB) before network requests or body allocation. It checks bucket
policy, versioning and retention, then explicitly pins both HEAD and GET to the
same version and expected owner. GET additionally uses HEAD's ETag. Both
responses must match version, length, producer metadata, applicable full-object
checksum and retention evidence; GET must agree with HEAD's ETag, modification
time and retention end. Missing/null versions and absent HEAD ETag fail closed.

Matching HEAD checksums never skip the body. The reader allocates at most the
claimed byte count plus one sentinel byte, verifies exact length and hashes the
bytes independently. Truncation, excess bytes, unexpected read errors, body
close errors and canceled contexts return no successful evidence. A 30-second
operation deadline includes policy checks and object reads. Cancellation closes
a blocked SDK response body; a sync.Once closure joins concurrent cancellation
and normal completion without a double Close. This relies on the HTTP response
body's Close unblocking Read; it does not claim arbitrary user implementations
of io.ReadCloser are interruptible.

Actual AWS SDK transport tests exercise the protected constructor, endpoint,
owner/version/ETag requests, adversarial response identity and checksum fields,
matching-header/wrong-body failures, truncation, oversize, configured cap,
closure failure, and both cancellation and deadline during blocked reading.
These finite tests do not establish live IAM or retention enforcement.

This is a per-object capability, not a manifest traversal service. A future
consumer must bound total object count and bytes, accept explicit trusted root
claims, verify every claim/final/part/raw edge and retain unresolved SIG session
mapping. No discovery scans, database writes, pricing, billing egress mapping or
complete-window upgrade are provided here; existing Complete=false facts remain
unchanged. The API returns source bytes, not authority over their interpretation.
