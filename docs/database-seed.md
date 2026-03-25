# E2B Database Seeding

## Problem

The in-cluster Postgres runs as a Nomad job on the API node. When the API node
terminates (scale-to-zero, spot reclaim), all data is lost. This includes teams,
API keys, template definitions, and build history.

Until we migrate to persistent storage (EBS volume or external RDS), the database
must be re-seeded after every cold start.

## Seed Data

### Tier: `base_v1`
Already created by db-migrator. Default tier with:
- 8 max vCPU, 8192 MB max RAM, 512 MB disk
- 20 concurrent instances, 1 hour max length

### Team: `Superintelligent Group`
- ID: `a0000000-0000-0000-0000-000000000001`
- Tier: `base_v1`
- Slug: `sig`
- Email: `admin@superintelligent.group`

### API Key
- Stored in Secrets Manager: `e2b-dev/e2b-api-key`
- Hash stored in DB: `$sha256$<base64>`
- Key generation: See `packages/shared/pkg/keys/` (SHA256 of raw bytes, base64 encoded, `$sha256$` prefix)

## Re-Seeding Process

The seed runs as a Nomad batch job because the Postgres instance is only accessible
from within the VPC (on the API node's overlay network).

**Important**: Use datacenter `us-east-1c` (not `us-east-1`). Nomad nodes register
with their AZ as datacenter.

### Manual Seed via Nomad API

```bash
NOMAD_TOKEN=$(aws secretsmanager get-secret-value \
  --secret-id "e2b-dev/nomad-acl-token" \
  --query SecretString --output text)

# Get Postgres allocation address
POSTGRES_ALLOC=$(curl -sk -H "X-Nomad-Token: $NOMAD_TOKEN" \
  "https://nomad.e2b.superintelligent.group/v1/job/postgres/allocations" \
  | python -c "import sys,json; a=[x for x in json.load(sys.stdin) if x['ClientStatus']=='running']; print(a[0]['ID'])" 2>/dev/null)

# Get IP and port from allocation
curl -sk -H "X-Nomad-Token: $NOMAD_TOKEN" \
  "https://nomad.e2b.superintelligent.group/v1/allocation/$POSTGRES_ALLOC" \
  | python -c "import sys,json; a=json.load(sys.stdin); n=a['Resources']['Networks'][0]; print(f'{n[\"IP\"]}:{n[\"DynamicPorts\"][0][\"Value\"]}')"
```

### Automated Seed (TODO)

Add database seeding to the wake Lambda after Nomad jobs are running:
1. Wait for postgres job to be healthy
2. Submit seed batch job via Nomad API
3. Wait for completion
4. Verify team and API key exist

This would make scale-to-zero fully automated without manual intervention.

## Key Format Reference

```
API Key:  e2b_<40-char-hex>           (e.g., e2b_5caa69b7c44573ce6397...)
          └─prefix─┘└─20 random bytes hex encoded─┘

DB Hash:  $sha256$<base64-no-padding>  (SHA256 of the raw 20 bytes, base64 encoded)

Access Token: sk_e2b_<40-char-hex>     (same generation, different prefix)
```
