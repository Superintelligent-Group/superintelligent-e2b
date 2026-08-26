# Canonical runtime DNS cutover

This is the bounded handoff between the canonical CQ E2B runtime and its public
Nomad endpoint. It is intentionally separate from a full Terraform apply: a
DNS cutover must not also change jobs, capacity, or provider state.

## Preconditions

1. The canonical control-plane and build ASGs use the same VPC and account.
2. The canonical control-plane Consul catalog contains `nomad-server`.
3. The canonical Nomad target group is healthy on port `4646`.
4. Build and client ASGs are at desired/minimum capacity zero.
5. The operator has an account-pinned Route 53 permission for the delegated
   `e2b.superintelligent.group` zone.

The preconditions are read-only checks. Do not infer them from a local
bootstrap log or from the legacy endpoint.

## Single-record change

Use the account-pinned admin profile and replace only the `nomad` record. The
ALB DNS name and hosted-zone ID must come from the current Terraform state or a
read-only AWS query; never copy them from an old receipt.

```powershell
$env:AWS_PROFILE = "<cq-route53-admin-profile>"
$env:AWS_REGION = "us-east-1"

aws route53 change-resource-record-sets `
  --hosted-zone-id <delegated-zone-id> `
  --change-batch file://nomad-dns-change.json
```

The change batch contains exactly one `UPSERT` for
`nomad.<domain>` and an alias to the canonical ingress ALB. It must not include
the apex, wildcard, API, or customer-facing records.

## Verification and rollback

1. Wait for Route 53 `INSYNC`.
2. Resolve the public hostname from two resolvers.
3. Query `/v1/status/leader` and `/v1/status/peers` through the hostname.
4. Confirm the returned server address belongs to the canonical VPC.
5. Run one bounded build canary; require node-pool registration, allocation,
   receipt persistence, and cleanup.
6. If any check fails, restore the recorded prior alias with the same one-record
   operation, then keep capacity at zero and attach both records to the anomaly
   in Linear.

The handoff is complete only when the exact DNS change, propagation result,
canonical server identity, canary receipt, and cleanup result are linked to the
owning SUP issue. A successful Route 53 mutation alone is not runtime proof.

