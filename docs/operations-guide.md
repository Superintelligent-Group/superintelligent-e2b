# E2B Self-Hosted Operations Guide

> This is our custom self-hosted E2B deployment on AWS. It extends the upstream
> [e2b-dev/infra](https://github.com/e2b-dev/infra) with scale-to-zero auto-scaling,
> spot instance fleets, and integration with our swarm worker.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        AWS VPC (10.0.0.0/16)                     │
│                                                                   │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐     │
│  │ Control Server│   │  API Node    │   │  Client Node     │     │
│  │ (always-on)  │   │ (spot, s2z)  │   │ (spot, s2z)      │     │
│  │              │   │              │   │                   │     │
│  │ • Nomad Srv  │   │ • API        │   │ • Orchestrator   │     │
│  │ • Consul Srv │   │ • Traefik    │   │ • Firecracker    │     │
│  │              │   │ • Redis      │   │ • Sandbox VMs    │     │
│  │              │   │ • Postgres   │   │                   │     │
│  │              │   │ • OTEL       │   │                   │     │
│  │ t3.medium    │   │ t3a.large    │   │ c8i.2xlarge      │     │
│  │ on-demand    │   │ spot fleet   │   │ spot fleet       │     │
│  └──────────────┘   └──────────────┘   └──────────────────┘     │
│                                                                   │
│  ┌──────────────┐   ┌──────────────────────────────────────┐     │
│  │ Build Node   │   │ Auto-Scaling (Lambda + EventBridge)  │     │
│  │ (spot, s2z)  │   │ • Wake: Lambda Function URL          │     │
│  │              │   │ • Shutdown: 5-min EventBridge check   │     │
│  │ • Template   │   │ • Idle timeout: 30 min               │     │
│  │   Manager    │   │ • Activity: CloudWatch metric         │     │
│  │ c8i.2xlarge  │   └──────────────────────────────────────┘     │
│  │ spot fleet   │                                                 │
│  └──────────────┘                                                 │
│                                                                   │
│  s2z = scale-to-zero                                             │
└─────────────────────────────────────────────────────────────────┘
```

## Node Roles

| Role | Nomad Pool | Instance Type | Lifecycle | Purpose |
|------|------------|--------------|-----------|---------|
| Control Server | — | t3.medium | Always-on, on-demand | Nomad + Consul servers. Must stay up to preserve cluster state. |
| API | `api` | t3a.large (spot) | Scale-to-zero | Traefik ingress, E2B API, Redis, Postgres, OTEL collectors |
| Client | `default` | c8i.2xlarge (spot) | Scale-to-zero | Orchestrator + Firecracker microVMs (sandboxes). **Requires nested virt.** |
| Build | `build` | c8i.2xlarge (spot) | Scale-to-zero | Template-manager for building sandbox templates. **Requires nested virt.** |

**Nested virtualization**: Only 8th-gen Intel instances (c8i, m8i, r8i and -flex variants) support
nested KVM on AWS. See [instance-pricing.md](./instance-pricing.md) for details.

## DNS & Endpoints

| Endpoint | Purpose |
|----------|---------|
| `api.e2b.superintelligent.group` | E2B API (health, sandboxes, templates) |
| `*.e2b.superintelligent.group` | Sandbox port access: `{port}-{sandboxID}.e2b.superintelligent.group` |
| `nomad.e2b.superintelligent.group` | Nomad UI (requires ACL token from Secrets Manager) |

## Authentication

### Auth Schemes

| Scheme | Header | Format | Endpoints |
|--------|--------|--------|-----------|
| API Key | `X-API-Key` | `e2b_<40-char-hex>` | Sandboxes, templates — **primary for SDK/CLI** |
| Access Token | `Authorization` | `Bearer sk_e2b_<hex>` | Sandboxes, templates — CLI login sessions |
| Admin Token | `X-Admin-Token` | raw string | `/nodes/*` only (node management) |
| Supabase JWT | `X-Supabase-Token` + `X-Supabase-Team` | JWT + team UUID | Dashboard access |

### Current API Key

- **Secret**: `e2b-dev/e2b-api-key` in AWS Secrets Manager
- **Team**: Superintelligent Group (`a0000000-0000-0000-0000-000000000001`)
- **Tier**: `base_v1` (8 max vCPU, 8GB max RAM, 20 concurrent instances)

### How API Keys Work (Implementation)

1. Key format: `e2b_` prefix + 20 random bytes hex-encoded (40 chars)
2. DB storage: SHA256 hash as `$sha256$<base64-raw>` in `team_api_keys.api_key_hash`
3. Verification: strip prefix → hex-decode → SHA256 → base64 → lookup hash in DB
4. Code: `packages/shared/pkg/keys/key.go` (GenerateKey, VerifyKey), `sha256.go` (hashing)

### Creating Keys (Official Method)

The **official tool** is `packages/db/scripts/seed/postgres/seed-db.go`:
```bash
cd packages/db
POSTGRES_CONNECTION_STRING="postgresql://postgres:pw@host:port/e2b" go run ./scripts/seed/postgres/seed-db.go
```
This prompts for an email, then creates: user → team → access token → API key.

For our self-hosted setup (in-cluster Postgres, no direct access), we seed via
Nomad batch jobs. See [database-seed.md](./database-seed.md).

## Using the SDK with Self-Hosted E2B

Point the SDK at our domain using the `domain` parameter:

```typescript
// TypeScript
import { Sandbox } from "e2b";
const sandbox = await Sandbox.create({
  domain: "e2b.superintelligent.group",
  apiKey: process.env.E2B_API_KEY,  // e2b_...
});
```

```python
# Python
from e2b import Sandbox
sandbox = Sandbox.create(
    domain="e2b.superintelligent.group",
    api_key=os.environ["E2B_API_KEY"],
)
```

```bash
# CLI
E2B_DOMAIN=e2b.superintelligent.group E2B_API_KEY=e2b_... e2b template list
```

## Template Building

Templates are Firecracker VM snapshots (memory + rootfs + metadata) stored in S3.
The build pipeline:

```
SDK/CLI → POST /templates → API → template-manager (gRPC) → Firecracker VM → snapshot → S3
```

### Build Flow (Detailed)

1. **Register**: `POST /v3/templates` creates DB records (template + build + aliases)
2. **Start**: `POST /v2/templates/{id}/builds/{buildID}` → API finds available build node →
   sends gRPC `TemplateCreate` to template-manager on that node
3. **Execute** (on build node): Pull Docker image → create ext4 rootfs → boot Firecracker VM →
   run build steps → capture snapshot (memfile + rootfs)
4. **Upload**: Artifacts stored in S3 (`{bucket}/builds/{buildID}/memfile`, `rootfs.ext4`, etc.)
5. **Complete**: Build status set to `READY`, template alias resolves to this build

### Building the Base Template

The base template is required before any sandboxes can be created. Official method:

```bash
# In packages/shared/
make prep-cluster  # runs seed-db + build-base-template
```

This requires:
1. Build nodes running (ASG `e2b-orch-build` scaled to ≥1)
2. Template-manager Nomad job running on build node
3. `E2B_API_KEY` and `E2B_DOMAIN` configured

### Orchestrator Binary Modes

The single `orchestrator` binary runs in different modes based on `ORCHESTRATOR_SERVICES`:
- `orchestrator` → sandbox creation/management (client nodes)
- `template-manager` → template building (build nodes)

## Scale-to-Zero Lifecycle

### Wake Flow (Lambda → ASG → Nomad)

1. Swarm worker calls wake Lambda URL before sandbox creation
2. Lambda records activity metric in CloudWatch **first** (race protection)
3. If API + Client ASGs already have capacity → returns `already_running`
4. Scales Control → API → Client ASGs (spot preferred, on-demand fallback)
5. Waits for ASG instances to reach `InService`
6. Waits for Nomad to register nodes in expected pools (`api`, `default`)
7. Resubmits dead/pending Nomad jobs so they reschedule on new nodes
8. Records activity metric again

### Shutdown Flow (EventBridge → Lambda)

1. EventBridge fires every 5 minutes
2. Checks if API + Client ASGs already at 0 → skip
3. Checks for instances in `Pending` state (boot grace) → skip
4. Reads last activity from CloudWatch (2-hour lookback)
5. If idle > 30 min → scales Client, Build, API ASGs to 0
6. **Control server stays running** (Nomad/Consul state not persistent)

### Race Condition Protections

- Wake records activity **before** any ASG operations
- Shutdown checks for `Pending` instances (catches in-progress boots)
- Shutdown keeps `max_size > 0` so wake can scale back up immediately
- No-metric-yet case seeds the metric and skips shutdown

## Secrets (AWS Secrets Manager)

| Secret | Purpose |
|--------|---------|
| `e2b-dev/nomad-acl-token` | Nomad ACL management token |
| `e2b-dev/consul-acl-token` | Consul ACL management token |
| `e2b-dev/e2b-api-key` | E2B API key for SDK access |
| `e2b-admin-token` | Admin token for `/nodes` endpoints |
| `e2b-postgres-connection-string` | In-cluster Postgres connection string |
| `e2b-supabase-jwt-secrets` | JWT signing secret (for Supabase/dashboard auth) |
| `e2b-cloudflare` | JSON `{"TOKEN": "..."}` for DNS management |
| `e2b-launch-darkly-api-key` | Feature flags (optional, placeholder OK) |

## Nomad Jobs

| Job | Pool | Type | Purpose |
|-----|------|------|---------|
| `api` | api | service | E2B API server + db-migrator sidecar |
| `ingress` | api | service | Traefik reverse proxy (Consul catalog provider) |
| `redis` | api | service | Redis for caching + feature flags |
| `postgres` | api | service | In-cluster PostgreSQL (**volatile — see below**) |
| `client-proxy` | default | service | Sandbox port forwarding proxy |
| `orchestrator-dev` | default | system | Firecracker sandbox orchestrator |
| `logs-collector` | — | system | Log aggregation |
| `otel-collector` | — | system | OpenTelemetry on all nodes |
| `otel-collector-nomad-server` | api | service | OTEL for Nomad server |
| `template-manager` | build | service | Template build service (needs build nodes) |

## Known Limitations & Gotchas

### In-Cluster Postgres Persistence (EBS Volume)
Postgres data is stored on a **dedicated EBS volume** (`{prefix}postgres-data`, 10GB gp3,
~$0.80/month) that persists across scale-to-zero cycles. The API node's startup script
(`start-api.sh`) auto-attaches, formats (first boot only), and mounts the volume at
`/opt/e2b-postgres-data` before Nomad starts.

If the EBS volume is lost or corrupted, re-seed using [database-seed.md](./database-seed.md).

### VPC CIDR Overlap
E2B VPC and main infra VPC both use `10.0.0.0/16` — can't peer.
**Fix**: Re-CIDR E2B VPC to `10.1.0.0/16` in next major infra revision.

### Nomad Datacenter = AZ
Nodes register as datacenter `us-east-1c` (their AZ), NOT `us-east-1`.
All Nomad jobs and batch jobs must use `datacenters = ["us-east-1c"]`.

### EventBridge Shutdown Rule
Currently **DISABLED** (`e2b-cluster-idle-check`) during debugging.
Re-enable with: `aws events enable-rule --name e2b-cluster-idle-check`

### Traefik + Consul Health
Traefik discovers routes via Consul service catalog. If a service's Consul health
check fails (e.g., API returns 503), Traefik won't route to it — all requests
fall through to client-proxy which returns "Invalid host".

## Common Operations

### Check Cluster Status
```bash
# Health check
curl -sk https://api.e2b.superintelligent.group/health

# List sandboxes
curl -sk -H "X-API-Key: $(aws secretsmanager get-secret-value \
  --secret-id e2b-dev/e2b-api-key --query SecretString --output text)" \
  https://api.e2b.superintelligent.group/sandboxes

# List Nomad jobs
NOMAD_TOKEN=$(aws secretsmanager get-secret-value \
  --secret-id e2b-dev/nomad-acl-token --query SecretString --output text)
curl -sk -H "X-Nomad-Token: $NOMAD_TOKEN" \
  https://nomad.e2b.superintelligent.group/v1/jobs | python -m json.tool
```

### Wake Cluster
```bash
curl -sk https://yuv3yud5a22hsnmnp2arvu7u6u0pgykd.lambda-url.us-east-1.on.aws/
```

### Scale Build Nodes (for Template Building)
```bash
aws autoscaling update-auto-scaling-group \
  --auto-scaling-group-name e2b-orch-build \
  --desired-capacity 1 --max-size 1
```

### Build Base Template
```bash
# After build nodes are up and template-manager is running:
cd packages/shared
E2B_API_KEY=$(aws secretsmanager get-secret-value \
  --secret-id e2b-dev/e2b-api-key --query SecretString --output text) \
E2B_DOMAIN=e2b.superintelligent.group \
make build-base-template
```

### Run SQL Against E2B Postgres
Submit a Nomad batch job — Postgres is only accessible from within the VPC.
Use datacenter `us-east-1c`, default NodePool, and `postgres:15-alpine` image.
See examples in the repo's operational history.

## Upstream E2B Setup Reference

The canonical self-hosting guide is in [`self-host.md`](../self-host.md). Key steps:
1. Create `.env.dev` from `.env.aws.template`
2. `make set-env ENV=dev` → `make provider-login` → `make init`
3. Populate Secrets Manager entries
4. Build Packer AMI: `cd iac/provider-aws/nomad-cluster-disk-image && make build`
5. `make build-and-upload` → `make copy-public-builds`
6. `make plan-without-jobs && make apply` → `make plan && make apply`
7. `cd packages/shared && make prep-cluster` (seed DB + build base template)

## File Structure

```
iac/provider-aws/
├── auto-scaling.tf                    # Wires auto-scaling module to main config
├── modules/auto-scaling/
│   ├── main.tf                        # Lambda + EventBridge + IAM
│   ├── variables.tf                   # Instance types, timeouts
│   ├── outputs.tf                     # Wake function URL output
│   └── lambda/cluster_scaler.py       # Wake + shutdown Python handlers
packages/
├── api/                               # E2B API server (Go)
├── orchestrator/                      # Sandbox orchestrator + template-manager (Go)
├── shared/pkg/keys/                   # API key generation + verification
├── db/scripts/seed/postgres/          # Official DB seeding tool (Go)
├── shared/scripts/                    # Base template builder (TypeScript)
└── envd/                              # In-VM guest daemon
docs/
├── operations-guide.md                # This file
├── instance-pricing.md                # Spot pricing + instance selection
└── database-seed.md                   # DB seeding for volatile Postgres
```
